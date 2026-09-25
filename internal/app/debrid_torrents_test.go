package app

// Torrents through a debrid service: the priority card decides between the
// service and the built-in client, a refusal falls back to the next of them,
// the wait while the service fetches is no stall, and the files arrive through
// the engine under the task.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const debridMagnet = "magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef&dn=Show"

// torrentOrderApp has Real-Debrid take torrents beside the built-in client,
// with the queue halted so nothing reaches the swarm, and the order given.
func torrentOrderApp(t *testing.T, order []string) (*App, chan string) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.ResolverOrder = order
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 16)
	a.bmu.Lock()
	a.debrid["realdebrid"] = &routeSpy{name: "realdebrid", events: events}
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: "realdebrid", Prio: 48, Torrents: true})
	return a, events
}

func queueTorrent(a *App, task *core.Task) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
}

func TestAMagnetGoesToTheDebridServiceFirst(t *testing.T) {
	a, events := torrentOrderApp(t, []string{"realdebrid", "torrent"})

	if got := a.stagingResolverFor(debridMagnet).Info().ID; got != "realdebrid" {
		t.Errorf("a magnet is collected for %q, want the service the card puts first", got)
	}
	// Collected while the built-in client came first, so the recorded backend
	// is only the pick of that moment.
	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, Resolver: "torrent", Status: core.StatusQueued, Enabled: true})

	if got := settle(events); !got["realdebrid download m1"] {
		t.Errorf("the magnet did not go to Real-Debrid: %v", got)
	}
	if got := liveTask(a, "m1").Mode; got != core.ModeUnknown {
		t.Errorf("mode = %q; a torrent is neither a free nor a premium hoster download", got)
	}
}

func TestWithoutAnOrderTorrentsStayWithTheBuiltInClient(t *testing.T) {
	a, _ := torrentOrderApp(t, nil)
	if got := a.stagingResolverFor(debridMagnet).Info().ID; got != "torrent" {
		t.Errorf("with the automatic order a magnet goes to %q, want the built-in client as before", got)
	}
}

func TestAPinnedTorrentIgnoresTheOrder(t *testing.T) {
	a, events := torrentOrderApp(t, []string{"torrent", "realdebrid"})
	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, ResolverPin: "realdebrid", Status: core.StatusQueued, Enabled: true})
	if got := settle(events); !got["realdebrid download m1"] {
		t.Errorf("the pinned magnet did not go to Real-Debrid: %v", got)
	}
}

func TestATorrentTheServiceRefusesFallsBackToTheBuiltInClient(t *testing.T) {
	a, events := torrentOrderApp(t, []string{"realdebrid", "torrent"})
	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, Status: core.StatusQueued, Enabled: true})
	if got := settle(events); !got["realdebrid download m1"] {
		t.Fatalf("the magnet did not go to Real-Debrid first: %v", got)
	}
	// Halted, so the handover stops short of the swarm.
	a.SetHalted(true)

	a.onUpdate("m1", core.Update{
		Status: core.StatusError, Err: "realdebrid: torrent too big",
		Reason: core.ReasonUnsupported, Unsupported: true,
	})

	got := liveTask(a, "m1")
	if got.Resolver != "torrent" || got.Status != core.StatusQueued {
		t.Errorf("after the refusal the task is %s on %q, want queued for the built-in client", got.Status, got.Resolver)
	}
	if !settle(events)["realdebrid remove m1"] {
		t.Error("Real-Debrid was not told to let go of the task")
	}
	if !a.acctHealthTracker().Usable("realdebrid", "") {
		t.Error("a declined torrent benched the Real-Debrid account")
	}
}

func TestTheServiceFetchingATorrentIsNoStall(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	task := runningTask(a, "t1", 0)
	a.mu.Lock()
	task.Remote = &core.RemoteFetch{Progress: 0.1}
	a.mu.Unlock()

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(3 * time.Hour))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Fatalf("a torrent the service is still fetching was marked as standing still (%s)", got)
	}

	// Its files arriving here start the clock afresh.
	a.mu.Lock()
	task.Remote = nil
	a.mu.Unlock()
	a.stallPass(base.Add(3*time.Hour + 30*time.Second))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked 30s after the service finished (%s); the wait counted towards the timeout", got)
	}
}

