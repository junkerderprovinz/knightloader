package debrid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DebridItalia speaks the api.php interface of debriditalia.com, which has no
// published documentation. Calls and answers follow JDownloader's
// DebridItaliaCom plugin (rev 52202, jd/plugins/hoster/DebridItaliaCom.java)
// and pyLoad's DebridItaliaCom downloader and account plugins.
type DebridItalia struct {
	user string
	pass string
	base string
	hc   *http.Client
	pace *debriditaliaPacer
}

func NewDebridItalia(user, pass string) *DebridItalia {
	return &DebridItalia{
		user: user,
		pass: pass,
		base: "https://debriditalia.com/api.php",
		// JD's plugin gives each call a minute, and generate has to reach the
		// hoster before it answers.
		hc:   httpx.New(httpx.Options{Timeout: time.Minute, ResponseHeaderTimeout: time.Minute}),
		pace: debriditaliaShared,
	}
}

func (*DebridItalia) ID() string    { return "debriditalia" }
func (*DebridItalia) Label() string { return "DebridItalia" }

// debriditaliaPerMinute is the allowance JD's plugin keeps to. Going over it
// blocks further requests for an hour, so waiting for a slot costs less.
const debriditaliaPerMinute = 30

// debriditaliaShared paces every client together: JD applies the limit to the
// whole domain, and the app builds a fresh client for each account check.
var debriditaliaShared = &debriditaliaPacer{window: time.Minute, max: debriditaliaPerMinute}

// debriditaliaPacer lets at most max calls start within any window.
type debriditaliaPacer struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	sent   []time.Time
}

