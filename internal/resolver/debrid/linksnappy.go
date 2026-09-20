package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Linksnappy speaks the Linksnappy API.
//
// Linksnappy publishes no API documentation. The envelope and the host list
// were measured against the live service; the account calls (AUTHENTICATE with
// the plain password and a session cookie, linkgen answering a bare object)
// follow ResolveURL's linksnappy.py. Everything decodes loosely, so a renamed
// field makes unlocking fail with an error instead of producing a bad link.
type Linksnappy struct {
	user string
	pass string
	base string
	hc   *http.Client
}

func NewLinksnappy(user, pass string) *Linksnappy {
	// The session cookie from AUTHENTICATE is the credential, so the jar must
	// not be shared with other services.
	jar, _ := cookiejar.New(nil)
	c := httpx.New(httpx.Options{Timeout: 30 * time.Second})
	c.Jar = jar
	return &Linksnappy{user: user, pass: pass, base: "https://linksnappy.com/api", hc: c}
}

func (*Linksnappy) ID() string    { return "linksnappy" }
func (*Linksnappy) Label() string { return "Linksnappy" }

// lsEnvelope is the wrapper FILEHOSTS, AUTHENTICATE and USERDETAILS share.
// Error is false on success and a sentence on failure, hence the RawMessage.
type lsEnvelope struct {
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error"`
	Return json.RawMessage `json:"return"`
}

// errText is the sentence the service sent, or "" when it reported no error.
func (e lsEnvelope) errText() string {
	var s string
	if json.Unmarshal(e.Error, &s) == nil && strings.TrimSpace(s) != "" {
		return s
	}
	return ""
}

func (l *Linksnappy) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := l.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := l.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// envelope performs a call and unwraps the shared wrapper.
func (l *Linksnappy) envelope(ctx context.Context, path string, q url.Values, out any) error {
	raw, err := l.get(ctx, path, q)
	if err != nil {
		return err
	}
	var env lsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("linksnappy %s: unreadable answer", path)
	}
	if env.Status != "OK" {
		if msg := env.errText(); msg != "" {
			return fmt.Errorf("linksnappy %s: %s", path, msg)
		}
		return fmt.Errorf("linksnappy %s: refused without a reason", path)
	}
	if out != nil && len(env.Return) > 0 {
		// The call succeeded, so an unexpected shape only leaves out empty.
		_ = json.Unmarshal(env.Return, out)
	}
	return nil
}

// Authenticate logs in and keeps the session cookie. It is exported to verify
// the credential, since the host list needs no account.
func (l *Linksnappy) Authenticate(ctx context.Context) error {
	if l.user == "" || l.pass == "" {
		return errors.New("linksnappy: a username and a password are required")
	}
	return l.envelope(ctx, "/AUTHENTICATE", url.Values{
		"username": {l.user},
		"password": {l.pass},
	}, nil)
}

// Hosts asks /FILEHOSTS, a map keyed by domain whose entries carry Status as a
// string ("1" for up).
func (l *Linksnappy) Hosts(ctx context.Context) (map[string]bool, error) {
	var hosts map[string]struct {
		Status string `json:"Status"`
	}
	if err := l.envelope(ctx, "/FILEHOSTS", nil, &hosts); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for domain, info := range hosts {
		// An empty Status counts as up so a missing field cannot empty the
		// routing table.
		if info.Status == "0" {
			continue
		}
		if d := NormalizeHost(domain); d != "" && strings.Contains(d, ".") {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("linksnappy: /FILEHOSTS named no hosts")
	}
	return set, nil
}

// lsGenerated is one entry of linkgen's own `links` array.
type lsGenerated struct {
	Status    string          `json:"status"`
	Error     json.RawMessage `json:"error"`
	Generated string          `json:"generated"`
	Filename  string          `json:"filename"`
	// Size has been seen as a string, so both shapes are accepted.
	Size json.RawMessage `json:"size"`
}

func (l *Linksnappy) Unlock(ctx context.Context, link string) (Direct, error) {
	if err := l.Authenticate(ctx); err != nil {
		return Direct{}, err
	}
	q := url.Values{"genLinks": {`{"link":` + strconv.Quote(link) + `}`}}
	raw, err := l.get(ctx, "/linkgen", q)
	if err != nil {
		return Direct{}, err
	}
	// linkgen answers a bare object with a `links` array on success, and the
	// shared envelope when it refuses. Both are tried, in that order.
	var ok struct {
		Links []lsGenerated `json:"links"`
	}
	if json.Unmarshal(raw, &ok) == nil && len(ok.Links) > 0 {
		first := ok.Links[0]
		if first.Status != "" && first.Status != "OK" {
			var msg string
			_ = json.Unmarshal(first.Error, &msg)
			if msg == "" {
				msg = "the link could not be generated"
			}
			return Direct{}, fmt.Errorf("linksnappy: %s", msg)
		}
		if first.Generated == "" {
			return Direct{}, errors.New("linksnappy: no direct link returned")
		}
		return Direct{URL: first.Generated, Name: first.Filename, Size: looseInt(first.Size)}, nil
	}
	var env lsEnvelope
	if json.Unmarshal(raw, &env) == nil {
		if msg := env.errText(); msg != "" {
			return Direct{}, fmt.Errorf("linksnappy /linkgen: %s", msg)
		}
	}
	return Direct{}, errors.New("linksnappy: unreadable answer to /linkgen")
}

// Account reads /USERDETAILS. Its field names come from third-party clients
// only, so every one of them is optional.
func (l *Linksnappy) Account(ctx context.Context) (AccountInfo, error) {
	if err := l.Authenticate(ctx); err != nil {
		return AccountInfo{}, err
	}
	var d struct {
		Type         string          `json:"type"`
		Expire       json.RawMessage `json:"expire"`
		TrafficUsed  json.RawMessage `json:"trafficused"`
		TrafficLimit json.RawMessage `json:"trafficlimit"`
	}
	if err := l.envelope(ctx, "/USERDETAILS", nil, &d); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: strings.TrimSpace(strings.ToLower(d.Type))}
	if info.Tier == "" {
		info.Tier = "premium"
	}
	// "lifetime" parses to 0 and leaves ExpiresAt zero, meaning no expiry.
	if secs := looseInt(d.Expire); secs > 0 {
		info.ExpiresAt = time.Unix(secs, 0).UTC()
	}
	used, limit := looseInt(d.TrafficUsed), looseInt(d.TrafficLimit)
	if limit > 0 {
		info.Traffic = TrafficInfo{UsedBytes: used, LimitBytes: limit}
	}
	return info, nil
}

// looseInt reads a number sent as a JSON number or as a string, and returns 0
// for anything else.
func looseInt(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return v
		}
	}
	return 0
}
