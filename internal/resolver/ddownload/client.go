package ddownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// DefaultSite is where the pages are read.
const DefaultSite = "https://" + Site

// maxPage caps how much of a page is read. A file page is a few hundred
// kilobytes, most of it styles and scripts.
const maxPage = 4 << 20

// sessionCookie is the cookie a signed-in session carries.
const sessionCookie = "xfss"

// The reasons a download or a sign-in cannot go ahead, whatever the wait.
var (
	ErrGone            = errors.New("ddownload: the file has been deleted or never existed")
	ErrPremiumOnly     = errors.New("ddownload: the owner keeps this file for premium accounts")
	ErrPassword        = errors.New("ddownload: the file is protected by a password, and the download has no download password")
	ErrPasswordRefused = errors.New("ddownload: the download password was not accepted")
	ErrCaptchaRefused  = errors.New("ddownload: the answer to the captcha was not accepted")
	ErrNoAnswer        = errors.New("ddownload: nobody answered the captcha")
	ErrTraffic         = errors.New("ddownload: the premium traffic for today is used up")
	ErrPremiumExpired  = errors.New("ddownload: the premium of the ddownload login has run out")
	ErrLoginRefused    = errors.New("ddownload: the username or password of the ddownload login was not accepted")
	ErrSignInBlocked   = errors.New("ddownload: ddownload takes no sign-ins from this address")
	ErrSignInFailed    = errors.New("ddownload: ddownload did not sign the login in")
	ErrNoLogin         = errors.New("ddownload: there is no ddownload login to sign in with")

	// errSignedOut is a session ddownload has ended, which a new
	// sign-in mends.
	errSignedOut = errors.New("ddownload: the session has ended")
)

// WaitError is ddownload asking for a pause before the free download.
type WaitError struct {
	Wait time.Duration
}

func (e *WaitError) Error() string {
	return fmt.Sprintf("ddownload: this address has to wait %s before its next free download", e.Wait)
}

// Client reads ddownload's pages and sends their forms.
type Client struct {
	hc   *http.Client
	site *url.URL
}

// NewClient reads pages from site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	u, err := url.Parse(strings.TrimSuffix(site, "/"))
	if err != nil {
		panic(err)
	}
	c := *hc
	// A redirect is read rather than followed: the one that leaves the site
	// is the download link.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{hc: &c, site: u}
}

// Session is one visit with its cookies: anonymous for a free download and a
// link check, signed in for a premium login.
type Session struct {
	c   *Client
	hc  *http.Client
	jar *cookiejar.Jar
}

// NewSession starts an anonymous visit.
func (c *Client) NewSession() *Session {
	jar, _ := cookiejar.New(nil)
	hc := *c.hc
	hc.Jar = jar
	s := &Session{c: c, hc: &hc, jar: jar}
	// English pages, which the patterns are written for.
	jar.SetCookies(c.site, []*http.Cookie{{Name: "lang", Value: "english", Path: "/"}})
	return s
}

// Resume picks up a signed-in session from what Cookies wrote.
func (c *Client) Resume(cookies string) *Session {
	s := c.NewSession()
	if list, err := http.ParseCookie(cookies); err == nil {
		for _, ck := range list {
			ck.Path = "/"
		}
		s.jar.SetCookies(c.site, list)
	}
	return s
}

