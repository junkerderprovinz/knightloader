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

// ProLeech speaks deb_api.php on proleech.link, which has no public
// documentation. Calls, fields and error codes follow ProLeech's own Firefox
// add-on (ProLeech Downloader 1.1.0, lib/api.js) and JDownloader's
// ProLeechLink.java.
type ProLeech struct {
	user string
	key  string
	base string
	hc   *http.Client

	pace proleechPacer

	// capMu guards chunkCaps, written by Unlock on download goroutines and
	// read by HostLimit from dispatch.
	capMu     sync.Mutex
	chunkCaps map[string]int
}

// NewProLeech takes the API username and API key that
// proleech.link/v2/jdownloader shows to premium members.
func NewProLeech(apiUser, apiKey string) *ProLeech {
	return &ProLeech{
		user: strings.TrimSpace(apiUser),
		key:  strings.TrimSpace(apiKey),
		base: "https://proleech.link/dl/debrid/deb_api.php",
		// A direct link only works from the IP that asked for it, and downloads
		// leave through the loopback proxy, which dials direct. Sending the API
		// calls through an HTTP_PROXY would bind every link to the proxy's IP.
		hc: httpx.New(httpx.Options{Timeout: proleechCallTimeout, Proxy: httpx.NoProxy}),
	}
}

func (*ProLeech) ID() string    { return "proleech" }
func (*ProLeech) Label() string { return "ProLeech" }

// query adds the credentials, which the API takes in the query string on every
// call except the host list.
func (p *ProLeech) query(q url.Values) (string, error) {
	if p.user == "" || p.key == "" {
		return "", errors.New("proleech: an API username and an API key are required")
	}
	q.Set("apiusername", p.user)
	q.Set("apikey", p.key)
	return q.Encode(), nil
}

func (p *ProLeech) get(ctx context.Context, query string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"?"+query, nil)
	if err != nil {
		return nil, "", proleechScrub(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, "", proleechScrub(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", proleechScrub(err)
	}
	return raw, resp.Status, nil
}

// proleechScrub drops the request URL from a transport error, because the
// credentials travel in its query string.
func proleechScrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("proleech: %w", err)
}

// call decodes the whole answer into out, so each caller declares only the
// fields it reads.
func (p *ProLeech) call(ctx context.Context, what, query string, out any) error {
	raw, status, err := p.get(ctx, query)
	if err != nil {
		return err
	}
	var st proleechStatus
	if json.Unmarshal(raw, &st) != nil {
		return fmt.Errorf("proleech %s: unreadable answer (%s)", what, status)
	}
	if err := st.err(); err != nil {
		return err
	}
	if out != nil {
		// The answer parsed as an object above, so a field of an unexpected
		// type only stays empty.
		_ = json.Unmarshal(raw, out)
	}
	return nil
}

// proleechStatus is the error code and message every authenticated answer
// carries.
type proleechStatus struct {
	Error   json.RawMessage `json:"error"`
	Message json.RawMessage `json:"message"`
}

// err reads "error", which is 0, absent or false on success. A quoted code
// counts as that code, and any other text as a refusal without one.
func (s proleechStatus) err() error {
	var msg string
	_ = json.Unmarshal(s.Message, &msg)
	var code int64
	var text string
	var flag bool
	switch {
	case json.Unmarshal(s.Error, &code) == nil:
		if code == 0 {
			return nil
		}
	case json.Unmarshal(s.Error, &text) == nil:
		text = strings.TrimSpace(text)
		if text == "" || text == "0" {
			return nil
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			code = n
		} else if msg == "" {
			msg = text
		}
	case json.Unmarshal(s.Error, &flag) == nil && !flag:
		return nil
	case len(s.Error) == 0:
		return nil
	}
	return &proleechError{code: code, msg: strings.TrimSpace(msg)}
}

// proleechError is a refusal from the API. The codes are the admin's list as
// JD's ProLeechLink.java quotes it, plus -12 from the add-on.
type proleechError struct {
	code int64
	msg  string
}

func (e *proleechError) Error() string {
	var why string
	switch e.code {
	case 1, 3:
		why = "ProLeech does not support this link"
	case 2:
		why = "the link has to start with http:// or https://"
	case 4:
		why = "ProLeech has no working account for this hoster at the moment; try again later"
	case 7:
		why = "the file is gone or its hoster is down"
	case 8:
		why = "the daily limit for this hoster is used up"
	case 9:
		why = "the file is larger than the traffic this account has left"
	case -1, -6:
		why = "the API username or API key was refused; check both on proleech.link/v2/jdownloader"
	case -4:
		why = "the API is closed for maintenance"
	case -5:
		why = "the API only serves premium accounts"
	case -8, -10:
		why = "ProLeech reports a problem with this account"
	case -9:
		why = "the account is locked for account sharing"
	case -12:
		why = "the API is locked for this account after too many unlocks"
	case 0:
		why = "ProLeech refused the request"
	default:
		why = fmt.Sprintf("ProLeech answered with error %d", e.code)
	}
	if e.msg == "" {
		return "proleech: " + why
	}
	return "proleech: " + why + " (" + e.msg + ")"
}

