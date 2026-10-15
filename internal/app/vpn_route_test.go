package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// exitURL is reached through the tunnel only, and answers vpntest.ExitAddr.
var exitURL = "http://" + vpntest.ServerAddr.String() + "/exit.txt"

// vpnApp is an app with a VPN up and the given switches on.
func vpnApp(t *testing.T, routes vpnconn.Routes) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.MaxConcurrent, s.MaxPerHost = 4, 4 })
	connectedVPN(t, a)
	a.VPN.SetRoutes(routes)
	return a
}

// fetchedThroughTheTunnel checks that file holds what only the tunnel's
// far end answers.
func fetchedThroughTheTunnel(t *testing.T, file string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil || string(b) != vpntest.ExitAddr {
		t.Errorf("%s holds %q, %v; want the answer from the far end of the tunnel", file, b, err)
	}
}

func skipRealTransfersUnderRace(t *testing.T) {
	t.Helper()
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
}

// realDebrid unlocks every link to url under the id of a service of the
// debrid group, so its downloads fall under the Debrid switch.
type realDebrid struct{ url string }

func (realDebrid) ID() string    { return "realdebrid" }
func (realDebrid) Label() string { return "Real-Debrid" }

func (realDebrid) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{"hoster.example": true}, nil
}

func (s realDebrid) Unlock(context.Context, string) (debrid.Direct, error) {
	return debrid.Direct{URL: s.url, Name: "exit.txt"}, nil
}

