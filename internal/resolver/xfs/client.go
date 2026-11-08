package xfs

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

// maxPage caps how much of a page is read. A file page is a few hundred
// kilobytes, most of it styles and scripts.
const maxPage = 4 << 20

// maxImage caps the picture of an image captcha.
const maxImage = 1 << 20

// sessionCookie is the cookie a signed-in session carries.
const sessionCookie = "xfss"

// The reasons a download or a sign-in cannot go ahead, whatever the wait.
var (
	ErrGone            = errors.New("the file has been deleted or never existed")
	ErrPremiumOnly     = errors.New("the owner keeps this file for premium accounts")
	ErrPassword        = errors.New("the file is protected by a password, and the download has no download password")
	ErrPasswordRefused = errors.New("the download password was not accepted")
	ErrCaptchaRefused  = errors.New("the answer to the captcha was not accepted")
	ErrNoAnswer        = errors.New("nobody answered the captcha")
	ErrTraffic         = errors.New("the premium traffic for today is used up")
	ErrPremiumExpired  = errors.New("the premium of the login has run out")
	ErrLoginRefused    = errors.New("the username or password of the login was not accepted")
	ErrSignInBlocked   = errors.New("the site takes no sign-ins from this address")
	ErrSignInFailed    = errors.New("the site did not sign the login in")
	ErrNoLogin         = errors.New("there is no login to sign in with")

	// errSignedOut is a session the site has ended, which a new sign-in
	// mends.
	errSignedOut = errors.New("the session has ended")
)

// WaitError is the site asking for a pause before the free download.
type WaitError struct {
	Wait time.Duration
}

func (e *WaitError) Error() string {
	return fmt.Sprintf("this address has to wait %s before its next free download", e.Wait)
}

// Client reads a site's pages and sends their forms.
type Client struct {
	site *Site
	pat  Patterns
	hc   *http.Client
	base *url.URL
}

// NewClient reads site's pages from base, "https://" + site.Host outside
// tests, through hc.
func NewClient(site *Site, base string, hc *http.Client) *Client {
	u, err := url.Parse(strings.TrimSuffix(base, "/"))
	if err != nil {
		panic(err)
	}
	c := *hc
	// A redirect is read rather than followed: the one that leaves the site
	// is the download link.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{site: site, pat: site.Patterns.with(Classic), hc: &c, base: u}
}

// Site is the hoster the client reads.
func (c *Client) Site() *Site { return c.site }

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
	cookies := []*http.Cookie{{Name: "lang", Value: "english", Path: "/"}}
	for name, value := range c.site.Cookies {
		cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	jar.SetCookies(c.base, cookies)
	return s
}

// Resume picks up a signed-in session from what Cookies wrote.
func (c *Client) Resume(cookies string) *Session {
	s := c.NewSession()
	if list, err := http.ParseCookie(cookies); err == nil {
		for _, ck := range list {
			ck.Path = "/"
		}
		s.jar.SetCookies(c.base, list)
	}
	return s
}

