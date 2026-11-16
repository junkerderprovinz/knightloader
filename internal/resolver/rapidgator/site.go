package rapidgator

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// The API turns some accounts away that the website signs in, and passes
// over a two-factor code, as JDownloader's plugin found. The website's session
// then stands in for the API's token: its cookies are the token, marked by
// sitePrefix, and the account and the download links are read from its pages.
const sitePrefix = "site:"

// The website's markers, as JDownloader's plugin reads them.
var (
	formPattern    = regexp.MustCompile(`(?is)(<form[^>]*>)(.*?)</form>`)
	inputPattern   = regexp.MustCompile(`(?is)<input[^>]*>`)
	attrName       = regexp.MustCompile(`(?i)\sname="([^"]*)"`)
	attrValue      = regexp.MustCompile(`(?i)\svalue="([^"]*)"`)
	attrAction     = regexp.MustCompile(`(?i)\saction="([^"]*)"`)
	siteCaptcha    = regexp.MustCompile(`"(/auth/captcha/v/[a-z0-9]+)"`)
	siteBadCode    = regexp.MustCompile(`>\s*Invalid auth code`)
	siteBadLogin   = regexp.MustCompile(`>\s*Wrong e-mail or password\.\s*<`)
	siteSignedIn   = regexp.MustCompile(`(?i)auth/logout"`)
	siteFree       = regexp.MustCompile(`(?i)Account\s*:\s*(?:&nbsp;)?\s*<a href="/article/premium">\s*Free\s*</a>`)
	sitePremiumEnd = regexp.MustCompile(`Premium services will end on ([^<>"]*?)\.<br`)
	siteTraffic    = regexp.MustCompile(`(?i)>\s*Bandwith available\s*</td>\s*<td>\s*([\d.,]+)\s*([KMGT]?B)(?:\s*of\s*([\d.,]+)\s*([KMGT]?B))?`)
	premiumLinks   = []*regexp.Regexp{
		regexp.MustCompile(`var premium_download_link\s*=\s*'(https?://[^<>"']+)';`),
		regexp.MustCompile(`'(https?://pr_srv\.rapidgator\.net//\?r=download/index&session_id=[A-Za-z0-9]+)'`),
		regexp.MustCompile(`'(https?://pr\d+\.rapidgator\.net//\?r=download/index&session_id=[A-Za-z0-9]+)'`),
	}
)

// siteLogin signs in on the website with code for a second factor, "" for
// none, and returns its session as a token. solve answers the page's captcha,
// nil none.
func (c *Client) siteLogin(ctx context.Context, email, password, code string, solve hoster.Solve) (string, Account, error) {
	s := c.newSession()
	body, err := s.page(ctx, "/auth/login")
	if err != nil {
		return "", Account{}, err
	}
	action, fields, ok := loginForm(body)
	if !ok {
		return "", Account{}, errors.New("rapidgator: the website's sign-in page holds no sign-in form")
	}
	if m := siteCaptcha.FindStringSubmatch(body); m != nil {
		answer, err := s.solve(ctx, m[1], solve)
		if err != nil {
			return "", Account{}, err
		}
		fields.Set("LoginForm[verifyCode]", answer)
	}
	fields.Set("LoginForm[email]", email)
	fields.Set("LoginForm[password]", password)
	if code != "" {
		fields.Set("LoginForm[twoStepAuthCode]", code)
	}
	body, err = s.post(ctx, action, fields)
	if err != nil {
		return "", Account{}, err
	}
	switch {
	case siteBadCode.MatchString(body) && code != "":
		return "", Account{}, &LoginError{Msg: "rapidgator: Rapidgator turned the two-factor code down, so enter the current one", Err: ownlogin.ErrWrongCode}
	case siteBadCode.MatchString(body):
		return "", Account{}, &LoginError{Msg: "rapidgator: the account signs in with a two-factor code, so enter the one the authenticator app shows", Err: ownlogin.ErrNeedsCode}
	case siteBadLogin.MatchString(body):
		return "", Account{}, &LoginError{Msg: "rapidgator: Rapidgator turned the login down, so check the email address and password"}
	case siteCaptcha.MatchString(body):
		return "", Account{}, &LoginError{Captcha: true, Msg: "rapidgator: Rapidgator's website turned the captcha's answer down"}
	case !siteSignedIn.MatchString(body) && !s.has("user__"):
		log.Printf("rapidgator: the website answered the sign-in with a page that does not sign in (code sent: %v)", code != "")
		return "", Account{}, errSiteRefused
	}
	token := s.token()
	acct, err := c.siteAccount(ctx, token)
	return token, acct, err
}

// errSiteRefused is a sign-in page the website answered with nothing it
// names. The API's refusal of the account then stands.
var errSiteRefused = errors.New("rapidgator: the website did not sign the login in")

