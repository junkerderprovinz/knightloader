package app

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crossseed"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
	"github.com/junkerderprovinz/knightloader/internal/torrents/torrentstest"
)

// redAlbum writes an album of two files into downloads and returns the
// finished task of its RED .torrent and the info hash its twin on OPS has.
func redAlbum(t *testing.T, downloads string) (*core.Task, string) {
	t.Helper()
	root := filepath.Join(downloads, "Album")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var list []any
	var picked []core.TorrentFile
	var all []byte
	for _, f := range []struct {
		name string
		size int
	}{{"01.flac", 3000}, {"02.flac", 2000}} {
		body := testenv.Content(f.name, f.size)
		if err := os.WriteFile(filepath.Join(root, f.name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		all = append(all, body...)
		list = append(list, map[string]any{"length": f.size, "path": []any{f.name}})
		picked = append(picked, core.TorrentFile{Path: f.name, Size: int64(f.size), Selected: true})
	}
	sum := sha1.Sum(all)
	info := map[string]any{"name": "Album", "piece length": 16384, "pieces": sum[:], "private": 1, "source": "RED", "files": list}
	raw, err := bencode.Encode(info)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bencode.Encode(map[string]any{"announce": "https://flacsfor.me/pk/announce", "info": info})
	if err != nil {
		t.Fatal(err)
	}
	cands, err := crossseed.Candidates(b, crossseed.OPS)
	if err != nil {
		t.Fatal(err)
	}
	return &core.Task{
		ID: "album", URL: torrent.EncodeBytes(b), Name: "Album", Resolver: "torrent", InfoHash: bencode.InfoHash(raw),
		File: root, TorrentFiles: picked, Status: core.StatusDone, Enabled: true, CreatedAt: time.Now(),
	}, cands[0].InfoHash
}

// opsKnowing is a fake OPS that has the torrents with the given hashes and
// counts the questions.
type opsKnowing struct {
	mu    sync.Mutex
	known map[string]bool
	asked int
}

func (o *opsKnowing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("action") == "index" {
		if r.Header.Get("Authorization") != "ops-key" {
			json.NewEncoder(w).Encode(map[string]any{"status": "failure", "error": "bad credentials"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "response": map[string]any{"username": "listener"}})
		return
	}
	o.mu.Lock()
	o.asked++
	ok := o.known[strings.ToLower(r.URL.Query().Get("hash"))]
	o.mu.Unlock()
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{"status": "failure", "error": "bad hash parameter"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"status": "success", "response": map[string]any{"group": map[string]any{"id": 1}, "torrent": map[string]any{"id": 2}}})
}

func (o *opsKnowing) questions() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.asked
}

type crossSeedRig struct {
	a         *App
	bt        *torrentstest.Client
	ops       *opsKnowing
	task      *core.Task
	twin      string
	downloads string
}

// crossSeedApp has a finished RED album on its list and OPS answering on
// loopback, with cross-seeding still off.
func crossSeedApp(t *testing.T) *crossSeedRig {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	downloads := t.TempDir()
	s := a.Settings.Get()
	s.DownloadDir = downloads
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	task, twin := redAlbum(t, downloads)
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()
	ops := &opsKnowing{known: map[string]bool{twin: true}}
	srv := httptest.NewServer(ops)
	t.Cleanup(srv.Close)
	site := crossseed.OPS
	site.API = srv.URL
	a.xsMu.Lock()
	a.xsSites[site.Name] = crossseed.NewGazelle(site, "")
	a.xsMu.Unlock()
	return &crossSeedRig{a: a, bt: a.Engine.Torrents().(*torrentstest.Client), ops: ops, task: task, twin: twin, downloads: downloads}
}

func (r *crossSeedRig) switchOn(t *testing.T, edit func(*settings.CrossSeed)) {
	t.Helper()
	s := r.a.Settings.Get()
	s.Torrent.CrossSeed = settings.CrossSeed{
		Enabled: true,
		OPS:     settings.GazelleSite{APIKey: "ops-key", Announce: "https://home.opsfet.ch/pk/announce"},
	}
	if edit != nil {
		edit(&s.Torrent.CrossSeed)
	}
	if _, err := r.a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	r.settle(t)
}

func (r *crossSeedRig) settle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.a.xsRunner.Wait(ctx); err != nil {
		t.Fatal("the cross-seed search did not settle: ", err)
	}
}

func (r *crossSeedRig) added(t *testing.T) []torrents.Add {
	t.Helper()
	var out []torrents.Add
	for _, add := range r.bt.Added() {
		if add.Hash == r.twin {
			out = append(out, add)
		}
	}
	return out
}