// Cookies writes the session's cookies for Resume.
func (s *Session) Cookies() string {
	var parts []string
	for _, ck := range s.jar.Cookies(s.c.base) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

func (s *Session) signedIn() bool {
	for _, ck := range s.jar.Cookies(s.c.base) {
		if ck.Name == sessionCookie && ck.Value != "" {
			return true
		}
	}
	return false
}

// reply is one answer: a page, or where the site sent the request on to.
type reply struct {
	status   int
	body     string
	location *url.URL
}

// onSite reports whether u is one of the site's pages rather than a
// download, which the download servers and some sites themselves serve.
func (s *Session) onSite(u *url.URL) bool {
	if !strings.EqualFold(u.Host, s.c.base.Host) {
		return false
	}
	return first(s.c.pat.Link, `"`+u.String()+`"`) == nil
}

// signInWall reports whether u is the sign-in or sign-up page, where a site
// sends a request that needs a session.
func signInWall(u *url.URL) bool {
	op := u.Query().Get("op")
	return u.Path == "/login" || u.Path == "/login.html" || op == "login" || op == "registration"
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
	return reply{}, errors.New("the site sent the request round in circles")
}

// Page is one step of a download: a page with the form that takes it on, or
// the download link the site handed out.
type Page struct {
	Name string
	Size int64
	// Countdown is how long the page wants before its form goes in.
	Countdown time.Duration
	// Captcha is the check the form asks for, nil for none.
	Captcha *Captcha
	// Direct is the download link, set once the site has handed it out.
	Direct string
	// URL is the page's address.
	URL string

	form     *Form
	body     string
	s        *Session
	signedIn bool
}

// free reports whether the page is a free download's, with its countdown or
// its captcha.
func (p *Page) free() bool {
	return p.Countdown > 0 || p.Captcha != nil && !p.s.c.site.PremiumCaptcha
}

// Open reads the page of a file link in s. ErrPremiumOnly, ErrTraffic and
// *WaitError come with the page, which still names the file. Every other
// error comes without one.
func (c *Client) Open(ctx context.Context, s *Session, link string) (*Page, error) {
	norm, ok := c.site.Normalize(link)
	if !ok {
		return nil, fmt.Errorf("%s is not a link to a file", link)
	}
	pageURL := c.base.String() + strings.TrimPrefix(norm, "https://"+c.site.Host)
	r, err := s.get(ctx, pageURL, "")
	switch {
	case errors.Is(err, errSignedOut) && !s.signedIn():
		return nil, ErrPremiumOnly
	case err != nil:
		return nil, err
	case r.location != nil:
		return &Page{Direct: r.location.String(), URL: pageURL}, nil
	case r.status == http.StatusNotFound || r.status == http.StatusGone:
		return nil, ErrGone
	}
	pg := readPage(r.body, c.pat)
	if pg.gone {
		return nil, ErrGone
	}
	if r.status != http.StatusOK {
		return nil, fmt.Errorf("the file page answered %d", r.status)
	}
	p, err := c.step(ctx, s, pg, r.body, pageURL)
	if err != nil {
		return p, err
	}
	if p.form == nil && p.Direct == "" {
		return p, errors.New("the file page has no download form")
	}
	return p, nil
}

// step makes a Page of a page read in s, with the form a download in s
// sends: a premium one on a signed-in session. Its error is why the page
// holds the download back, and the page comes with it.
func (c *Client) step(ctx context.Context, s *Session, pg page, body, pageURL string) (*Page, error) {
	p := &Page{Name: pg.name, Size: pg.size, Countdown: pg.countdown, URL: pageURL, body: body, s: s, signedIn: pg.signedIn}
	if err := pg.hold(s.signedIn()); err != nil {
		return p, err
	}
	if pg.link != "" {
		p.Direct = pg.link
		return p, nil
	}
	if p.form = pg.downloadForm(s.signedIn()); p.form == nil {
		return p, nil
	}
	f := *p.form
	f.Fields = maps.Clone(f.Fields)
	p.form = &f
	p.Captcha = captchaOf(p.form, pageURL)
	if err := s.picture(ctx, p.Captcha); err != nil {
		return nil, err
	}
	return p, nil
}

// picture reads the picture of an image captcha into it. Any other check
// has none.
func (s *Session) picture(ctx context.Context, c *Captcha) error {
	if c == nil || c.Kind != Image {
		return nil
	}
	u, err := url.Parse(c.PageURL)
	if err == nil {
		u, err = u.Parse(c.imageURL)
	}
	if err != nil {
		return fmt.Errorf("the captcha's picture is nowhere: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", c.PageURL)
	resp, err := s.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the captcha's picture answered %d", resp.StatusCode)
	}
	c.Image, err = io.ReadAll(io.LimitReader(resp.Body, maxImage))
	return err
}

// hold is why a page holds the download back, nil when nothing stands in the
// way. Used-up traffic only counts for a signed-in session: an anonymous
// visitor has no traffic of its own.
func (p page) hold(signedIn bool) error {
	switch {
	case p.gone:
		return ErrGone
	case p.premiumOnly:
		return ErrPremiumOnly
	case p.traffic && signedIn:
		return ErrTraffic
	case p.wait > 0:
		return &WaitError{Wait: p.wait}
	}
	return nil
}

// Input is what goes into a form beside the fields it carries.
type Input struct {
	// Password is the download password, for a form with a password field.
	Password string
	// Answer is the answer to the form's captcha.
	Answer string
}

// Send sends the page's form and returns the next step: the page it answers
// with, or one whose Direct is the download link.
func (p *Page) Send(ctx context.Context, in Input) (*Page, error) {
	f := Form{Op: p.form.Op, Action: p.form.Action, Fields: maps.Clone(p.form.Fields), Password: p.form.Password}
	if f.Password != "" {
		if in.Password == "" {
			return nil, ErrPassword
		}
		f.Fields.Set(f.Password, in.Password)
	}
	if p.Captcha != nil {
		answer(f.Fields, p.Captcha.Kind, in.Answer)
	}
	if f.Fields.Has("adblock_detected") {
		f.Fields.Set("adblock_detected", "0")
	}
	if f.Op == "download1" {
		press(&f, p.form.buttons, p.s.signedIn())
	}
	if adjust := p.s.c.site.Adjust; adjust != nil {
		adjust(&f, p.body)
	}
	action, err := url.Parse(p.URL)
	if err == nil {
		action, err = action.Parse(f.Action)
	}
	if err != nil {
		return nil, fmt.Errorf("the download form goes nowhere: %w", err)
	}
	r, err := p.s.do(ctx, http.MethodPost, action.String(), f.Fields, p.URL)
	if err != nil {
		return nil, err
	}
	at := action.String()
	if r.location != nil {
		switch {
		case !p.s.onSite(r.location):
			return &Page{Direct: r.location.String(), URL: p.URL}, nil
		case signInWall(r.location):
			return nil, errSignedOut
		}
		at = r.location.String()
		if r, err = p.s.get(ctx, at, p.URL); err != nil {
			return nil, err
		}
		if r.location != nil {
			return &Page{Direct: r.location.String(), URL: p.URL}, nil
		}
	}
	if r.status != http.StatusOK {
		return nil, fmt.Errorf("the download form answered %d", r.status)
	}
	pat := p.s.c.pat
	pg := readPage(r.body, pat)
	switch {
	case pg.link != "":
		return &Page{Direct: pg.link, URL: p.URL}, nil
	case pat.WrongCaptcha.MatchString(r.body):
		return nil, ErrCaptchaRefused
	case pat.WrongPassword.MatchString(r.body):
		return nil, ErrPasswordRefused
	}
	next, err := p.s.c.step(ctx, p.s, pg, r.body, at)
	switch {
	case err != nil:
		return nil, err
	case next.form == nil && next.Direct == "":
		return nil, errors.New("the answer to the download form held no download link")
	}
	return next, nil
}

// answer puts a captcha's answer where the widget or the picture's field
// would.
func answer(fields url.Values, kind CaptchaKind, token string) {
	switch kind {
	case Turnstile:
		fields.Set("cf-turnstile-response", token)
		// The field reCAPTCHA would fill, which JDownloader's plugins send
		// too.
		fields.Set("g-recaptcha-response", token)
	case Recaptcha:
		fields.Set("g-recaptcha-response", token)
	case HCaptcha:
		fields.Set("h-captcha-response", token)
		fields.Set("g-recaptcha-response", token)
	case Image:
		fields.Set("code", token)
	}
}

// press sends the first step's form as its free or premium button would,
// the label the page gives the button or the script's own.
func press(f *Form, buttons url.Values, premium bool) {
	pressed, other := buttonFor(premium), buttonFor(!premium)
	f.Fields.Del(other)
	label := buttons.Get(pressed)
	if label == "" {
		label = f.Fields.Get(pressed)
	}
	switch {
	case label != "":
	case premium:
		label = "Premium Download"
	default:
		label = "Free Download"
	}
	f.Fields.Set(pressed, label)
}

// SignIn signs in with user and password. solve answers the captcha of the
// sign-in form.
func (c *Client) SignIn(ctx context.Context, user, password string, solve func(context.Context, Captcha) (string, error)) (*Session, error) {
	s := c.NewSession()
	loginURL := c.base.String() + c.site.loginPath()
	r, err := s.get(ctx, loginURL, "")
	if err != nil {
		return nil, err
	}
	pg := readPage(r.body, c.pat)
	f := pg.formWith("login")
	if f == nil {
		if c.pat.BlockedLogin.MatchString(r.body) {
			return nil, ErrSignInBlocked
		}
		return nil, errors.New("the sign-in page has no form")
	}
	fields := maps.Clone(f.Fields)
	// The form goes in by its button, as a browser sends it.
	for name, values := range f.buttons {
		fields[name] = values
	}
	fields.Set("login", user)
	fields.Set("password", password)
	// Left empty, so the answer is the account page rather than a file.
	fields.Set("redirect", "")
	if ch := captchaOf(&Form{Fields: fields, inner: f.inner}, loginURL); ch != nil {
		if err := s.picture(ctx, ch); err != nil {
			return nil, err
		}
		token, err := solve(ctx, *ch)
		if err != nil {
			return nil, err
		}
		answer(fields, ch.Kind, token)
	}
	action, err := url.Parse(loginURL)
	if err == nil {
		action, err = action.Parse(f.Action)
	}
	if err != nil {
		return nil, fmt.Errorf("the sign-in form goes nowhere: %w", err)
	}
	if r, err = s.do(ctx, http.MethodPost, action.String(), fields, loginURL); err != nil {
		return nil, err
	}
	switch {
	case s.signedIn():
		return s, nil
	case c.pat.RefusedLogin.MatchString(r.body):
		return nil, ErrLoginRefused
	case c.pat.BlockedLogin.MatchString(r.body):
		return nil, ErrSignInBlocked
	case c.pat.WrongCaptcha.MatchString(r.body):
		return nil, ErrCaptchaRefused
	}
	return nil, ErrSignInFailed
}

// Account reads the account page of a signed-in session.
func (c *Client) Account(ctx context.Context, s *Session) (Account, error) {
	r, err := s.get(ctx, c.base.String()+"/?op=my_account", "")
	switch {
	case err != nil:
		return Account{}, err
	case r.location != nil || !s.signedIn():
		return Account{}, errSignedOut
	case r.status != http.StatusOK:
		return Account{}, fmt.Errorf("the account page answered %d", r.status)
	}
	return c.site.readAccount(r.body, time.Now()), nil
}
