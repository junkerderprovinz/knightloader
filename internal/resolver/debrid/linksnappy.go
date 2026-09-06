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
// WHERE THIS ONE'S FIELD NAMES COME FROM, stated plainly because it is not the
// standard the other four in this package meet. Linksnappy publishes no API
// documentation any more: linksnappy.com/api is a 404, the FAQ carries none,
// and the Internet Archive has no copy. So this file rests on two sources
// instead of one:
//
//   - MEASURED against the live service (2026-09-06, no account needed):
//     GET /api/FILEHOSTS answers
//     {"status":"OK","error":false,"return":{"rapidgator.net":{"Status":"1",…}}},
//     and both /api/USERDETAILS and /api/linkgen answer the same envelope with
//     status "ERROR" plus a sentence when nobody is logged in. The envelope and
//     the host list are therefore certain.
//   - READ off a working open-source client, ResolveURL's linksnappy.py
//     (script.module.resolveurl, GPL-3.0), for the two calls that need an
//     account: authentication is GET /api/AUTHENTICATE?username=&password= with
//     the PLAIN password, keeping the session cookie it sets, and unlocking is
//     GET /api/linkgen?genLinks={"link":"…"} answering {"links":[{status, error,
//     generated, filename, filehost, …}]} - note that this one answers a bare
//     object, not the envelope above.
//
// jdp asked for it on those terms (2026-09-06, after being told the risk:
// "Jetzt einbauen, Feldnamen aus fremden Bibliotheken"). Everything below
// decodes LOOSELY as a result: a renamed or missing key leaves a zero value and
// produces a plain error, never a panic and never a direct link that is
// actually an error message. If the service ever changes one of these names,
// what happens is that unlocking fails with a sentence, which is the failure
// mode worth having.
type Linksnappy struct {
	user string
	pass string
	base string
	hc   *http.Client
}

func NewLinksnappy(user, pass string) *Linksnappy {
	// Its own cookie jar, and that is the whole authentication scheme:
	// AUTHENTICATE sets a session cookie and every later call is trusted by it.
	// A jar shared with the other services would mean one provider's session
	// travelling to another's host.
	jar, _ := cookiejar.New(nil)
	c := httpx.New(httpx.Options{Timeout: 30 * time.Second})
	c.Jar = jar
	return &Linksnappy{user: user, pass: pass, base: "https://linksnappy.com/api", hc: c}
}

func (*Linksnappy) ID() string    { return "linksnappy" }
func (*Linksnappy) Label() string { return "Linksnappy" }

// lsEnvelope is the wrapper FILEHOSTS, AUTHENTICATE and USERDETAILS share.
// Error is `false` on success and a SENTENCE on failure, which is why it is a
// RawMessage rather than a string: decoding a bool into a string fails, and
// that failure would swallow the successful case.
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
		// A shape that does not fit is not fatal on its own: the call
		// succeeded, and a caller that only needed to know THAT can carry on.
		_ = json.Unmarshal(env.Return, out)
	}
	return nil
}

// Authenticate logs in and keeps the session cookie. Exported because it is
// also the only honest way to check this service's credential: its host list
// needs no account at all, so a wrong password would otherwise verify happily.
func (l *Linksnappy) Authenticate(ctx context.Context) error {
	if l.user == "" || l.pass == "" {
		return errors.New("linksnappy: a username and a password are required")
	}
	return l.envelope(ctx, "/AUTHENTICATE", url.Values{
		"username": {l.user},
		"password": {l.pass},
	}, nil)
}

// Hosts asks /FILEHOSTS. Measured: the value is a map keyed by domain whose
// entries carry Status as a STRING ("1" for up), not a number.
func (l *Linksnappy) Hosts(ctx context.Context) (map[string]bool, error) {
	var hosts map[string]struct {
		Status string `json:"Status"`
	}
	if err := l.envelope(ctx, "/FILEHOSTS", nil, &hosts); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for domain, info := range hosts {
		// Only a host the service says is up. An empty Status is taken as up:
		// the field is what varies between their two host endpoints, and a
		// missing one must not empty the whole routing table.
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
	// Size arrives as a string on this endpoint in every sample seen, so it is
	// read as a RawMessage and parsed permissively - a number would otherwise
	// be the one shape that fails.
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

// Account reads /USERDETAILS. The least attested call in this file: its field
// names come from third-party clients only, so every one of them is optional
// and an unrecognised answer leaves the plan unknown rather than inventing one.
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
	// "lifetime" is a documented value of this field in every client that
	// touches it, and it is not a timestamp - a numeric parse leaves the zero
	// time, which reads as "nothing to expire", which is exactly right.
	if secs := looseInt(d.Expire); secs > 0 {
		info.ExpiresAt = time.Unix(secs, 0).UTC()
	}
	used, limit := looseInt(d.TrafficUsed), looseInt(d.TrafficLimit)
	if limit > 0 {
		info.Traffic = TrafficInfo{UsedBytes: used, LimitBytes: limit}
	}
	return info, nil
}

// looseInt reads a number that may have arrived as a JSON number or as a
// string. Linksnappy sends both, on different endpoints, for the same kind of
// value; 0 for anything else, which every caller here treats as "not stated".
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
