package app

// Site sessions read straight from the browsers on this machine
// (internal/browsercookies): signing in to a site in the browser signs this
// program in too, also when it happened long before. A session found this way
// is never stored. It is read again when it is needed, so signing out in the
// browser takes effect, and the extension's hand-over stands in only where no
// browser here has one, as in a container.

import (
	"crypto/sha256"
	"log"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/browsercookies"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// A read of the browsers is reused for a while, since the accounts page polls
// and every download of a site asks. While a Sign in waits they are read
// sooner, so the page notices the sign-in within seconds.
const (
	browserScanFresh = time.Minute
	browserWaitFresh = 3 * time.Second
	browserScanEvery = 10 * time.Minute
)

// Where a site's session comes from, as SiteLoginState.Source says.
const (
	SourceBrowser   = "browser"
	SourceExtension = "extension"
)

// pastedJars is where the credential store files cookies.txt files pasted for
// yt-dlp. Nothing reads them, so a start deletes them.
const pastedJars = "ytdlpcookies"

// browserReader reads the domains' cookies from the browsers on this machine.
type browserReader func(domains []string) ([]browsercookies.Found, []browsercookies.Unreadable)

// browserScan is what the browsers held for a site at one read.
type browserScan struct {
	at time.Time
	// jar is the session to use, "" when no browser has one the site takes.
	jar string
	// from names the browser and profile of jar, or without one, of the
	// session the site turned down.
	from       string
	status     sitelogin.Status
	unreadable []string
}

// StartBrowserSessions reads the sites' sessions from the browsers on this
// machine from now on: at once, every few minutes, and whenever one is asked
// for and the last read is old. A container has no browser of its own, so
// there this does nothing.
func (a *App) StartBrowserSessions() {
	if buildinfo.InContainer() {
		return
	}
	st := a.siteLogins()
	st.mu.Lock()
	st.read = browsercookies.Read
	st.mu.Unlock()
	go func() {
		tick := time.NewTicker(browserScanEvery)
		defer tick.Stop()
		for {
			for _, site := range sitelogin.Sites {
				a.browserSession(site, 0)
			}
			select {
			case <-a.ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

// browserSession is the site's session in the browsers, read again when the
// last read is older than maxAge. A maxAge of 0 also asks the site again.
func (a *App) browserSession(site sitelogin.Site, maxAge time.Duration) browserScan {
	st := a.siteLogins()
	st.mu.Lock()
	read, lock := st.read, st.scanning[site.ID]
	st.mu.Unlock()
	if read == nil {
		return browserScan{}
	}
	// One read per site at a time: the accounts page and a download asking
	// together take the same answer.
	lock.Lock()
	defer lock.Unlock()
	st.mu.Lock()
	last, ok := st.scans[site.ID]
	st.mu.Unlock()
	if ok && maxAge > 0 && time.Since(last.at) < maxAge {
		return last
	}
	scan := a.scanBrowsers(site, read, maxAge == 0)
	st.mu.Lock()
	st.scans[site.ID] = scan
	st.mu.Unlock()
	return scan
}

type browserCandidate struct {
	from string
	jar  string
	used time.Time
}

// scanBrowsers takes the freshest session the site signs somebody in with.
func (a *App) scanBrowsers(site sitelogin.Site, read browserReader, fresh bool) browserScan {
	now := time.Now()
	found, unreadable := read(site.Domains)
	scan := browserScan{at: now, unreadable: a.unreadableBrowsers(unreadable)}
	var candidates []browserCandidate
	for _, f := range found {
		if !slices.ContainsFunc(f.Cookies, func(c browsercookies.Cookie) bool { return site.SignInCookie(c.Name) && live(c, now) }) {
			continue
		}
		if jar := sitelogin.JarFromCookies(site, asBrowserCookies(f.Cookies), now); jar != "" {
			candidates = append(candidates, browserCandidate{from: f.Profile.String(), jar: jar, used: lastUsed(f.Cookies)})
		}
	}
	slices.SortStableFunc(candidates, func(x, y browserCandidate) int { return y.used.Compare(x.used) })
	for i, c := range candidates {
		status := a.browserVerdict(site, c.jar, fresh)
		if status.State == sitelogin.SignedIn || status.State == sitelogin.Unchecked {
			scan.jar, scan.from, scan.status = c.jar, c.from, status
			return scan
		}
		if i == 0 {
			scan.from, scan.status = c.from, status
		}
	}
	return scan
}

// browserVerdict asks the site about a session read from a browser, reusing
// the answer for the same cookies as siteLoginState does for a stored one.
func (a *App) browserVerdict(site sitelogin.Site, jar string, fresh bool) sitelogin.Status {
	st := a.siteLogins()
	key := sha256.Sum256([]byte(site.ID + "\x00" + jar))
	now := time.Now()
	st.mu.Lock()
	last, ok := st.verdicts[key]
	st.mu.Unlock()
	keep := siteCheckFresh
	if last.status.State == sitelogin.Unchecked {
		keep = siteUncheckedFresh
	}
	if ok && !fresh && now.Sub(last.at) < keep {
		return last.status
	}
	status := a.askSite(site, jar)
	st.mu.Lock()
	// A session changes with every sign-in, so old answers are dropped
	// rather than collected.
	for k, v := range st.verdicts {
		if now.Sub(v.at) > siteCheckFresh {
			delete(st.verdicts, k)
		}
	}
	st.verdicts[key] = siteCheck{status: status, at: now}
	st.mu.Unlock()
	return status
}

// unreadableBrowsers names each browser once and logs each profile's cause
// once, since the same read comes round every few minutes.
func (a *App) unreadableBrowsers(unreadable []browsercookies.Unreadable) []string {
	st := a.siteLogins()
	names := []string{}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, u := range unreadable {
		if !slices.Contains(names, u.Profile.Browser) {
			names = append(names, u.Profile.Browser)
		}
		line := u.Profile.String() + ": " + u.Err.Error()
		if !st.logged[line] {
			st.logged[line] = true
			log.Printf("site sign-in: the cookies of %s cannot be read, so its sessions are not used: %v", u.Profile, u.Err)
		}
	}
	slices.Sort(names)
	return names
}

// sessionOf is the site's session for yt-dlp: a browser's first, the one the
// extension handed over otherwise.
func (a *App) sessionOf(site sitelogin.Site) string {
	if scan := a.browserSession(site, browserScanFresh); scan.jar != "" {
		return scan.jar
	}
	return a.siteStore().Session(site.ID)
}

// siteSession is the session that goes along with every link of a site that
// plays nothing without one, for yt-dlp's Sessions hook.
func (a *App) siteSession(rawurl string) string {
	site, ok := sitelogin.ForURL(rawurl)
	if !ok || site.Use != sitelogin.Always {
		return ""
	}
	return a.sessionOf(site)
}

// signInSession is the session yt-dlp tries a link once more with after it
// failed for want of a sign-in, for its SignIn hook: the site's own where it
// is one of sitelogin.Sites, otherwise whatever a browser here holds for the
// link's domain.
func (a *App) signInSession(rawurl string) string {
	if site, ok := sitelogin.ForURL(rawurl); ok {
		return a.sessionOf(site)
	}
	st := a.siteLogins()
	st.mu.Lock()
	read := st.read
	st.mu.Unlock()
	if read == nil {
		return ""
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(u.Hostname()))
	if err != nil {
		return ""
	}
	now := time.Now()
	found, _ := read([]string{domain})
	var best *browsercookies.Found
	for i, f := range found {
		if looksSignedIn(f.Cookies, now) && (best == nil || lastUsed(f.Cookies).After(lastUsed(best.Cookies))) {
			best = &found[i]
		}
	}
	if best == nil {
		return ""
	}
	jar := sitelogin.JarFromCookies(sitelogin.Site{Domains: []string{domain}}, asBrowserCookies(best.Cookies), now)
	if jar != "" {
		log.Printf("site sign-in: %s asked for a sign-in, so the link is tried again with the session from %s", domain, best.Profile)
	}
	return jar
}

// sessionWords are parts of the cookie names sites keep a sign-in under. A
// site of its own may call it anything, so this only tells a set worth a try
// from one of nothing but consent and analytics cookies.
var sessionWords = []string{"sess", "sid", "auth", "token", "login", "logged", "remember"}

func looksSignedIn(cookies []browsercookies.Cookie, now time.Time) bool {
	return slices.ContainsFunc(cookies, func(c browsercookies.Cookie) bool {
		name := strings.ToLower(c.Name)
		return live(c, now) && slices.ContainsFunc(sessionWords, func(w string) bool { return strings.Contains(name, w) })
	})
}

func live(c browsercookies.Cookie, now time.Time) bool {
	return c.Expires.IsZero() || c.Expires.After(now)
}

func lastUsed(cookies []browsercookies.Cookie) time.Time {
	var t time.Time
	for _, c := range cookies {
		if c.LastUsed.After(t) {
			t = c.LastUsed
		}
	}
	return t
}

// asBrowserCookies puts cookies read from a browser's store into the shape the
// extension hands them over in, which sitelogin builds jars from.
func asBrowserCookies(cookies []browsercookies.Cookie) []sitelogin.BrowserCookie {
	out := make([]sitelogin.BrowserCookie, 0, len(cookies))
	for _, c := range cookies {
		bc := sitelogin.BrowserCookie{
			Domain:   c.Host,
			HostOnly: !strings.HasPrefix(c.Host, "."),
			Path:     c.Path,
			Secure:   c.Secure,
			Session:  c.Expires.IsZero(),
			Name:     c.Name,
			Value:    c.Value,
		}
		if !c.Expires.IsZero() {
			bc.Expires = float64(c.Expires.Unix())
		}
		out = append(out, bc)
	}
	return out
}

// forgetPastedJars deletes the cookies.txt files pasted for yt-dlp.
func (a *App) forgetPastedJars() {
	for _, host := range a.Accounts.AccountIDs(pastedJars) {
		if err := a.Accounts.SetCredential(pastedJars, host, accounts.Credential{}); err != nil {
			log.Printf("a pasted cookies.txt could not be deleted from the credential store: %v", err)
		}
	}
}

// scanLocks gives each site the lock its browser reads take turns on.
func scanLocks() map[string]*sync.Mutex {
	out := make(map[string]*sync.Mutex, len(sitelogin.Sites))
	for _, s := range sitelogin.Sites {
		out[s.ID] = &sync.Mutex{}
	}
	return out
}
