package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// MyDebrid speaks the MyDebrid API v1 documented at
// https://api.mydebrid.com/v1/: the website's username and password buy a
// token, which every further call carries as a form field. What the
// documentation leaves open, the date format of expiryDate and that every
// call works as a POST, follows JDownloader's MydebridCom.java.
type MyDebrid struct {
	user string
	pass string
	base string
	hc   *http.Client

	// mu guards token and is held across a login, so parallel unlocks wait
	// for one login instead of each sending their own.
	mu    loginLock
	token string

	capMu sync.Mutex
	// chunkCaps is each host's maxChunks from the last /get-hosts, or 1 for a
	// host MyDebrid cannot resume.
	chunkCaps map[string]int
}

func NewMyDebrid(user, pass string) *MyDebrid {
	return &MyDebrid{
		user:      user,
		pass:      pass,
		base:      "https://api.mydebrid.com/v1",
		hc:        httpx.New(httpx.Options{Timeout: 30 * time.Second}),
		mu:        newLoginLock(),
		chunkCaps: map[string]int{},
	}
}

func (*MyDebrid) ID() string    { return "mydebrid" }
func (*MyDebrid) Label() string { return "MyDebrid" }

// mydebridRefusal is an answer with success false, named by its error code.
type mydebridRefusal struct {
	path    string
	code    string
	details string
}

func (e *mydebridRefusal) Error() string {
	var why string
	switch e.code {
	case "INVALID_CREDENTIALS":
		why = "the username or password was refused"
	case "TOKEN_EXPIRED", "INVALID_TOKEN":
		why = "the session token was refused"
	case "LIMIT_EXCEEDED":
		why = "the daily limit for this hoster is used up"
	case "HOST_UNAVAILABLE":
		why = "MyDebrid cannot reach this hoster right now, try again later"
	case "FILE_NOT_FOUND":
		why = "the file was not found, so the link is probably dead"
	case "INVALID_URL":
		why = "MyDebrid does not take this as a valid link"
	case "MISSING_PARAMS":
		why = "the request lacked " + e.details
	case "":
		why = "refused without a reason"
	default:
		why = "refused with " + e.code
		if e.details != "" {
			why += ": " + e.details
		}
	}
	return "mydebrid " + e.path + ": " + why
}

// mydebridTokenRefused reports whether a call failed on its token, which a fresh
// login cures.
func mydebridTokenRefused(err error) bool {
	var r *mydebridRefusal
	return errors.As(err, &r) && (r.code == "TOKEN_EXPIRED" || r.code == "INVALID_TOKEN")
}

// call posts form to path and returns the body of a successful answer.
// Refusals come as HTTP 400 with the same JSON body, so the body decides.
// Secrets travel in the body only, and errors leave the URL out as well.
func (m *MyDebrid) call(ctx context.Context, path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("mydebrid %s: %w", path, httpx.StripURL(err))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mydebrid %s: %w", path, httpx.StripURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("mydebrid %s: %w", path, err)
	}
	var env struct {
		Success bool            `json:"success"`
		Error   string          `json:"error"`
		Details json.RawMessage `json:"error_details"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return nil, fmt.Errorf("mydebrid %s: unreadable answer (%s)", path, resp.Status)
	}
	if !env.Success {
		return nil, &mydebridRefusal{path: path, code: strings.TrimSpace(env.Error), details: megadebridString(env.Details)}
	}
	return raw, nil
}

// loginLocked trades the username and password for a token. m.mu must be held.
func (m *MyDebrid) loginLocked(ctx context.Context) error {
	m.token = ""
	if m.user == "" || m.pass == "" {
		return errors.New("mydebrid: a username and a password are required")
	}
	raw, err := m.call(ctx, "/login", url.Values{"username": {m.user}, "password": {m.pass}})
	if err != nil {
		return err
	}
	var got struct {
		Token json.RawMessage `json:"token"`
	}
	_ = json.Unmarshal(raw, &got)
	token := megadebridString(got.Token)
	if token == "" {
		return errors.New("mydebrid /login: answered without a token")
	}
	m.token = token
	return nil
}

// session returns the cached token, logging in first when there is none or
// when it equals stale, the token a call just had refused. A caller that
// waited out another caller's login gets that login's token.
func (m *MyDebrid) session(ctx context.Context, stale string) (string, error) {
	if err := m.mu.lock(ctx); err != nil {
		return "", err
	}
	defer m.mu.unlock()
	if m.token != "" && m.token != stale {
		return m.token, nil
	}
	if err := m.loginLocked(ctx); err != nil {
		return "", err
	}
	return m.token, nil
}

// authed sends a call with the session token, logging in again once when the
// token has expired.
func (m *MyDebrid) authed(ctx context.Context, path string, form url.Values) ([]byte, error) {
	token, err := m.session(ctx, "")
	if err != nil {
		return nil, err
	}
	with := func(token string) url.Values {
		f := url.Values{"token": {token}}
		for k, v := range form {
			f[k] = v
		}
		return f
	}
	raw, err := m.call(ctx, path, with(token))
	if !mydebridTokenRefused(err) {
		return raw, err
	}
	if token, err = m.session(ctx, token); err != nil {
		return nil, err
	}
	return m.call(ctx, path, with(token))
}

// Authenticate logs in afresh. It is exported for the credential check, so a
// cached token cannot vouch for a password that was changed since.
func (m *MyDebrid) Authenticate(ctx context.Context) error {
	if err := m.mu.lock(ctx); err != nil {
		return err
	}
	defer m.mu.unlock()
	return m.loginLocked(ctx)
}

// mydebridHost is one entry of /get-hosts. dailyLimit and remaining are bytes
// or the string "unlimited".
type mydebridHost struct {
	Name      string          `json:"name"`
	Remaining json.RawMessage `json:"remaining"`
	MaxChunks json.RawMessage `json:"maxChunks"`
	Resumable json.RawMessage `json:"resumable"`
}

// Hosts reads /get-hosts, which needs the token. A host whose daily
// allowance is used up is left out until the list is read again, as
// JDownloader stops sending it links.
func (m *MyDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := m.authed(ctx, "/get-hosts", nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Hosts []json.RawMessage `json:"hosts"`
	}
	_ = json.Unmarshal(raw, &body)
	set := map[string]bool{}
	caps := map[string]int{}
	for _, e := range body.Hosts {
		// Decoded one by one, so a malformed entry costs that host only.
		var h mydebridHost
		if json.Unmarshal(e, &h) != nil {
			continue
		}
		// Lower-cased first, because NormalizeHost strips only a lower-case "www.".
		d := NormalizeHost(strings.ToLower(h.Name))
		if d == "" || !strings.Contains(d, ".") {
			continue
		}
		if left, sized := mydebridBytes(h.Remaining); sized && left <= 0 {
			continue
		}
		set[d] = true
		chunks := int(looseInt(h.MaxChunks))
		if strings.Trim(string(h.Resumable), `" `) == "false" {
			chunks = 1
		}
		if chunks > 0 {
			caps[d] = chunks
		}
	}
	if len(set) == 0 {
		return nil, errors.New("mydebrid /get-hosts: named no hosts")
	}
	m.capMu.Lock()
	m.chunkCaps = caps
	m.capMu.Unlock()
	return set, nil
}

