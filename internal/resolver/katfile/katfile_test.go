package katfile

import (
	"embed"
	"io/fs"
	"net/url"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

//go:embed testdata/*.html
var testdata embed.FS

func pages(t *testing.T) fs.FS {
	sub, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://katfile.com/073x8xif2bc5":                  "https://katfile.com/073x8xif2bc5",
		"https://katfile.com/073x8xif2bc5/a.part1.rar.html": "https://katfile.com/073x8xif2bc5",
		"https://www.katfile.cloud/073X8XIF2BC5":            "https://katfile.com/073x8xif2bc5",
		"https://katfile.online/073x8xif2bc5":               "https://katfile.com/073x8xif2bc5",
		"http://katfile.co/073x8xif2bc5":                    "https://katfile.com/073x8xif2bc5",
		"https://katfile.com/?op=payments":                  "",
		"https://katfile.com/073x8xif2bc":                   "",
		"https://katfiles.com/073x8xif2bc5":                 "",
	} {
		got, ok := Site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestKatFilesOwnPagesPassTheHosterChecks(t *testing.T) {
	xfstest.CheckHoster(t, xfstest.Hoster{
		Site:         Site,
		Pages:        pages(t),
		LoginCaptcha: true,
		// The file page counts down before its free button goes in.
		Countdown: 31 * time.Second,
		// The account page writes the traffic as used of the day's.
		TrafficLeft: (51200 - 20480) << 20,
	})
}

func TestTheSecondFormCarriesADeviceID(t *testing.T) {
	f := xfs.Form{Op: "download2", Fields: url.Values{"op": {"download2"}}}
	Site.Adjust(&f, "")
	if id := f.Fields.Get("device_id"); len(id) != 36 {
		t.Errorf("device_id = %q, want a UUID", id)
	}
	f = xfs.Form{Op: "download1", Fields: url.Values{"op": {"download1"}}}
	Site.Adjust(&f, "")
	if f.Fields.Has("device_id") {
		t.Error("the first form got a device_id")
	}
}

func TestTheAccountPageWithItsPackageRowNamesThePlan(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	page := `<TR><TD>Premium Package 30 days, until <b>2026-12-01 10:00:00</b></TD></TR>` +
		`<span>Traffic available today</span> <small>1024.5 / 51200 MB</small>`
	got := Site.ReadAccount(page, now)
	if !got.Premium || got.Expires != time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC) {
		t.Errorf("ReadAccount = %+v, want premium until the package's end", got)
	}
	if got.TrafficMax != 51200<<20 || got.TrafficLeft != (51200<<20)-int64(1024.5*(1<<20)) {
		t.Errorf("traffic %d of %d, want what is left of 51200 MB", got.TrafficLeft, got.TrafficMax)
	}
}
