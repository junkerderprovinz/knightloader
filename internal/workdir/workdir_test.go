package workdir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// TestAWorkingFolderIsKeyedByTheDestinationAndNotTheTask is the property every
// multi-volume archive in the app rests on. internal/extract finds the other
// four parts of a five-part rar by listing the folder the first one is in, so
// two downloads heading for the same destination have to share one working
// folder; a folder per task would leave every set with four parts missing.
func TestAWorkingFolderIsKeyedByTheDestinationAndNotTheTask(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(t.TempDir(), "Films")
	if For(root, dest) != For(root, dest) {
		t.Fatal("one destination produced two working folders")
	}
	other := For(root, filepath.Join(t.TempDir(), "Films"))
	if other == For(root, dest) {
		t.Fatal("two destinations ending in the same word share one working folder")
	}
	// The off switch: with no working folder configured everything keeps
	// writing exactly where it wrote before.
	if got := For("", dest); got != dest {
		t.Fatalf("For(\"\", %q) = %q, want the destination itself", dest, got)
	}
}

// TestOneDestinationSpelledTwoWaysIsOneWorkingFolder. Windows reaches one
// folder under several spellings, so "D:\dl" and "D:\DL" are the same
// destination; a set split across two working folders because two rules
// capitalised the path differently would be two half sets neither of which
// unpacks.
func TestOneDestinationSpelledTwoWaysIsOneWorkingFolder(t *testing.T) {
	root := t.TempDir()
	lower, upper := Key("/srv/media/films"), Key("/srv/media/FILMS")
	if !strings.HasSuffix(lower, upper[len(upper)-8:]) {
		t.Fatalf("%q and %q do not share a digest, so one folder becomes two", lower, upper)
	}
	if For(root, "/srv/media/films") == For(root, "/srv/other/films") {
		t.Fatal("two different destinations share one working folder")
	}
}

// TestAMoveThatCannotRenameKeepsTheSourceUntilTheCopyIsDone is the question a
// working folder on another filesystem raises: the move becomes a copy, and a
// copy can fail with the source already half read. If this fails, a delivery
// that ran out of space has taken the download with it.
func TestAMoveThatCannotRenameKeepsTheSourceUntilTheCopyIsDone(t *testing.T) {
	work, dest := t.TempDir(), t.TempDir()
	src := filepath.Join(work, "film.mkv")
	write(t, src, "the whole film")

	full := errors.New("no space left on device")
	o := Options{
		Policy: collide.Rename,
		// Every rename fails, which is what a cross-filesystem move looks like
		// from here, and the copy underneath then has to carry the bytes.
		Rename: func(string, string) error { return full },
	}
	// The copy cannot be made to fail through the seam above, so the
	// destination is made unusable instead: a file where the folder has to be.
	blocked := filepath.Join(dest, "blocked")
	write(t, blocked, "in the way")
	if _, err := Move(context.Background(), src, filepath.Join(blocked, "deeper"), o); err == nil {
		t.Fatal("a move into an impossible destination reported success")
	}
	if read(t, src) != "the whole film" {
		t.Fatal("the source was touched by a move that failed")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			t.Fatalf("the failed copy left %q behind in the destination", e.Name())
		}
	}

	// And the same move, with a destination that works, does deliver.
	res, err := Move(context.Background(), src, dest, o)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if !res.Copied {
		t.Error("the move reported a rename, but every rename was refused")
	}
	if read(t, res.Path) != "the whole film" {
		t.Errorf("the delivered file at %s does not hold the source's bytes", res.Path)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Error("the source is still in the working folder after a finished copy")
	}
}

