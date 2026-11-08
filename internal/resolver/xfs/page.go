package xfs

import (
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	formPattern    = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	inputPattern   = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	buttonPattern  = regexp.MustCompile(`(?is)<button\b([^>]*)>`)
	checkedPattern = regexp.MustCompile(`(?i)\bchecked\b`)
	attrPattern    = regexp.MustCompile(`(?s)([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

	widgetPattern = regexp.MustCompile(`<div\b[^>]*\bclass="[^"]*\b(cf-turnstile|g-recaptcha|h-captcha)\b[^"]*"[^>]*>`)
	imagePattern  = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc="([^"]*/captchas/[^"]+)"`)
	// The script's own captcha draws its digits as characters placed by their
	// left padding, so their order on the page is not the order to type.
	plainDigit = regexp.MustCompile(`<span style=.position:absolute;padding-left:(\d+)px;padding-top:\d+px;.>(&#\d+;)</span>`)
)

// CaptchaKind is the kind of check a form asks for.
type CaptchaKind string

// The checks the client can carry. A widget's kind is the vendor name the
// captcha package uses.
const (
	Turnstile CaptchaKind = "turnstile"
	Recaptcha CaptchaKind = "recaptcha"
	HCaptcha  CaptchaKind = "hcaptcha"
	Image     CaptchaKind = "image"
)

var widgetKinds = map[string]CaptchaKind{"cf-turnstile": Turnstile, "g-recaptcha": Recaptcha, "h-captcha": HCaptcha}

// Captcha is the check a form asks for before it goes in.
type Captcha struct {
	Kind CaptchaKind
	// SiteKey is a widget's key.
	SiteKey string
	// PageURL is the page the check is on, which a widget is solved for.
	PageURL string
	// Image is the picture of an image captcha, read in the page's session.
	Image []byte
	// imageURL is where Image was read from.
	imageURL string
}

// Form is one of a page's forms as it would go in.
type Form struct {
	// Op is the form's op field, which names the step it takes.
	Op     string
	Action string
	Fields url.Values
	// Password names the password field, "" for none.
	Password string
	// buttons are the named submit buttons, which go in only when pressed.
	buttons url.Values
	inner   string
}

// page is what one of a site's pages says.
type page struct {
	name        string
	size        int64
	gone        bool
	premiumOnly bool
	traffic     bool
	wait        time.Duration
	countdown   time.Duration
	signedIn    bool
	link        string
	forms       []Form
}