// HostLimit satisfies HostLimiter with the host's maxChunks. A host the list
// has not been read for gets one connection, JDownloader's default here.
func (m *MyDebrid) HostLimit(host string) int {
	m.capMu.Lock()
	defer m.capMu.Unlock()
	if n := m.chunkCaps[NormalizeHost(host)]; n > 0 {
		return n
	}
	return 1
}

// Unlock posts the link to /get-download-url.
func (m *MyDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	raw, err := m.authed(ctx, "/get-download-url", url.Values{"fileUrl": {link}})
	if err != nil {
		return Direct{}, err
	}
	var got struct {
		Name        string          `json:"name"`
		Size        json.RawMessage `json:"size"`
		DownloadURL string          `json:"downloadUrl"`
	}
	if json.Unmarshal(raw, &got) != nil {
		return Direct{}, errors.New("mydebrid /get-download-url: unreadable answer")
	}
	if !strings.HasPrefix(got.DownloadURL, "http") {
		return Direct{}, errors.New("mydebrid /get-download-url: no direct link returned")
	}
	size, _ := mydebridBytes(got.Size)
	return Direct{URL: got.DownloadURL, Name: got.Name, Size: size}, nil
}

// Account reads /account-status. Only a premium account can download, so
// anything else, an expired premium included, reads as free.
func (m *MyDebrid) Account(ctx context.Context) (AccountInfo, error) {
	raw, err := m.authed(ctx, "/account-status", nil)
	if err != nil {
		return AccountInfo{}, err
	}
	var got struct {
		AccountType      string          `json:"accountType"`
		RemainingTraffic json.RawMessage `json:"remainingTraffic"`
		ExpiryDate       string          `json:"expiryDate"`
	}
	_ = json.Unmarshal(raw, &got)
	tier := strings.ToLower(strings.TrimSpace(got.AccountType))
	expiry := strings.TrimSpace(got.ExpiryDate)
	if tier != "premium" || strings.EqualFold(expiry, "expired") {
		if tier == "" || tier == "premium" {
			tier = "free"
		}
		return AccountInfo{Tier: tier}, nil
	}
	info := AccountInfo{Tier: tier}
	// The documentation's example reads 07-03-2020; JDownloader takes it as
	// month first.
	if day, err := time.Parse("01-02-2006", expiry); err == nil {
		info.ExpiresAt = day
	}
	// A sized remainingTraffic has no ceiling to set it against, so only
	// "unlimited" is taken from it.
	if strings.EqualFold(megadebridString(got.RemainingTraffic), "unlimited") {
		info.Traffic.Unlimited = true
	}
	return info, nil
}

// mydebridBytes reads a byte count sent as a number, possibly with a
// fraction, or as a numeric string. sized is false for anything else, such as
// "unlimited".
func mydebridBytes(raw json.RawMessage) (n int64, sized bool) {
	// Unmarshal takes null into a float without complaint.
	if string(raw) == "null" {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int64(f), true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}
