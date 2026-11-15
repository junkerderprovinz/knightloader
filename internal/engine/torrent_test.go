package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
	"github.com/junkerderprovinz/knightloader/internal/torrents/torrentstest"
)

// updateLog keeps every update the engine sends, for a test that has to know
// what was never said.
type updateLog struct {
	mu  sync.Mutex
	all []core.Update
}

func (l *updateLog) add(_ string, u core.Update) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, u)
}

func (l *updateLog) snapshot() []core.Update {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.all)
}

// waitFor returns the first update that meets cond.
func (l *updateLog) waitFor(t *testing.T, what string, cond func(core.Update) bool) core.Update {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, u := range l.snapshot() {
			if cond(u) {
				return u
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no update with %s among %+v", what, l.snapshot())
	return core.Update{}
}

func isStatus(s core.Status) func(core.Update) bool {
	return func(u core.Update) bool { return u.Status == s }
}

// torrentEngine is an engine whose torrents run in a client in memory.
func torrentEngine(t *testing.T) (*Engine, *torrentstest.Client, *updateLog) {
	t.Helper()
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	c := torrentstest.New()
	e.UseTorrents(c)
	return e, c, log
}

// sharedUpload is a .torrent of a folder named name that the swarm shares, as
// the URI an upload is carried as.
func sharedUpload(t *testing.T, name string, files map[string]int) (uri, hash string) {
	t.Helper()
	root := t.TempDir()
	raw, hash := testenv.MakeTorrent(t, root, name, files)
	var shared []torrents.File
	for p, size := range files {
		shared = append(shared, torrents.File{Path: name + "/" + p, Size: int64(size)})
	}
	slices.SortFunc(shared, func(a, b torrents.File) int { return strings.Compare(a.Path, b.Path) })
	torrentstest.Share(hash, root, shared)
	t.Cleanup(func() { torrentstest.Unshare(hash) })
	return torrent.EncodeBytes(raw), hash
}

func TestATorrentRunsInTheClientAndIsDoneOnceItHasEveryFile(t *testing.T) {
	e, c, log := torrentEngine(t)
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 40000, "sub/b.bin": 100})
	dir := t.TempDir()
	e.Start(Job{TaskID: "t", URL: magnet, Dir: dir})

	run := log.waitFor(t, "running", isStatus(core.StatusRunning))
	if run.Name != "Pack" || run.Size != 40100 || run.File != filepath.Join(dir, "Pack") {
		t.Errorf("running = %+v", run)
	}
	if !slices.Equal(run.MagnetFiles, []string{"a.bin", "sub/b.bin"}) || len(run.TorrentFiles) != 2 {
		t.Errorf("files = %v, %+v", run.MagnetFiles, run.TorrentFiles)
	}
	done := log.waitFor(t, "done", isStatus(core.StatusDone))
	if done.File != filepath.Join(dir, "Pack") || done.Loaded != 40100 || done.Torrent == nil || !done.Torrent.Seeding {
		t.Errorf("done = %+v, torrent %+v", done, done.Torrent)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "Pack", "sub", "b.bin")); err != nil || string(got) != string(testenv.Content("sub/b.bin", 100)) {
		t.Errorf("b.bin = %q, %v", got, err)
	}
	if st, ok := c.State(hash); !ok || st != torrents.Seeding {
		t.Errorf("the client holds it as %v, %v", st, ok)
	}
}

func TestAnUploadsFileRulesChooseFromItsOwnFileList(t *testing.T) {
	e, c, log := torrentEngine(t)
	uri, hash := sharedUpload(t, "Show", map[string]int{"ep1.mkv": 30000, "info.nfo": 50})
	dir := t.TempDir()
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, FileRules: torrent.FileRules{Exclude: []string{`\.nfo$`}}})

	log.waitFor(t, "done", isStatus(core.StatusDone))
	files, _ := c.Files(context.Background(), hash)
	if !files[0].Picked || files[1].Picked {
		t.Errorf("picked %+v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "Show", "info.nfo")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file left out is on disk: %v", err)
	}
	run := log.waitFor(t, "running", isStatus(core.StatusRunning))
	if run.Size != 30000 || !run.TorrentFiles[0].Selected || run.TorrentFiles[1].Selected {
		t.Errorf("running = %+v", run)
	}
}

