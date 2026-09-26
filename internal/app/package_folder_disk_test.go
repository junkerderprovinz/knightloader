package app

// A package rename and what is on disk beside the package's own files: a live
// package's working folder, the unpacked output, a folder another program
// holds open, a name that differs only in case, and a torrent still seeding.

import (
	cryptorand "crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// A package still downloading through the working folder has its files there
// and nothing yet in its own folder. Its name is taken all the same: renaming
// another package onto it counts the name up and leaves the live package's
// files alone, although one of them has the same name.
func TestRenamingOntoAPackageStillDownloadingLeavesItsFilesAlone(t *testing.T) {
	work := t.TempDir()
	a, base := packageAppWith(t, func(s *settings.Settings) {
		s.WorkDir = work
		s.CollisionPolicy = "rename"
	})
	theirs := stagedIn(t, workdir.For(work, filepath.Join(base, "New")), "film.part1.rar", "theirs, complete")
	putTask(t, a, core.Task{ID: "b1", URL: "https://host.example/b/film.part1.rar", Name: "film.part1.rar",
		Package: "New", Status: core.StatusRunning, Loaded: 16, File: theirs, Enabled: true})
	mine := stagedIn(t, workdir.For(work, filepath.Join(base, "Old")), "film.part1.rar", "mine, partial")
	putTask(t, a, core.Task{ID: "a1", URL: "https://host.example/a/film.part1.rar", Name: "film.part1.rar",
		Package: "Old", Status: core.StatusPaused, Loaded: 13, File: mine, Enabled: true})

	if _, err := a.RenamePackage([]string{"a1"}, "New"); err != nil {
		t.Fatal(err)
	}

	if body, err := os.ReadFile(theirs); err != nil || string(body) != "theirs, complete" {
		t.Errorf("the live package's file now reads %q, %v", body, err)
	}
	live := liveTask(a, "a1")
	counted := filepath.Join(base, "New (2)")
	if live.Package != "New" || a.TaskFolder("a1") != counted {
		t.Errorf("the renamed part is in package %q downloading to %q, want New and %q", live.Package, a.TaskFolder("a1"), counted)
	}
	if body, err := os.ReadFile(live.File); err != nil || string(body) != "mine, partial" {
		t.Errorf("the renamed part records %s, which reads %q, %v", live.File, body, err)
	}
}

// A set whose last part lands while the rename moves its folder is unpacked
// once the move is over, from where the volumes are then; started during the
// move, the unpacking would open the old paths.
func TestAnUnpackingThatComesDueDuringTheMoveWaitsForIt(t *testing.T) {
	a, base := packageAppWith(t, func(s *settings.Settings) { s.Extract = true })
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	var parts []*core.Task
	for _, id := range []string{"part1", "part2"} {
		p := finishedTask(t, a, old, id, "film."+id+".rar")
		editTask(a, id, func(x *core.Task) { x.Package = "Old" })
		parts = append(parts, p)
	}

	a.mu.Lock()
	a.relocating = map[string]bool{"part1": true, "part2": true}
	early := a.extractNowLocked(a.tasks["part2"], a.Settings.Get())
	a.mu.Unlock()
	if early != nil {
		t.Fatal("the unpacking started while the volumes were being moved")
	}

	a.endRelocation([]*packageFolder{{tasks: parts}}, nil)

	if len(a.ExtractJobs()) == 0 {
		t.Error("the unpacking that came due during the move never started")
	}
}

// On a disk that does not tell case apart, a package renamed to another case
// of its name keeps its folder, which is empty while the files are still in
// the working folder.
func TestAPackageRenamedToAnotherCaseKeepsItsEmptyFolder(t *testing.T) {
	work := t.TempDir()
	a, base := packageAppWith(t, func(s *settings.Settings) { s.WorkDir = work })
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := stagedIn(t, workdir.For(work, old), "film.mkv", "the film")
	putTask(t, a, core.Task{ID: "1", URL: "https://host.example/film.mkv", Name: "film.mkv",
		Package: "Old", Status: core.StatusPaused, Loaded: 8, File: staged, Enabled: true})

	if _, err := a.RenamePackage([]string{"1"}, "old"); err != nil {
		t.Fatal(err)
	}

	if fi, err := os.Stat(a.TaskFolder("1")); err != nil || !fi.IsDir() {
		t.Errorf("the package's folder is gone: %v", err)
	}
	if body, err := os.ReadFile(liveTask(a, "1").File); err != nil || string(body) != "the film" {
		t.Errorf("the partial file reads %q, %v", body, err)
	}
}

// Windows will not rename a folder holding a file another program has open.
// The rename is turned down with everything as it was, rather than copying
// the whole folder and leaving the open file behind.
func TestAPackageWhoseFolderCannotBeRenamedStaysAsItWas(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to rename a folder holding an open file")
	}
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	held, err := os.Open(filepath.Join(old, "film.part1.rar"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	_, err = a.RenamePackage([]string{"part1", "part2"}, "Film")

	var refusal *RenameRefusal
	if !errors.As(err, &refusal) || refusal.Code != "notMoved" {
		t.Fatalf("RenamePackage = %v, want the notMoved refusal", err)
	}
	if live := liveTask(a, "part1"); live.Package != "Old" {
		t.Errorf("the package became %q despite the refusal", live.Package)
	}
	if _, err := os.Stat(filepath.Join(base, "Film")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("something was copied to the new folder: %v", err)
	}
}

// With unpacking collected in a folder per package, what the package unpacked
// is in a folder named after it too, and it takes the new name along; the
// extraction list follows it.
func TestAPackagesUnpackedFilesFollowTheName(t *testing.T) {
	unpacked := t.TempDir()
	a, base := packageAppWith(t, func(s *settings.Settings) {
		s.ExtractTo = unpacked
		s.ExtractSubfolder = true
	})
	old := oldPackage(t, a, base)
	stagedIn(t, filepath.Join(unpacked, "Old", "film"), "film.mkv", "the film")
	a.mu.Lock()
	st := a.unpackLocked()
	st.jobs["j"] = &extractJob{
		ExtractJob: ExtractJob{ID: "j", TaskID: "part1", Package: "Old", Status: ExtractDone,
			Dir: filepath.Join(unpacked, "Old", "film")},
		path: filepath.Join(old, "film.part1.rar"),
	}
	st.order = append(st.order, "j")
	a.mu.Unlock()

	if _, err := a.RenamePackage([]string{"part1", "part2"}, "Film"); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(unpacked, "Film", "film")
	if body, err := os.ReadFile(filepath.Join(moved, "film.mkv")); err != nil || string(body) != "the film" {
		t.Errorf("the unpacked film reads %q, %v; want it under the new name", body, err)
	}
	if _, err := os.Stat(filepath.Join(unpacked, "Old")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old extraction folder is still there: %v", err)
	}
	a.mu.Lock()
	j := a.unpackLocked().jobs["j"]
	dir, pkg, path := j.Dir, j.Package, j.path
	a.mu.Unlock()
	if dir != moved || pkg != "Film" || path != filepath.Join(base, "Film", "film.part1.rar") {
		t.Errorf("the extraction list says %s, package %s, archive %s; want where they went", dir, pkg, path)
	}
}

// seedableTorrent writes a finished two-file torrent into dir and returns it
// as the app carries a .torrent, where it landed and its info hash.
func seedableTorrent(t *testing.T, dir string) (uri, root, hash string) {
	t.Helper()
	root = filepath.Join(dir, "Pack")
	for _, name := range []string{"a.bin", "b.bin"} {
		data := make([]byte, 80<<10)
		_, _ = cryptorand.Read(data)
		stagedIn(t, root, name, string(data))
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(root); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}
	b, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatal(err)
	}
	return torrent.EncodeBytes(b), root, mi.HashInfoBytes().HexString()
}

// A torrent that was seeding is taken up again where its folder went and seeds
// on from there: it stays done, it does not count as having stopped seeding,
// and what it had uploaded is kept.
func TestASeedingTorrentGoesOnSeedingFromTheNewFolder(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity")
	}
	a, base := newPackageApp(t)
	uri, root, hash := seedableTorrent(t, filepath.Join(base, "Old"))
	putTask(t, a, core.Task{ID: "seed", URL: uri, Name: "Pack", Package: "Old", Resolver: "torrent", InfoHash: hash,
		Status: core.StatusDone, Seeding: true, Peers: 3, Uploaded: 7000, Ratio: 0.7, File: root, Enabled: true})

	if _, err := a.RenamePackage([]string{"seed"}, "New"); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, "New", "Pack")
	// Peers is a live reading, so the stale 3 turning into the engine's 0 is
	// the engine reporting the torrent seeding again.
	waitFor(t, "the torrent seeding again", func() bool {
		live := liveTask(a, "seed")
		return live.Seeding && live.Peers == 0
	})
	live := liveTask(a, "seed")
	if live.Status != core.StatusDone || !live.SeedingEnded.IsZero() {
		t.Errorf("the torrent is %q, seeding ended %v; want it done and still seeding", live.Status, live.SeedingEnded)
	}
	if live.File != want || live.Uploaded < 7000 {
		t.Errorf("the torrent records %s with %d bytes uploaded; want %s and the 7000 it had", live.File, live.Uploaded, want)
	}
	if _, err := os.Stat(filepath.Join(want, "a.bin")); err != nil {
		t.Errorf("the torrent's files did not move: %v", err)
	}
}
