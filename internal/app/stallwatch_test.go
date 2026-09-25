package app

// The stall watcher in app_stallwatch.go. stallPass takes the clock as an
// argument, so a twenty-minute silence plays out in a millisecond and the
// assertions are about the rule rather than about how long the test ran.

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stallHost is the one host every task in this file is on. It has a resolver
// and a backend of its own (below), so a restart re-dispatches into a fake
// instead of opening a socket to somebody's server.
const stallHost = "stalled.example"

// hostResolver matches every link on one host, where hostcap_test.go's
// hostCapResolver matches a single fixed URL. These tests need several links on
// one host to exercise a per-host limit.
type hostResolver struct {
	id   string
	host string
}

func (r hostResolver) Info() resolver.Info { return resolver.Info{ID: r.id, Prio: 90} }
func (r hostResolver) Match(raw string) bool {
	return strings.HasPrefix(raw, "https://"+r.host+"/")
}
func (hostResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// stallApp wires that resolver to a capBackend (hostcap_test.go), so a restart
// lands somewhere countable and nothing in this file reaches the network.
func stallApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be := &capBackend{got: make(chan int, 8)}
	a.bmu.Lock()
	a.debrid["stalled"] = be
	a.bmu.Unlock()
	a.Registry.Register(hostResolver{id: "stalled", host: stallHost})
	return a, be
}

// runningTask puts one transfer in flight, exactly the way the dispatcher
// would have left it: in a.active, started, and reporting bytes.
func runningTask(a *App, id string, loaded int64) *core.Task {
	task := &core.Task{
		ID: id, URL: "https://" + stallHost + "/" + id + ".bin", Name: id + ".bin",
		Resolver: "stalled", Status: core.StatusRunning, Enabled: true, Loaded: loaded,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.active[id] = true
	a.started[id] = true
	a.mu.Unlock()
	return task
}

// The row counts up from the moment the bytes stopped rather than the moment
// the watcher noticed, or a stall found after a night reads as five seconds.
func TestAStandingStillTransferIsMarkedWithWhenItStopped(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 120 })
	runningTask(a, "t1", 4096)
	base := time.Now()

	a.stallPass(base)
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Fatalf("marked on the very first look, with nothing to compare against yet (%s)", got)
	}
	a.stallPass(base.Add(119 * time.Second))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked after 119s with a 120s timeout (%s)", got)
	}
	a.stallPass(base.Add(121 * time.Second))
	got := liveTask(a, "t1").StalledSince
	if got.IsZero() {
		t.Fatal("a transfer that has moved no bytes for over two minutes is not marked at all")
	}
	if !got.Equal(base) {
		t.Errorf("StalledSince = %s, want %s, the moment the bytes stopped", got, base)
	}
}

// The mark is recomputed rather than remembered, as core.Waiting is, so a
// connection that comes back on its own clears its own warning.
func TestBytesAgainTakeTheMarkOff(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 1000)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(90 * time.Second))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	a.mu.Lock()
	a.tasks["t1"].Loaded = 2000
	a.mu.Unlock()
	a.stallPass(base.Add(95 * time.Second))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("still marked at %s after the bytes started moving again", got)
	}
}

// A download sitting on a captcha moves no bytes for as long as it takes a
// human to answer, and it is the healthiest row in the queue. The wait also
// resets the clock rather than merely being ignored: otherwise the ten minutes
// spent away from the keyboard stay in the record and the row is marked the
// instant they answer.
func TestACaptchaWaitIsNotAStall(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 512)
	a.captchaStateFor().store.Sync([]captcha.Challenge{
		{ID: "c1", Host: stallHost, TaskID: "t1", Kind: captcha.KindImage},
	})

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(10 * time.Minute))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Fatalf("a link waiting for a human was marked as standing still (%s)", got)
	}

	// Answered: the challenge leaves the store, and the ten minutes it took do
	// not count towards the timeout.
	a.captchaStateFor().store.Sync(nil)
	a.stallPass(base.Add(10*time.Minute + 30*time.Second))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked 30s after the captcha was answered (%s); the wait counted towards the timeout", got)
	}
}

// Zero is the off switch, not a very short timeout.
func TestNothingIsMarkedWhileTheTimeoutIsOff(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 0 })
	runningTask(a, "t1", 77)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(48 * time.Hour))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked at %s on an install with the feature switched off", got)
	}
}

// A reading left on a row after the watcher is switched off would sit there
// with nothing left to update it.
func TestSwitchingTheWatcherOffTakesBackWhatItWrote(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 10)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	cfg := a.Settings.Get()
	cfg.StallTimeout = 0
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	a.stallPass(base.Add(3 * time.Minute))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("the mark survived the feature being switched off (%s)", got)
	}
}

// RestartTasks takes a task out of the running set and puts it back in the
// queue without knowing this mark exists, so the watcher walks what it has
// marked rather than only what is running.
func TestAMarkComesOffATaskThatStoppedRunning(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	// What a restart from elsewhere leaves behind: out of the running set, back
	// in the queue, mark untouched.
	a.mu.Lock()
	delete(a.active, "t1")
	a.tasks["t1"].Status = core.StatusQueued
	a.mu.Unlock()

	a.stallPass(base.Add(3 * time.Minute))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("a queued task still says it has been standing still since %s", got)
	}
}