func readPage(body string, p Patterns) page {
	pg := page{
		gone:        p.Gone.MatchString(body),
		premiumOnly: p.PremiumOnly.MatchString(body),
		traffic:     p.Traffic.MatchString(body),
		signedIn:    p.SignedIn.MatchString(body),
	}
	if m := first(p.Name, body); m != nil {
		pg.name = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := first(p.Size, body); m != nil {
		pg.size = ParseSize(m[1], m[2])
	}
	if m := match(p.WaitSeconds, body); m != nil {
		n, _ := strconv.Atoi(m[1])
		pg.wait = time.Duration(n) * time.Second
	} else if m := match(p.WaitText, body); m != nil {
		pg.wait = parseSpan(m[1])
	} else if match(p.Limit, body) != nil {
		pg.wait = LimitWait
	}
	if m := first(p.Countdown, body); m != nil {
		n, _ := strconv.Atoi(m[1])
		pg.countdown = time.Duration(n) * time.Second
	}
	for _, m := range formPattern.FindAllStringSubmatch(body, -1) {
		pg.forms = append(pg.forms, readForm(m[1], m[2]))
	}
	// A page that still holds a download form has not handed the file out,
	// so a link on it is an advert.
	if m := first(p.Link, body); m != nil && pg.downloadForm(false) == nil {
		pg.link = html.UnescapeString(m[1])
	}
	return pg
}

func readForm(tag, inner string) Form {
	a := attrs(tag)
	f := Form{Action: html.UnescapeString(a["action"]), Fields: url.Values{}, buttons: url.Values{}, inner: inner}
	for _, in := range inputPattern.FindAllStringSubmatch(inner, -1) {
		a := attrs(in[1])
		name := a["name"]
		if name == "" {
			continue
		}
		switch kind := strings.ToLower(a["type"]); {
		case kind == "submit" || kind == "image":
			f.buttons.Set(name, html.UnescapeString(a["value"]))
			continue
		case kind == "password":
			f.Password = name
		case (kind == "checkbox" || kind == "radio") && !checkedPattern.MatchString(in[1]):
			// A browser sends a box only when it is ticked.
			continue
		}
		f.Fields.Set(name, html.UnescapeString(a["value"]))
	}
	for _, b := range buttonPattern.FindAllStringSubmatch(inner, -1) {
		a := attrs(b[1])
		if name := a["name"]; name != "" && strings.ToLower(a["type"]) != "button" {
			f.buttons.Set(name, html.UnescapeString(a["value"]))
		}
	}
	f.Op = f.Fields.Get("op")
	return f
}

// formWith is the page's form whose op field is op, nil when it has none.
func (p page) formWith(op string) *Form {
	for i, f := range p.forms {
		if f.Op == op {
			return &p.forms[i]
		}
	}
	return nil
}

// downloadForm is the form that takes the download a step on: the second
// step's form when the page has one, else the first step's form with the
// button for a premium or a free download, else any first step's form.
func (p page) downloadForm(premium bool) *Form {
	if f := p.formWith("download2"); f != nil {
		return f
	}
	button := buttonFor(premium)
	var any *Form
	for i, f := range p.forms {
		if f.Op != "download1" {
			continue
		}
		if f.buttons.Has(button) {
			return &p.forms[i]
		}
		if any == nil {
			any = &p.forms[i]
		}
	}
	return any
}

func buttonFor(premium bool) string {
	if premium {
		return "method_premium"
	}
	return "method_free"
}

// captcha is the check f asks for, nil for none. One the client answers
// itself, the script's own digits, goes straight into f.
func captchaOf(f *Form, pageURL string) *Captcha {
	if m := widgetPattern.FindStringSubmatch(f.inner); m != nil {
		return &Captcha{Kind: widgetKinds[m[1]], SiteKey: attrs(m[0])["data-sitekey"], PageURL: pageURL}
	}
	if m := imagePattern.FindStringSubmatch(f.inner); m != nil {
		return &Captcha{Kind: Image, PageURL: pageURL, imageURL: html.UnescapeString(m[1])}
	}
	if digits := plainDigit.FindAllStringSubmatch(f.inner, -1); len(digits) > 0 {
		sort.SliceStable(digits, func(i, j int) bool {
			a, _ := strconv.Atoi(digits[i][1])
			b, _ := strconv.Atoi(digits[j][1])
			return a < b
		})
		var code strings.Builder
		for _, d := range digits {
			code.WriteString(html.UnescapeString(d[2]))
		}
		f.Fields.Set("code", code.String())
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

func first(list []*regexp.Regexp, body string) []string {
	for _, p := range list {
		if m := p.FindStringSubmatch(body); m != nil {
			return m
		}
	}
	return nil
}

func match(p *regexp.Regexp, body string) []string {
	if p == nil {
		return nil
	}
	return p.FindStringSubmatch(body)
}

// LimitWait is how long a free download waits once the page says the
// address has used up its free downloads without saying for how long.
const LimitWait = time.Hour

// ParseSize reads a size as the script writes it. Its units are binary,
// whatever their names say, and a figure without one is in bytes.
func ParseSize(figure, unit string) int64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(figure, ",", ""), 64)
	if err != nil {
		return 0
	}
	shift := 0
	if unit != "" {
		shift = max(strings.IndexByte("BKMGT", strings.ToUpper(unit)[0]), 0)
	}
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
		switch unit := strings.ToLower(f[i+1]); {
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

var (
	expiryPattern  = regexp.MustCompile(`(?i)Premium(?:\s+Pro)?(?:[- ]account)?\s*(?:expires?|until|expiration)\b[^<>]*?:?\s*(?:<[^>]+>\s*)*(\d{1,2} [A-Za-z]+ \d{4}|\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}:\d{2})?)`)
	trafficPattern = regexp.MustCompile(`(?is)(?:Remaining traffic|Traffic available)(?:\s*today)?[^<>]*?:?\s*(?:<[^>]+>\s*)*(-?[\d.,]+)\s*([KMGT]?B)\b`)
)

// ReadAccount reads the account page of the script's own template: a premium
// plan names the day it runs out, and the traffic left is in binary units.
func ReadAccount(body string, now time.Time) Account {
	var a Account
	if m := expiryPattern.FindStringSubmatch(body); m != nil {
		a.Expires = ParseExpiry(m[1])
	}
	a.Premium = !a.Expires.IsZero() && a.Expires.After(now)
	if m := trafficPattern.FindStringSubmatch(body); m != nil {
		figure, neg := strings.CutPrefix(m[1], "-")
		a.TrafficLeft = ParseSize(figure, m[2])
		if neg {
			a.TrafficLeft = -a.TrafficLeft
		}
		a.TrafficMax = max(a.TrafficLeft, 0)
	}
	return a
}

// ParseExpiry reads the day a plan runs out, as "2 January 2006" or
// "2006-01-02 15:04:05". A day without a time is still a premium day to its
// end.
func ParseExpiry(s string) time.Time {
	if t, err := time.Parse(time.DateTime, s); err == nil {
		return t
	}
	for _, layout := range []string{"2 January 2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Add(24*time.Hour - time.Second)
		}
	}
	return time.Time{}
}
