package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// MegaDebrid speaks the Mega-Debrid API documented at
// https://www.mega-debrid.eu/index.php?page=api, trading a user name and
// password for a session token that lives until the next login.
//
// The documentation names no error codes, so TOKEN_ERROR, UNKNOWN_USER,
// UNALLOWED_IP and the HTTP 401 and 405 answers follow pyLoad's MegaDebridEu
// plugins, and the refusal texts follow JDownloader's. Bodies go out
// form-encoded as both clients send them, and filename on getLink comes from
// the archived newer documentation at api.mega-debrid.eu.
type MegaDebrid struct {
	user string
	pass string
	base string
	hc   *http.Client

	// mu guards token and refused and is held across a login: every
	// connectUser voids the token before it, so two logins racing would leave
	// one caller holding a dead token.
	mu    loginLock
	token string
	// refused keeps a rejected login without asking again, because four of
	// them ban this address for some minutes.
	refused error
}

func NewMegaDebrid(user, pass string) *MegaDebrid {
	return &MegaDebrid{
		user: user,
		pass: pass,
		base: "https://www.mega-debrid.eu/api.php",
		hc:   httpx.New(httpx.Options{Timeout: 30 * time.Second}),
		mu:   newLoginLock(),
	}
}

func (*MegaDebrid) ID() string    { return "megadebrid" }
func (*MegaDebrid) Label() string { return "Mega-Debrid" }

// megadebridErrToken marks a getLink refused for its token, which Unlock
// answers with one fresh login.
var megadebridErrToken = errors.New("the session token was refused")

// megadebridAnswer is one reply: response_code is "ok" or an error code, and
// response_text describes the error.
type megadebridAnswer struct {
	status int
	code   string
	text   string
	body   []byte
}

func (a megadebridAnswer) ok() bool { return strings.EqualFold(a.code, "ok") }

func (a megadebridAnswer) banned() bool {
	return a.code == "UNALLOWED_IP" || a.status == http.StatusMethodNotAllowed
}

// call sends one action, as a POST when form is set. A transport error is cut
// down to its cause because *url.Error prints the request URL, and that
// carries the password or the token.
func (m *MegaDebrid) call(ctx context.Context, action string, q, form url.Values) (megadebridAnswer, error) {
	params := url.Values{"action": {action}}
	for k, v := range q {
		params[k] = v
	}
	method, body := http.MethodGet, io.Reader(nil)
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+"?"+params.Encode(), body)
	if err != nil {
		return megadebridAnswer{}, fmt.Errorf("mega-debrid %s: the request could not be built", action)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return megadebridAnswer{}, fmt.Errorf("mega-debrid %s: %w", action, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return megadebridAnswer{}, fmt.Errorf("mega-debrid %s: %w", action, err)
	}
	a := megadebridAnswer{status: resp.StatusCode, body: raw}
	var env struct {
		Code json.RawMessage `json:"response_code"`
		Text json.RawMessage `json:"response_text"`
	}
	if json.Unmarshal(raw, &env) == nil {
		a.code, a.text = megadebridString(env.Code), megadebridString(env.Text)
	}
	return a, nil
}

// fail describes a refused call. The address ban is the one refusal every
// action shares; anything else carries the service's own text.
func (a megadebridAnswer) fail(action string) error {
	switch {
	case a.banned():
		return megadebridFail("this IP address is blocked, which Mega-Debrid does for some minutes after four failed logins or too many requests", a.text)
	case a.text != "":
		return fmt.Errorf("mega-debrid %s: %s", action, a.text)
	case a.code != "":
		return fmt.Errorf("mega-debrid %s: refused with %s", action, a.code)
	}
	return fmt.Errorf("mega-debrid %s: unreadable answer (HTTP %d)", action, a.status)
}

// unlockFail reads a refused getLink. The documentation names no codes for
// these refusals, so they are told apart by the texts JDownloader matches. The
// quota text is not known at all: the help centre only says some hosters are
// capped daily in links or volume, so a text naming a limit or quota counts.
func (a megadebridAnswer) unlockFail() error {
	lower := strings.ToLower(a.text)
	switch {
	case a.banned():
		return a.fail("getLink")
	case a.code == "TOKEN_ERROR" || strings.Contains(lower, "token error"):
		if a.text == "" {
			return fmt.Errorf("mega-debrid: %w", megadebridErrToken)
		}
		return fmt.Errorf("mega-debrid: %w: %s", megadebridErrToken, a.text)
	case strings.Contains(lower, "unable to load file"):
		return megadebridFail("the file could not be loaded from the hoster, so the link is probably dead", a.text)
	case strings.Contains(lower, "lien incorrect"):
		return megadebridFail("Mega-Debrid rejects this link, so the hoster is probably not supported", a.text)
	case strings.Contains(lower, "limit") || strings.Contains(lower, "quota"):
		return megadebridFail("the daily limit for this hoster is used up until Mega-Debrid resets it at midnight", a.text)
	case strings.Contains(lower, "vpn, proxy"):
		return megadebridFail("Mega-Debrid refuses requests from VPNs, proxies and servers", a.text)
	case strings.Contains(lower, "débrideur"):
		return megadebridFail("the unlocker failed on Mega-Debrid's side, try again later", a.text)
	}
	return a.fail("getLink")
}

// megadebridFail joins a sentence with the service's own text, when it sent one.
func megadebridFail(sentence, text string) error {
	if text == "" {
		return errors.New("mega-debrid: " + sentence)
	}
	return fmt.Errorf("mega-debrid: %s: %s", sentence, text)
}