// The watcher would clear the mark within a tick, but the answer to the button
// somebody just pressed has to carry the right row already.
func TestPausingAStalledTransferAnswersImmediately(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	a.Pause("t1")

	got := liveTask(a, "t1")
	if got.Status != core.StatusPaused {
		t.Fatalf("status = %q, want paused", got.Status)
	}
	if !got.StalledSince.IsZero() {
		t.Errorf("a paused row still claims to be standing still since %s", got.StalledSince)
	}
}

// Seeing a stall costs nothing, while restarting throws away the bytes the
// attempt did fetch, so the mark never implies the restart.
func TestTheMarkAloneRestartsNothing(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(5 * time.Minute))

	got := liveTask(a, "t1")
	if got.StalledSince.IsZero() {
		t.Fatal("not marked at all")
	}
	if got.StallRestarts != 0 {
		t.Errorf("restarted %d times with the restart switch off", got.StallRestarts)
	}
	if got.Status != core.StatusRunning {
		t.Errorf("status = %q, want the transfer left exactly where it was", got.Status)
	}
}

// The opt-in restart and its ceiling are one rule: the restart happens, it is
// counted on the task, and at the cap it stops. Without the cap this loops
// against a refusing host, spending a queue slot and the last attempt's bytes.
func TestAutomaticRestartIsCounted(t *testing.T) {
	a, be := stallApp(t, func(s *settings.Settings) {
		s.StallTimeout = 60
		s.StallRestart = true
		s.StallMaxRestarts = 1
	})
	runningTask(a, "t1", 4096)

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))

	select {
	case <-be.got:
	case <-time.After(2 * time.Second):
		t.Fatal("the backend was never handed the link again")
	}
	after := liveTask(a, "t1")
	if after.StallRestarts != 1 {
		t.Fatalf("StallRestarts = %d, want 1", after.StallRestarts)
	}
	if !after.StalledSince.IsZero() {
		t.Errorf("still marked as standing still after being started again (%s)", after.StalledSince)
	}

	// Stalled a second time, with the cap already reached: marked again, and
	// left alone.
	a.mu.Lock()
	a.tasks["t1"].Status = core.StatusRunning
	a.tasks["t1"].Loaded = 0
	a.mu.Unlock()
	later := base.Add(10 * time.Minute)
	a.stallPass(later)
	a.stallPass(later.Add(2 * time.Minute))

	final := liveTask(a, "t1")
	if final.StalledSince.IsZero() {
		t.Error("the second standstill was not marked; the cap must stop the restart, not the mark")
	}
	if final.StallRestarts != 1 {
		t.Errorf("StallRestarts = %d, want it held at the configured cap of 1", final.StallRestarts)
	}
	select {
	case <-be.got:
		t.Error("restarted a second time past the cap")
	case <-time.After(200 * time.Millisecond):
	}
}

// flakyOrigin serves one file with ranges until it goes quiet. Quiet, it cuts
// off every body under way and holds each new request without an answer, as an
// upstream that has stopped talking does. After heal it answers new requests
// again, while those it is already holding stay unanswered.
type flakyOrigin struct {
	srv  *httptest.Server
	data []byte
	stop chan struct{}

	mu     sync.Mutex
	quiet  bool
	healed bool
	// last is when the latest request came in, bodies how many answers are
	// being written right now.
	last   time.Time
	bodies int
	// afterHeal is the Range header of every request answered after heal.
	afterHeal []string
}

func newFlakyOrigin(t *testing.T, size int) *flakyOrigin {
	t.Helper()
	o := &flakyOrigin{data: make([]byte, size), stop: make(chan struct{})}
	_, _ = cryptorand.Read(o.data)
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(func() {
		close(o.stop)
		o.srv.Close()
	})
	return o
}

func (o *flakyOrigin) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.last = time.Now()
	held := o.quiet && !o.healed
	if !held {
		o.bodies++
		if o.healed {
			o.afterHeal = append(o.afterHeal, r.Header.Get("Range"))
		}
	}
	o.mu.Unlock()
	if held {
		select {
		case <-r.Context().Done():
		case <-o.stop:
		}
		return
	}
	defer func() {
		o.mu.Lock()
		o.bodies--
		o.mu.Unlock()
	}()

	lo, hi := 0, len(o.data)-1
	if rg, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		from, to, _ := strings.Cut(rg, "-")
		lo, _ = strconv.Atoi(from)
		if to != "" {
			hi, _ = strconv.Atoi(to)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi, len(o.data)))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(hi-lo+1))
	if r.Header.Get("Range") != "" {
		w.WriteHeader(http.StatusPartialContent)
	}
	for off := lo; off <= hi; off += 32 << 10 {
		o.mu.Lock()
		cut := o.quiet && !o.healed
		o.mu.Unlock()
		if cut {
			panic(http.ErrAbortHandler)
		}
		if _, err := w.Write(o.data[off:min(off+32<<10, hi+1)]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
	}
}

