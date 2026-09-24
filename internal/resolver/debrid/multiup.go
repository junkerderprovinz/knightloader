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
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// MultiUp speaks the MultiUp API documented at
// https://multiup.io/en/upload/from-api. What that page leaves out, the login
// that unlocks the calling address and the older numeric status field, follows
// JDownloader's MultiupOrg.java.
type MultiUp struct {
	user string
	pass string
	base string
	hc   *http.Client

	// mu makes parallel unlocks wait for one login instead of each sending
	// their own, and guards loggedIn.
	mu       loginLock
	loggedIn time.Time
}

func NewMultiUp(user, pass string) *MultiUp {
	// The login may leave a session cookie next to the address unlock, so the
	// jar belongs to this account alone.
	jar, _ := cookiejar.New(nil)
	c := httpx.New(httpx.Options{Timeout: 30 * time.Second})
	c.Jar = jar
	return &MultiUp{user: user, pass: pass, base: "https://multiup.io/api", hc: c, mu: newLoginLock()}
}

func (*MultiUp) ID() string    { return "multiup" }
func (*MultiUp) Label() string { return "MultiUp" }

// multiupLoginTTL is how long a login is trusted before the next unlock logs in
// again. MultiUp returns no token, does not say how long an address stays
// unlocked and serves a lapsed one as a free user instead of refusing it, so
// the login is renewed often rather than kept until it fails.
const multiupLoginTTL = 5 * time.Minute

// multiupAnswer is the part every answer shares: error is "success" or the
// reason for a refusal, status the older API's numeric code.
type multiupAnswer struct {
	Error  json.RawMessage `json:"error"`
	Status json.RawMessage `json:"status"`
}

// refusal returns nil for a successful answer. The numeric status wins over
// the message because it is the only reason MultiUp gives in a fixed form.
func (a multiupAnswer) refusal(path string, httpStatus int) error {
	var msg string
	_ = json.Unmarshal(a.Error, &msg)
	msg = strings.TrimSpace(msg)
	code := looseInt(a.Status)
	if strings.EqualFold(msg, "success") {
		if code < 400 {
			return nil
		}
		msg = ""
	}
	return &multiupRefusal{path: path, kind: multiupClassify(code, httpStatus, msg), msg: msg}
}

// multiupKind sorts refusals into the few a person acts on differently.
type multiupKind int

const (
	multiupOther multiupKind = iota
	multiupBadLogin
	multiupFileGone
	multiupHostUnsupported
	multiupLimitReached
	multiupUnavailable
)

// multiupRefusal is an answer in which MultiUp declined the call. It is typed
// so Unlock can tell a lapsed login from a refusal of the link.
type multiupRefusal struct {
	path string
	kind multiupKind
	msg  string
}

func (r *multiupRefusal) Error() string {
	var s string
	switch r.kind {
	case multiupBadLogin:
		s = "multiup: the login was refused"
	case multiupFileGone:
		s = "multiup: the file is gone from the hoster"
	case multiupHostUnsupported:
		s = "multiup: this hoster is not supported or is down at MultiUp"
	case multiupLimitReached:
		s = "multiup: this account has reached a download limit"
	case multiupUnavailable:
		s = "multiup: the service is unavailable"
	default:
		if r.msg == "" {
			return "multiup " + r.path + ": refused without a reason"
		}
		return "multiup " + r.path + ": " + r.msg
	}
	if r.msg != "" {
		s += ": " + r.msg
	}
	return s
}

// multiupWords sorts a refusal by the wording of its message. MultiUp documents
// no error texts; "file not found" is the one seen live, from /check-file, and
// a message none of these match is passed on unchanged. "password" is no login
// word here: /generate-debrid-link takes the password of a protected file, so
// a refusal naming one is about the file and logging in again cannot help.
var multiupWords = []struct {
	kind  multiupKind
	words []string
}{
	{multiupLimitReached, []string{"limit", "quota", "exceeded", "too many"}},
	{multiupHostUnsupported, []string{"not supported", "unsupported"}},
	{multiupFileGone, []string{"not found", "deleted", "removed", "does not exist", "no longer"}},
	{multiupBadLogin, []string{"login", "logged"}},
}

func multiupClassify(code int64, httpStatus int, msg string) multiupKind {
	// The older codes as JDownloader reads them: 401 login failed, 404 hoster
	// offline or unsupported, 503 service unavailable.
	switch code {
	case 401:
		return multiupBadLogin
	case 404:
		return multiupHostUnsupported
	case 503:
		return multiupUnavailable
	}
	if kind := multiupHTTPKind(httpStatus); kind != multiupOther {
		return kind
	}
	lower := strings.ToLower(msg)
	for _, w := range multiupWords {
		for _, word := range w.words {
			if strings.Contains(lower, word) {
				return w.kind
			}
		}
	}
	return multiupOther
}

// multiupHTTPKind reads the status line, which is all there is to go on when
// the body is not JSON.
func multiupHTTPKind(status int) multiupKind {
	switch status {
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		return multiupLimitReached
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return multiupUnavailable
	}
	return multiupOther
}

