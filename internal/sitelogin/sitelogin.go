// Package sitelogin knows the sites that yt-dlp loads only for a signed-in
// session and that cannot be signed into with a user name and password: a
// captcha or a mailed code stands in the way. The session comes from the
// user's own browser instead, read from its cookie store on this machine or
// handed over by the extension. Store keeps a handed-over one; JarFromCookies
// builds one from what a browser holds, and Check asks it who it signs in.
package sitelogin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Site is one site signed into through the browser. A new one is an entry in
// Sites and a logo the page already finds for its Host.
type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Host is the site's own name, which the page draws its logo for.
	Host     string `json:"host"`
	LoginURL string `json:"loginUrl"`
	// Domains are the cookie domains the extension reads for the site. A
	// cookie of any other domain is dropped from the jar.
	Domains []string `json:"domains"`
	// Links are further hosts whose links play the site's media, such as a
	// short-link domain no cookie is ever set for.
	Links []string `json:"links,omitempty"`
	// Use is when the session goes along with a link.
	Use Use `json:"use"`

	// signIn are the cookies only a signed-in session carries.
	signIn []string
	// checkURL, when set, answers who the session belongs to, and account
	// reads the answer.
	checkURL string
	account  func(io.Reader) (string, State)
}

// Use is when a site's session goes to yt-dlp.
type Use string

const (
	// Always sends the session with every link, for a site that lists and
	// plays nothing without one.
	Always Use = "always"
	// OnNeed keeps the session back until a link fails for want of a
	// sign-in, for a site that flags accounts which download a lot while
	// signed in.
	OnNeed Use = "onNeed"
)

// Sites is every site offered, in the order the page shows them. The cookie
// names are the ones yt-dlp's extractor for the site takes a sign-in from.
var Sites = []Site{
	{
		ID:       "facebook",
		Name:     "Facebook",
		Host:     "facebook.com",
		LoginURL: "https://www.facebook.com/login/",
		Domains:  []string{"facebook.com"},
		Links:    []string{"fb.watch"},
		Use:      OnNeed,
		signIn:   []string{"c_user", "xs"},
	},
	{
		ID:       "instagram",
		Name:     "Instagram",
		Host:     "instagram.com",
		LoginURL: "https://www.instagram.com/accounts/login/",
		Domains:  []string{"instagram.com"},
		Links:    []string{"instagr.am"},
		Use:      OnNeed,
		signIn:   []string{"sessionid"},
	},
	{
		ID:       "patreon",
		Name:     "Patreon",
		Host:     "patreon.com",
		LoginURL: "https://www.patreon.com/login",
		Domains:  []string{"patreon.com"},
		Use:      Always,
		signIn:   []string{"session_id"},
		checkURL: "https://www.patreon.com/api/current_user",
		account:  patreonAccount,
	},
	{
		ID:       "reddit",
		Name:     "Reddit",
		Host:     "reddit.com",
		LoginURL: "https://www.reddit.com/login/",
		Domains:  []string{"reddit.com"},
		Links:    []string{"redd.it", "v.redd.it"},
		Use:      OnNeed,
		signIn:   []string{"reddit_session"},
	},
	{
		ID:       "tiktok",
		Name:     "TikTok",
		Host:     "tiktok.com",
		LoginURL: "https://www.tiktok.com/login",
		Domains:  []string{"tiktok.com"},
		Links:    []string{"www.tiktokv.com"},
		Use:      OnNeed,
		signIn:   []string{"sessionid", "sessionid_ss", "sid_tt"},
		checkURL: "https://www.tiktok.com/passport/web/account/info/",
		account:  tiktokAccount,
	},
	{
		ID:       "twitch",
		Name:     "Twitch",
		Host:     "twitch.tv",
		LoginURL: "https://www.twitch.tv/login",
		Domains:  []string{"twitch.tv"},
		Use:      OnNeed,
		signIn:   []string{"auth-token"},
	},
	{
		ID:       "vimeo",
		Name:     "Vimeo",
		Host:     "vimeo.com",
		LoginURL: "https://vimeo.com/log_in",
		Domains:  []string{"vimeo.com"},
		Use:      OnNeed,
		signIn:   []string{"vimeo"},
		checkURL: "https://vimeo.com/_next/viewer",
		account:  vimeoAccount,
	},
	{
		// yt-dlp asks api.x.com, so only a session on x.com signs it in, while
		// twitter.com still holds the cookies of an older sign-in.
		ID:       "x",
		Name:     "X",
		Host:     "x.com",
		LoginURL: "https://x.com/i/flow/login",
		Domains:  []string{"x.com", "twitter.com"},
		Use:      OnNeed,
		signIn:   []string{"auth_token"},
	},
	{
		// YouTube has no endpoint that names the account and is meant to be
		// asked, so its sign-in cookies' expiry is what is known.
		ID:       "youtube",
		Name:     "YouTube",
		Host:     "youtube.com",
		LoginURL: "https://accounts.google.com/ServiceLogin?service=youtube&continue=https%3A%2F%2Fwww.youtube.com%2F",
		Domains:  []string{"youtube.com"},
		Links:    []string{"youtu.be"},
		Use:      OnNeed,
		signIn:   []string{"SID", "__Secure-1PSID", "__Secure-3PSID", "LOGIN_INFO"},
	},
}