func TestSwitchingCrossSeedingOnInjectsTheTwinOfAFinishedTorrent(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.switchOn(t, func(c *settings.CrossSeed) { c.Category = "music" })

	added := r.added(t)
	if len(added) != 1 {
		t.Fatalf("qBittorrent was handed %d twins, want one", len(added))
	}
	add := added[0]
	if !add.Complete || add.Dir != r.downloads || add.Category != "music" || !slices.Equal(add.Tags, []string{CrossSeedTag}) {
		t.Errorf("the twin was added as %+v", add)
	}
	if st, _ := r.bt.State(r.twin); st != torrents.Seeding {
		t.Errorf("the twin is %v, want it seeding", st)
	}
	entries, err := r.a.CrossSeedLog(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Result != crossseed.ResultInjected || entries[0].Tracker != "OPS" || entries[0].InfoHash != r.twin {
		t.Errorf("the log says %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(r.a.DataDir, crossSeedLogFile)); err != nil {
		t.Errorf("the log is not in the data folder: %v", err)
	}
}

func TestATorrentThatFinishesIsCrossSeeded(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.a.mu.Lock()
	r.task.Status = core.StatusRunning
	r.a.active[r.task.ID] = true
	r.a.mu.Unlock()
	r.switchOn(t, nil)
	if n := r.ops.questions(); n != 0 {
		t.Fatalf("OPS was asked %d times about a torrent still running", n)
	}
	r.a.onUpdate(r.task.ID, core.Update{Status: core.StatusDone, File: r.task.File})
	r.settle(t)
	if len(r.added(t)) != 1 {
		t.Fatal("the torrent that finished was not cross-seeded")
	}
}

func TestATorrentIsNotAskedForAgainAfterARestartOfTheSearch(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.switchOn(t, nil)
	asked := r.ops.questions()
	r.switchOn(t, func(c *settings.CrossSeed) { c.Category = "other" })
	r.a.offerFinishedTorrents()
	r.settle(t)
	if n := r.ops.questions(); n != asked {
		t.Errorf("OPS was asked %d more times about the same torrent", n-asked)
	}
}

func TestHardlinksPutTheTwinsFilesInTheLinkFolder(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	links := filepath.Join(r.downloads, "cross-seeds")
	r.switchOn(t, func(c *settings.CrossSeed) { c.LinkMode, c.LinkDir = settings.LinkHardlink, links })
	added := r.added(t)
	if len(added) != 1 || added[0].Dir != filepath.Join(links, "OPS") {
		t.Fatalf("the twin was added as %+v, want it in the link folder", added)
	}
	for _, name := range []string{"01.flac", "02.flac"} {
		a, err := os.Stat(filepath.Join(r.task.File, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(filepath.Join(links, "OPS", "Album", name))
		if err != nil || !os.SameFile(a, b) {
			t.Errorf("%s is not linked into the link folder: %v", name, err)
		}
	}
}

func TestATwinWhoseFilesAreGoneIsRefusedWithTheReason(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	if err := os.Remove(filepath.Join(r.task.File, "02.flac")); err != nil {
		t.Fatal(err)
	}
	r.switchOn(t, nil)
	if len(r.added(t)) != 0 {
		t.Fatal("a twin was injected without its files")
	}
	entries, _ := r.a.CrossSeedLog(10)
	if len(entries) != 1 || entries[0].Result != crossseed.ResultRefused || !strings.Contains(entries[0].Reason, "02.flac") {
		t.Errorf("the log says %+v", entries)
	}
}

func TestRemovingATaskTakesItsTwinOutOfQBittorrent(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.switchOn(t, nil)
	if !r.bt.Has(r.twin) {
		t.Fatal("the twin was not injected")
	}
	r.a.Remove(r.task.ID, false)
	deadline := time.Now().Add(10 * time.Second)
	for r.bt.Has(r.twin) {
		if time.Now().After(deadline) {
			t.Fatal("the twin of the removed task stayed in qBittorrent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(r.task.File, "01.flac")); err != nil {
		t.Errorf("removing the task without its files took them: %v", err)
	}
}

func TestATorrentAddedAgainAfterItsRemovalIsAskedAboutAgain(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.switchOn(t, nil)
	asked := r.ops.questions()
	r.a.Remove(r.task.ID, false)
	deadline := time.Now().Add(10 * time.Second)
	for r.bt.Has(r.twin) {
		if time.Now().After(deadline) {
			t.Fatal("the twin of the removed task stayed in qBittorrent")
		}
		time.Sleep(10 * time.Millisecond)
	}

	again := *r.task
	again.ID = "album-again"
	r.a.mu.Lock()
	r.a.tasks[again.ID] = &again
	r.a.mu.Unlock()
	r.a.offerCrossSeed(&again)
	r.settle(t)
	if n := r.ops.questions(); n == asked {
		t.Fatal("OPS was not asked again about the torrent added once more")
	}
	if !r.bt.Has(r.twin) {
		t.Error("the twin of the torrent added once more was not injected")
	}
}

func TestATorrentWithFilesLeftOutIsNotCrossSeeded(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.a.mu.Lock()
	r.task.TorrentFiles[1].Selected = false
	r.a.mu.Unlock()
	r.switchOn(t, nil)
	if n := r.ops.questions(); n != 0 {
		t.Errorf("OPS was asked %d times about a torrent with a file left out", n)
	}
}

// A key test sends the masked key and the server asks with the one it
// stores, through the same client the search uses.
func TestAKeyTestAsksTheSiteWithTheStoredKey(t *testing.T) {
	t.Parallel()
	r := crossSeedApp(t)
	r.switchOn(t, nil)
	shown := r.a.Settings.Get().Redacted().Torrent.CrossSeed
	who, err := r.a.TestCrossSeed(context.Background(), shown, "ops", 0)
	if err != nil || who != "listener" {
		t.Fatalf("the test of the stored key = %q, %v", who, err)
	}
	shown.OPS.APIKey = "typed-but-wrong"
	if _, err := r.a.TestCrossSeed(context.Background(), shown, "ops", 0); err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("the test of a wrong key gave %v", err)
	}
	if _, err := r.a.TestCrossSeed(context.Background(), shown, "red", 0); err == nil {
		t.Fatal("a test of RED without a key passed")
	}
}