func TestADebridHandoverGoesThroughTheTunnel(t *testing.T) {
	skipRealTransfersUnderRace(t)
	a := vpnApp(t, vpnconn.Routes{Debrid: true})
	svc := realDebrid{url: exitURL}
	a.bmu.Lock()
	a.debrid[svc.ID()] = debrid.NewBackend(svc, engineHandoff{a.Engine, a}, debrid.NewHandovers(), a.onUpdate)
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: svc.ID(), Prio: 90, Hosts: map[string]bool{"hoster.example": true}, Svc: svc})

	queueTask(a, &core.Task{ID: "d1", URL: "https://hoster.example/file/abc", Resolver: svc.ID(), Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the download finishing", func() bool { return liveTask(a, "d1").Status == core.StatusDone })

	live := liveTask(a, "d1")
	fetchedThroughTheTunnel(t, live.File)
	if live.Connection != vpnRouteID {
		t.Errorf("the task records connection %q, want %q", live.Connection, vpnRouteID)
	}
}

// handedOver puts a task in the list as dispatch leaves one it handed to a
// backend.
func handedOver(a *App, task core.Task) {
	task.Status, task.Enabled = core.StatusRunning, true
	a.mu.Lock()
	a.tasks[task.ID] = &task
	a.active[task.ID], a.started[task.ID] = true, true
	a.mu.Unlock()
}

func TestAFileAUsenetServiceFetchedGoesThroughTheTunnel(t *testing.T) {
	skipRealTransfersUnderRace(t)
	a := vpnApp(t, vpnconn.Routes{Usenet: true})
	handedOver(a, core.Task{ID: "u1", URL: "nzb://job/1", Resolver: usenet.ResolverID, Dir: t.TempDir()})

	engineHandoff{a.Engine, a}.Handover("u1", exitURL, 1, nil)
	waitFor(t, "the download finishing", func() bool { return liveTask(a, "u1").Status == core.StatusDone })

	fetchedThroughTheTunnel(t, liveTask(a, "u1").File)
}

func TestAFileOfADebridTorrentGoesThroughTheTunnel(t *testing.T) {
	skipRealTransfersUnderRace(t)
	a := vpnApp(t, vpnconn.Routes{Debrid: true})
	handedOver(a, core.Task{ID: "m1", URL: debridMagnet, Resolver: "realdebrid", Dir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	file, err := engineHandoff{a.Engine, a}.FetchPart(ctx, debrid.Part{
		TaskID: "m1", ID: "m1/0", URL: exitURL, Path: "Show/exit.txt", Progress: func(int64, int64, string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	fetchedThroughTheTunnel(t, file)
}

func TestTheLinkCheckGoesThroughTheTunnelWithHostersOn(t *testing.T) {
	a := newAccountsTestApp(t)
	connectedVPN(t, a)
	a.VPN.SetRoutes(vpnconn.Routes{Hosters: true})

	req, _ := http.NewRequest(http.MethodGet, exitURL, nil)
	resp, err := a.Probe.Do(req)
	if err != nil {
		t.Fatalf("the probe did not reach the far end of the tunnel: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != vpntest.ExitAddr {
		t.Errorf("the probe was answered %q", b)
	}
}

func TestEachSwitchCarriesTheAppsOwnRequestsOfItsKind(t *testing.T) {
	a := newAccountsTestApp(t)
	connectedVPN(t, a)
	a.VPN.SetRoutes(vpnconn.Routes{Hosters: true, Debrid: true, Ytdlp: true, Usenet: true})
	for tr, class := range vpnClasses {
		c := httpx.New(httpx.Options{Class: class, Timeout: 15 * time.Second})
		resp, err := c.Get(exitURL)
		if err != nil {
			t.Errorf("%s: %v", tr, err)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != vpntest.ExitAddr {
			t.Errorf("%s was answered %q", tr, b)
		}
	}
}

// slowFile serves size bytes slowly, in ranges, and counts the requests and
// the transfers still open.
type slowFile struct {
	size           int
	requests, open atomic.Int32
}

func (f *slowFile) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	f.open.Add(1)
	defer f.open.Add(-1)
	http.ServeContent(w, r, "big.bin", time.Time{}, &slowReader{bytes.NewReader(make([]byte, f.size))})
}

type slowReader struct{ *bytes.Reader }

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return s.Reader.Read(p[:min(len(p), 4<<10)])
}

// startDirect starts a direct download from f on a, with every switch off, and
// waits until bytes arrive.
func startDirect(t *testing.T, a *App, f *slowFile) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	queueTask(a, &core.Task{ID: "h1", URL: srv.URL + "/big.bin", Resolver: "direct", Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the download receiving bytes", func() bool { return liveTask(a, "h1").Loaded > 0 })
	if c := liveTask(a, "h1").Connection; c != "" {
		t.Fatalf("with every switch off the download records connection %q", c)
	}
}

// leftTheDirectRoute checks that the download records the tunnel and no
// longer reaches f, which only the machine's own connection leads to.
func leftTheDirectRoute(t *testing.T, a *App, f *slowFile) {
	t.Helper()
	waitFor(t, "the download moving into the tunnel", func() bool { return liveTask(a, "h1").Connection == vpnRouteID })
	waitFor(t, "the direct transfers ending", func() bool { return f.open.Load() == 0 })
	before := f.requests.Load()
	time.Sleep(time.Second)
	if n := f.requests.Load() - before; n > 0 {
		t.Errorf("%d requests reached the server directly after Hosters went through the VPN", n)
	}
}

func TestARunningDownloadMovesIntoTheTunnelWhenItsSwitchIsTurnedOn(t *testing.T) {
	skipRealTransfersUnderRace(t)
	a := vpnApp(t, vpnconn.Routes{})
	f := &slowFile{size: 16 << 20}
	startDirect(t, a, f)

	a.VPN.SetRoutes(vpnconn.Routes{Hosters: true})

	leftTheDirectRoute(t, a, f)
}

func TestAPausedDownloadResumesInTheTunnelWhenItsSwitchWasTurnedOn(t *testing.T) {
	skipRealTransfersUnderRace(t)
	a := vpnApp(t, vpnconn.Routes{})
	f := &slowFile{size: 16 << 20}
	startDirect(t, a, f)
	a.Pause("h1")
	waitFor(t, "the transfer pausing", func() bool { return f.open.Load() == 0 })
	before := f.requests.Load()

	a.VPN.SetRoutes(vpnconn.Routes{Hosters: true})
	a.Resume("h1")

	leftTheDirectRoute(t, a, f)
	if n := f.requests.Load() - before; n > 0 {
		t.Errorf("%d requests reached the server directly on the resume", n)
	}
}

func TestUsenetArticlesStopReusingDirectConnectionsOnceUsenetGoesThroughTheVPN(t *testing.T) {
	s := nntptest.New(t)
	for i, id := range []string{"a@test", "b@test"} {
		s.AddPart(id, yenc.Part{Name: "f.bin", FileSize: 20, Number: i + 1, Total: 2, Begin: int64(i * 10), Data: bytes.Repeat([]byte{'x'}, 10)})
	}
	a, _ := newRuleApp(t, func(set *settings.Settings, _ string) {
		set.UsenetServers = []settings.UsenetServer{{ID: "own", Host: s.Host, Port: s.Port, Connections: 1, Enabled: true}}
	})
	connectedVPN(t, a)
	c := a.nntpClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Fetch(ctx, "a@test", time.Time{}); err != nil {
		t.Fatal(err)
	}

	a.VPN.SetRoutes(vpnconn.Routes{Usenet: true})

	// The server listens on this machine, which the tunnel does not lead to.
	short, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	_, err := c.Fetch(short, "b@test", time.Time{})
	if err == nil || s.Bodies("b@test") > 0 {
		t.Errorf("the next article was fetched directly (%v); want it to go through the tunnel", err)
	}
}

// driveAccount is a debrid account the drive shows, holding one file whose
// link leads to the far end of the tunnel.
type driveAccount struct{}

func (driveAccount) ID() string    { return "realdebrid" }
func (driveAccount) Label() string { return "Real-Debrid" }

func (driveAccount) AddTorrent(context.Context, debrid.TorrentSource) (string, bool, error) {
	return "", false, nil
}

func (driveAccount) TorrentStatus(context.Context, string) (debrid.TorrentJob, error) {
	return debrid.TorrentJob{Name: "Exit", State: debrid.TorrentReady, Files: []debrid.TorrentFile{
		{ID: "f", Path: "Exit/exit.txt", Size: int64(len(vpntest.ExitAddr)), Held: true},
	}}, nil
}

func (driveAccount) FileURL(context.Context, string, debrid.TorrentFile) (debrid.Direct, error) {
	return debrid.Direct{URL: exitURL, Size: int64(len(vpntest.ExitAddr))}, nil
}

func (driveAccount) DeleteTorrent(context.Context, string) error { return nil }

func (driveAccount) List(context.Context) ([]debrid.Listed, bool, error) {
	return []debrid.Listed{{ID: "1", Name: "Exit", Size: int64(len(vpntest.ExitAddr))}}, true, nil
}

func TestTheDebridDriveReadsThroughTheTunnelWithDebridOn(t *testing.T) {
	a := newAccountsTestApp(t)
	connectedVPN(t, a)
	a.VPN.SetRoutes(vpnconn.Routes{Debrid: true})
	a.bmu.Lock()
	a.debrid["realdebrid"] = a.torrentsVia("realdebrid", driveAccount{}, &routeSpy{name: "links", events: make(chan string, 4)}, engineHandoff{a.Engine, a})
	a.bmu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.DebridDrive.Serve(w, r, "/dav")
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/dav/Real-Debrid/Exit/exit.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != vpntest.ExitAddr {
		t.Errorf("the drive read %s %q; want the answer from the far end of the tunnel", resp.Status, b)
	}
}