// Hosts reads the public host list and, when credentials are set, the domain
// map the add-on matches links with, which adds each hoster's other domains.
// The API marks no host as down, so every listed host is taken.
func (p *ProLeech) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, status, err := p.get(ctx, "hosts")
	if err != nil {
		return nil, err
	}
	var st proleechStatus
	if json.Unmarshal(raw, &st) == nil {
		if err := st.err(); err != nil {
			return nil, err
		}
	}
	set := map[string]bool{}
	proleechAddDomains(set, raw)

	// The public list is enough to route by, so a refused domain map only
	// costs the extra domains.
	if query, err := p.query(url.Values{"domainmap": {"1"}}); err == nil {
		var dm struct {
			Domains json.RawMessage `json:"domains"`
		}
		if p.call(ctx, "domain map", query, &dm) == nil {
			proleechAddDomains(set, dm.Domains)
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("proleech: the host list named no hosts (%s)", status)
	}
	return set, nil
}

// proleechAddDomains adds each domain of a JSON array of strings to set. One
// entry can name several domains separated by "/".
func proleechAddDomains(set map[string]bool, raw json.RawMessage) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return
	}
	for _, e := range entries {
		var s string
		if json.Unmarshal(e, &s) != nil {
			continue
		}
		for _, part := range strings.Split(s, "/") {
			if d := NormalizeHost(part); strings.Contains(d, ".") && !strings.ContainsAny(d, " :") {
				set[d] = true
			}
		}
	}
}

// Unlock asks for a direct link. The link works only from the IP that asked
// for it and only for a few seconds, so it has to be fetched right before the
// download starts.
func (p *ProLeech) Unlock(ctx context.Context, link string) (Direct, error) {
	query, err := p.query(url.Values{"link": {link}})
	if err != nil {
		return Direct{}, err
	}
	if err := p.pace.wait(ctx); err != nil {
		return Direct{}, err
	}
	var d struct {
		Link      string          `json:"link"`
		Filename  string          `json:"filename"`
		Size      json.RawMessage `json:"size"`
		MaxChunks json.RawMessage `json:"max_chunks"`
	}
	if err := p.call(ctx, "unlock", query, &d); err != nil {
		return Direct{}, err
	}
	// JD likewise takes a "link" that is not an http URL as no link at all.
	if u, err := url.Parse(d.Link); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Direct{}, errors.New("proleech: no direct link returned")
	}
	p.rememberChunks(link, int(looseInt(d.MaxChunks)))
	return Direct{URL: d.Link, Name: d.Filename, Size: proleechSize(d.Size)}, nil
}

// rememberChunks keeps the max_chunks of the latest unlock per hoster, since
// it is the service's current setting rather than a measurement. An answer
// without one stores 0, which puts the hoster back on the default.
func (p *ProLeech) rememberChunks(link string, chunks int) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return
	}
	p.capMu.Lock()
	defer p.capMu.Unlock()
	if p.chunkCaps == nil {
		p.chunkCaps = map[string]int{}
	}
	p.chunkCaps[NormalizeHost(u.Hostname())] = chunks
}

// HostLimit satisfies HostLimiter. A hoster gets one connection, the default
// JD downloads ProLeech links with, until an unlock answer raises it with
// max_chunks.
func (p *ProLeech) HostLimit(host string) int {
	p.capMu.Lock()
	defer p.capMu.Unlock()
	if n := p.chunkCaps[NormalizeHost(host)]; n > 0 {
		return n
	}
	return 1
}

// Authenticate checks the API username and key against the account call,
// because the host list answers without them.
func (p *ProLeech) Authenticate(ctx context.Context) error {
	query, err := p.query(url.Values{"account": {"1"}})
	if err != nil {
		return err
	}
	return p.call(ctx, "account", query, nil)
}

