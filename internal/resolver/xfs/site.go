// Package xfs downloads from file hosters built on XFileSharing, the script
// behind most small hosters, so their links need no JDownloader: on the
// user's premium login when the site's sign-in fits, and for free otherwise,
// with the captchas the site shows going to the app's captcha prompts and
// solvers and its waits showing on the task. A debrid service that carries
// the hoster stays ahead of a free download, and a premium login ranks where
// the same login in JDownloader would.
//
// Each hoster is a Site: its domains, its names and the patterns its pages
// write differently from the script's own. The page patterns, the two
// download forms, the captchas, the sign-in and the account page follow
// JDownloader's XFileSharingProBasic and pyLoad's XFSDownloader and
// XFSAccount (all GPLv3).
package xfs

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/hostalias"
)

// Site is one hoster that runs XFileSharing.
type Site struct {
	// ID is the routing id of the hoster's resolver and backend.
	ID string
	// Name is what notes and errors call the hoster.
	Name string
	// Host is the domain links are written out with and a login is filed
	// under. Its alias domains come from hostalias.
	Host string
	// Cookies are set on every visit beside the one that asks for English
	// pages, for a site that wants more to keep to that.
	Cookies map[string]string
	// LoginPath is the page with the sign-in form, "/login.html" when empty.
	LoginPath string
	// OwnLogin is set when KnightLoader signs in with the user's login
	// itself. Without it the login stays with JDownloader and the client
	// downloads for free only.
	OwnLogin bool
	// PremiumCaptcha is set when a premium download's form asks for a
	// captcha too, so a captcha alone does not mark the free page.
	PremiumCaptcha bool
	// FreeConns and PremiumConns are the connections a download may use.
	FreeConns, PremiumConns int
	// FreeOnce is set for a site whose free link answers a single request,
	// one that JDownloader's plugin downloads without resume. The file is
	// then read whole from the first response (see engine.Job.Once).
	FreeOnce bool
	// DailyTraffic is what a premium plan gets per day, in bytes, when the
	// account page does not say. 0 is not known.
	DailyTraffic int64
	// Patterns are the site's own patterns. A field left nil is the
	// script's, from Classic.
	Patterns Patterns
	// ReadAccount reads the account page. Nil is ReadAccount.
	ReadAccount func(body string, now time.Time) Account
	// Adjust changes a form of the site's before it goes in. page is the page
	// the form is on.
	Adjust func(f *Form, page string)
}

// Patterns are what the pages of a site are read with. Each list is tried in
// order, and the first that matches counts.
type Patterns struct {
	// Name finds the file name in group 1.
	Name []*regexp.Regexp
	// Size finds the size, the figure in group 1 and the unit in group 2.
	Size []*regexp.Regexp
	// Gone matches a file that was deleted or never existed.
	Gone *regexp.Regexp
	// PremiumOnly matches a file its owner keeps for premium accounts.
	PremiumOnly *regexp.Regexp
	// WaitSeconds finds the wait before the next free download in seconds,
	// WaitText the same written out in words, such as "1 hours, 5 minutes".
	WaitSeconds *regexp.Regexp
	WaitText    *regexp.Regexp
	// Limit matches a page that says the address has used up its free
	// downloads without saying for how long, which is sat out for LimitWait.
	Limit *regexp.Regexp
	// Traffic matches a premium login whose traffic is used up.
	Traffic *regexp.Regexp
	// Countdown finds the seconds a form wants before it goes in.
	Countdown []*regexp.Regexp
	// Link finds the download link in group 1.
	Link []*regexp.Regexp
	// The answers a form can come back with.
	WrongCaptcha  *regexp.Regexp
	WrongPassword *regexp.Regexp
	RefusedLogin  *regexp.Regexp
	BlockedLogin  *regexp.Regexp
	// SignedIn matches a page shown to a session that is signed in.
	SignedIn *regexp.Regexp
}

