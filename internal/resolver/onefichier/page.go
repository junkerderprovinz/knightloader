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
	namePattern = regexp.MustCompile(`<span class="tier-name">(.+?)<|<td class="normal"><span style="font-weight:bold">(.+?)<`)
	// The unit is spelled out so that figures in a file name are not taken
	// for the size.
	tierSizePattern = regexp.MustCompile(`(?i)<span class="tier-feat">([\d.,]+)\s*((?:[KMGT]i?)?[BO]|bytes|octets)</span>`)
	sizePattern     = regexp.MustCompile(`(?i)<span [^>]*>([\d.,]+)\s*((?:[KMGT]i?)?[BO]|bytes|octets)</span></td>`)
	gonePattern     = regexp.MustCompile(`File not found !\s*<|>\s*The requested file (?:has been deleted|do(?:es)? not exist)`)
	premiumPattern  = regexp.MustCompile(`not possible to (?:free )?unregistered users|need a subscription|reserved access to the subscribers`)
	busyPattern     = regexp.MustCompile(`Our services are in maintenance|temporarily limited due to high demand|all free guest slots are currently in use`)
	coolPattern     = regexp.MustCompile(`(?i)(?:>\s*You must wait|you must wait (?:at least|up to)|>\s*Vous devez attendre encore)\s*(\d+)\s*minute`)
	lockedPattern   = regexp.MustCompile(`>\s*IP Locked|>\s*Will be unlocked within 1h\.`)
	// A wait without a figure, which JDownloader's plugin sits out for five
	// minutes.
	blockedPattern = regexp.MustCompile(`(?i)>\s*You must wait for another download|>\s*You already downloading (?:some|a) file|>\s*You can download only one file at a time|>\s*Please wait a few seconds before downloading new ones|Without (?:premium status|premium|subscription), you can (?:only )?download (?:only )?one file at|Without Premium, you must wait between downloads|En téléchargement standard, vous ne pouvez télécharger qu'un seul fichier|>\s*veuillez patienter avant de télécharger un autre fichier`)
	// The page counts down in its script, in seconds. A countdown written out
	// in the text names its unit after the figure and is taken for seconds
	// without one: read too short, it shows up again on the next page and is
	// waited out once more.
	scriptCountdownPattern = regexp.MustCompile(`\bvar ct\s*=\s*(\d+)`)
	countdownPattern       = regexp.MustCompile(`(?i)Free download in\s*(?:⏳\s*)?(\d+)\s*([a-z]*)`)
	linkPattern            = regexp.MustCompile(`(?i)<a href="([^"]+)"[^>]*>\s*(?:Start (?:your )?download|Click here to download the file)\s*</a>`)
	formPattern            = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	inputPattern           = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	checkedPattern         = regexp.MustCompile(`(?i)\bchecked\b`)
	attrPattern            = regexp.MustCompile(`(?s)([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
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
		p.name = strings.TrimSpace(html.UnescapeString(m[1] + m[2]))
	}
	if m := tierSizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1], m[2])
	} else if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1], m[2])
	}
	if m := coolPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.coolDown = time.Duration(n) * time.Minute
	} else if lockedPattern.MatchString(body) {
		p.coolDown = time.Hour
	} else if blockedPattern.MatchString(body) {
		p.coolDown = blockedWait
	}
	if m := scriptCountdownPattern.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.countdown = time.Duration(n) * time.Second
	} else if m := countdownPattern.FindStringSubmatch(body); m != nil {
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
			kind := strings.ToLower(a["type"])
			if kind == "password" {
				f.pass = true
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
// with a decimal point or a decimal comma. Its units count in thousands, so a
// file of 1048576 bytes is 1.05 MB. A KiB or MiB counts in 1024s.
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
	base := 1000.0
	if strings.ContainsAny(unit, "iI") {
		base = 1024
	}
	return int64(math.Round(v * math.Pow(base, float64(shift))))
}