func (p *debriditaliaPacer) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		now := time.Now()
		spent := 0
		for spent < len(p.sent) && now.Sub(p.sent[spent]) >= p.window {
			spent++
		}
		p.sent = p.sent[spent:]
		if len(p.sent) < p.max {
			p.sent = append(p.sent, now)
			p.mu.Unlock()
			return nil
		}
		delay := p.window - now.Sub(p.sent[0])
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// call performs one GET and returns the trimmed plain-text answer. The API
// takes the login as u and p in the query string of every account call.
func (d *DebridItalia) call(ctx context.Context, q url.Values, login bool) (string, error) {
	if login {
		if d.user == "" || d.pass == "" {
			return "", errors.New("debriditalia: a username and a password are required")
		}
		q.Set("u", d.user)
		q.Set("p", d.pass)
	}
	if err := d.pace.wait(ctx); err != nil {
		return "", fmt.Errorf("debriditalia: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base, nil)
	if err != nil {
		return "", err
	}
	req.URL.RawQuery = q.Encode()
	resp, err := d.hc.Do(req)
	if err != nil {
		// url.Error quotes the request URL, and the password is part of it.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("debriditalia: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("debriditalia: %w", err)
	}
	body := strings.TrimSpace(string(raw))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return "", errors.New("debriditalia: the username or password was refused")
	case http.StatusTooManyRequests:
		return "", errors.New("debriditalia: too many requests, the service allows 30 a minute")
	}
	const refused = "ERROR:"
	if len(body) >= len(refused) && strings.EqualFold(body[:len(refused)], refused) {
		return "", debriditaliaError(strings.TrimSpace(body[len(refused):]))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("debriditalia: %s", resp.Status)
	}
	return body, nil
}

// debriditaliaError reads the code after "ERROR:". The three known codes come
// from the JD and pyLoad plugins; any other code is passed on as sent.
func debriditaliaError(code string) error {
	switch strings.ToLower(code) {
	case "not_supported":
		return errors.New("debriditalia: this hoster is not supported (not_supported)")
	case "not_available":
		return errors.New("debriditalia: the hoster reports the file as unavailable, it may have been removed (not_available)")
	case "bandwidth_limit":
		return errors.New("debriditalia: the daily traffic cap for this hoster is used up (bandwidth_limit)")
	case "":
		return errors.New("debriditalia: the request was refused without a reason")
	}
	return fmt.Errorf("debriditalia: the request was refused (%s)", code)
}

// debriditaliaDomain keeps words and markup out of the routing table when the
// host list answers with something other than domains.
var debriditaliaDomain = regexp.MustCompile(`^(?:[a-z0-9-]+\.)+[a-z][a-z0-9-]+$`)

// Hosts reads the public host list: quoted domains separated by commas, without
// the brackets that would make it JSON. The list carries neither a status nor
// aliases, so every listed domain is taken.
func (d *DebridItalia) Hosts(ctx context.Context) (map[string]bool, error) {
	body, err := d.call(ctx, url.Values{"hosts": {""}}, false)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, entry := range strings.Split(strings.Trim(body, "[]"), ",") {
		// NormalizeHost strips "www." before it lower-cases, so the case goes
		// first or "WWW." would stay.
		host := NormalizeHost(strings.ToLower(strings.Trim(strings.TrimSpace(entry), `"'`)))
		if debriditaliaDomain.MatchString(host) {
			set[host] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("debriditalia: the host list named no hosts")
	}
	return set, nil
}

// Unlock asks generate for a direct link, which arrives as the whole answer.
// The file name only comes with the download's Content-Disposition, so the
// engine fills in name and size.
func (d *DebridItalia) Unlock(ctx context.Context, link string) (Direct, error) {
	// JD and pyLoad both send the hoster link as http; JD's plugin calls it a
	// workaround for a server-side bug with https links.
	const secure = "https://"
	if len(link) > len(secure) && strings.EqualFold(link[:len(secure)], secure) {
		link = "http://" + link[len(secure):]
	}
	body, err := d.call(ctx, url.Values{"generate": {"on"}, "link": {link}}, true)
	if err != nil {
		return Direct{}, err
	}
	u, err := url.Parse(body)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(body, "<>\"") {
		return Direct{}, errors.New("debriditalia: unreadable answer to generate")
	}
	// The file name at the end of the path may come with its spaces unescaped,
	// which JD accepts, so the engine is handed the escaped form.
	return Direct{URL: u.String()}, nil
}

var (
	debriditaliaStatusTag     = regexp.MustCompile(`(?is)<status>\s*(.*?)\s*</status>`)
	debriditaliaExpirationTag = regexp.MustCompile(`(?is)<expiration>\s*(.*?)\s*</expiration>`)
)

// check reads the account answer: a status of valid, expired or invalid, and
// the premium expiry. JD takes an expired account as a correct login, and so
// does check.
func (d *DebridItalia) check(ctx context.Context) (string, time.Time, error) {
	body, err := d.call(ctx, url.Values{"check": {"on"}}, true)
	if err != nil {
		return "", time.Time{}, err
	}
	m := debriditaliaStatusTag.FindStringSubmatch(body)
	if m == nil {
		return "", time.Time{}, errors.New("debriditalia: unreadable answer to the account check")
	}
	status := strings.ToLower(strings.Trim(m[1], `"' `))
	switch status {
	case "valid", "expired":
		var until time.Time
		if e := debriditaliaExpirationTag.FindStringSubmatch(body); e != nil {
			until = debriditaliaUnix(e[1])
		}
		return status, until, nil
	case "invalid":
		return "", time.Time{}, errors.New("debriditalia: the username or password was refused")
	}
	return "", time.Time{}, fmt.Errorf("debriditalia: unknown account status %q", status)
}

// debriditaliaUnix reads Unix seconds written bare, quoted or with a fraction,
// and returns the zero time for anything else.
func debriditaliaUnix(s string) time.Time {
	secs, err := strconv.ParseFloat(strings.Trim(s, `"' `), 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(secs), 0).UTC()
}

// Authenticate checks the username and password, which the host list never
// asks for. An expired account passes, since its login is right and Account
// reports the lapse.
func (d *DebridItalia) Authenticate(ctx context.Context) error {
	_, _, err := d.check(ctx)
	return err
}

// Account reads check=on. The service states no traffic figure and caps bytes
// only per hoster, so a valid account reads Unlimited, as it does in JD and
// pyLoad.
func (d *DebridItalia) Account(ctx context.Context) (AccountInfo, error) {
	status, until, err := d.check(ctx)
	if err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free", ExpiresAt: until}
	if status == "valid" {
		info.Tier = "premium"
		info.Traffic.Unlimited = true
	}
	return info, nil
}
