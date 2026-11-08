// Package katfile is KatFile on the XFileSharing client. Its sign-in asks for
// a Turnstile captcha, its premium download can ask for one as well, and its
// file page counts down before the free button goes in.
//
// The patterns follow the KatfileCom plugins of JDownloader and pyLoad (both
// GPLv3).
package katfile

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Site is KatFile, under any of the domains it has moved through.
var Site = &xfs.Site{
	ID:   "katfile",
	Name: "KatFile",
	Host: "katfile.com",
	// KatFile picks the language by the visitor's address unless the choice
	// is marked as made by hand.
	Cookies:        map[string]string{"lang_manual": "2"},
	OwnLogin:       true,
	PremiumCaptcha: true,
	FreeConns:      1,
	FreeOnce:       true,
	PremiumConns:   2,
	ReadAccount:    readAccount,
	Adjust:         adjust,
	Patterns: xfs.Patterns{
		Size: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`id="m?fsize_?"[^>]*>\s*([\d.,]+)\s*([KMGT]?B)\s*<`),
		}, xfs.Classic.Size),
		// A deleted file's page shows a picture where the file would be.
		Gone:     regexp.MustCompile(`(?i)/404-remove|>\s*(?:File Not Found|File has been removed)|>\s*The file (?:was (?:removed|deleted) by|expired)`),
		WaitText: regexp.MustCompile(`(?i)(?:You have to wait|Delay between downloads must be not less than) ((?:\d+\s+[a-z]+,?\s*)+)`),
		Countdown: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`var estimated_time = (\d+)`),
		}, xfs.Classic.Countdown),
	},
}

// adjust gives the second form the browser id KatFile's own script adds.
func adjust(f *xfs.Form, _ string) {
	if f.Op == "download2" && !f.Fields.Has("device_id") {
		f.Fields.Set("device_id", deviceID())
	}
}

// deviceID is a random id in the form of a UUID.
func deviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var (
	packagePattern = regexp.MustCompile(`(?is)Premium\s*Package.*?until\s*(?:<b>)?\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s*<`)
	usedPattern    = regexp.MustCompile(`(?i)>\s*Traffic available(?:\s*today)?\s*</span>\s*<small>\s*([\d.]+)\s*/\s*([\d.]+)\s*([KMGT]?B)`)
)

// readAccount reads KatFile's account page, which may name the plan's end in
// its package row and write the traffic as used of the day's.
func readAccount(body string, now time.Time) xfs.Account {
	a := xfs.ReadAccount(body, now)
	if m := packagePattern.FindStringSubmatch(body); m != nil {
		a.Expires = xfs.ParseExpiry(m[1])
		a.Premium = a.Expires.After(now)
	}
	if m := usedPattern.FindStringSubmatch(body); m != nil {
		used, most := xfs.ParseSize(m[1], m[3]), xfs.ParseSize(m[2], m[3])
		a.TrafficLeft, a.TrafficMax = max(most-used, 0), most
	}
	return a
}
