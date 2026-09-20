package hosterauth

// A minimal client for the "accounts" namespace of JD's Deprecated API, which
// internal/resolver/jd's client does not cover. The calls follow JD's own
// sources:
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPI.java
//     (namespace and positional parameter order)
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPIImpl.java
//     (addAccount returns false when PluginFinder cannot resolve the hoster;
//     queryAccounts' infoMap keys are literal strings)
//   https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/accounts/AccountAPIStorable.java
//     (the uuid/hostname/infoMap shape)
//   https://github.com/jdownloader-mirror/appwork-utils/blob/master/src/org/appwork/remoteapi/APIQuery.java
//     (a query is one flat JSON object)
//
// The "uuid" and "hostname" keys follow the lower-camel naming JD uses for
// DownloadLink and CrawledLink. jdAccount decodes loosely, so a renamed key
// reads as an unconfirmed account rather than an error.

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

// jdAccountInfo is the part of queryAccounts' infoMap this package asks for.
// JD reports nothing about the plan beyond these six fields; a free account
// answers validUntil -1 and trafficMax 0, a premium one a real expiry:
//
//	{"valid":true,"trafficMax":0,"validUntil":-1,"trafficLeft":0,
//	 "enabled":true,"username":"…"}
type jdAccountInfo struct {
	Username string `json:"username,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	Valid    bool   `json:"valid,omitempty"`
	// ValidUntil is a unix timestamp in milliseconds, or -1 for an account
	// with nothing to expire.
	ValidUntil  int64 `json:"validUntil,omitempty"`
	TrafficLeft int64 `json:"trafficLeft,omitempty"`
	TrafficMax  int64 `json:"trafficMax,omitempty"`
}

// jdAccount is one row queryAccounts answers.
type jdAccount struct {
	UUID     int64          `json:"uuid"`
	Hostname string         `json:"hostname"`
	InfoMap  *jdAccountInfo `json:"infoMap"`
}

// jdAccounts is the part of JD's account API the reconciler uses; tests fake
// it.
type jdAccounts interface {
	queryAccounts(ctx context.Context) ([]jdAccount, error)
	addAccount(ctx context.Context, hoster, username, password string) (bool, error)
	removeAccounts(ctx context.Context, ids []int64) error
	listPremiumHosters(ctx context.Context) ([]string, error)
}

// jdClient talks to a headless JD's Deprecated API the way
// internal/resolver/jd's client does: GET, one URL-encoded JSON value per
// positional parameter, and a {"data": ...} envelope in the response.
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
	// JD can emit non-UTF-8 bytes inside strings, which encoding/json rejects.
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

// queryAccounts asks JD for every configured account; valid tells a queued
// login from a rejected one.
//
// The query has no maxResults or startAt: JD 48637 answers HTTP 500 to either
// field whatever its value, although APIQuery documents -1 as "all".
func (c *jdClient) queryAccounts(ctx context.Context) ([]jdAccount, error) {
	data, err := c.call(ctx, "/accounts/queryAccounts", map[string]any{
		"username":    true,
		"enabled":     true,
		"valid":       true,
		"validUntil":  true,
		"trafficLeft": true,
		"trafficMax":  true,
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

// addAccount asks JD to add a login for hoster. True means JD resolved the
// hoster to a plugin and filed the account, not that the password works,
// which is why Reconcile treats a new account as queued (see rejectGrace).
//
// The credential appears in this call only, never in a log line, error or
// return value; call's errors never echo their parameters.
func (c *jdClient) addAccount(ctx context.Context, hoster, username, password string) (bool, error) {
	data, err := c.call(ctx, "/accounts/addAccount", hoster, username, password)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := json.Unmarshal(data, &ok); err != nil {
		return false, fmt.Errorf("jd accounts/addAccount: %w", err)
	}
	return ok, nil
}

// removeAccounts asks JD to drop the given account ids. An empty list sends
// nothing rather than relying on how JD reads an empty array.
func (c *jdClient) removeAccounts(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := c.call(ctx, "/accounts/removeAccounts", ids)
	return err
}

// listPremiumHosters returns the hosts JD's premium plugins cover. It is the
// primary source for the host picker; curatedHosts is the fallback.
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