func TestAMagnetsFileRulesChooseOnceItsFilesAreKnown(t *testing.T) {
	e, c, log := torrentEngine(t)
	hash, magnet := testenv.SeedTorrent(t, "Show", map[string]int{"ep1.mkv": 30000, "Sample/s.mkv": 50})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir(), FileRules: torrent.FileRules{Exclude: []string{`^Sample/`}}})

	log.waitFor(t, "done", isStatus(core.StatusDone))
	files, _ := c.Files(context.Background(), hash)
	if files[0].Picked || !files[1].Picked {
		t.Errorf("picked %+v", files)
	}
}

func TestATorrentWithNothingSelectedIsNotStarted(t *testing.T) {
	e, c, log := torrentEngine(t)
	uri, _ := sharedUpload(t, "Pack", map[string]int{"a.bin": 10})
	e.Start(Job{TaskID: "t", URL: uri, Dir: t.TempDir(), TorrentSelect: []int{}})

	u := log.waitFor(t, "an error", isStatus(core.StatusError))
	if u.Err != errNothingSelected.Error() || len(c.Added()) != 0 {
		t.Errorf("err %q, added %v", u.Err, c.Added())
	}
}

func TestAStartWithoutATorrentClientFails(t *testing.T) {
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	_, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 10})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir()})
	if u := log.waitFor(t, "an error", isStatus(core.StatusError)); u.Err != errNoClient.Error() {
		t.Errorf("err %q", u.Err)
	}
}

func TestAMagnetNobodySharesFailsWithAReasonAndLeavesTheClient(t *testing.T) {
	e, c, log := torrentEngine(t)
	e.SetMetadataTimeout(300 * time.Millisecond)
	const hash = "0123456789abcdef0123456789abcdef01234567"
	e.Start(Job{TaskID: "t", URL: "magnet:?xt=urn:btih:" + hash + "&dn=Nobody", Dir: t.TempDir()})

	u := log.waitFor(t, "an error", isStatus(core.StatusError))
	if !strings.Contains(u.Err, "no peer sent this torrent's file list") {
		t.Errorf("err %q", u.Err)
	}
	if c.Has(hash) {
		t.Error("the client still holds the magnet")
	}
}

func TestAFileListThatClimbsOutIsRefused(t *testing.T) {
	dir := t.TempDir()
	for name, files := range map[string][]torrents.File{
		"out of the folder":         {{Path: "Pack/a"}, {Path: "Pack/../../evil"}},
		"out of the torrent folder": {{Path: "Pack/a"}, {Path: "Pack/../other"}},
	} {
		if err := checkLanding(dir, contentOf(files)); err == nil {
			t.Errorf("%s: landed", name)
		}
	}
	if err := checkLanding(dir, contentOf([]torrents.File{{Path: "Pack/a"}, {Path: "Pack/sub/b"}})); err != nil {
		t.Errorf("a plain torrent: %v", err)
	}
}

func TestTheClientsFileListTellsAFolderFromASingleFile(t *testing.T) {
	folder := contentOf([]torrents.File{{Path: "Pack/a", Size: 1}, {Path: "Pack/sub/b", Size: 2}})
	if folder.name != "Pack" || !slices.Equal(folder.paths(), []string{"a", "sub/b"}) {
		t.Errorf("folder %+v", folder)
	}
	single := contentOf([]torrents.File{{Path: "movie.mkv", Size: 3}})
	if single.name != "" || !slices.Equal(single.paths(), []string{"movie.mkv"}) {
		t.Errorf("single %+v", single)
	}
	if name, size := folder.shown([]int{1}); name != "Pack" || size != 2 {
		t.Errorf("shown %s %d", name, size)
	}
}