// TestACopyIsOnlyNamedOnceItIsComplete. The destination folder is watched by
// exactly the programs this package exists to keep away from half files, so the
// copy in flight must not carry the name they are waiting for.
func TestACopyIsOnlyNamedOnceItIsComplete(t *testing.T) {
	work, dest := t.TempDir(), t.TempDir()
	src := filepath.Join(work, "film.mkv")
	write(t, src, "bytes")

	seen := map[string]bool{}
	o := Options{Policy: collide.Rename, Rename: func(string, string) error {
		// Fired while the copy is not yet in place: whatever is in the
		// destination at this moment is what a scanner would see.
		for _, e := range mustReadDir(t, dest) {
			seen[e] = true
		}
		return errors.New("cross-device link")
	}}
	if _, err := Move(context.Background(), src, dest, o); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if seen["film.mkv"] {
		t.Fatal("the destination held film.mkv while the copy was still running")
	}
	if _, err := os.Stat(filepath.Join(dest, "film.mkv")); err != nil {
		t.Fatalf("the finished copy is not at its name: %v", err)
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestAWholeFolderCrossesWithItsFilesAndTimes covers what a delivered
// extraction actually is: a folder, several files deep, whose modification
// times a library sorts by.
func TestAWholeFolderCrossesWithItsFilesAndTimes(t *testing.T) {
	work, dest := t.TempDir(), t.TempDir()
	src := filepath.Join(work, "Show.S01")
	write(t, filepath.Join(src, "ep01.mkv"), "one")
	write(t, filepath.Join(src, "extras", "ep02.mkv"), "two")
	old := time.Date(2019, 4, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "ep01.mkv"), old, old); err != nil {
		t.Fatal(err)
	}

	o := Options{Policy: collide.Rename, Rename: func(string, string) error {
		return errors.New("cross-device link")
	}}
	res, err := Move(context.Background(), src, dest, o)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := read(t, filepath.Join(res.Path, "extras", "ep02.mkv")); got != "two" {
		t.Errorf("the nested file reads %q", got)
	}
	info, err := os.Stat(filepath.Join(res.Path, "ep01.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("the delivered file is stamped %v, want the source's own %v", info.ModTime(), old)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Error("the source folder survived a finished copy")
	}
	// A copy is written under a temporary name, and a temporary name is made
	// private (0700 for a folder, 0600 for a file). Delivered as it stands,
	// that is a release the media server running as another user cannot open -
	// which on a box where several users share one library is the whole point
	// of the delivery failing to work.
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o044 == 0 {
			t.Errorf("the delivered file is mode %v, want the source's own readable one", info.Mode().Perm())
		}
		dir, err := os.Stat(res.Path)
		if err != nil {
			t.Fatal(err)
		}
		if dir.Mode().Perm()&0o055 == 0 {
			t.Errorf("the delivered folder is mode %v, which nobody but its owner can enter", dir.Mode().Perm())
		}
	}
}

// TestTheCollisionPolicyDecidesTheDeliveredName. A destination already holding
// that name is the ordinary case for a second release, and all three answers
// have to mean here what they mean everywhere else in the app.
func TestTheCollisionPolicyDecidesTheDeliveredName(t *testing.T) {
	for _, c := range []struct {
		policy  collide.Policy
		want    string
		body    string
		skipped bool
	}{
		{collide.Rename, "film (2).mkv", "new", false},
		{collide.Skip, "film.mkv", "old", true},
		{collide.Overwrite, "film.mkv", "new", false},
		// Nobody is there to answer, and a result parked for ever in the
		// working folder is worse than a counted name.
		{collide.Ask, "film (2).mkv", "new", false},
	} {
		t.Run(string(c.policy), func(t *testing.T) {
			work, dest := t.TempDir(), t.TempDir()
			write(t, filepath.Join(work, "film.mkv"), "new")
			write(t, filepath.Join(dest, "film.mkv"), "old")

			res, err := Move(context.Background(), filepath.Join(work, "film.mkv"), dest, Options{Policy: c.policy})
			if err != nil {
				t.Fatalf("Move: %v", err)
			}
			if res.Skipped != c.skipped {
				t.Errorf("skipped = %v, want %v", res.Skipped, c.skipped)
			}
			if got := read(t, filepath.Join(dest, c.want)); got != c.body {
				t.Errorf("%s holds %q, want %q", c.want, got, c.body)
			}
			if c.skipped {
				if _, err := os.Stat(filepath.Join(work, "film.mkv")); err != nil {
					t.Error("a skipped move removed the source anyway")
				}
			}
		})
	}
}

// TestOverwriteMergesAFolderInsteadOfDeletingIt. collide refuses overwrite on a
// folder because there the word would mean deleting a tree of unknown size.
// Here the source is finished files going into a folder that is already there,
// and the useful reading is the one a person expects from dragging one folder
// onto another: what is in the way is replaced, what is not is left alone.
func TestOverwriteMergesAFolderInsteadOfDeletingIt(t *testing.T) {
	work, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(work, "Show.S01", "ep01.mkv"), "new")
	write(t, filepath.Join(dest, "Show.S01", "ep01.mkv"), "old")
	write(t, filepath.Join(dest, "Show.S01", "ep99.mkv"), "somebody else's")

	res, err := Move(context.Background(), filepath.Join(work, "Show.S01"), dest, Options{Policy: collide.Overwrite})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := read(t, filepath.Join(res.Path, "ep01.mkv")); got != "new" {
		t.Errorf("ep01.mkv reads %q, want the delivered copy", got)
	}
	if got := read(t, filepath.Join(res.Path, "ep99.mkv")); got != "somebody else's" {
		t.Errorf("ep99.mkv reads %q; a merge took a file it was never given", got)
	}
}