func TestTheServiceProgressIsOnTheTaskOnlyWhileItFetches(t *testing.T) {
	a, _ := stallApp(t, func(*settings.Settings) {})
	runningTask(a, "t1", 0)

	a.onUpdate("t1", core.Update{Status: core.StatusRunning, Remote: &core.RemoteFetch{Progress: 0.4}})
	if r := liveTask(a, "t1").Remote; r == nil || r.Progress != 0.4 {
		t.Fatalf("remote = %+v, want the service's 40%%", r)
	}
	a.onUpdate("t1", core.Update{Status: core.StatusRunning, Loaded: 10})
	if r := liveTask(a, "t1").Remote; r != nil {
		t.Errorf("remote = %+v after the first bytes arrived here", r)
	}
}

// The stall watcher reconnects what the engine is fetching, which for a
// torrent a debrid service fetched is the file in flight, not the task.
func TestTheFileInFlightStandsInForItsTask(t *testing.T) {
	var p torrentParts
	if got := p.engineIDFor("t1"); got != "t1" {
		t.Fatalf("engine id = %q with nothing in flight, want the task's own", got)
	}
	var loaded int64
	w := p.watch("t1", "t1/2", func(n, _ int64, _ string) { loaded = n })
	if got := p.engineIDFor("t1"); got != "t1/2" {
		t.Errorf("engine id = %q, want the file in flight", got)
	}
	if !p.deliver("t1/2", core.Update{Status: core.StatusRunning, Loaded: 77}) || loaded != 77 {
		t.Errorf("a progress report for the file did not reach its fetch (loaded %d)", loaded)
	}
	p.deliver("t1/2", core.Update{Status: core.StatusDone, File: "/dl/x"})
	if r := <-w.done; r.file != "/dl/x" || r.err != nil {
		t.Errorf("the fetch ended with %+v", r)
	}
	p.forget("t1/2", w)
	if p.deliver("t1/2", core.Update{Status: core.StatusRunning}) {
		t.Error("a report for a file nobody waits for was taken instead of passed on")
	}
	if got := p.engineIDFor("t1"); got != "t1" {
		t.Errorf("engine id = %q after the file finished", got)
	}
}

// A rewire builds every backend anew, every six hours and on every account
// change, while a torrent the service has not cached can take longer.
func TestARewireLeavesTheTorrentsInFlightWithinReach(t *testing.T) {
	site := &websiteAccount{}
	a := openImportApp(t, t.TempDir(), site, false)
	t.Cleanup(func() { a.Close() })
	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the torrent on the service", func() bool { return liveTask(a, "m1").ServiceJob != nil })

	// What rewireBackends does for the account.
	a.bmu.Lock()
	a.debrid["fakedebrid"] = a.torrentsVia("fakedebrid", site, &routeSpy{name: "links", events: make(chan string, 16)}, engineHandoff{a.Engine, a})
	a.bmu.Unlock()
	a.Remove("m1", false)

	waitFor(t, "the job of the removed task deleted", func() bool { return slices.Equal(site.deletedJobs(), []string{"OWN1"}) })
}

// holdingSpy is a debrid backend that still holds a job for every task.
type holdingSpy struct{ routeSpy }

func (*holdingSpy) Holds(string) bool { return true }

func TestARetryOfADebridTorrentGoesBackToTheJobItHolds(t *testing.T) {
	a, events := torrentOrderApp(t, []string{"realdebrid", "torrent"})
	a.bmu.Lock()
	a.debrid["realdebrid"] = &holdingSpy{routeSpy{name: "realdebrid", events: events}}
	a.bmu.Unlock()
	a.mu.Lock()
	a.tasks["m1"] = &core.Task{
		ID: "m1", URL: debridMagnet, Resolver: "realdebrid", Status: core.StatusError, Enabled: true,
		InfoHash: "89abcdef0123456789abcdef0123456789abcdef", Loaded: 700,
		ServiceJob: &core.ServiceJob{Slot: "realdebrid", ID: "RD1", Owned: true, Done: 1},
	}
	a.mu.Unlock()

	a.RestartTasks([]string{"m1"})

	got := settle(events)
	if got["realdebrid remove m1"] {
		t.Error("the retry made Real-Debrid give up the job and the files already here")
	}
	if !got["realdebrid download m1"] {
		t.Errorf("the retry did not go back to Real-Debrid: %v", got)
	}
	if j := liveTask(a, "m1").ServiceJob; j == nil || j.ID != "RD1" || j.Done != 1 {
		t.Errorf("the task holds %+v after the retry, want the job it had", j)
	}
}