// Lookup returns the site with the given id.
func Lookup(id string) (Site, bool) {
	i := slices.IndexFunc(Sites, func(s Site) bool { return s.ID == id })
	if i < 0 {
		return Site{}, false
	}
	return Sites[i], true
}

// ForURL returns the site a link belongs to, by its host or a parent domain.
func ForURL(rawurl string) (Site, bool) {
	u, err := url.Parse(rawurl)
	if err != nil || u.Hostname() == "" {
		return Site{}, false
	}
	host := strings.ToLower(u.Hostname())
	for _, s := range Sites {
		if s.Owns(host) || slices.Contains(s.Links, host) {
			return s, true
		}
	}
	return Site{}, false
}

// SignInCookie reports whether a cookie of this name is one only a signed-in
// session carries.
func (s Site) SignInCookie(name string) bool { return slices.Contains(s.signIn, name) }

// SignInCookies lists the cookies only a signed-in session carries, which the
// extension watches to tell a sign-in or a sign-out from other cookies.
func (s Site) SignInCookies() []string { return slices.Clone(s.signIn) }

// Owns reports whether a cookie or link domain is one of the site's or below.
func (s Site) Owns(domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	return slices.ContainsFunc(s.Domains, func(d string) bool { return domain == d || strings.HasSuffix(domain, "."+d) })
}