// Classic is how XFileSharing's own templates write their pages.
var Classic = Patterns{
	Name: []*regexp.Regexp{
		regexp.MustCompile(`name="fname"[^>]*value="([^"]+)"`),
		regexp.MustCompile(`class="dfilename"[^>]*>\s*([^<>"]+?)\s*<`),
		regexp.MustCompile(`<h2>\s*Download File\s*(?:<(?:span|b)[^>]*>)?\s*([^<>]+?)\s*</`),
	},
	Size: []*regexp.Regexp{
		regexp.MustCompile(`(?i)Size\s*:\s*</b>(?:\s*</td>\s*<td>)?\s*([\d.,]+)(?:\s|&nbsp;)*([KMGT]?B)\b`),
		regexp.MustCompile(`</font>\s*\(\s*([\d.,]+)(?:\s|&nbsp;)*([KMGT]?B)\s*\)`),
		regexp.MustCompile(`class="size"[^>]*>\s*([\d.,]+)(?:\s|&nbsp;)*([KMGT]?B)\s*<`),
	},
	Gone:        regexp.MustCompile(`(?i)>\s*(?:[*-]\s*)?(?:File Not Found\.?|File Deleted|No such file|File has been removed[^<]*)\s*<|>\s*(?:[*-]\s*)?The file (?:was (?:removed|deleted) by|expired)|>[^<>]*Reason for deletion`),
	PremiumOnly: regexp.MustCompile(`>\s*(?:This file is available for Premium Users only|Available Only for Premium Members|File is available only for Premium users|Only Premium user can download this file|Please Buy Premium To download|This file is not available for free download|(?:The file you requested|This file) reached max downloads)|>\s*(?:Sorry\s*,\s*)?This file (?:can|only can|can only) be downloaded by|>\s*You can download files up to \d+`),
	WaitText:    regexp.MustCompile(`(?i)You have to wait ((?:\d+\s+[a-z]+,?\s*)+)`),
	Traffic:     regexp.MustCompile(`(?i)You have reached the download[- ]limit|traffic limit (?:is )?(?:exceeded|reached)|not enough traffic`),
	Countdown: []*regexp.Regexp{
		regexp.MustCompile(`id=["']countdown_str["'][^>]*>[^<>]*<span id=[^>]*>\s*(\d+)\s*</span>`),
		regexp.MustCompile(`class="seconds"[^>]*>\s*(\d+)\s*<`),
		regexp.MustCompile(`id="seconds"[^>]*>\s*(\d+)\s*<`),
	},
	// The download servers name the file after a path of their own. An
	// address that does not look like one, such as an advert labelled as the
	// download, is passed over.
	Link: []*regexp.Regexp{
		regexp.MustCompile(`["'](https?://[\w.-]+(?::\d+)?/(?:files|d|dl|cgi-bin/dl\.cgi)/(?:\d+/)?[A-Za-z0-9]+/[^<>"'/]+)["']`),
	},
	WrongCaptcha:  regexp.MustCompile(`(?i)>[^<]*Wrong captcha`),
	WrongPassword: regexp.MustCompile(`(?i)>\s*Wrong password`),
	RefusedLogin:  regexp.MustCompile(`>\s*Incorrect (?:Login|Username) or Password\s*<`),
	BlockedLogin:  regexp.MustCompile(`>\s*(?:You can't login from this IP|Your IP (?:is|was) banned)`),
	// A signed-in page links to the sign-out, as JDownloader checks.
	SignedIn: regexp.MustCompile(`<a\b[^<>]*href="[^"]*(?:[?&]op=logout|/logout(?:\.html)?/?)"`),
}

// with fills the fields p leaves nil from base.
func (p Patterns) with(base Patterns) Patterns {
	pick := func(own, def *regexp.Regexp) *regexp.Regexp {
		if own != nil {
			return own
		}
		return def
	}
	list := func(own, def []*regexp.Regexp) []*regexp.Regexp {
		if own != nil {
			return own
		}
		return def
	}
	return Patterns{
		Name:          list(p.Name, base.Name),
		Size:          list(p.Size, base.Size),
		Gone:          pick(p.Gone, base.Gone),
		PremiumOnly:   pick(p.PremiumOnly, base.PremiumOnly),
		WaitSeconds:   pick(p.WaitSeconds, base.WaitSeconds),
		WaitText:      pick(p.WaitText, base.WaitText),
		Limit:         pick(p.Limit, base.Limit),
		Traffic:       pick(p.Traffic, base.Traffic),
		Countdown:     list(p.Countdown, base.Countdown),
		Link:          list(p.Link, base.Link),
		WrongCaptcha:  pick(p.WrongCaptcha, base.WrongCaptcha),
		WrongPassword: pick(p.WrongPassword, base.WrongPassword),
		RefusedLogin:  pick(p.RefusedLogin, base.RefusedLogin),
		BlockedLogin:  pick(p.BlockedLogin, base.BlockedLogin),
		SignedIn:      pick(p.SignedIn, base.SignedIn),
	}
}

// filePath is a file's code, twelve letters and digits, with or without the
// file name after it.
var filePath = regexp.MustCompile(`^/([A-Za-z0-9]{12})(?:\.html)?(?:/[^/]*)?$`)

// Normalize writes a file link of any of the site's domains, with or without
// the file name after the code, as https://Host/CODE. It reports false for
// anything else.
func (s *Site) Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	if hostalias.Canonical(u.Hostname()) != s.Host {
		return "", false
	}
	m := filePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return "https://" + s.Host + "/" + strings.ToLower(m[1]), true
}

func (s *Site) loginPath() string {
	if s.LoginPath == "" {
		return "/login.html"
	}
	return s.LoginPath
}

func (s *Site) readAccount(body string, now time.Time) Account {
	if s.ReadAccount != nil {
		return s.ReadAccount(body, now)
	}
	return ReadAccount(body, now)
}