// TestMoveContentsPutsTheFilesWhereTheFilesGo is the difference between
// "unpack into this folder" and "put the unpacked files here": a release
// dropped in whole leaves one folder level no library asked for.
func TestMoveContentsPutsTheFilesWhereTheFilesGo(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	inner := filepath.Join(src, "Show.S01.COMPLETE.WEB")
	write(t, filepath.Join(inner, "ep01.mkv"), "one")
	write(t, filepath.Join(inner, "ep02.mkv"), "two")

	rep, err := MoveContents(context.Background(), inner, dest, Options{Policy: collide.Rename, PruneSourceDir: true})
	if err != nil {
		t.Fatalf("MoveContents: %v", err)
	}
	if rep.Moved != 2 {
		t.Errorf("moved %d entries, want both episodes", rep.Moved)
	}
	if _, err := os.Stat(filepath.Join(dest, "ep01.mkv")); err != nil {
		t.Errorf("ep01.mkv is not where the episodes go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Show.S01.COMPLETE.WEB")); err == nil {
		t.Error("the release folder came along, which is the level this exists to drop")
	}
	if _, err := os.Stat(inner); !errors.Is(err, os.ErrNotExist) {
		t.Error("the emptied source folder was left behind")
	}
}

// TestSweepLeavesEverythingItWasNotToldToTake. This is the one function here
// that deletes something nobody asked it to, so each of its three guards is
// worth a case of its own.
func TestSweepLeavesEverythingItWasNotToldToTake(t *testing.T) {
	root := t.TempDir()
	live := For(root, "/srv/live")
	orphan := For(root, "/srv/orphan")
	fresh := For(root, "/srv/fresh")
	stranger := filepath.Join(root, "notes")
	for _, d := range []string{live, orphan, fresh, stranger} {
		write(t, filepath.Join(d, "film.mkv.part"), "half")
	}
	old := time.Now().Add(-72 * time.Hour)
	for _, d := range []string{live, orphan, stranger} {
		if err := os.Chtimes(filepath.Join(d, "film.mkv.part"), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, old, old); err != nil {
			t.Fatal(err)
		}
	}

	n, err := Sweep(root, func(key string) bool { return key == filepath.Base(live) }, 24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d folders, want only the orphan", n)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Error("the orphan survived")
	}
	if _, err := os.Stat(live); err != nil {
		t.Error("a working folder something still points at was swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a working folder written to minutes ago was swept")
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Error("a folder this package never made was swept")
	}
}

// TestSweepAgesAFolderByWhatIsInsideIt. A folder's own modification time only
// moves when something is created or removed in it, so a download three days
// into writing one .part file looks untouched since the day it started - and
// sweeping it would delete a transfer that is still running.
func TestSweepAgesAFolderByWhatIsInsideIt(t *testing.T) {
	root := t.TempDir()
	dir := For(root, "/srv/running")
	write(t, filepath.Join(dir, "film.mkv.part"), "still arriving")
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}

	n, err := Sweep(root, func(string) bool { return false }, 24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d folders; the one inside was written to a moment ago", n)
	}
}