// settled reports whether the origin is quiet and nothing has asked it for
// anything for a while: every connection the client has is held.
func (o *flakyOrigin) settled(quietFor time.Duration) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.bodies == 0 && time.Since(o.last) > quietFor
}

// A download whose server stopped answering sits at 0 B/s for good, since the
// engine waits for response headers without a limit. The watcher reconnects
// it: it finishes, keeps the bytes it had and is not restarted.
func TestAStalledDownloadIsReconnectedAndKeepsItsBytes(t *testing.T) {
	if raceEnabled {
		// gopeed v1.9.3 writes a running task's status, progress and timer on
		// its own goroutines while it hands the same task to its listener and
		// clones it to JSON, all without a lock, so every real HTTP transfer
		// trips -race inside the library. The run without -race covers this.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see comment")
	}
	t.Parallel()
	const size = 16 << 20
	o := newFlakyOrigin(t, size)
	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.StallTimeout = 60
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	task := &core.Task{
		ID: "t1", URL: o.srv.URL + "/big.bin", Name: "big.bin", Dir: dir,
		Status: core.StatusQueued, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()

	waitFor(t, "the first MiB arriving", func() bool { return liveTask(a, "t1").Loaded >= 1<<20 })
	o.mu.Lock()
	o.quiet = true
	o.mu.Unlock()
	// The engine waits up to five seconds before it asks again for a range
	// that broke off, and every such request is held from here on.
	waitFor(t, "every connection being held", func() bool { return o.settled(6 * time.Second) })
	kept := liveTask(a, "t1").Loaded
	if kept >= size {
		t.Fatalf("the download finished before the origin went quiet (%d bytes)", kept)
	}
	o.mu.Lock()
	o.healed = true
	o.mu.Unlock()

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(61 * time.Second))

	waitFor(t, "the download finishing", func() bool { return liveTask(a, "t1").Status == core.StatusDone })
	if got := liveTask(a, "t1").StallRestarts; got != 0 {
		t.Errorf("restarted %d times; a reconnect was enough", got)
	}
	o.mu.Lock()
	asked := slices.Clone(o.afterHeal)
	o.mu.Unlock()
	for _, rg := range asked {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") {
			t.Errorf("after the reconnect the file was asked for from the start (Range %q); the %d bytes it had were thrown away", rg, kept)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}

// With the restart switched on, the first timeout still reconnects, since that
// keeps the bytes. Only a standstill the reconnect did not end is restarted.
func TestARestartWaitsForAReconnectThatDidNotHelp(t *testing.T) {
	if raceEnabled {
		// See TestAStalledDownloadIsReconnectedAndKeepsItsBytes.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	t.Parallel()
	// Headers for the first request, which the engine needs before it starts
	// the transfer, and not a byte after them.
	var mu sync.Mutex
	requests := 0
	stop := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		first := requests == 1
		mu.Unlock()
		if first {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(64<<20))
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(func() {
		close(stop)
		origin.Close()
	})
	asked := func() int {
		mu.Lock()
		defer mu.Unlock()
		return requests
	}

	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.StallTimeout = 60
	s.StallRestart = true
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	task := &core.Task{
		ID: "t1", URL: origin.URL + "/big.bin", Name: "big.bin", Dir: t.TempDir(),
		Status: core.StatusQueued, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
	waitFor(t, "the engine taking the transfer on", func() bool { return a.Engine.Reconnectable("t1") })

	base := time.Now()
	a.stallPass(base)
	before := asked()
	a.stallPass(base.Add(61 * time.Second))
	if got := liveTask(a, "t1").StallRestarts; got != 0 {
		t.Fatalf("restarted %d times at the first timeout, before any reconnect", got)
	}
	waitFor(t, "the reconnect asking the server again", func() bool { return asked() > before })

	a.stallPass(base.Add(122 * time.Second))
	if got := liveTask(a, "t1").StallRestarts; got != 1 {
		t.Errorf("StallRestarts = %d after a reconnect that did not help, want 1", got)
	}
}

// A torrent that has stopped moving has found nobody to move bytes with, and
// the restart path deletes the partial data first, so restarting hands the same
// magnet to the same swarm minus everything it had already fetched.
func TestATorrentIsMarkedButNeverRestarted(t *testing.T) {
	a, be := stallApp(t, func(s *settings.Settings) {
		s.StallTimeout = 60
		s.StallRestart = true
	})
	task := runningTask(a, "t1", 4096)
	a.mu.Lock()
	task.Resolver = "torrent"
	task.InfoHash = "0123456789abcdef0123456789abcdef01234567"
	a.mu.Unlock()

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(5 * time.Minute))

	got := liveTask(a, "t1")
	if got.StalledSince.IsZero() {
		t.Error("a torrent with no swarm was not marked, which is the one thing worth seeing here")
	}
	if got.StallRestarts != 0 {
		t.Errorf("a torrent was restarted %d times, dropping its partial data for nothing", got.StallRestarts)
	}
	select {
	case <-be.got:
		t.Error("the torrent was handed out again")
	case <-time.After(200 * time.Millisecond):
	}
}
