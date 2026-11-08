package filespace

import (
	"embed"
	"io/fs"
	"net/url"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

//go:embed testdata/*.html
var testdata embed.FS

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://filespace.com/008mgmx9pwl9":                    "https://filespace.com/008mgmx9pwl9",
		"http://www.filespace.com/b74bjn2rttyz/a-file.exe.html": "https://filespace.com/b74bjn2rttyz",
		"https://spaceforfiles.com/008MGMX9PWL9":                "https://filespace.com/008mgmx9pwl9",
		"https://filespace.com/fd/008mgmx9pwl9":                 "",
		"https://filespace.com/login.html":                      "",
		"https://file-space.org/008mgmx9pwl9":                   "",
	} {
		got, ok := Site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestFileSpacesOwnPagesPassTheHosterChecks(t *testing.T) {
	pages, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	xfstest.CheckHoster(t, xfstest.Hoster{Site: Site, Pages: pages, TrafficLeft: 20480 << 20})
}

func TestTheFreeSecondFormGoesToTheFreeAccountTypeWhenThePageSaysSo(t *testing.T) {
	f := xfs.Form{Op: "download2", Action: "/fd/008mgmx9pwl9", Fields: url.Values{}}
	Site.Adjust(&f, `<script>var u = "/?accounttype=free";</script>`)
	if f.Action != "/fd/008mgmx9pwl9?accounttype=free" {
		t.Errorf("the form goes to %q", f.Action)
	}
	f = xfs.Form{Op: "download2", Action: "/fd/008mgmx9pwl9", Fields: url.Values{}}
	Site.Adjust(&f, "")
	if f.Action != "/fd/008mgmx9pwl9" {
		t.Errorf("a page that does not ask for it still sends the form to %q", f.Action)
	}
}

func TestTheCountdownInItsOwnSpanIsRead(t *testing.T) {
	page := `<div>Please wait <span id="x4kq9s" style="color:#c00">45</span> seconds</div>`
	if m := Site.Patterns.Countdown[0].FindStringSubmatch(page); m == nil || m[1] != "45" {
		t.Errorf("the countdown pattern found %q", m)
	}
}