// Cookies writes the session's cookies for Resume.
func (s *Session) Cookies() string {
	var parts []string
	for _, ck := range s.jar.Cookies(s.c.site) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

func (s *Session) signedIn() bool {
	for _, ck := range s.jar.Cookies(s.c.site) {
		if ck.Name == sessionCookie && ck.Value != "" {
			return true
		}
	}
	return false
}

// reply is one answer: a page, or where ddownload sent the request on to.
type reply struct {
	status   int
	body     string
	location *url.URL
}

// onSite reports whether u is one of ddownload's pages rather than a
// download, which the download servers and ddownload itself serve under /d/.
func (s *Session) onSite(u *url.URL) bool {
	return strings.EqualFold(u.Host, s.c.site.Host) && !strings.HasPrefix(u.Path, "/d/")
}

// signInWall reports whether u is the sign-in page, where ddownload sends a
// request that needs a session.
func signInWall(u *url.URL) bool {
	return u.Path == "/login" || u.Query().Get("op") == "login"
}

func (s *Session) do(ctx context.Context, method, target string, fields url.Values, referer string) (reply, error) {
	var body io.Reader
	if fields != nil {
		body = strings.NewReader(fields.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return reply{}, err
	}
	if fields != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	r := reply{status: resp.StatusCode}
	if loc, err := resp.Location(); err == nil && resp.StatusCode/100 == 3 {
		r.location = loc
		return r, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return reply{}, err
	}
	r.body = string(b)
	return r, nil
}

// get reads target, following redirects within the site. A redirect to the
// sign-in page is errSignedOut, one off the site comes back as it is.
func (s *Session) get(ctx context.Context, target, referer string) (reply, error) {
	for range 4 {
		r, err := s.do(ctx, http.MethodGet, target, nil, referer)
		if err != nil || r.location == nil || !s.onSite(r.location) {
			return r, err
		}
		if signInWall(r.location) {
			return r, errSignedOut
		}
		referer, target = target, r.location.String()
	}
	return reply{}, errors.New("ddownload: the site sent the request round in circles")
}

// Page is one read of a file's page, with the session it was read in.
type Page struct {
	Name string
	Size int64
	// Countdown is how long the page wants before its form goes in.
	Countdown time.Duration
	// SiteKey is the Turnstile key of the download form, "" when the form
	// asks for no captcha.
	SiteKey string
	// Direct is the download link ddownload sent the page request on to, as
	// it does for a premium login with direct downloads switched on.
	Direct string
	// URL is the page's address, which the captcha is solved for.
	URL string

	form     *form
	s        *Session
	signedIn bool
}

// Open reads the page of a file link in s. ErrPremiumOnly, ErrTraffic and
// *WaitError come with the page, which still names the file. Every other
// error comes without one.
func (c *Client) Open(ctx context.Context, s *Session, link string) (*Page, error) {
	norm, ok := Normalize(link)
	if !ok {
		return nil, fmt.Errorf("ddownload: %s is not a link to a file", link)
	}
	pageURL := c.site.String() + strings.TrimPrefix(norm, DefaultSite)
	r, err := s.get(ctx, pageURL, "")
	switch {
	case errors.Is(err, errSignedOut) && !s.signedIn():
		return nil, ErrPremiumOnly
	case err != nil:
		return nil, err
	case r.location != nil:
		return &Page{Direct: r.location.String(), URL: pageURL}, nil
	}
	pg := readPage(r.body)
	if pg.gone || r.status == http.StatusNotFound || r.status == http.StatusGone {
		return nil, ErrGone
	}
	if r.status != http.StatusOK {
		return nil, fmt.Errorf("ddownload: the file page answered %d", r.status)
	}
	p := &Page{Name: pg.name, Size: pg.size, Countdown: pg.countdown, SiteKey: pg.siteKey, URL: pageURL, s: s, signedIn: pg.signedIn}
	if err := pg.hold(); err != nil {
		return p, err
	}
	if p.form = pg.formWith("download2"); p.form == nil {
		return p, errors.New("ddownload: the file page has no download form")
	}
	return p, nil
}

// hold is why a page holds the download back, nil when nothing stands in the
// way.
func (p page) hold() error {
	switch {
	case p.gone:
		return ErrGone
	case p.premiumOnly:
		return ErrPremiumOnly
	case p.traffic:
		return ErrTraffic
	case p.wait > 0:
		return &WaitError{Wait: p.wait}
	}
	return nil
}

// Send sends the page's download form and returns the download link it
// answers with. password goes into the form's password field and token, the
// Turnstile answer, where the widget would put it.
func (p *Page) Send(ctx context.Context, password, token string) (string, error) {
	fields := maps.Clone(p.form.fields)
	if p.form.pass != "" {
		if password == "" {
			return "", ErrPassword
		}
		fields.Set(p.form.pass, password)
	}
	if token != "" {
		fields.Set("cf-turnstile-response", token)
		// The field reCAPTCHA would fill, which JDownloader's plugin sends
		// too.
		fields.Set("g-recaptcha-response", token)
	}
	if fields.Has("adblock_detected") {
		fields.Set("adblock_detected", "0")
	}
	action, err := url.Parse(p.URL)
	if err == nil {
		action, err = action.Parse(p.form.action)
	}
	if err != nil {
		return "", fmt.Errorf("ddownload: the download form goes nowhere: %w", err)
	}
	r, err := p.s.do(ctx, http.MethodPost, action.String(), fields, p.URL)
	if err != nil {
		return "", err
	}
	if r.location != nil {
		switch {
		case !p.s.onSite(r.location):
			return r.location.String(), nil
		case signInWall(r.location):
			return "", errSignedOut
		}
		if r, err = p.s.get(ctx, r.location.String(), p.URL); err != nil {
			return "", err
		}
		if r.location != nil {
			return r.location.String(), nil
		}
	}
	if r.status != http.StatusOK {
		return "", fmt.Errorf("ddownload: the download form answered %d", r.status)
	}
	pg := readPage(r.body)
	switch {
	case pg.link != "":
		return pg.link, nil
	case wrongCaptcha.MatchString(r.body):
		return "", ErrCaptchaRefused
	case wrongPassword.MatchString(r.body):
		return "", ErrPasswordRefused
	}
	if err := pg.hold(); err != nil {
		return "", err
	}
	return "", errors.New("ddownload: the answer to the download form held no download link")
}

// SignIn signs in with user and password. solve answers the Turnstile check
// of the sign-in form, given the page it is on and its key.
func (c *Client) SignIn(ctx context.Context, user, password string, solve func(ctx context.Context, pageURL, siteKey string) (string, error)) (*Session, error) {
	s := c.NewSession()
	loginURL := c.site.String() + "/login"
	r, err := s.get(ctx, loginURL, "")
	if err != nil {
		return nil, err
	}
	pg := readPage(r.body)
	f := pg.formWith("login")
	if f == nil {
		if blockedLogin.MatchString(r.body) {
			return nil, ErrSignInBlocked
		}
		return nil, errors.New("ddownload: the sign-in page has no form")
	}
	fields := maps.Clone(f.fields)
	fields.Set("login", user)
	fields.Set("password", password)
	// Left empty, so the answer is the account page rather than a file.
	fields.Set("redirect", "")
	if pg.siteKey != "" {
		token, err := solve(ctx, loginURL, pg.siteKey)
		if err != nil {
			return nil, err
		}
		fields.Set("cf-turnstile-response", token)
		fields.Set("g-recaptcha-response", token)
	}
	action, err := url.Parse(loginURL)
	if err == nil {
		action, err = action.Parse(f.action)
	}
	if err != nil {
		return nil, fmt.Errorf("ddownload: the sign-in form goes nowhere: %w", err)
	}
	if r, err = s.do(ctx, http.MethodPost, action.String(), fields, loginURL); err != nil {
		return nil, err
	}
	switch {
	case s.signedIn():
		return s, nil
	case refusedLogin.MatchString(r.body):
		return nil, ErrLoginRefused
	case blockedLogin.MatchString(r.body):
		return nil, ErrSignInBlocked
	case wrongCaptcha.MatchString(r.body):
		return nil, ErrCaptchaRefused
	}
	return nil, ErrSignInFailed
}

// Account is what the account page says about the plan.
type Account struct {
	// Premium is a paid plan that has not run out.
	Premium bool
	// Expires is when the plan runs out, zero when the page does not say.
	Expires time.Time
	// TrafficLeft and TrafficMax are bytes, 0 when the page states neither.
	// The traffic left can drop below zero.
	TrafficLeft, TrafficMax int64
}

// Account reads the account page of a signed-in session.
func (c *Client) Account(ctx context.Context, s *Session) (Account, error) {
	r, err := s.get(ctx, c.site.String()+"/?op=my_account", "")
	switch {
	case err != nil:
		return Account{}, err
	case r.location != nil || !s.signedIn():
		return Account{}, errSignedOut
	case r.status != http.StatusOK:
		return Account{}, fmt.Errorf("ddownload: the account page answered %d", r.status)
	}
	return readAccount(r.body, time.Now()), nil
}