func TestPauseStopsTheTorrentAndResumeStartsItAgain(t *testing.T) {
	e, c, log := torrentEngine(t)
	c.Stall()
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir()})
	log.waitFor(t, "running", isStatus(core.StatusRunning))
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })

	e.Pause("t")
	log.waitFor(t, "paused", isStatus(core.StatusPaused))
	if st, _ := c.State(hash); st != torrents.Stopped {
		t.Fatalf("state %v after the pause", st)
	}
	e.Resume("t")
	if st, _ := c.State(hash); st != torrents.Fetching {
		t.Fatalf("state %v after the resume", st)
	}
	c.Flow()
	log.waitFor(t, "done", isStatus(core.StatusDone))
}

func TestRemovingATorrentWithItsFilesTakesOnlyItsOwn(t *testing.T) {
	e, c, log := torrentEngine(t)
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100, "sub/b.bin": 10})
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Start(Job{TaskID: "t", URL: magnet, Dir: dir})
	log.waitFor(t, "done", isStatus(core.StatusDone))
	stray := filepath.Join(dir, "Pack", "stray.txt")
	if err := os.WriteFile(stray, []byte("somebody's"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.Remove("t", true)
	if c.Has(hash) {
		t.Error("the client still holds the torrent")
	}
	for _, p := range []string{"a.bin", "sub/b.bin"} {
		if _, err := os.Stat(filepath.Join(dir, "Pack", p)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", p, err)
		}
	}
	for _, p := range []string{notes, stray} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s went with the torrent: %v", p, err)
		}
	}
}

func TestTwoTorrentsOfOneNameLandApart(t *testing.T) {
	e, _, log := torrentEngine(t)
	_, first := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100})
	_, second := testenv.SeedTorrent(t, "Pack", map[string]int{"b.bin": 200})
	dir := t.TempDir()
	e.Start(Job{TaskID: "one", URL: first, Dir: dir})
	log.waitFor(t, "the first done", isStatus(core.StatusDone))
	e.Start(Job{TaskID: "two", URL: second, Dir: dir})
	u := log.waitFor(t, "the second done", func(u core.Update) bool {
		return u.Status == core.StatusDone && u.File != filepath.Join(dir, "Pack")
	})
	if u.File != filepath.Join(dir, "Pack.1", "Pack") {
		t.Fatalf("the second landed at %s", u.File)
	}
}

func TestATorrentTheClientKeptIsTakenOverAfterARestart(t *testing.T) {
	c := torrentstest.New()
	c.Stall()
	dir := t.TempDir()
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100})
	first, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatal(err)
	}
	first.UseTorrents(c)
	first.Start(Job{TaskID: "t", URL: magnet, Dir: dir})
	waitUntil(t, "the torrent running", func() bool { return first.runOf("t") != nil })
	_ = first.Close()

	log := &updateLog{}
	second, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	second.UseTorrents(c)
	c.Flow()
	// The app starts it again where the first start said it landed.
	second.Start(Job{TaskID: "t", URL: magnet, Dir: dir, TorrentRoot: filepath.Join(dir, "Pack"), TorrentName: "Pack"})
	log.waitFor(t, "done", isStatus(core.StatusDone))
	if added := c.Added(); len(added) != 1 || added[0].Hash != hash {
		t.Errorf("added %+v", added)
	}
}

// finishedTorrent writes a torrent's files into dir, as a download that
// finished there left them, and returns its upload URI, where it landed and
// its hash.
func finishedTorrent(t *testing.T, dir string) (uri, root, hash string) {
	t.Helper()
	raw, hash := testenv.MakeTorrent(t, dir, "Pack", map[string]int{"a.bin": 40000, "b.bin": 1000})
	return torrent.EncodeBytes(raw), filepath.Join(dir, "Pack"), hash
}

