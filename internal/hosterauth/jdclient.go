package hosterauth

// A minimal client for JD's account-management Deprecated API - the
// "accounts" namespace, which nothing else in this app talks to yet
// (internal/resolver/jd/client.go only ever touches downloadsV2, linkgrabberv2
// and config/set). Kept as this package's own small client rather than a
// method added to that file's Client type: this wave owns
// internal/resolver/jd/resolver.go only, not client.go, and the account
// namespace is a genuinely different concern (who JD is logged in as) from
// what that client already does (moving bytes).
//
// VERIFIED, NOT GUESSED - read directly off JD's own open-source
// implementation (fetched 2026-08-09), not inferred from a third-party
// wrapper or the unrelated cloud "AccountsV2" MyJDownloader API:
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPI.java
//     - @ApiNamespace("accounts") fixes the endpoint prefix; @APIParameterNames
//       on each method fixes the positional query-parameter order below.
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPIImpl.java
//     - what each method actually does: addAccount resolves the hoster string
//       through JD's own PluginFinder.assignHost and returns false rather than
//       erroring when it cannot; queryAccounts' infoMap carries exactly the
//       keys asked for ("username", "validUntil", "trafficLeft", "trafficMax",
//       "enabled", "valid") as literal JsonMap.put keys, not getter-derived
//       names, so those six strings are certain.
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPIStorable.java
//     - the getUUID()/getHostname()/getInfoMap() shape queryAccounts returns.
//   https://github.com/jdownloader-mirror/appwork-utils/blob/master/src/org/appwork/remoteapi/APIQuery.java
//     - APIQuery is a bare HashMap<String,Object>, so "query" travels as one
//       flat JSON object, the same shape internal/resolver/jd/client.go's
//       QueryDownloads already sends for its own map[string]any query param.
//
// The one thing NOT verified against a real instance: getUUID()/getHostname()
// serialise as JSON keys "uuid"/"hostname". That is inferred by analogy with
// DownloadLink and CrawledLink in internal/resolver/jd/client.go, whose own
// doc history shows JD's Storable layer lower-camels a getter the same way
// (getBytesLoaded -> "bytesLoaded", getUUID -> "uuid" there) - a real pattern
// in this exact API family, not a guess out of nowhere, but this package has
// never been run against a live JD. jdAccount's fields are decoded loosely
// (missing or renamed keys leave the zero value rather than erroring) so a
// wrong guess here degrades to "this account looks unconfirmed" instead of a
// hard failure - see Reconcile's handling of a present-but-unrecognised
// account.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// jdAccountInfo is the subset of AccountAPIImpl.queryAccounts' infoMap this
// package asks for - see the fieldRequested calls in that method.
type jdAccountInfo struct {
	Username string `json:"username,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	Valid    bool   `json:"valid,omitempty"`
}

// jdAccount is one row queryAccounts answers.
type jdAccount struct {
	UUID     int64          `json:"uuid"`
	Hostname string         `json:"hostname"`
	InfoMap  *jdAccountInfo `json:"infoMap"`
}

// jdAccounts is the narrow slice of JD's account API the reconciler needs -
// exactly what jdClient implements against a real JD sidecar, and what a test
// fakes instead of one. Named for what it is asked to do, not for the fact
// that jdClient happens to answer it.
type jdAccounts interface {
	queryAccounts(ctx context.Context) ([]jdAccount, error)
	addAccount(ctx context.Context, hoster, username, password string) (bool, error)
	removeAccounts(ctx context.Context, ids []int64) error
	listPremiumHosters(ctx context.Context) ([]string, error)
}

// jdClient is the real jdAccounts, talking to a headless JD's Deprecated API
// exactly the way internal/resolver/jd/client.go's own call() helper does:
// GET, one URL-encoded JSON blob per positional parameter, a {"data": ...}
// envelope in the response. Kept as a private, minimal copy of that
// convention rather than a shared helper, because sharing one would mean
// editing client.go, which belongs to internal/resolver/jd this wave.
type jdClient struct {
	base string
	hc   *http.Client
}

func newJDClient(base string) *jdClient {
	return &jdClient{base: strings.TrimRight(base, "/"), hc: httpx.New(httpx.Options{Timeout: 15 * time.Second})}
}

func (c *jdClient) call(ctx context.Context, path string, params ...any) (json.RawMessage, error) {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		b, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		parts = append(parts, url.QueryEscape(string(b)))
	}
	u := c.base + path
	if len(parts) > 0 {
		u += "?" + strings.Join(parts, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Same scrub internal/resolver/jd/client.go applies: JD's Deprecated API can
	// emit non-UTF-8 bytes inside string values, which breaks encoding/json.
	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), "�"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jd %s: HTTP %d", path, resp.StatusCode)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("jd %s: bad json: %w", path, err)
	}
	return env.Data, nil
}

// queryAccounts asks JD for every configured account, with username, enabled
// and valid included - valid is what tells a queued login from a rejected one
// apart (see plan in reconcile.go).
//
// NO maxResults and NO startAt, and that is the whole of a fix measured against
// a live JD rather than reasoned from the sources. The pair was here because
// APIQuery documents -1 as "all of them" - true of APIQuery, and AccountQuery
// is not one. JD answers HTTP 500 to the mere PRESENCE of either field,
// whatever its value: probed against the bundled JDownloader 48637 with -1, 0
// and both, all five hundreds, while the identical call without them returns
// `{"data":[]}` and the empty object does too.
//
// This is the first time this package has run against a real JD, and the
// file's own header said so: "this package has never been run against a live
// JD". The cost of that was a reconcile loop failing every thirty seconds since
// the day it shipped, and 408 identical lines in the log of an instance that
// otherwise looked healthy.
func (c *jdClient) queryAccounts(ctx context.Context) ([]jdAccount, error) {
	data, err := c.call(ctx, "/accounts/queryAccounts", map[string]any{
		"username": true,
		"enabled":  true,
		"valid":    true,
	})
	if err != nil {
		return nil, err
	}
	var out []jdAccount
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("jd accounts/queryAccounts: %w", err)
	}
	return out, nil
}

// addAccount asks JD to add a login for hoster. It reports the boolean
// AccountAPIImpl.addAccount returns - true once JD's PluginFinder resolved
// hoster to a plugin and filed the account, NOT once the login has been
// checked. A true here is "JD accepted the request", not "the password is
// right" - that distinction is exactly why Reconcile treats a freshly-added
// account as queued rather than active until JD's own account checker has
// had a turn (see the grace window in reconcile.go).
//
// The credential travels in this one call and nowhere else in this package:
// no log line, no error string and no return value here ever carries
// username or password - see the security tests in reconcile_test.go.
func (c *jdClient) addAccount(ctx context.Context, hoster, username, password string) (bool, error) {
	data, err := c.call(ctx, "/accounts/addAccount", hoster, username, password)
	if err != nil {
		// The error itself must never repeat the parameters call() was given -
		// and it does not: call()'s own error paths (HTTP status, bad JSON) never
		// echo their input, only the path and the response's own shape.
		return false, err
	}
	var ok bool
	if err := json.Unmarshal(data, &ok); err != nil {
		return false, fmt.Errorf("jd accounts/addAccount: %w", err)
	}
	return ok, nil
}

// removeAccounts asks JD to drop the given account ids. A nil or empty slice
// is a no-op rather than a call that means "remove nothing JD understands
// that as" - the caller (Reconcile) never has a reason to send one, but a
// defensive no-op costs nothing and avoids relying on JD's own reading of an
// empty array.
func (c *jdClient) removeAccounts(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := c.call(ctx, "/accounts/removeAccounts", ids)
	return err
}

// listPremiumHosters returns JD's own list of hosts its premium plugins
// cover - AccountAPIImpl.listPremiumHoster filters HostPluginController's
// full plugin list down to isPremium() ones. This is the primary source for
// the "add a login" host picker; curatedHosts (reconcile.go) is only the
// fallback while JD is unreachable.
func (c *jdClient) listPremiumHosters(ctx context.Context) ([]string, error) {
	data, err := c.call(ctx, "/accounts/listPremiumHoster")
	if err != nil {
		return nil, err
	}
	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("jd accounts/listPremiumHoster: %w", err)
	}
	return out, nil
}
