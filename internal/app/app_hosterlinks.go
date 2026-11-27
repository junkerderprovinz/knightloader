package app

import (
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share"
	"github.com/junkerderprovinz/knightloader/internal/resolver/krakenfiles"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mega"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onefichier"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator"
	"github.com/junkerderprovinz/knightloader/internal/resolver/workupload"
)

// hosterLinks are the hosters' own spellings of a file link, each reducing
// any form the hoster hands out to the one its file id decides. Nothing here
// asks the hoster, so the list and the history can tell one file from two by
// the address alone, whatever the mirror policy says.
var hosterLinks = func() []func(string) (string, bool) {
	out := []func(string) (string, bool){
		onefichier.Normalize, rapidgator.Normalize, nitroflare.Normalize, filer.Normalize,
		gofile.Normalize, pixeldrain.Normalize, mediafire.Normalize, workupload.Normalize,
		krakenfiles.Normalize, megaLink,
	}
	for _, h := range keep2share.Hosts {
		out = append(out, h.Normalize)
	}
	for _, s := range xfsSites {
		out = append(out, s.Normalize)
	}
	return out
}()

// megaLink spells a MEGA link the way MEGA hands it out today, which keeps
// the key: a file under another key is not one somebody can fetch.
func megaLink(raw string) (string, bool) {
	l, err := mega.Parse(raw)
	if err != nil {
		return "", false
	}
	return l.String(), true
}

// hosterLink is u as its hoster's own file link, or u where no hoster claims
// it.
func hosterLink(u string) string {
	for _, normalize := range hosterLinks {
		if n, ok := normalize(u); ok {
			return n
		}
	}
	return u
}