func TestARestartCarriesOnWithTheJobTheServiceHolds(t *testing.T) {
	dir := t.TempDir()
	site := &websiteAccount{}
	first := openImportApp(t, dir, site, false)
	stop := sync.OnceFunc(func() { first.Close() })
	t.Cleanup(stop)
	// The queue comes up stopped, so nothing starts before the account is
	// wired again.
	s := first.Settings.Get()
	s.ResumeOnStart = settings.ResumeNever
	if _, err := first.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	queueTorrent(first, &core.Task{ID: "m1", URL: debridMagnet, Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the job noted on the task", func() bool { return liveTask(first, "m1").ServiceJob != nil })
	stop()

	second := openImportApp(t, dir, site, false)
	t.Cleanup(func() { second.Close() })
	second.SetHalted(false)
	waitFor(t, "the torrent fetched again", func() bool { return liveTask(second, "m1").Remote != nil })
	time.Sleep(100 * time.Millisecond)
	if n := site.addCount(); n != 1 {
		t.Errorf("the torrent was added %d times; the restart carries on with the job the service holds", n)
	}

	second.Remove("m1", false)
	waitFor(t, "the job deleted with the task", func() bool { return slices.Contains(site.deletedJobs(), "OWN1") })
}

func TestAnImportedDownloadIsMarkedButNeverRestarted(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) {
		s.StallTimeout = 60
		s.StallRestart = true
	})
	task := runningTask(a, "t1", 4096)
	a.mu.Lock()
	task.URL = debrid.JobLink("stalled", "J1")
	a.mu.Unlock()

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(5 * time.Minute))

	got := liveTask(a, "t1")
	if got.StalledSince.IsZero() {
		t.Error("the stalled download was not marked")
	}
	if got.StallRestarts != 0 {
		t.Errorf("restarted %d times, which deletes every file of the download already here", got.StallRestarts)
	}
}

// FetchPart writes every file of an imported download to its destination, so
// that is where everything else has to look for it.
func TestAnImportedDownloadIsLookedForWhereItIsWritten(t *testing.T) {
	a := newQueueApp(t)
	s := a.Settings.Get()
	s.WorkDir = t.TempDir()
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	task := &core.Task{ID: "i1", URL: debrid.JobLink("realdebrid", "RD1"), Name: "Show", Resolver: "realdebrid"}
	if got, want := a.workDirFor(task), a.dirFor(task); got != want {
		t.Errorf("looked for in %s, written to %s", got, want)
	}
}