func TestASeedRunReportsNothingButItsSeedingAndCountsOnFromWhatItHad(t *testing.T) {
	e, c, log := torrentEngine(t)
	dir := t.TempDir()
	uri, root, hash := finishedTorrent(t, dir)
	from := core.TorrentStats{Uploaded: 1000, Ratio: 0.5, SeedSeconds: 60}
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true, SeedFrom: from})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })
	c.Upload(hash, 500, 10)

	u := log.waitFor(t, "the upload counted on", func(u core.Update) bool {
		return u.Torrent != nil && u.Torrent.Uploaded == 1500
	})
	// The client also counts the seconds it seeds between two readings.
	if !u.Torrent.Seeding || u.Torrent.SeedSeconds < 70 {
		t.Errorf("torrent %+v", u.Torrent)
	}
	for _, u := range log.snapshot() {
		if u.Status != "" {
			t.Errorf("a seed run reported %s", u.Status)
		}
	}
	if added := c.Added(); len(added) != 1 || !added[0].Complete {
		t.Errorf("added %+v, want its files taken as complete", added)
	}
}

func TestASeedRunStopsAtItsTargetAndLeavesTheClient(t *testing.T) {
	e, c, log := torrentEngine(t)
	if err := e.SetTorrentConfig(TorrentConfig{SeedRatio: 1}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	uri, root, hash := finishedTorrent(t, dir)
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true, SeedFrom: core.TorrentStats{Ratio: 0.9}})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })
	c.Upload(hash, 41000/5, 0)

	u := log.waitFor(t, "the target", func(u core.Update) bool { return u.Torrent != nil && u.Torrent.AtTarget })
	if u.Torrent.Seeding {
		t.Errorf("torrent %+v", u.Torrent)
	}
	waitUntil(t, "the client letting go", func() bool { return !c.Has(hash) })
	if _, err := os.Stat(filepath.Join(root, "a.bin")); err != nil {
		t.Errorf("the files went: %v", err)
	}
}

func TestASeedRunCountsItsTargetsFromItsMark(t *testing.T) {
	e, c, log := torrentEngine(t)
	if err := e.SetTorrentConfig(TorrentConfig{SeedRatio: 1}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	uri, root, hash := finishedTorrent(t, dir)
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true,
		SeedFrom: core.TorrentStats{Ratio: 2}, SeedMark: core.SeedMark{Ratio: 2}})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })
	c.Upload(hash, 41000/2, 0)
	log.waitFor(t, "a reading of half a ratio more", func(u core.Update) bool { return u.Torrent != nil && u.Torrent.Ratio >= 2.5 })
	if u := log.snapshot(); slices.ContainsFunc(u, func(u core.Update) bool { return u.Torrent != nil && u.Torrent.AtTarget }) {
		t.Fatal("stopped at a target counted from before the mark")
	}
	c.Upload(hash, 41000/2, 0)
	log.waitFor(t, "the target", func(u core.Update) bool { return u.Torrent != nil && u.Torrent.AtTarget })
}

func TestASeedRunWithAFileMissingSaysWhyAndHandsNothingOver(t *testing.T) {
	e, c, log := torrentEngine(t)
	dir := t.TempDir()
	uri, root, _ := finishedTorrent(t, dir)
	if err := os.Remove(filepath.Join(root, "b.bin")); err != nil {
		t.Fatal(err)
	}
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true, SeedFrom: core.TorrentStats{Uploaded: 7}})
	u := log.waitFor(t, "the end of the seeding", func(u core.Update) bool { return u.Torrent != nil && u.Torrent.NotSeeded != "" })
	if !strings.Contains(u.Torrent.NotSeeded, "b.bin is missing") || u.Torrent.Uploaded != 7 {
		t.Errorf("torrent %+v", u.Torrent)
	}
	if len(c.Added()) != 0 {
		t.Errorf("added %+v", c.Added())
	}
}

func TestThePieceCheckFindsFilesThatAreNotAsTheTorrentHasThem(t *testing.T) {
	dir := t.TempDir()
	uri, root, _ := finishedTorrent(t, dir)
	if err := CheckPieces(context.Background(), dir, uri); err != nil {
		t.Fatalf("the files as made: %v", err)
	}
	p := filepath.Join(root, "b.bin")
	b, _ := os.ReadFile(p)
	b[10] ^= 0xff
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	err := CheckPieces(context.Background(), dir, uri)
	if nip, ok := errors.AsType[*notInPlace](err); !ok || !strings.Contains(nip.why, "b.bin does not match") {
		t.Fatalf("err = %v", err)
	}
}

