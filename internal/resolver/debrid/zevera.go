package debrid

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Zevera speaks the Zevera API published at
// https://app.swaggerhub.com/apis-docs/zeveraAPI/zevera-com, which is
// Premiumize's API under another name. Where that spec is silent, the error
// codes and a free account's premium_until of false follow JDownloader's
// ZeveraCore plugin.
type Zevera struct {
	key  string
	base string
	hc   *http.Client
}

// NewZevera takes the API key from https://www.zevera.com/account, which is
// what JDownloader and pyLoad log in with. No client uses the customer_id and
// pin pair the spec also names.
func NewZevera(key string) *Zevera {
	return &Zevera{
		key:  key,
		base: "https://www.zevera.com/api",
		hc:   httpx.New(httpx.Options{Timeout: 30 * time.Second}),
	}
}

func (*Zevera) ID() string    { return "zevera" }
func (*Zevera) Label() string { return "Zevera" }

// zeveraAnswer is the part every answer shares. Refusals arrive as HTTP 200
// with status "error", so the body decides, not the status line.
type zeveraAnswer struct {
	Status  string          `json:"status"`
	Message json.RawMessage `json:"message"`
	// Code is the enum JDownloader sorts refusals by, such as not_found.
	// Error is a word such as "topup_required", or an object carrying the
	// message on the JSON-RPC shaped answer JDownloader has also seen.
	Code  json.RawMessage `json:"code"`
	Error json.RawMessage `json:"error"`
}

// call sends one request with the key as the apikey query parameter, the
// spec's security scheme for a key. account/info and services/list go out as
// GET because JDownloader and pyLoad send them that way, although the spec
// lists POST.
func (z *Zevera) call(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, z.base+path, body)
	if err != nil {
		return fmt.Errorf("zevera %s: %w", path, err)
	}
	req.URL.RawQuery = url.Values{"apikey": {z.key}}.Encode()
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := z.hc.Do(req)
	if err != nil {
		// A *url.Error spells out the request URL, and with it the key.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("zevera %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("zevera %s: %w", path, err)
	}
	var ans zeveraAnswer
	if json.Unmarshal(raw, &ans) != nil {
		return fmt.Errorf("zevera %s: unreadable answer (%s)", path, resp.Status)
	}
	if err := ans.failure(path); err != nil {
		return err
	}
	// Checked after the body, so that Zevera's own reason wins over the bare
	// HTTP status.
	if resp.StatusCode >= 400 {
		return fmt.Errorf("zevera %s: %s", path, resp.Status)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("zevera %s: unreadable answer", path)
	}
	return nil
}

// failure turns a refusal into an error carrying Zevera's own words. An answer
// with neither a status nor a reason passes, since the spec gives
// /services/list no status field.
func (a zeveraAnswer) failure(path string) error {
	msg, code, word := zeveraText(a.Message), zeveraText(a.Code), zeveraText(a.Error)
	// The JSON-RPC shape carries no status and reports its failure as error
	// code 0, so the error object alone marks a refusal, message or not.
	var rpc map[string]json.RawMessage
	if json.Unmarshal(a.Error, &rpc) == nil && rpc != nil {
		msg = cmp.Or(msg, zeveraText(rpc["message"]))
	}
	switch {
	case strings.EqualFold(a.Status, "success"):
		return nil
	case strings.EqualFold(a.Status, "deferred"):
		// Not in the spec: JDownloader keeps asking on this status while
		// Zevera fetches the file for a host on its queue list.
		return fmt.Errorf("zevera %s: Zevera has not finished fetching this file yet, try again later", path)
	case a.Status == "" && msg == "" && code == "" && word == "" && rpc == nil:
		return nil
	}
	detail := cmp.Or(msg, code, word)
	if phrase := zeveraPhrase(code, word, msg); phrase != "" {
		return fmt.Errorf("zevera %s: %s (%s)", path, phrase, detail)
	}
	if detail != "" {
		return fmt.Errorf("zevera %s: %s", path, detail)
	}
	return fmt.Errorf("zevera %s: the call failed and Zevera named no reason", path)
}

// zeveraPhrase names the kind of refusal, so that a dead file, a hoster Zevera
// cannot serve and a spent allowance read differently. The codes and sentences
// are the ones JDownloader's ZeveraCore matches, and the sentences are matched
// too because not every answer carries a code.
func zeveraPhrase(code, word, msg string) string {
	has := func(s string) bool { return strings.Contains(strings.ToLower(msg), s) }
	switch {
	case code == "authentication_failed", has("not logged in"):
		return "Zevera refused the API key"
	case code == "permission_denied":
		return "Zevera does not let this account do that"
	case code == "not_found", has("file not found"), has("item not found"):
		return "the hoster says this file is gone"
	case code == "service_unsupported":
		return "Zevera does not support this hoster"
	case code == "service_down":
		return "this hoster is down at Zevera"
	case code == "service_limit_reached", has("daily linklimit"):
		return "the daily link limit for this hoster is used up"
	case code == "account_limit_reached", word == "topup_required", has("fair use limit reached"):
		return "the account has no premium or no fair-use allowance left"
	case code == "rate_limit_reached":
		return "Zevera is rate limiting this account, try again later"
	}
	return ""
}