func TestAFileARestartCutShortIsFetchedAgainInItsPlace(t *testing.T) {
	if raceEnabled {
		// See TestAStalledDownloadIsReconnectedAndKeepsItsBytes.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	body := bytes.Repeat([]byte{'a'}, 2<<10)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	defer origin.Close()
	a := newQueueApp(t)
	dir := t.TempDir()
	a.mu.Lock()
	a.tasks["m1"] = &core.Task{ID: "m1", URL: debridMagnet, Dir: dir, Status: core.StatusRunning, Enabled: true}
	a.mu.Unlock()
	// What the library had preallocated when the process ended.
	left := filepath.Join(dir, "Show", "e01.mkv")
	if err := os.MkdirAll(filepath.Dir(left), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(left, make([]byte, len(body)), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := engineHandoff{a.Engine, a}.FetchPart(context.Background(), debrid.Part{
		TaskID: "m1", ID: "m1/0", URL: origin.URL + "/a", Path: "Show/e01.mkv", Size: int64(len(body)),
		Leftover: left, Progress: func(int64, int64, string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(got, left) {
		t.Errorf("written to %s, want %s in place of what the restart left", got, left)
	}
	if b, err := os.ReadFile(left); err != nil || !bytes.Equal(b, body) {
		t.Errorf("%s holds %d bytes of the wrong content, %v", left, len(b), err)
	}
}

// cachedTorrents is a debrid service that has the torrent cached: every file is
// ready at the first look and served by origin.
type cachedTorrents struct {
	origin string
	mu     sync.Mutex
	gone   []string
}

func (s *cachedTorrents) ID() string    { return "fakedebrid" }
func (s *cachedTorrents) Label() string { return "Fake debrid" }
func (s *cachedTorrents) AddTorrent(context.Context, debrid.TorrentSource) (string, bool, error) {
	return "J1", false, nil
}
func (s *cachedTorrents) TorrentStatus(context.Context, string) (debrid.TorrentJob, error) {
	return debrid.TorrentJob{Name: "Show", Size: 3 << 10, State: debrid.TorrentReady, Files: []debrid.TorrentFile{
		{ID: "a", Path: "Show/e01.mkv", Size: 2 << 10, Held: true},
		{ID: "b", Path: "Show/extras/sample.mkv", Size: 1 << 10, Held: true},
	}}, nil
}
func (s *cachedTorrents) FileURL(_ context.Context, _ string, f debrid.TorrentFile) (debrid.Direct, error) {
	return debrid.Direct{URL: s.origin + "/" + f.ID}, nil
}
func (s *cachedTorrents) DeleteTorrent(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gone = append(s.gone, id)
	return nil
}

func TestADebridTorrentArrivesThroughTheEngineUnderOneTask(t *testing.T) {
	if raceEnabled {
		// See TestAStalledDownloadIsReconnectedAndKeepsItsBytes.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	body := map[string][]byte{"a": bytes.Repeat([]byte{'a'}, 2<<10), "b": bytes.Repeat([]byte{'b'}, 1<<10)}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A CDN link names no file, as TorBox's do, so the name has to come
		// from the torrent.
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body[filepath.Base(r.URL.Path)]))
	}))
	defer origin.Close()

	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	svc := &cachedTorrents{origin: origin.URL}
	a.bmu.Lock()
	a.debrid["fakedebrid"] = a.torrentsVia("fakedebrid", svc, &routeSpy{name: "links", events: make(chan string, 4)}, engineHandoff{a.Engine, a})
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: "fakedebrid", Prio: 90, Torrents: true})

	dir := t.TempDir()
	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, Dir: dir, Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the torrent finishing", func() bool { return liveTask(a, "m1").Status == core.StatusDone })

	got := liveTask(a, "m1")
	if got.Resolver != "fakedebrid" || got.Name != "Show" || got.Loaded != 3<<10 {
		t.Errorf("finished as %q on %q with %d bytes", got.Name, got.Resolver, got.Loaded)
	}
	for rel, want := range map[string][]byte{"Show/e01.mkv": body["a"], "Show/extras/sample.mkv": body["b"]} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s: %d bytes, %v", rel, len(b), err)
		}
	}
	waitFor(t, "the torrent deleted on the service", func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return len(svc.gone) == 1
	})
}

// A debrid torrent's files are chosen by the file rules of its category, as
// the built-in client's are. A download imported from an account was not
// handed to this instance as a torrent, and keeps what the account holds.
func TestADebridTorrentGoesByItsCategorysFileRules(t *testing.T) {
	a := newQueueApp(t)
	s := a.Settings.Get()
	s.Torrent.ExcludeFiles = []string{"(?i)sample"}
	s.Categories = []settings.Category{{ID: "music", Name: "Music", TorrentFiles: &settings.TorrentFileRules{MinFileSize: 1 << 20}}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	putTask(t, a, core.Task{ID: "films", URL: debridMagnet, Status: core.StatusCollected})
	putTask(t, a, core.Task{ID: "album", URL: debridMagnet, Category: "music", Status: core.StatusCollected})
	putTask(t, a, core.Task{ID: "imported", URL: debrid.JobLink("fakedebrid", "J7"), Status: core.StatusCollected})

	for _, c := range []struct {
		task, path string
		size       int64
		want       bool
	}{
		{"films", "e01.mkv", 700, true},
		{"films", "Extras/Sample.mkv", 700, false},
		{"album", "Extras/Sample.mkv", 2 << 20, true},
		{"album", "cover.jpg", 40 << 10, false},
	} {
		keep, err := a.torrentRules(c.task)
		if err != nil || keep == nil {
			t.Fatalf("%s has no file rules: %v", c.task, err)
		}
		if got := keep(c.path, c.size); got != c.want {
			t.Errorf("%s keeps %s: %v, want %v", c.task, c.path, got, c.want)
		}
	}
	if keep, err := a.torrentRules("imported"); keep != nil || err != nil {
		t.Errorf("an imported download goes by file rules (%v)", err)
	}
}