func TestThePieceCheckStopsWhenItsContextEnds(t *testing.T) {
	dir := t.TempDir()
	uri, _, _ := finishedTorrent(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckPieces(ctx, dir, uri); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFilesChosenWhileATorrentRunsArePickedInTheClient(t *testing.T) {
	e, c, log := torrentEngine(t)
	c.Stall()
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100, "b.bin": 200})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir()})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })

	if err := e.SelectTorrentFiles("t", []int{1}); err != nil {
		t.Fatal(err)
	}
	files, _ := c.Files(context.Background(), hash)
	if files[0].Picked || !files[1].Picked {
		t.Fatalf("picked %+v", files)
	}
	c.Flow()
	log.waitFor(t, "done", isStatus(core.StatusDone))
	got, ok := e.TorrentFileProgress("t")
	if !ok || !slices.Equal(got, []int64{-1, 200}) {
		t.Errorf("progress %v, %v", got, ok)
	}
	if err := e.SelectTorrentFiles("t", nil); !errors.Is(err, ErrTorrentFinished) {
		t.Errorf("a finished torrent took a new selection: %v", err)
	}
}

func TestATorrentsFileIsReadOnceItsPiecesAreHere(t *testing.T) {
	e, c, _ := torrentEngine(t)
	c.Stall()
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 40000})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir()})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })

	r, err := e.Stream("t", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Streaming(hash) {
		t.Error("the torrent does not fetch in order")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := r.ReadContext(ctx, make([]byte, 10)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a read before the bytes came: %v", err)
	}
	c.Flow()
	if _, err := c.Readings(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != string(testenv.Content("a.bin", 40000)) {
		t.Fatalf("read %d bytes, %v", len(got), err)
	}
	r.Close()
	if c.Streaming(hash) {
		t.Error("the torrent still fetches in order once nobody reads it")
	}
}

func TestATorrentTheClientGivesUpOnFailsWithItsReason(t *testing.T) {
	e, c, log := torrentEngine(t)
	c.Stall()
	hash, magnet := testenv.SeedTorrent(t, "Pack", map[string]int{"a.bin": 100})
	e.Start(Job{TaskID: "t", URL: magnet, Dir: t.TempDir()})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })
	c.Fail(hash, "the disk is full")
	if u := log.waitFor(t, "an error", isStatus(core.StatusError)); u.Err != "the disk is full" {
		t.Errorf("err %q", u.Err)
	}
}

func TestTheTorrentSettingsReachTheClient(t *testing.T) {
	e, c, _ := torrentEngine(t)
	if err := e.SetTorrentConfig(TorrentConfig{Port: 6881, UploadLimit: 1000, DisablePEX: true, Interface: "wg0"}); err != nil {
		t.Fatal(err)
	}
	want := torrents.Prefs{Port: 6881, UploadLimit: 1000, DHT: true, Interface: "wg0"}
	waitUntil(t, "the settings in the client", func() bool { return slices.Contains(c.Prefs(), want) })
	if e.TorrentPrefs() != want {
		t.Errorf("TorrentPrefs = %+v", e.TorrentPrefs())
	}
}

// Closing the engine while torrents are being handed over must not take the
// process down, as when a restart resumes seeding and is shut down at once.
func TestCloseRightAfterATorrentStartDoesNotCrash(t *testing.T) {
	for range 20 {
		e, err := New(t.TempDir(), func(string, core.Update) {})
		if err != nil {
			t.Fatal(err)
		}
		e.UseTorrents(torrentstest.New())
		dir := t.TempDir()
		uri, root, _ := finishedTorrent(t, dir)
		e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, Seed: true})
		_, magnet := testenv.SeedTorrent(t, "Other", map[string]int{"x": 10})
		e.Start(Job{TaskID: "m", URL: magnet, Dir: dir})
		_ = e.Close()
	}
}