// loginForm finds the sign-in form on body: where it posts to and the fields
// it already carries, such as its token against forged requests.
func loginForm(body string) (string, url.Values, bool) {
	for _, m := range formPattern.FindAllStringSubmatch(body, -1) {
		tag, inner := m[1], m[2]
		action := ""
		if a := attrAction.FindStringSubmatch(tag); a != nil {
			action = a[1]
		}
		if !strings.Contains(tag, `id="login"`) && !strings.HasSuffix(action, "/auth/login") {
			continue
		}
		fields := url.Values{}
		for _, input := range inputPattern.FindAllString(inner, -1) {
			if n := attrName.FindStringSubmatch(input); n != nil {
				v := ""
				if m := attrValue.FindStringSubmatch(input); m != nil {
					v = m[1]
				}
				fields.Set(n[1], v)
			}
		}
		if u, err := url.Parse(action); err == nil && action != "" {
			action = u.Path
		} else {
			action = "/auth/login"
		}
		return action, fields, true
	}
	return "", nil, false
}

// siteAccount reads the account behind a website session from its profile
// page, which also proves the session still holds.
func (c *Client) siteAccount(ctx context.Context, token string) (Account, error) {
	s, err := c.sessionOf(token)
	if err != nil {
		return Account{}, err
	}
	body, err := s.page(ctx, "/profile/index")
	if err != nil {
		return Account{}, err
	}
	if !siteSignedIn.MatchString(body) {
		return Account{}, errSession
	}
	a := Account{Premium: !siteFree.MatchString(body)}
	if m := sitePremiumEnd.FindStringSubmatch(body); m != nil {
		if end, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(m[1])+" 23:59:59"); err == nil {
			a.Expiry = end
		}
	}
	if m := siteTraffic.FindStringSubmatch(body); m != nil && m[3] != "" {
		a.TrafficLeft, a.TrafficMax = parseSize(m[1], m[2]), parseSize(m[3], m[4])
	}
	return a, nil
}

// siteLink reads the premium download link of the file id from its page.
func (c *Client) siteLink(ctx context.Context, token, id string) (string, error) {
	s, err := c.sessionOf(token)
	if err != nil {
		return "", err
	}
	body, err := s.page(ctx, "/file/"+id)
	if err != nil {
		return "", err
	}
	for _, p := range premiumLinks {
		if m := p.FindStringSubmatch(body); m != nil {
			return m[1], nil
		}
	}
	if !siteSignedIn.MatchString(body) {
		return "", errSession
	}
	return "", errors.New("rapidgator: the file page of the website session holds no premium download link")
}

// solve has the captcha whose picture is at path answered. Without anybody
// to answer it, posting the form would only count as one more failed
// sign-in.
func (s *session) solve(ctx context.Context, path string, solve hoster.Solve) (string, error) {
	refused := &LoginError{Captcha: true, Msg: "rapidgator: Rapidgator's website wants a captcha solved before it takes the login"}
	if solve == nil {
		return "", refused
	}
	img, err := s.page(ctx, path)
	if err != nil {
		return "", err
	}
	answer, err := solve(ctx, "", hoster.Captcha{Kind: hoster.Image, Image: []byte(img)})
	if err != nil || answer == "" {
		return "", refused
	}
	return answer, nil
}

// has reports whether the session holds the cookie name.
func (s *session) has(name string) bool {
	site, _ := url.Parse(s.c.site)
	for _, c := range s.hc.Jar.Cookies(site) {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (c *Client) newSession() *session {
	jar, _ := cookiejar.New(nil)
	s := &session{c: c, hc: *c.hc}
	s.hc.Jar = jar
	return s
}

// sessionOf is a session carrying the cookies of token.
func (c *Client) sessionOf(token string) (*session, error) {
	cookies, err := url.ParseQuery(strings.TrimPrefix(token, sitePrefix))
	if err != nil {
		return nil, errSession
	}
	s := c.newSession()
	site, err := url.Parse(c.site)
	if err != nil {
		return nil, err
	}
	var jar []*http.Cookie
	for name, v := range cookies {
		jar = append(jar, &http.Cookie{Name: name, Value: v[0], Path: "/"})
	}
	s.hc.Jar.SetCookies(site, jar)
	return s, nil
}

// token is the session's cookies as a token.
func (s *session) token() string {
	site, _ := url.Parse(s.c.site)
	v := url.Values{}
	for _, c := range s.hc.Jar.Cookies(site) {
		v.Set(c.Name, c.Value)
	}
	return sitePrefix + v.Encode()
}

func isSiteToken(token string) bool { return strings.HasPrefix(token, sitePrefix) }