// Authenticate checks the key against /account/info. The spec does not say
// whether /services/list needs an account, so the host list alone might
// accept a wrong key.
func (z *Zevera) Authenticate(ctx context.Context) error {
	if z.key == "" {
		return errors.New("zevera: an API key is required")
	}
	return z.call(ctx, http.MethodGet, "/account/info", nil, nil)
}

// Hosts reads the directdl list of /services/list plus its aliases. A host
// listed only under cache is served only when Zevera already holds the file,
// and one under queue answers "deferred" until Zevera has fetched it, so
// neither is claimed.
func (z *Zevera) Hosts(ctx context.Context) (map[string]bool, error) {
	var data struct {
		DirectDL []string        `json:"directdl"`
		Aliases  json.RawMessage `json:"aliases"`
	}
	if err := z.call(ctx, http.MethodGet, "/services/list", nil, &data); err != nil {
		return nil, err
	}
	// An alias table in another shape, such as an empty [], leaves just the
	// directdl hosts.
	var aliases map[string][]string
	_ = json.Unmarshal(data.Aliases, &aliases)
	set := map[string]bool{}
	add := func(domain string) {
		// A bare service name such as "usenet" is not a domain a link can
		// match.
		if h := NormalizeHost(domain); h != "" && strings.Contains(h, ".") {
			set[h] = true
		}
	}
	for _, name := range data.DirectDL {
		add(name)
		for _, alias := range aliases[name] {
			add(alias)
		}
	}
	if len(set) == 0 {
		return nil, errors.New("zevera: /services/list named no direct-download hosts")
	}
	return set, nil
}

// Unlock asks /transfer/directdl for one link. The spec calls content the
// field to read and keeps location, filename and filesize for older clients,
// so those answer only when content has no link.
func (z *Zevera) Unlock(ctx context.Context, link string) (Direct, error) {
	var data struct {
		Location string          `json:"location"`
		Filename string          `json:"filename"`
		Filesize json.RawMessage `json:"filesize"`
		Content  json.RawMessage `json:"content"`
	}
	if err := z.call(ctx, http.MethodPost, "/transfer/directdl", url.Values{"src": {link}}, &data); err != nil {
		return Direct{}, err
	}
	var files []struct {
		Path string          `json:"path"`
		Size json.RawMessage `json:"size"`
		Link string          `json:"link"`
	}
	_ = json.Unmarshal(data.Content, &files)
	dl, name, size := data.Location, data.Filename, data.Filesize
	// content lists every file of a folder link; a hoster link is one file.
	if len(files) > 0 && files[0].Link != "" {
		dl, name, size = files[0].Link, files[0].Path, files[0].Size
	}
	if dl == "" {
		return Direct{}, errors.New("zevera: no direct link returned")
	}
	// A task named "Folder/video.mkv" would become a folder tree on disk.
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	n, _ := zeveraNumber(size)
	return Direct{URL: dl, Name: name, Size: int64(n)}, nil
}

// Account reads /account/info. limit_used is a fair-use fraction without a
// byte ceiling behind it, so it fills UsedPercent and never Unlimited.
func (z *Zevera) Account(ctx context.Context) (AccountInfo, error) {
	var data struct {
		// premium_until is false or null on a free account.
		PremiumUntil json.RawMessage `json:"premium_until"`
		LimitUsed    json.RawMessage `json:"limit_used"`
	}
	if err := z.call(ctx, http.MethodGet, "/account/info", nil, &data); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if until, ok := zeveraNumber(data.PremiumUntil); ok && until > 0 {
		info.ExpiresAt = time.Unix(int64(until), 0).UTC()
		if info.ExpiresAt.After(time.Now()) {
			info.Tier = "premium"
		}
	}
	if used, ok := zeveraNumber(data.LimitUsed); ok {
		info.Traffic.UsedPercent = used * 100
		info.Traffic.PercentKnown = true
	}
	return info, nil
}

// zeveraNumber reads a number sent as a JSON number or as a numeric string.
// false, null and a missing field report ok as false.
func zeveraNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// zeveraText reads a field that is a string on one answer and a number on
// another, and returns "" for anything else.
func zeveraText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	if n, ok := zeveraNumber(raw); ok {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}
