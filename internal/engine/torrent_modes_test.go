package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/filemode"
)

// TestAFinishedTorrentTakesTheModesTheUmaskAsksFor starts from what the
// torrent library leaves: fixed modes and complete files made read-only.
func TestAFinishedTorrentTakesTheModesTheUmaskAsksFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "Show.S01")
	sub := filepath.Join(root, "Extras")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ep := filepath.Join(root, "ep01.mkv")
	extra := filepath.Join(sub, "making-of.mkv")
	for _, p := range []string{ep, extra} {
		if err := os.WriteFile(p, []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	// Somebody else's file beside the torrent keeps its mode.
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, nil, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(other, 0o666) })

	e := &Engine{roots: map[string]torrentRoot{"t": {
		dir:  dir,
		path: root,
		// A file left out of the selection was never written.
		files: []string{"Show.S01/ep01.mkv", "Show.S01/Extras/making-of.mkv", "Show.S01/skipped.nfo"},
	}}}
	e.settleModes("t")

	mask := filemode.Mask()
	for p, want := range map[string]fs.FileMode{
		ep:    filemode.File &^ mask,
		extra: filemode.File &^ mask,
		root:  filemode.Dir &^ mask,
		sub:   filemode.Dir &^ mask,
		other: 0o444,
	} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s came out %04o, want %04o", filepath.Base(p), got, want)
		}
	}
}