// BrowserCookie is a cookie as the extension reads it from chrome.cookies.
type BrowserCookie struct {
	Domain   string  `json:"domain"`
	HostOnly bool    `json:"hostOnly"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	Session  bool    `json:"session"`
	Expires  float64 `json:"expirationDate"`
	Name     string  `json:"name"`
	Value    string  `json:"value"`
}

const jarHeader = "# Netscape HTTP Cookie File"

// sessionCookieLife is the expiry a cookie that lives as long as the browser
// is given in the jar: an entry without one counts as a session cookie, which
// some loaders skip.
const sessionCookieLife = 30 * 24 * time.Hour

// JarFromCookies writes the site's live cookies as a Netscape cookies.txt,
// or "" when none is left.
func JarFromCookies(site Site, cookies []BrowserCookie, now time.Time) string {
	var lines []string
	for _, c := range cookies {
		if !site.Owns(c.Domain) || c.Name == "" || strings.ContainsAny(c.Name+c.Value+c.Domain+c.Path, "\t\r\n") {
			continue
		}
		expires := int64(c.Expires)
		if c.Session || expires == 0 {
			expires = now.Add(sessionCookieLife).Unix()
		} else if expires <= now.Unix() {
			continue
		}
		domain, sub := strings.ToLower(c.Domain), "FALSE"
		if !c.HostOnly {
			domain, sub = "."+strings.TrimPrefix(domain, "."), "TRUE"
		}
		path, secure := c.Path, "FALSE"
		if path == "" {
			path = "/"
		}
		if c.Secure {
			secure = "TRUE"
		}
		lines = append(lines, strings.Join([]string{domain, sub, path, secure, strconv.FormatInt(expires, 10), c.Name, c.Value}, "\t"))
	}
	if len(lines) == 0 {
		return ""
	}
	return jarHeader + "\n" + strings.Join(lines, "\n") + "\n"
}

// jarCookie is one line of a cookies.txt. A zero Expires lives as long as the
// browser does.
type jarCookie struct {
	Domain  string
	Path    string
	Expires time.Time
	Name    string
	Value   string
}

func (c jarCookie) live(now time.Time) bool { return c.Expires.IsZero() || c.Expires.After(now) }

// parseJar reads a Netscape cookies.txt, skipping what it cannot read.
func parseJar(text string) []jarCookie {
	var out []jarCookie
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimPrefix(line, "#HttpOnly_")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		c := jarCookie{Domain: strings.ToLower(f[0]), Path: f[2], Name: f[5], Value: f[6]}
		if n, err := strconv.ParseInt(f[4], 10, 64); err == nil && n > 0 {
			c.Expires = time.Unix(n, 0).UTC()
		}
		out = append(out, c)
	}
	return out
}

// State is how a site's stored session stands.
type State string

const (
	SignedOut State = "signedOut"
	SignedIn  State = "signedIn"
	Expired   State = "expired"
	// Unchecked is a session the site did not answer about. It is used as it
	// is, since a site being down says nothing about the sign-in.
	Unchecked State = "unchecked"
)

// Status is what is known of a site's session. Expires is RFC 3339, or empty
// where the cookies do not say.
type Status struct {
	State   State  `json:"state"`
	Account string `json:"account,omitempty"`
	Expires string `json:"expires,omitempty"`
}

// Check works out the state of a stored jar: from its sign-in cookies, and
// where the site names the account, from asking it with the session.
func Check(ctx context.Context, client *http.Client, site Site, jar string, now time.Time) Status {
	if strings.TrimSpace(jar) == "" {
		return Status{State: SignedOut}
	}
	cookies := parseJar(jar)
	var ends time.Time
	signedIn := false
	for _, c := range cookies {
		if !slices.Contains(site.signIn, c.Name) || !c.live(now) {
			continue
		}
		signedIn = true
		if !c.Expires.IsZero() && (ends.IsZero() || c.Expires.Before(ends)) {
			ends = c.Expires
		}
	}
	if !signedIn {
		return Status{State: Expired}
	}
	st := Status{State: SignedIn}
	if !ends.IsZero() {
		st.Expires = ends.Format(time.RFC3339)
	}
	if site.checkURL == "" {
		return st
	}
	name, state := ask(ctx, client, site, cookies, now)
	st.State, st.Account = state, name
	// The cookies' end says nothing once the site has turned the session down.
	if state == Expired {
		st.Expires = ""
	}
	return st
}

// ask fetches the site's account document with its live cookies and reads
// the account's name from it.
func ask(ctx context.Context, client *http.Client, site Site, cookies []jarCookie, now time.Time) (string, State) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site.checkURL, nil)
	if err != nil {
		return "", Unchecked
	}
	req.Header.Set("Accept", "application/json")
	if header := cookieHeader(site, cookies, now); header != "" {
		req.Header.Set("Cookie", header)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", Unchecked
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", Expired
	case resp.StatusCode != http.StatusOK:
		return "", Unchecked
	}
	return site.account(io.LimitReader(resp.Body, 1<<20))
}

// firstName is the first of names that is not blank.
func firstName(names ...string) string {
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return ""
}

// patreonAccount reads Patreon's current_user, which a session it turned
// down gets a 401 for.
func patreonAccount(body io.Reader) (string, State) {
	var doc struct {
		Data struct {
			Attributes struct {
				FullName  string `json:"full_name"`
				FirstName string `json:"first_name"`
				Vanity    string `json:"vanity"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return "", Unchecked
	}
	a := doc.Data.Attributes
	return firstName(a.FullName, a.FirstName, a.Vanity), SignedIn
}

// tiktokSessionExpired is the error_code TikTok's account info answers a
// session it does not take with, still with a 200.
const tiktokSessionExpired = 13

// tiktokAccount reads TikTok's account info.
func tiktokAccount(body io.Reader) (string, State) {
	var doc struct {
		Message string `json:"message"`
		Data    struct {
			ScreenName string `json:"screen_name"`
			Username   string `json:"username"`
			Name       string `json:"name"`
			ErrorCode  int    `json:"error_code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return "", Unchecked
	}
	switch {
	case doc.Message == "success":
		return firstName(doc.Data.ScreenName, doc.Data.Username), SignedIn
	case doc.Data.ErrorCode == tiktokSessionExpired || doc.Data.Name == "session_expired":
		return "", Expired
	}
	return "", Unchecked
}

// vimeoAccount reads the viewer Vimeo's own pages start from, whose user is
// null for a visitor who is not signed in.
func vimeoAccount(body io.Reader) (string, State) {
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return "", Unchecked
	}
	raw, ok := doc["user"]
	if !ok {
		return "", Unchecked
	}
	var user *struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &user); err != nil {
		return "", Unchecked
	}
	if user == nil {
		return "", Expired
	}
	return firstName(user.Name), SignedIn
}

// cookieHeader is the Cookie header that carries the site's live cookies.
func cookieHeader(site Site, cookies []jarCookie, now time.Time) string {
	var pairs []string
	for _, c := range cookies {
		if site.Owns(c.Domain) && c.live(now) {
			pairs = append(pairs, c.Name+"="+c.Value)
		}
	}
	return strings.Join(pairs, "; ")
}