// Account reads the plan, its last day and today's traffic.
func (p *ProLeech) Account(ctx context.Context) (AccountInfo, error) {
	query, err := p.query(url.Values{"account": {"1"}})
	if err != nil {
		return AccountInfo{}, err
	}
	var d struct {
		Premium           json.RawMessage `json:"premium"`
		SubscriptionsDate string          `json:"subscriptions_date"`
		UsedToday         json.RawMessage `json:"used_today"`
		TrafficLeft       json.RawMessage `json:"traffic_left"`
	}
	if err := p.call(ctx, "account", query, &d); err != nil {
		// The API turns free accounts away as a whole, so -5 is the plan
		// rather than a fault.
		var pe *proleechError
		if errors.As(err, &pe) && pe.code == -5 {
			return AccountInfo{Tier: "free"}, nil
		}
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if proleechYes(d.Premium) {
		info.Tier = "premium"
	}
	// subscriptions_date is the plan's last day, so it runs out at the end of it.
	if len(d.SubscriptionsDate) >= 10 {
		if day, err := time.Parse("2006-01-02", d.SubscriptionsDate[:10]); err == nil {
			info.ExpiresAt = day.AddDate(0, 0, 1)
		}
	}
	used, left := proleechSize(d.UsedToday), proleechSize(d.TrafficLeft)
	info.Traffic.UsedBytes = used
	// The API names no cap and traffic_left is often 0, so a limit comes only
	// from a positive traffic_left, sized so that exactly that much reads as left.
	if left > 0 {
		info.Traffic.LimitBytes = used + left
	}
	return info, nil
}

// proleechYes reads a flag the API sends as "yes" or "true", and takes a JSON
// bool or 1 as well.
func proleechYes(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(string(raw)), `"`)) {
	case "yes", "true", "1":
		return true
	}
	return false
}

var proleechUnits = map[string]float64{
	"": 1, "B": 1, "BYTES": 1,
	"KB": 1 << 10, "KIB": 1 << 10,
	"MB": 1 << 20, "MIB": 1 << 20,
	"GB": 1 << 30, "GIB": 1 << 30,
	"TB": 1 << 40, "TIB": 1 << 40,
}

// proleechSize reads a byte count sent as a number, as digits or as text such
// as "10.15 MB". Units step by 1024, as JD reads the same field, so text gives
// an estimate until the download reports its own length.
func proleechSize(raw json.RawMessage) int64 {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int64(f)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0
	}
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i < 0 {
		i = len(s)
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	mult, ok := proleechUnits[strings.ToUpper(strings.TrimSpace(s[i:]))]
	if err != nil || !ok {
		return 0
	}
	return int64(v * mult)
}

// The API locks an account for twelve hours after about 30 unlocks in five
// minutes. These are the limits ProLeech's own add-on keeps to.
const (
	proleechUnlockGap    = 2500 * time.Millisecond
	proleechUnlockWindow = 5 * time.Minute
	proleechUnlockBudget = 25
)

const (
	proleechCallTimeout = 30 * time.Second

	// proleechCallRoom is how much time a slot has to leave before the
	// caller's deadline. A call sent with less could be cut off by the
	// deadline and would still use up a place in the budget.
	proleechCallRoom = proleechCallTimeout + 10*time.Second
)

// proleechPacer spaces unlocks at least proleechUnlockGap apart and allows no
// more than proleechUnlockBudget in any proleechUnlockWindow.
type proleechPacer struct {
	mu   sync.Mutex
	sent []time.Time // booked send times, oldest first
}

// reserve books the earliest free slot at or after now and returns it. A slot
// that leaves less than proleechCallRoom before deadline is not booked, so a
// caller that gives up leaves no gap.
func (pc *proleechPacer) reserve(now, deadline time.Time) (time.Time, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	kept := pc.sent[:0]
	for _, t := range pc.sent {
		if now.Sub(t) < proleechUnlockWindow {
			kept = append(kept, t)
		}
	}
	pc.sent = kept
	at := now
	if n := len(pc.sent); n > 0 {
		if t := pc.sent[n-1].Add(proleechUnlockGap); t.After(at) {
			at = t
		}
		if n >= proleechUnlockBudget {
			if t := pc.sent[n-proleechUnlockBudget].Add(proleechUnlockWindow); t.After(at) {
				at = t
			}
		}
	}
	if !deadline.IsZero() && at.Add(proleechCallRoom).After(deadline) {
		return at, false
	}
	pc.sent = append(pc.sent, at)
	return at, true
}

// wait blocks until this caller's slot. A slot too close to ctx's deadline
// fails at once with the time it would have been, instead of waiting only to
// run out of time.
func (pc *proleechPacer) wait(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	at, ok := pc.reserve(time.Now(), deadline)
	if !ok {
		return fmt.Errorf("proleech: holding unlocks back until %s, because ProLeech locks the API after about 30 unlocks in five minutes", at.Local().Format("15:04:05"))
	}
	d := time.Until(at)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		pc.release(at)
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// release gives back the slot of a caller that stopped waiting, since an
// unlock that was never sent does not count against the window.
func (pc *proleechPacer) release(at time.Time) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for i, t := range pc.sent {
		if t.Equal(at) {
			pc.sent = append(pc.sent[:i], pc.sent[i+1:]...)
			return
		}
	}
}
