package jd

// JDownloader reads a page it has no plugin for and offers every address it
// finds there. For a page whose video plays in a player nobody supports, those
// are the page's own scripts, styles and images, which would then be saved as
// the download. Such a crawl is told by the names JD gave its links, and the
// task fails with core.ReasonUnsupportedPlayer instead.

import (
	"net/url"
	"path"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// pageFile holds the extensions of a page's own files: scripts, styles,
// images, fonts and HTML.
var pageFile = map[string]bool{
	".js": true, ".mjs": true, ".css": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".avif": true, ".svg": true, ".ico": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".html": true, ".htm": true, ".xhtml": true, ".php": true, ".asp": true, ".aspx": true,
}

// readsPage reports whether JD can do no more with rawurl than read it as a
// page: the link names no file, and JD has no plugin for its host. A plugin's
// answer is the file the link stands for, whatever its type.
func readsPage(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return !HostKnown(u.Hostname()) && !(resolver.Direct{}).Match(rawurl)
}

// onlyPageParts reports whether names, the links JD made of a page, are that
// page's own parts: each a page file or a name without an extension (another
// page), with a script or stylesheet among them. One link is a hoster
// plugin's file even when it is a picture, and pictures alone are an album,
// so neither counts.
func onlyPageParts(names []string) bool {
	code := 0
	for _, n := range names {
		ext := strings.ToLower(path.Ext(n))
		switch {
		case ext == ".js" || ext == ".mjs" || ext == ".css":
			code++
		case ext != "" && !pageFile[ext]:
			return false
		}
	}
	return len(names) > 1 && code > 0
}

func crawledNames(links []CrawledLink) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.Name
	}
	return out
}

func downloadNames(links []DownloadLink) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.Name
	}
	return out
}

// pagePartsFailure is how a task ends whose page gave JD nothing but its own
// parts.
func pagePartsFailure() core.Update {
	return core.Update{
		Status: core.StatusError,
		Err:    "jd: the page has nothing on it but its own scripts, styles and images, so no backend here can read its video player",
		Reason: core.ReasonUnsupportedPlayer,
	}
}

// steadyCount tells when a crawl has stopped adding links: the same number for
// settleReadings polls in a row, since JD adds a page's links in batches and
// the first batch is not the whole page.
type steadyCount struct{ last, same int }

func (s *steadyCount) steady(n int) bool {
	if n == 0 || n != s.last {
		s.last, s.same = n, 0
		return false
	}
	s.same++
	return s.same >= settleReadings
}
