package sendnow

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

//go:embed testdata
var testdata embed.FS

func pages(t *testing.T, dir string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(testdata, dir)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://send.now/nuazfxwh18g4":                     "https://send.now/nuazfxwh18g4",
		"https://send.cm/ce3zr4cp0i7k":                      "https://send.now/ce3zr4cp0i7k",
		"http://www.send.now/NUAZFXWH18G4/Wino.zip":         "https://send.now/nuazfxwh18g4",
		"https://tusfiles.com/nuazfxwh18g4":                 "https://send.now/nuazfxwh18g4",
		"https://userscloud.com/nuazfxwh18g4.html":          "https://send.now/nuazfxwh18g4",
		"https://send.now/d/abc":                            "",
		"https://send.now/s/folder":                         "",
		"https://dl9750.usercdn.com/d/nryutd7t6cosj4/a.mkv": "",
	} {
		got, ok := Site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// The file page holds the second form, names the file in a dialog and the
// size in bytes in its forum code.
func TestSendNowsOwnPagesPassTheHosterChecks(t *testing.T) {
	xfstest.CheckHoster(t, xfstest.Hoster{Site: Site, Pages: pages(t, "testdata"), GoneStatus: http.StatusNotFound})
}

func TestTheNameAndTheExactSizeComeFromTheFilePage(t *testing.T) {
	srv := xfstest.New(t, Site.Host, pages(t, "testdata"))
	link := srv.Add("nuazfxwh18g4", xfstest.File{Name: "Wino.Mail.CrashDumps.zip", Body: make([]byte, 101213)})
	got := xfs.Resolver{Site: Site, Client: xfs.NewClient(Site, srv.URL, srv.Client())}.Inspect(context.Background(), []string{link})
	if want := (resolver.LinkInfo{Availability: core.AvailOnline, Name: "Wino.Mail.CrashDumps.zip", Size: 101213}); !slices.Equal(got, []resolver.LinkInfo{want}) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
}

func TestAUsedUpFreeLimitIsSatOutBeforeTheDownload(t *testing.T) {
	srv := xfstest.New(t, Site.Host, pages(t, "testdata/limit"))
	link := srv.Add("nuazfxwh18g4", xfstest.File{Name: "a.zip", Body: make([]byte, 1024)})
	srv.Wait(1)
	got := make(chan string, 1)
	var waited time.Duration
	b := xfs.NewBackend(xfs.NewClient(Site, srv.URL, srv.Client()), engine{got}, func(string, core.Update) {})
	b.Sleep = func(ctx context.Context, d time.Duration) error {
		waited += d
		return ctx.Err()
	}

	b.Download("t1", link, nil, 1)
	select {
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was handed on")
	}
	if waited != xfs.LimitWait {
		t.Errorf("waited %s, want %s for the limit the page gives no time for", waited, xfs.LimitWait)
	}
}

type engine struct{ got chan string }

func (e engine) Handover(_, url string, _ int, _ func(context.Context) (string, error)) { e.got <- url }
func (e engine) HandoverOnce(_, url string, _ func(context.Context) (string, error))    { e.got <- url }
func (engine) Pause(string)                                                             {}
func (engine) Resume(string)                                                            {}
func (engine) Remove(string, bool)                                                      {}
