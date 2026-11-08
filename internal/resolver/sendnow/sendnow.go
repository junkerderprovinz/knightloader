// Package sendnow is Send.now, formerly Send.cm, on the XFileSharing client,
// for free downloads. Its file page holds the second form straight away,
// names the file in a dialog, writes the exact size in its forum code, and
// says when the address has used up the day's free downloads without saying
// for how long. A Send.now login stays with JDownloader, whose plugin signs in
// with a key of the site's API or a code sent by mail, which KnightLoader's
// sign-in does not carry.
//
// The domains and the patterns follow the SendNow plugin of JDownloader
// (GPLv3).
package sendnow

import (
	"regexp"
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Site is Send.now, under any of the domains it has used.
var Site = &xfs.Site{
	ID:   "sendnow",
	Name: "Send.now",
	Host: "send.now",
	// One connection for a free download, as JDownloader's plugin opens.
	FreeConns: 1,
	Patterns: xfs.Patterns{
		Name: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`class="modal-title" id="qr"[^>]*>\s*([^<]+?)\s*</h\d>`),
			regexp.MustCompile(`data-feather="file"[^>]*>\s*</i>\s*([^<]+?)\s*</h\d>`),
		}, xfs.Classic.Name),
		Size: slices.Concat([]*regexp.Regexp{
			// The forum code ends in the size in bytes, with no unit.
			regexp.MustCompile(`\[URL=[^\]]+\][^\[]*? - \s*(\d+)()\[/URL\]`),
			regexp.MustCompile(`id="downloadbtn"[^>]*>(?:\s*<i[^>]*></i>)?\s*Download \[([\d.,]+)\s*([KMGT]?B)\]`),
		}, xfs.Classic.Size),
		Gone:  regexp.MustCompile(`>\s*The file you were looking for doesn|>\s*File Not Found\.?\s*<`),
		Limit: regexp.MustCompile(`>\s*You can download up to(?:\s|&nbsp;)*<strong>`),
	},
}
