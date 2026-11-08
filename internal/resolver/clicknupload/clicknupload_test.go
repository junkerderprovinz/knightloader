package clicknupload

import (
	"embed"
	"io/fs"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

//go:embed testdata/*.html
var testdata embed.FS

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://clicknupload.click/0hhlflto0sva":            "https://clicknupload.click/0hhlflto0sva",
		"https://clicknupload.co/ses53quzv5rb":               "https://clicknupload.click/ses53quzv5rb",
		"https://clicknupload.org/ses53quzv5rb/foo.txt.html": "https://clicknupload.click/ses53quzv5rb",
		"https://www.clicknupload.name/4PAV6577ESOE":         "https://clicknupload.click/4pav6577esoe",
		"https://clickndownload.org/4pav6577esoe":            "https://clicknupload.click/4pav6577esoe",
		"https://clicknupload.click/?op=login":               "",
		"https://clicknupload.click/0hhlflto0sv":             "",
		"https://clicknuploads.com/0hhlflto0sva":             "",
	} {
		got, ok := Site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestClicknuploadsOwnPagesPassTheHosterChecks(t *testing.T) {
	pages, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	xfstest.CheckHoster(t, xfstest.Hoster{Site: Site, Pages: pages, TrafficLeft: 20480 << 20})
}

func TestTheLinkTheSecondFormOpensInAWindowIsTheDownload(t *testing.T) {
	page := `<button onClick="window.open('https://s3.clicknupload.click:8080/download/aGVsbG8/a.bin');">Download</button>`
	m := Site.Patterns.Link[0].FindStringSubmatch(page)
	if m == nil || m[1] != "https://s3.clicknupload.click:8080/download/aGVsbG8/a.bin" {
		t.Errorf("the link pattern found %q", m)
	}
}