func TestARecheckedSeedRunIsTakenAsCompleteOnlyWhenItsPiecesMatch(t *testing.T) {
	for _, damaged := range []bool{false, true} {
		e, c, _ := torrentEngine(t)
		dir := t.TempDir()
		uri, root, _ := finishedTorrent(t, dir)
		if damaged {
			if err := os.WriteFile(filepath.Join(root, "b.bin"), make([]byte, 1000), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true, Recheck: true})
		waitUntil(t, "the hand-over", func() bool { return len(c.Added()) == 1 })
		if got := c.Added()[0].Complete; got == damaged {
			t.Errorf("damaged %v: taken as complete %v", damaged, got)
		}
	}
}

func TestOnlyTheSystemsOwnInterfacesAreOffered(t *testing.T) {
	ifs, err := NetInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(ifs, func(ni NetInterface) bool { return ni.Name == "knightloader-vpn" }) {
		t.Error("a network inside this process is offered to the torrent client")
	}
	if InterfaceUp("knightloader-vpn") || !InterfaceUp("") {
		t.Error("the built-in VPN counts as up, or any interface as down")
	}
}

func TestATorrentTheClientTakesInTheBackgroundIsWaitedFor(t *testing.T) {
	e, c, log := torrentEngine(t)
	c.Lag(3)
	uri, _ := sharedUpload(t, "Pack", map[string]int{"a.bin": 100})
	e.Start(Job{TaskID: "t", URL: uri, Dir: t.TempDir()})
	u := log.waitFor(t, "done or an error", func(u core.Update) bool {
		return u.Status == core.StatusDone || u.Status == core.StatusError
	})
	if u.Status != core.StatusDone {
		t.Fatalf("update %+v", u)
	}
}

func TestAMagnetWaitsForItsFileListWhileTorrentsAreHeld(t *testing.T) {
	e, c, log := torrentEngine(t)
	e.SetMetadataTimeout(200 * time.Millisecond)
	var held atomic.Bool
	held.Store(true)
	e.HoldTorrentsWhile(held.Load)
	const hash = "0123456789abcdef0123456789abcdef01234567"
	e.Start(Job{TaskID: "t", URL: "magnet:?xt=urn:btih:" + hash + "&dn=Later", Dir: t.TempDir()})
	time.Sleep(700 * time.Millisecond)
	for _, u := range log.snapshot() {
		if u.Status == core.StatusError {
			t.Fatalf("the magnet failed while torrents were held: %s", u.Err)
		}
	}
	if !c.Has(hash) {
		t.Fatal("the client lost the magnet")
	}
	held.Store(false)
	log.waitFor(t, "an error once torrents may run", isStatus(core.StatusError))
}

func TestAProxyReachesTheClientWithTheOtherSettings(t *testing.T) {
	e, c, _ := torrentEngine(t)
	px := &torrents.Proxy{Host: "127.0.0.1", Port: 41080, User: "knightloader", Pass: "pw"}
	if err := e.SetTorrentConfig(TorrentConfig{Port: 6881, Proxy: px}); err != nil {
		t.Fatal(err)
	}
	if got := e.TorrentPrefs(); got.Proxy != px || got.Port != 6881 {
		t.Errorf("prefs %+v", got)
	}
	waitUntil(t, "the client's settings", func() bool { p := c.Prefs(); return len(p) > 0 && p[len(p)-1].Proxy == px })
}

func TestTheSecondFileOfAPaddedTorrentIsReadFromItsOwnPiece(t *testing.T) {
	e, c, _ := torrentEngine(t)
	c.Stall()
	dir := t.TempDir()
	raw, _ := paddedTorrent(t, t.TempDir(), "Film", 100000, 5000)
	e.Start(Job{TaskID: "t", URL: torrent.EncodeBytes(raw), Dir: dir})
	waitUntil(t, "the torrent running", func() bool { return e.runOf("t") != nil })
	r, err := e.Stream("t", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if start := r.(*torrentReader).start; start != 114688 {
		t.Errorf("the second file starts at %d of the torrent's bytes, want 114688", start)
	}
}
