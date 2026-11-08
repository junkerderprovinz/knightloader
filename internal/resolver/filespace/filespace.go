// Package filespace is FileSpace on the XFileSharing client. Its file page
// has a form of its own for each button, the size in a hidden field, and a
// countdown in a span with a random id.
//
// The patterns follow the FilespaceCom plugin of JDownloader (GPLv3).
package filespace

import (
	"regexp"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Site is FileSpace. It serves every download over one connection.
var Site = &xfs.Site{
	ID:           "filespace",
	Name:         "FileSpace",
	Host:         "filespace.com",
	OwnLogin:     true,
	FreeConns:    1,
	PremiumConns: 1,
	Adjust:       adjust,
	Patterns: xfs.Patterns{
		Size: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`id="fileSize"[^>]*value="([\d.,]+)(?:\s|&nbsp;)*([KMGT]?B)"`),
		}, xfs.Classic.Size),
		Countdown: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`Please wait <span id="[a-z0-9]+"[^>]*>(\d+)</span>`),
		}, xfs.Classic.Countdown),
	},
}

// freeType is what the free second form adds to where it goes, when the page
// says it.
const freeType = "accounttype=free"

func adjust(f *xfs.Form, page string) {
	if f.Op == "download2" && f.Action != "" && strings.Contains(page, freeType) {
		f.Action += "?" + freeType
	}
}