// call sends one request and decodes a successful answer into out. A refusal
// comes back as a *multiupRefusal.
func (m *MultiUp) call(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, body)
	if err != nil {
		return err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var ans multiupAnswer
	if json.Unmarshal(raw, &ans) != nil {
		if kind := multiupHTTPKind(resp.StatusCode); kind != multiupOther {
			return &multiupRefusal{path: path, kind: kind, msg: resp.Status}
		}
		return fmt.Errorf("multiup %s: unreadable answer (%s)", path, resp.Status)
	}
	if err := ans.refusal(path, resp.StatusCode); err != nil {
		return err
	}
	if out != nil {
		// A field of an unexpected type only stays empty; each caller checks
		// for what it needs.
		_ = json.Unmarshal(raw, out)
	}
	return nil
}

// multiupLogin is the part of the /login answer Account reads.
type multiupLogin struct {
	AccountType     string          `json:"account_type"`
	PremiumDaysLeft json.RawMessage `json:"premium_days_left"`
}

// login needs mu held. The credentials go in the POST body, where the
// documentation puts them.
func (m *MultiUp) login(ctx context.Context) (multiupLogin, error) {
	if m.user == "" || m.pass == "" {
		return multiupLogin{}, errors.New("multiup: a username and a password are required")
	}
	var ans multiupLogin
	form := url.Values{"username": {m.user}, "password": {m.pass}}
	if err := m.call(ctx, http.MethodPost, "/login", form, &ans); err != nil {
		m.loggedIn = time.Time{}
		// However it is worded, a refused login is a login problem unless the
		// service itself is down.
		var r *multiupRefusal
		if errors.As(err, &r) && r.kind != multiupUnavailable {
			r.kind = multiupBadLogin
		}
		return multiupLogin{}, err
	}
	m.loggedIn = time.Now()
	return ans, nil
}

func (m *MultiUp) session(ctx context.Context) error {
	if err := m.mu.lock(ctx); err != nil {
		return err
	}
	defer m.mu.unlock()
	if !m.loggedIn.IsZero() && time.Since(m.loggedIn) < multiupLoginTTL {
		return nil
	}
	_, err := m.login(ctx)
	return err
}

// Authenticate logs in, which is the only call that checks the password: the
// host list is public and an unlock without a login is served as a free user.
func (m *MultiUp) Authenticate(ctx context.Context) error {
	if err := m.mu.lock(ctx); err != nil {
		return err
	}
	defer m.mu.unlock()
	_, err := m.login(ctx)
	return err
}

// Hosts reads /get-list-hosts-debrid, which needs no login and lists alias
// domains such as 1fichier's as entries of their own.
func (m *MultiUp) Hosts(ctx context.Context) (map[string]bool, error) {
	var ans struct {
		Hosts json.RawMessage `json:"hosts"`
	}
	if err := m.call(ctx, http.MethodGet, "/get-list-hosts-debrid", nil, &ans); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, d := range multiupDomains(ans.Hosts) {
		if d = NormalizeHost(d); d != "" && strings.Contains(d, ".") {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("multiup: /get-list-hosts-debrid named no hosts")
	}
	return set, nil
}

// multiupDomains reads the host list as the documented array or as a map keyed
// by domain, the second shape JDownloader accepts.
func multiupDomains(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var keyed map[string]json.RawMessage
	if json.Unmarshal(raw, &keyed) != nil {
		return nil
	}
	out := make([]string, 0, len(keyed))
	for d := range keyed {
		out = append(out, d)
	}
	return out
}

// Unlock asks /generate-debrid-link for a direct URL. The answer names no file
// and no size, so both come from the download itself.
func (m *MultiUp) Unlock(ctx context.Context, link string) (Direct, error) {
	if err := m.session(ctx); err != nil {
		return Direct{}, err
	}
	d, err := m.generate(ctx, link)
	var r *multiupRefusal
	if errors.As(err, &r) && r.kind == multiupBadLogin {
		// A refused login here means the address lost its unlock before the
		// cached login ran out, so one fresh login earns one more try.
		if err := m.Authenticate(ctx); err != nil {
			return Direct{}, err
		}
		d, err = m.generate(ctx, link)
	}
	return d, err
}

func (m *MultiUp) generate(ctx context.Context, link string) (Direct, error) {
	var ans struct {
		DebridLink string `json:"debrid_link"`
	}
	if err := m.call(ctx, http.MethodPost, "/generate-debrid-link", url.Values{"link": {link}}, &ans); err != nil {
		return Direct{}, err
	}
	if ans.DebridLink == "" {
		return Direct{}, errors.New("multiup: no direct link returned")
	}
	return Direct{URL: ans.DebridLink}, nil
}

// HostLimit satisfies HostLimiter with one connection per download, as
// JDownloader uses: MultiUp allows three per address across all downloads,
// fewer than a single download opens by default.
func (*MultiUp) HostLimit(string) int { return 1 }

// Account logs in and reads the plan from the login answer, the only account
// data MultiUp exposes. Premium is sold as unlimited and no traffic figure
// exists, so a premium account reads Unlimited.
func (m *MultiUp) Account(ctx context.Context) (AccountInfo, error) {
	if err := m.mu.lock(ctx); err != nil {
		return AccountInfo{}, err
	}
	ans, err := m.login(ctx)
	m.mu.unlock()
	if err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: strings.ToLower(strings.TrimSpace(ans.AccountType))}
	if info.Tier == "" {
		info.Tier = "free"
	}
	if info.Tier != "premium" {
		return info, nil
	}
	info.Traffic.Unlimited = true
	// premium_days_left counts whole days, so the expiry is only good to a day.
	if days := looseInt(ans.PremiumDaysLeft); days > 0 {
		info.ExpiresAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).UTC()
	}
	return info, nil
}
