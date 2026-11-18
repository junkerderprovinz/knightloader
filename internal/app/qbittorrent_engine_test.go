package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
)

// fakeQBit stands in for fetching and running qbittorrent-nox: it records
// which versions were fetched and started, and each start is a runner that
// counts as running at once.
type fakeQBit struct {
	mu       sync.Mutex
	fetched  []string
	started  []string
	stopped  []string
	fetchErr error
}

type fakeRunner struct {
	f       *fakeQBit
	version string
	stopped bool
}

func (r *fakeRunner) Status() nox.Status {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	if r.stopped {
		return nox.Status{State: nox.StateStopped}
	}
	return nox.Status{State: nox.StateRunning, Version: r.version, Port: 41000}
}

func (r *fakeRunner) Ready(ctx context.Context) (*qbittorrent.Client, error) {
	return qbittorrent.New("http://127.0.0.1:41000", "knightloader", "x")
}

func (r *fakeRunner) Stop() error {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	if !r.stopped {
		r.stopped = true
		r.f.stopped = append(r.f.stopped, r.version)
	}
	return nil
}

func (f *fakeQBit) wire(a *App) {
	a.fetchQBit = func(ctx context.Context, v nox.Version, progress func(done, total int64)) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.fetched = append(f.fetched, v.Version)
		return f.fetchErr
	}
	a.runQBit = func(v nox.Version) qbitRunner {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.started = append(f.started, v.Version)
		return &fakeRunner{f: f, version: v.Version}
	}
}

func (f *fakeQBit) seen() (fetched, started, stopped []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fetched), slices.Clone(f.started), slices.Clone(f.stopped)
}

func ownQBitApp(t *testing.T, version string) (*App, *fakeQBit) {
	t.Helper()
	a := autoUpdateApp(t, true)
	s := a.Settings.Get()
	s.Torrent.EngineVersion = version
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	f := &fakeQBit{}
	f.wire(a)
	return a, f
}

