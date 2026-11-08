package ddownload

import (
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// The free page names the file in dk-dl-*, the premium-only page in
	// dk-prem-*.
	namePattern = regexp.MustCompile(`class="dk-(?:dl|prem)-name"[^>]*>\s*([^<]+?)\s*<`)
	sizePattern = regexp.MustCompile(`(?i)class="dk-(?:dl|prem)-size"[^>]*>\s*([\d.,]+)\s*([KMGT]?B)\s*<`)
	gonePattern = regexp.MustCompile(`>\s*(?:File Not Found|File Deleted)\s*<|>\s*The file (?:was (?:removed|deleted) by|expired)|>\s*This file was banned by copyright|>[^<>]*Reason for deletion`)
	// The premium-only page is a template of its own, told apart by its card.
	premiumOnlyPattern = regexp.MustCompile(`class="dk-prem-(?:card|wrap)"|>\s*(?:This file is available for Premium Users only|Available Only for Premium Members|File is available only for Premium users|Only Premium user can download this file|(?:The file you requested|This file) reached max downloads)`)
	// A wait between free downloads blocks the download button and carries the
	// seconds left. Every page holds the sentence as a template with {t} in
	// place of the figures, which the text pattern does not take.
	waitSecondsPattern = regexp.MustCompile(`\bdata-wait-seconds="(\d+)"`)
	waitTextPattern    = regexp.MustCompile(`You have to wait ((?:\d+\s+[a-z]+,?\s*)+)till (?:the )?next download`)
	trafficPattern     = regexp.MustCompile(`(?i)You have reached the download[- ]limit|traffic limit (?:is )?(?:exceeded|reached)|not enough traffic`)
	countdownPattern   = regexp.MustCompile(`id="dk2CountdownNum"[^>]*>\s*(\d+)`)
	turnstilePattern   = regexp.MustCompile(`<div\b[^>]*\bclass="cf-turnstile"[^>]*>`)
	wrongCaptcha       = regexp.MustCompile(`>\s*Wrong captcha`)
	wrongPassword      = regexp.MustCompile(`>\s*Wrong password`)
	refusedLogin       = regexp.MustCompile(`>\s*Incorrect (?:Login|Username) or Password\s*<`)
	blockedLogin       = regexp.MustCompile(`>\s*(?:You can't login from this IP|Your IP (?:is|was) banned)`)
	// A signed-in page links to the sign-out, as JDownloader checks.
	signOutPattern = regexp.MustCompile(`<a\b[^<>]*href="[^"]*(?:[?&]op=logout|/logout)"`)
	// The download servers answer on a port of their own, under /d/.
	linkPattern = regexp.MustCompile(`https?://[\w.-]+(?::\d+)?/d/[^"'<>\s]+`)

	premiumPattern    = regexp.MustCompile(`ma-ultimate-pill[^>]*>\s*Ultimate\s*<`)
	untilPattern      = regexp.MustCompile(`>\s*Active until\s+(\d{1,2} [A-Za-z]+ \d{4})\s*<`)
	trafficPatterns   = []*regexp.Regexp{regexp.MustCompile(`\bdata-traffic="(-?\d+)"`), regexp.MustCompile(`<span id="trafficValue">(-?\d+)</span>`)}
	trafficMaxPattern = regexp.MustCompile(`var\s*totalTrafficMB\s*=\s*(\d+)`)

	formPattern    = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	inputPattern   = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	checkedPattern = regexp.MustCompile(`(?i)\bchecked\b`)
	attrPattern    = regexp.MustCompile(`(?s)([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// trafficMax is what a premium plan gets per day when the account page does
// not say, as JDownloader assumes.
const trafficMax = 200000

// page is what one of ddownload's pages says.
type page struct {
	name        string
	size        int64
	gone        bool
	premiumOnly bool
	traffic     bool
	wait        time.Duration
	countdown   time.Duration
	siteKey     string
	signedIn    bool
	link        string
	forms       []form
}

type form struct {
	name   string
	action string
	fields url.Values
	// pass is the name of a password field, "" for none.
	pass string
}

func readPage(body string) page {
	p := page{
		gone:        gonePattern.MatchString(body),
		premiumOnly: premiumOnlyPattern.MatchString(body),
		traffic:     trafficPattern.MatchString(body),
		signedIn:    signOutPattern.MatchString(body),
	}
	if m := namePattern.FindStringSubmatch(body); m != nil {
		p.name = html.UnescapeString(m[1])
	}
	if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1], m[2])
	}
	if m := waitSecondsPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.wait = time.Duration(n) * time.Second
	} else if m := waitTextPattern.FindStringSubmatch(body); m != nil {
		p.wait = parseSpan(m[1])
	}
	if m := countdownPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.countdown = time.Duration(n) * time.Second
	}
	if tag := turnstilePattern.FindString(body); tag != "" {
		p.siteKey = attrs(tag)["data-sitekey"]
	}
	if m := linkPattern.FindString(body); m != "" {
		p.link = html.UnescapeString(m)
	}
	for _, m := range formPattern.FindAllStringSubmatch(body, -1) {
		a := attrs(m[1])
		f := form{name: a["name"], action: html.UnescapeString(a["action"]), fields: url.Values{}}
		for _, in := range inputPattern.FindAllStringSubmatch(m[2], -1) {
			a := attrs(in[1])
			name := a["name"]
			if name == "" {
				continue
			}
			kind := strings.ToLower(a["type"])
			if kind == "password" {
				f.pass = name
			}
			// A browser sends a box only when it is ticked.
			if (kind == "checkbox" || kind == "radio") && !checkedPattern.MatchString(in[1]) {
				continue
			}
			f.fields.Set(name, html.UnescapeString(a["value"]))
		}
		p.forms = append(p.forms, f)
	}
	return p
}

// formWith is the page's form whose op field is op, nil when it has none.
func (p page) formWith(op string) *form {
	for i, f := range p.forms {
		if f.fields.Get("op") == op {
			return &p.forms[i]
		}
	}
	return nil
}

// attrs reads the attributes of one tag, names lower-cased.
func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
	}
	return out
}

// parseSize reads a size as ddownload writes it. Its units are binary,
// whatever their names say.
func parseSize(figure, unit string) int64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(figure, ",", ""), 64)
	if err != nil {
		return 0
	}
	shift := max(strings.IndexByte("BKMGT", strings.ToUpper(unit)[0]), 0)
	return int64(math.Round(v * math.Pow(1024, float64(shift))))
}

// parseSpan reads a wait written out in words, such as "1 hours, 2 minutes,
// 5 seconds".
func parseSpan(s string) time.Duration {
	var d time.Duration
	f := strings.Fields(strings.ReplaceAll(s, ",", " "))
	for i := 0; i+1 < len(f); i += 2 {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			continue
		}
		switch unit := f[i+1]; {
		case strings.HasPrefix(unit, "day"):
			d += time.Duration(n) * 24 * time.Hour
		case strings.HasPrefix(unit, "hour"):
			d += time.Duration(n) * time.Hour
		case strings.HasPrefix(unit, "minute"):
			d += time.Duration(n) * time.Minute
		case strings.HasPrefix(unit, "second"):
			d += time.Duration(n) * time.Second
		}
	}
	return d
}

// readAccount reads the account page, which names the plan, when it runs out
// and the traffic left in megabytes of a million bytes.
func readAccount(body string, now time.Time) Account {
	var a Account
	premium := premiumPattern.MatchString(body)
	if m := untilPattern.FindStringSubmatch(body); m != nil {
		if t, err := time.Parse("2 January 2006", m[1]); err == nil {
			// The day it is active until is still a premium day.
			a.Expires = t.Add(24*time.Hour - time.Second)
		}
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
