package app

import (
	"context"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/browsercookies"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// closeWaits reports whether Close sits in its wait for the work it has
// accepted.
func closeWaits() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	for n == len(buf) {
		buf = make([]byte, 2*len(buf))
		n = runtime.Stack(buf, true)
	}
	for _, g := range strings.Split(string(buf[:n]), "\n\n") {
		i := strings.Index(g, "sync.(*WaitGroup).Wait(")
		if i < 0 {
			continue
		}
		if lines := strings.SplitN(g[i:], "\n", 4); len(lines) > 2 && strings.Contains(lines[2], "app.(*App).Close(") {
			return true
		}
	}
	return false
}

// untilCloseWaits holds the caller, a piece of work Close may or may not wait
// for, until Close waits for it or the test is over. Work Close does not wait
// for is still held when Close returns.
func untilCloseWaits(t *testing.T) {
	for !closeWaits() {
		select {
		case <-t.Context().Done():
			return
		case <-time.After(time.Millisecond):
		}
	}
}

// closeResolver claims the links of close.example for closeBackend.
type closeResolver struct{}

func (closeResolver) Info() resolver.Info { return resolver.Info{ID: "closetest", Prio: 90} }

func (closeResolver) Match(raw string) bool { return strings.Contains(raw, "close.example") }

func (closeResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: "file.bin"}, nil
}

// closeBackend records what it is asked. removing, when set, runs inside
// Remove before the call counts.
type closeBackend struct {
	mu       sync.Mutex
	calls    []string
	removing func()
}

func (b *closeBackend) record(call string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, call)
}

func (b *closeBackend) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

func (b *closeBackend) Download(id, _ string, _ map[string]string, _ int) { b.record("download " + id) }
func (b *closeBackend) Pause(id string)                                   { b.record("pause " + id) }
func (b *closeBackend) Resume(id string)                                  { b.record("resume " + id) }

func (b *closeBackend) Remove(id string, _ bool) {
	if b.removing != nil {
		b.removing()
	}
	b.record("remove " + id)
}

func closeApp(t *testing.T) (*App, *closeBackend) {
	t.Helper()
	a := newQueueApp(t)
	a.Registry.Register(closeResolver{})
	be := &closeBackend{}
	withBackend(a, "closetest", be)
	return a, be
}

// dispatchAfterClose runs a dispatch pass, as an update coming in while the
// app closes does, and waits until every start it handed on is over.
func dispatchAfterClose(a *App, id string) {
	a.Close()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.awaitHandoversLocked([]string{id})
}

func TestAStartAfterCloseHasBegunReachesNoBackend(t *testing.T) {
	a, be := closeApp(t)
	putTask(t, a, core.Task{ID: "new", URL: "https://close.example/new.bin", Status: core.StatusQueued, Enabled: true})

	dispatchAfterClose(a, "new")
	if got := be.seen(); len(got) != 0 {
		t.Errorf("the backend was asked %v after Close", got)
	}
}

func TestAResumeAfterCloseHasBegunReachesNoBackend(t *testing.T) {
	a, be := closeApp(t)
	putTask(t, a, core.Task{ID: "again", URL: "https://close.example/again.bin", Resolver: "closetest",
		Status: core.StatusQueued, Enabled: true})
	a.mu.Lock()
	a.started["again"] = true
	a.mu.Unlock()

	dispatchAfterClose(a, "again")
	if got := be.seen(); len(got) != 0 {
		t.Errorf("the backend was asked %v after Close", got)
	}
}

func TestCloseWaitsForTheBackendATaskLeft(t *testing.T) {
	a, _ := closeApp(t)
	entered := make(chan struct{})
	left := &closeBackend{removing: func() {
		close(entered)
		untilCloseWaits(t)
	}}
	withBackend(a, "lefttest", left)
	putTask(t, a, core.Task{ID: "moved", URL: "https://close.example/moved.bin", Resolver: "lefttest",
		Status: core.StatusQueued, Enabled: true})
	a.mu.Lock()
	a.queue = append(a.queue, "moved")
	a.dispatchLocked()
	a.mu.Unlock()
	<-entered

	a.Close()
	if got := left.seen(); !slices.Equal(got, []string{"remove moved"}) {
		t.Errorf("when Close returned, the backend the task left had done %v, want the removal", got)
	}
}

func TestCloseCancelsAnIconFetchAndKeepsNoMiss(t *testing.T) {
	entered := make(chan struct{})
	stubIconFetch(t, func(ctx context.Context, _ string) ([]byte, string, error) {
		close(entered)
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-t.Context().Done():
			return nil, "", errors.New("the test is over")
		}
	})
	a := newQueueApp(t)
	done := a.fetchHosterIcon("example.org")
	<-entered

	a.Close()
	select {
	case <-done:
	default:
		t.Fatal("Close returned while the icon fetch was still under way")
	}
	if files, _ := os.ReadDir(a.iconDir()); len(files) != 0 {
		t.Errorf("a fetch cut short by Close left %d files, which the next start would take for a site without an icon", len(files))
	}
}

func TestCloseWaitsForABrowserReadUnderWay(t *testing.T) {
	prevContainer := buildinfo.InContainer
	buildinfo.InContainer = func() bool { return false }
	t.Cleanup(func() { buildinfo.InContainer = prevContainer })
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	read := false
	prevRead := readBrowsers
	readBrowsers = func([]string) ([]browsercookies.Found, []browsercookies.Unreadable) {
		once.Do(func() {
			close(entered)
			untilCloseWaits(t)
			mu.Lock()
			read = true
			mu.Unlock()
		})
		return nil, nil
	}
	t.Cleanup(func() { readBrowsers = prevRead })
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	a.StartBrowserSessions()
	<-entered

	a.Close()
	mu.Lock()
	defer mu.Unlock()
	if !read {
		t.Error("Close returned while the browsers were still being read")
	}
}

// heldRunner is a qBittorrent that came up after a newer start took over.
type heldRunner struct {
	t       *testing.T
	mu      sync.Mutex
	stopped bool
}

func (r *heldRunner) Status() nox.Status { return nox.Status{State: nox.StateRunning} }

func (r *heldRunner) Ready(context.Context) (*qbittorrent.Client, error) {
	return nil, errors.New("not reachable")
}

func (r *heldRunner) Stop() error {
	untilCloseWaits(r.t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	return nil
}

func TestCloseWaitsForAQBittorrentANewerStartReplaced(t *testing.T) {
	a, f := ownQBitApp(t, "")
	first := &heldRunner{t: t}
	asked := make(chan struct{})
	proceed := make(chan struct{})
	calls := 0
	var mu sync.Mutex
	a.runQBit = func(v nox.Version) qbitRunner {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			return &fakeRunner{f: f, version: v.Version}
		}
		close(asked)
		<-proceed
		return first
	}
	a.StartQBittorrent()
	<-asked
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	close(proceed)

	a.Close()
	first.mu.Lock()
	defer first.mu.Unlock()
	if !first.stopped {
		t.Error("Close returned while the qBittorrent a newer start replaced was still being stopped")
	}
}
