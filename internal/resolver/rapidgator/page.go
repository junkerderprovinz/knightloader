package rapidgator

import (
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	namePattern  = regexp.MustCompile(`Downloading\s*:\s*</strong>\s*<a href=""[^>]*>([^<>"]+)<`)
	titlePattern = regexp.MustCompile(`<title>\s*Download file\s*([^<>"]+)</title>`)
	sizePattern  = regexp.MustCompile(`(?i)File size:\s*<strong>\s*([\d.,]+)\s*([KMGT]?B)\s*</strong>`)
	notFound     = regexp.MustCompile(`>\s*404 File not found`)
)

// page is what a public file page says. The page rounds the size, to "1 GB"
// for a file of a little over that, so the API's figure is taken where there
// is one.
type page struct {
	name string
	size int64
}

func readPage(body string) page {
	var p page
	m := namePattern.FindStringSubmatch(body)
	if m == nil {
		m = titlePattern.FindStringSubmatch(body)
	}
	if m != nil {
		p.name = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1], m[2])
	}
	return p
}

// parseSize reads a size as the page writes it, in binary units, as
// JDownloader's plugin reads them.
func parseSize(figure, unit string) int64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(figure, ",", ""), 64)
	if err != nil {
		return 0
	}
	shift := strings.IndexByte("BKMGT", strings.ToUpper(unit)[0])
	return int64(math.Round(v * math.Pow(1024, float64(shift))))
}
