package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A hoster hands one file out under several links: an alias domain, the file
// name in the path, a referral parameter, an older link form. Each pair is the
// same file, the second spelled the way another site or forum post spells it.
var sameHosterFile = []struct {
	hoster, first, again string
}{
	{"1fichier", "https://1fichier.com/?abc123def4&af=12345", "https://1fichier.com/?abc123def4"},
	{"1fichier alias", "https://1fichier.com/?abc123def4", "https://megadl.fr/?abc123def4"},
	{"rapidgator", "https://rapidgator.net/file/0123456789abcdef0123456789abcdef/Film.mkv.html", "https://rg.to/file/0123456789abcdef0123456789abcdef"},
	{"nitroflare", "https://nitroflare.com/view/ABC123DEF456/Film.mkv", "https://nitro.download/view/ABC123DEF456"},
	{"keep2share", "https://k2s.cc/file/abcdef1234567/Film.mkv", "https://keep2share.cc/file/abcdef1234567"},
	{"ddownload", "https://ddownload.com/k3m9x2p7q1ab/Film.mkv.html", "https://ddl.to/k3m9x2p7q1ab"},
	{"katfile", "https://katfile.com/k3m9x2p7q1ab/Film.mkv.html", "https://katfile.cloud/k3m9x2p7q1ab"},
	{"pixeldrain", "https://pixeldrain.com/u/iniGVSnw", "https://pixeldrain.com/api/file/iniGVSnw"},
	{"gofile", "https://gofile.io/d/AMZyDw", "https://gofile.io/?c=AMZyDw"},
	{"mega", "https://mega.nz/file/AbCdEfGh#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "https://mega.nz/#!AbCdEfGh!AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	{"mediafire", "https://www.mediafire.com/file/a1b2c3d4e5f6g7h/build.zip/file", "https://mediafire.com/?a1b2c3d4e5f6g7h"},
	{"workupload", "https://workupload.com/file/TUcAcRxAyzq", "https://workupload.com/start/TUcAcRxAyzq"},
	{"krakenfiles", "https://krakenfiles.com/view/DP3nGKJNsX/file.html", "https://krakenfiles.com/embed-video/DP3nGKJNsX"},
	{"filer", "https://filer.net/get/abcdefghijklmnop", "https://filer.net/dl/abcdefghijklmnop"},
}

// The mirror policy is off, so neither name nor size can tell, only the
// hoster's own file id.
func noMirrors(s *settings.Settings) { s.MirrorPolicy = string(dedupe.PolicyOff) }

func TestTheSameHosterFileUnderAnotherLinkIsAlreadyInTheList(t *testing.T) {
	for _, c := range sameHosterFile {
		t.Run(c.hoster, func(t *testing.T) {
			a := historyApp(t, noMirrors)
			onlyTask(t, a.AddLinks([]string{c.first}, ""))
			if again := a.AddLinks([]string{c.again}, ""); len(again) != 0 {
				t.Errorf("%s was staged beside %s, want it folded into the row the list has", c.again, c.first)
			}
		})
	}
}

func TestTheSameHosterFileUnderAnotherLinkIsRejectedAsDownloaded(t *testing.T) {
	for _, c := range sameHosterFile {
		t.Run(c.hoster, func(t *testing.T) {
			a := historyApp(t, noMirrors)
			downloadedBefore(t, a, "old", c.first, "Film.mkv", 4096)
			got := onlyTask(t, a.AddLinks([]string{c.again}, ""))
			if !got.Skipped || got.SkipCode != skipDownloaded {
				t.Errorf("%s was staged (skipped=%v, code=%q), want it rejected as the download of %s", c.again, got.Skipped, got.SkipCode, c.first)
			}
		})
	}
}

func TestTwoFilesOfOneHosterStayTwo(t *testing.T) {
	a := historyApp(t, noMirrors)
	onlyTask(t, a.AddLinks([]string{"https://rapidgator.net/file/0123456789abcdef0123456789abcdef/Film.mkv.html"}, ""))
	if again := a.AddLinks([]string{"https://rapidgator.net/file/fedcba9876543210fedcba9876543210/Film.mkv.html"}, ""); len(again) != 1 {
		t.Errorf("another rapidgator file was folded away (%d staged), want it staged", len(again))
	}
}