// loginLocked calls connectUser, keeps the token and returns vip_end. m.mu
// must be held.
func (m *MegaDebrid) loginLocked(ctx context.Context) (int64, error) {
	if m.refused != nil {
		return 0, m.refused
	}
	if m.user == "" || m.pass == "" {
		return 0, errors.New("mega-debrid: a user name and a password are required")
	}
	a, err := m.call(ctx, "connectUser", url.Values{"login": {m.user}, "password": {m.pass}}, nil)
	if err != nil {
		return 0, err
	}
	if a.code == "UNKNOWN_USER" || a.status == http.StatusUnauthorized {
		m.token = ""
		m.refused = megadebridFail("the user name or password was refused", a.text)
		return 0, m.refused
	}
	if !a.ok() {
		return 0, a.fail("connectUser")
	}
	var got struct {
		Token  json.RawMessage `json:"token"`
		VipEnd json.RawMessage `json:"vip_end"`
	}
	_ = json.Unmarshal(a.body, &got)
	token := megadebridString(got.Token)
	if token == "" {
		return 0, errors.New("mega-debrid: connectUser answered without a token")
	}
	m.token = token
	return looseInt(got.VipEnd), nil
}

// session returns the cached token, logging in first when there is none or
// when it equals stale, the token a call just had refused. A caller that
// waited out another caller's login gets that login's token.
func (m *MegaDebrid) session(ctx context.Context, stale string) (string, error) {
	if err := m.mu.lock(ctx); err != nil {
		return "", err
	}
	defer m.mu.unlock()
	if m.token != "" && m.token != stale {
		return m.token, nil
	}
	if _, err := m.loginLocked(ctx); err != nil {
		return "", err
	}
	return m.token, nil
}

// Authenticate logs in and keeps the token. It is exported to verify the
// credential, since the host list is public and would accept any.
func (m *MegaDebrid) Authenticate(ctx context.Context) error {
	if err := m.mu.lock(ctx); err != nil {
		return err
	}
	defer m.mu.unlock()
	_, err := m.loginLocked(ctx)
	return err
}

// megadebridHoster is one entry of getHostersList. Domains lists every domain
// the hoster answers under, aliases included.
type megadebridHoster struct {
	Status  json.RawMessage `json:"status"`
	Type    json.RawMessage `json:"type"`
	Domains []string        `json:"domains"`
}

// Hosts reads the public getHostersList, leaving out hosters not marked "up"
// and stream sites. AllDebrid and Debrid-Link read only their file hosters too,
// and a missing status counts as up so a renamed field cannot empty the
// routing table.
func (m *MegaDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	a, err := m.call(ctx, "getHostersList", nil, nil)
	if err != nil {
		return nil, err
	}
	if !a.ok() {
		return nil, a.fail("getHostersList")
	}
	var body struct {
		Hosters []json.RawMessage `json:"hosters"`
	}
	_ = json.Unmarshal(a.body, &body)
	set := map[string]bool{}
	for _, raw := range body.Hosters {
		// Decoded one by one, so a malformed entry costs that hoster only.
		var h megadebridHoster
		if json.Unmarshal(raw, &h) != nil {
			continue
		}
		status := megadebridString(h.Status)
		if status != "" && !strings.EqualFold(status, "up") {
			continue
		}
		if strings.EqualFold(megadebridString(h.Type), "stream") {
			continue
		}
		for _, d := range h.Domains {
			if d = NormalizeHost(d); d != "" && strings.Contains(d, ".") {
				set[d] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("mega-debrid: getHostersList named no hosts")
	}
	return set, nil
}

// Unlock posts the link to getLink. A refused token gets one fresh login and
// one retry, since any login elsewhere, an account check included, voids it.
func (m *MegaDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	token, err := m.session(ctx, "")
	if err != nil {
		return Direct{}, err
	}
	d, err := m.getLink(ctx, token, link)
	if !errors.Is(err, megadebridErrToken) {
		return d, err
	}
	if token, err = m.session(ctx, token); err != nil {
		return Direct{}, err
	}
	return m.getLink(ctx, token, link)
}

// getLink reports no size; the engine takes it from Content-Length.
func (m *MegaDebrid) getLink(ctx context.Context, token, link string) (Direct, error) {
	a, err := m.call(ctx, "getLink", url.Values{"token": {token}}, url.Values{"link": {link}})
	if err != nil {
		return Direct{}, err
	}
	if !a.ok() {
		return Direct{}, a.unlockFail()
	}
	var got struct {
		DebridLink json.RawMessage `json:"debridLink"`
		Filename   json.RawMessage `json:"filename"`
	}
	_ = json.Unmarshal(a.body, &got)
	direct := megadebridString(got.DebridLink)
	switch {
	case direct == "cantDebridLink":
		return Direct{}, megadebridFail("Mega-Debrid could not unlock this link", a.text)
	case !strings.HasPrefix(direct, "http"):
		return Direct{}, megadebridFail("no direct link returned", a.text)
	}
	return Direct{URL: direct, Name: megadebridString(got.Filename)}, nil
}

// Account logs in, because vip_end on connectUser is the only account data the
// API gives. Mega-Debrid caps only some hosters, daily, and the API does not
// report those caps, so an active premium reads Unlimited.
func (m *MegaDebrid) Account(ctx context.Context) (AccountInfo, error) {
	if err := m.mu.lock(ctx); err != nil {
		return AccountInfo{}, err
	}
	vipEnd, err := m.loginLocked(ctx)
	m.mu.unlock()
	if err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if vipEnd > 0 {
		info.ExpiresAt = time.Unix(vipEnd, 0).UTC()
		if info.ExpiresAt.After(time.Now()) {
			info.Tier = "premium"
			info.Traffic.Unlimited = true
		}
	}
	return info, nil
}

// megadebridString reads a field sent as a JSON string, number or bool as
// text, and returns "" for anything else.
func megadebridString(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64, bool:
		return strings.TrimSpace(string(raw))
	}
	return ""
}
