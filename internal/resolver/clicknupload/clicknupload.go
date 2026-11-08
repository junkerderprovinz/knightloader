// Package clicknupload is Clicknupload on the XFileSharing client. It moves
// from domain to domain, writes the size in a panel of its own and opens the
// download from the second form's answer in a window.
//
// The patterns follow the ClicknuploadOrg plugin of JDownloader and the
// ClicknuploadCom plugin of pyLoad (both GPLv3).
package clicknupload

import (
	"regexp"
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Site is Clicknupload, under any of the domains it has used.
var Site = &xfs.Site{
	ID:           "clicknupload",
	Name:         "Clicknupload",
	Host:         "clicknupload.click",
	OwnLogin:     true,
	FreeConns:    1,
	PremiumConns: 5,
	Patterns: xfs.Patterns{
		Size: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`(?i)>\s*size\s*</span>\s*<span>\s*([\d.,]+)\s*([KMGT]?B)\s*<`),
		}, xfs.Classic.Size),
		Link: slices.Concat([]*regexp.Regexp{
			regexp.MustCompile(`(?i)onClick="window\.open\('(https?://[^']+)'\)`),
		}, xfs.Classic.Link),
	},
}
