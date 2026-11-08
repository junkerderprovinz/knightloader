// Package ddownload is ddownload on the XFileSharing client: a free download
// with ddownload's Turnstile captcha and its waits, and a premium login that
// ddownload asks a Turnstile captcha for at every sign-in. ddownload runs a
// template of its own, so most of its patterns differ from the script's.
//
// The patterns follow the DdownloadCom plugins of JDownloader and pyLoad (both
// GPLv3).
package ddownload

import (
	"regexp"
	"strconv"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Site is ddownload. Links of ddl.to are ddownload's as well.
var Site = &xfs.Site{
	ID:        "ddownload",
	Name:      "ddownload",
	Host:      "ddownload.com",
	LoginPath: "/login",
	OwnLogin:  true,
	// ddownload serves every download over one connection.
	FreeConns: 1, PremiumConns: 1,
	DailyTraffic: trafficMax * 1e6,
	ReadAccount:  readAccount,
	Patterns: xfs.Patterns{
		// The free page names the file in dk-dl-*, the premium-only page in
		// dk-prem-*.
		Name: []*regexp.Regexp{regexp.MustCompile(`class="dk-(?:dl|prem)-name"[^>]*>\s*([^<]+?)\s*<`)},
		Size: []*regexp.Regexp{regexp.MustCompile(`(?i)class="dk-(?:dl|prem)-size"[^>]*>\s*([\d.,]+)\s*([KMGT]?B)\s*<`)},
		Gone: regexp.MustCompile(`>\s*(?:File Not Found|File Deleted)\s*<|>\s*The file (?:was (?:removed|deleted) by|expired)|>\s*This file was banned by copyright|>[^<>]*Reason for deletion`),
		// The premium-only page is a template of its own, told apart by its
		// card.
		PremiumOnly: regexp.MustCompile(`class="dk-prem-(?:card|wrap)"|>\s*(?:This file is available for Premium Users only|Available Only for Premium Members|File is available only for Premium users|Only Premium user can download this file|(?:The file you requested|This file) reached max downloads)`),
		// A wait between free downloads blocks the download button and carries
		// the seconds left. Every page holds the sentence as a template with
		// {t} in place of the figures, which the text pattern does not take.
		WaitSeconds:  regexp.MustCompile(`\bdata-wait-seconds="(\d+)"`),
		WaitText:     regexp.MustCompile(`You have to wait ((?:\d+\s+[a-z]+,?\s*)+)till (?:the )?next download`),
		Traffic:      regexp.MustCompile(`(?i)You have reached the download[- ]limit|traffic limit (?:is )?(?:exceeded|reached)|not enough traffic`),
		Countdown:    []*regexp.Regexp{regexp.MustCompile(`id="dk2CountdownNum"[^>]*>\s*(\d+)`)},
		WrongCaptcha: regexp.MustCompile(`>\s*Wrong captcha`),
		// The download servers answer on a port of their own, under /d/, and
		// so does ddownload itself.
		Link:     []*regexp.Regexp{regexp.MustCompile(`(https?://[\w.-]+(?::\d+)?/d/[^"'<>\s]+)`)},
		SignedIn: regexp.MustCompile(`<a\b[^<>]*href="[^"]*(?:[?&]op=logout|/logout)"`),
	},
}

// trafficMax is what a premium plan gets per day, in megabytes, when the
// account page does not say, as JDownloader assumes.
const trafficMax = 200000

var (
	premiumPattern    = regexp.MustCompile(`ma-ultimate-pill[^>]*>\s*Ultimate\s*<`)
	untilPattern      = regexp.MustCompile(`>\s*Active until\s+(\d{1,2} [A-Za-z]+ \d{4})\s*<`)
	trafficPatterns   = []*regexp.Regexp{regexp.MustCompile(`\bdata-traffic="(-?\d+)"`), regexp.MustCompile(`<span id="trafficValue">(-?\d+)</span>`)}
	trafficMaxPattern = regexp.MustCompile(`var\s*totalTrafficMB\s*=\s*(\d+)`)
)

// readAccount reads the account page, which names the plan, when it runs out
// and the traffic left in megabytes of a million bytes.
func readAccount(body string, now time.Time) xfs.Account {
	var a xfs.Account
	premium := premiumPattern.MatchString(body)
	if m := untilPattern.FindStringSubmatch(body); m != nil {
		a.Expires = xfs.ParseExpiry(m[1])
	}
	a.Premium = premium && (a.Expires.IsZero() || a.Expires.After(now))
	for _, p := range trafficPatterns {
		if m := p.FindStringSubmatch(body); m != nil {
			mb, _ := strconv.ParseInt(m[1], 10, 64)
			a.TrafficLeft = mb * 1e6
			a.TrafficMax = trafficMax * 1e6
			if m := trafficMaxPattern.FindStringSubmatch(body); m != nil {
				mb, _ := strconv.ParseInt(m[1], 10, 64)
				a.TrafficMax = mb * 1e6
			}
			break
		}
	}
	return a
}
