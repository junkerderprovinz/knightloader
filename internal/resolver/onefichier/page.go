package onefichier

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
	namePattern = regexp.MustCompile(`<td class="normal"><span style="font-weight:bold">(.+?)<`)
	// The unit is spelled out so that figures in a file name are not taken
	// for the size.
	sizePattern    = regexp.MustCompile(`(?i)<span [^>]*>([\d.,]+)\s*((?:[KMGT]i?)?[BO]|bytes|octets)</span></td>`)
	gonePattern    = regexp.MustCompile(`File not found !\s*<|>\s*The requested file (?:has been deleted|do(?:es)? not exist)`)
	premiumPattern = regexp.MustCompile(`is not possible to unregistered users|need a subscription`)
	busyPattern    = regexp.MustCompile(`Without subscription, you can only download one file at|Our services are in maintenance|temporarily limited due to high demand`)
	coolPattern    = regexp.MustCompile(`(?i)>\s*You must wait\s*(\d+)\s*minute`)
	// Whether the countdown counts seconds or minutes is read from the word
	// after it, and taken for seconds when there is none: a countdown read too
	// short shows up again on the next page and is waited out once more.
	countdownPattern = regexp.MustCompile(`(?i)Free download in\s*(?:⏳\s*)?(\d+)\s*([a-z]*)`)
	linkPattern      = regexp.MustCompile(`(?i)<a href="([^"]+)"[^>]*>\s*(?:Start (?:your )?download|Click here to download the file)\s*</a>`)
	formPattern      = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	inputPattern     = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	attrPattern      = regexp.MustCompile(`(?s)([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// page is what one of 1fichier's pages says.
type page struct {
	name string
	size int64
	gone bool
	// premium is a file only subscribers may fetch.
	premium bool
	// busy is 1fichier holding back free downloads as a whole.
	busy bool
	// coolDown is the wait this address has to sit out before its next free
	// download.
	coolDown  time.Duration
	countdown time.Duration
	link      string
	forms     []form
}

type form struct {
	action string
	fields url.Values
	// pass is a password field.
	pass bool
}

func readPage(body string) page {
	p := page{
		gone:    gonePattern.MatchString(body),
		premium: premiumPattern.MatchString(body),
		busy:    busyPattern.MatchString(body),
	}
	if m := namePattern.FindStringSubmatch(body); m != nil {
		p.name = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1], m[2])
	}
	if m := coolPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.coolDown = time.Duration(n) * time.Minute
	}
	if m := countdownPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := time.Second
		if strings.HasPrefix(strings.ToLower(m[2]), "min") {
			unit = time.Minute
		}
		p.countdown = time.Duration(n) * unit
	}
	if m := linkPattern.FindStringSubmatch(body); m != nil {
		p.link = html.UnescapeString(m[1])
	}
	for _, m := range formPattern.FindAllStringSubmatch(body, -1) {
		f := form{action: html.UnescapeString(attrs(m[1])["action"]), fields: url.Values{}}
		for _, in := range inputPattern.FindAllStringSubmatch(m[2], -1) {
			a := attrs(in[1])
			name := a["name"]
			if name == "" {
				continue
			}
			if strings.EqualFold(a["type"], "password") {
				f.pass = true
			}
			f.fields.Set(name, html.UnescapeString(a["value"]))
		}
		p.forms = append(p.forms, f)
	}
	return p
}

// attrs reads the attributes of one tag, names lower-cased.
func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
	}
	return out
}

// formFor is the download form of the file id, nil when the page has none.
func (p page) formFor(id string) *form {
	for i, f := range p.forms {
		u, err := url.Parse(f.action)
		if err == nil && fileID.FindString(u.RawQuery) == id {
			return &p.forms[i]
		}
	}
	return nil
}

// parseSize reads a size as 1fichier writes it, in English or French units,
// with a decimal point or a decimal comma.
func parseSize(figure, unit string) int64 {
	if strings.Contains(figure, ".") {
		figure = strings.ReplaceAll(figure, ",", "")
	} else {
		figure = strings.ReplaceAll(figure, ",", ".")
	}
	v, err := strconv.ParseFloat(figure, 64)
	if err != nil {
		return 0
	}
	shift := strings.IndexByte("BKMGT", strings.ToUpper(unit)[0])
	if shift < 0 {
		// O for octets.
		shift = 0
	}
	return int64(math.Round(v * math.Pow(1024, float64(shift))))
}
