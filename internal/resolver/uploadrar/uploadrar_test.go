package uploadrar

import (
	"embed"
	"io/fs"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

//go:embed testdata/*.html
var testdata embed.FS

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://uploadrar.com/0wbd9tswzqph":            "https://uploadrar.com/0wbd9tswzqph",
		"https://uploadrar.com/0wbd9tswzqph/a.rar.html": "https://uploadrar.com/0wbd9tswzqph",
		"http://uploadrar.net/0WBD9TSWZQPH":             "https://uploadrar.com/0wbd9tswzqph",
		"https://cloud.getpczone.com/0wbd9tswzqph":      "https://uploadrar.com/0wbd9tswzqph",
		"https://uploadrar.com/register/":               "",
		"https://uploadrar.com/0wbd9tswzqp":             "",
		"https://getpczone.com/0wbd9tswzqph":            "",
	} {
		got, ok := Site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// The file page carries an advert labelled as the download, which the checks
// would hand on if it were taken for the link.
func TestUploadrarsOwnPagesPassTheHosterChecks(t *testing.T) {
	pages, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	xfstest.CheckHoster(t, xfstest.Hoster{Site: Site, Pages: pages, GoneStatus: http.StatusNotFound, TrafficLeft: 20480 << 20})
}