func awaitOwnQBit(t *testing.T, a *App, state string) QBittorrentStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := a.QBittorrentStatus()
		if st.State == state {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("qBittorrent is %+v, want %s", st, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheDefaultQBittorrentIsFetchedAndStarted(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	st := awaitOwnQBit(t, a, nox.StateRunning)
	def := nox.Versions().Default
	fetched, started, _ := f.seen()
	if !slices.Equal(fetched, []string{def}) || !slices.Equal(started, []string{def}) {
		t.Fatalf("fetched %v, started %v, want %s", fetched, started, def)
	}
	if st.Version != def || st.Chosen != def || st.Default != def || len(st.Versions) == 0 {
		t.Fatalf("status %+v", st)
	}
}

func TestAHeldVersionIsTheOneThatRuns(t *testing.T) {
	a, f := ownQBitApp(t, "4.6.7")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	if _, started, _ := f.seen(); !slices.Equal(started, []string{"4.6.7"}) {
		t.Fatalf("started %v", started)
	}
}

func TestChoosingAnotherVersionSwapsTheRunningQBittorrent(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	s := a.Settings.Get()
	s.Torrent.EngineVersion = "4.5.5"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.QBittorrentStatus().Version != "4.5.5" {
		if time.Now().After(deadline) {
			t.Fatalf("still %+v", a.QBittorrentStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}
	def := nox.Versions().Default
	if _, started, stopped := f.seen(); !slices.Equal(started, []string{def, "4.5.5"}) || !slices.Equal(stopped, []string{def}) {
		t.Fatalf("started %v, stopped %v", started, stopped)
	}
}

func TestSavingOtherSettingsLeavesQBittorrentRunning(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	s := a.Settings.Get()
	s.Torrent.SeedRatioTarget = 2
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, started, stopped := f.seen(); len(started) != 1 || len(stopped) != 0 {
		t.Fatalf("started %v, stopped %v", started, stopped)
	}
}

func TestAFetchThatFailsSaysWhy(t *testing.T) {
	a, f := ownQBitApp(t, "")
	f.fetchErr = errors.New("github answered 503")
	a.StartQBittorrent()
	st := awaitOwnQBit(t, a, QBitFailed)
	if st.Detail != "github answered 503" {
		t.Fatalf("status %+v", st)
	}
	if _, started, _ := f.seen(); len(started) != 0 {
		t.Fatalf("started %v without a build", started)
	}
}

func TestAVersionThisBuildDoesNotKnowRunsTheDefault(t *testing.T) {
	a, f := ownQBitApp(t, "9.9.9")
	a.StartQBittorrent()
	st := awaitOwnQBit(t, a, nox.StateRunning)
	def := nox.Versions().Default
	if _, started, _ := f.seen(); !slices.Equal(started, []string{def}) {
		t.Fatalf("started %v", started)
	}
	if st.Chosen != def || st.Detail == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestCloseStopsQBittorrent(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	a.Close()
	if _, _, stopped := f.seen(); len(stopped) != 1 {
		t.Fatalf("stopped %v", stopped)
	}
}

func TestUpdateNowLeavesQBittorrentAtItsPinnedVersion(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	run := a.StartComponentUpdate()
	if _, ok := run.Results[componentQBittorrent]; ok {
		t.Fatal("Update now asks after qBittorrent")
	}
	if fetched, _, _ := f.seen(); len(fetched) != 1 {
		t.Fatalf("fetched %v", fetched)
	}
}

func TestAVersionThatCannotBeFetchedLeavesTheRunningOneAlone(t *testing.T) {
	a, f := ownQBitApp(t, "")
	a.StartQBittorrent()
	awaitOwnQBit(t, a, nox.StateRunning)
	f.mu.Lock()
	f.fetchErr = errors.New("github answered 404")
	f.mu.Unlock()
	s := a.Settings.Get()
	s.Torrent.EngineVersion = "4.6.7"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.QBittorrentStatus().Detail == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no word of the failed fetch: %+v", a.QBittorrentStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := a.QBittorrentStatus()
	def := nox.Versions().Default
	if st.State != nox.StateRunning || st.Version != def || st.Chosen != "4.6.7" {
		t.Fatalf("status %+v", st)
	}
	if _, _, stopped := f.seen(); len(stopped) != 0 {
		t.Fatalf("stopped %v", stopped)
	}
}

func TestTorrentsReachTheQBittorrentThisAppRunsOnceItIsUp(t *testing.T) {
	a, _ := ownQBitApp(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.StartQBittorrent()
	if _, err := a.qbitClient(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestATorrentCallWithoutQBittorrentFailsAtOnce(t *testing.T) {
	a := newTorrentTestApp(t)
	if _, err := a.qbitClient(context.Background()); !errors.Is(err, errQBitOff) {
		t.Fatalf("err = %v", err)
	}
}

func TestAQBittorrentThatStartsGetsTheTorrentSettings(t *testing.T) {
	a := newTorrentTestApp(t)
	s := a.Settings.Get()
	s.Torrent.Port, s.Torrent.UploadLimit, s.Torrent.DHTEnabled = 6881, 4096, true
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	a.afterSettingsChange(a.Settings.Get())
	q := qbittest.New("knightloader", "x")
	srv := httptest.NewServer(q)
	t.Cleanup(srv.Close)
	c, err := qbittorrent.New(srv.URL, "knightloader", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.prepareQBit(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	p := q.Preferences()
	if p["listen_port"] != 6881.0 || p["up_limit"] != 4096.0 || p["dht"] != true || p["queueing_enabled"] != false {
		t.Fatalf("preferences %v", p)
	}
}

// An install that never picked a version runs the default, whatever it was
// when the install began, and one that picked a version keeps it.
func TestOnlyAVersionNobodyPickedFollowsTheDefault(t *testing.T) {
	for _, tc := range []struct{ held, want string }{
		{"", "5.0.5"},
		{"4.5.5", "4.5.5"},
		{"4.6.7", "4.6.7"},
		{"5.0.5", "5.0.5"},
	} {
		if v, note := pickQBitVersion(tc.held); v.Version != tc.want || note != "" {
			t.Errorf("held %q runs %s (%q), want %s", tc.held, v.Version, note, tc.want)
		}
	}
}
