package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// A magnet's file list comes from the swarm, and the library creates the empty
// files on it as soon as it arrives, before the engine sees the list. An empty
// file whose path climbs out of the torrent's folder lands beside it in the
// download folder, on whatever file is there.
func TestATorrentFileThatClimbsOutOfItsFolderTouchesNothingBesideIt(t *testing.T) {
	requireTorrentClient(t)
	root, err := os.MkdirTemp("", "kl-bt-seeder-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, "Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "Show", "ep01.mkv"))
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(filepath.Join(root, "Show")); err != nil {
		t.Fatal(err)
	}
	info.Files = append(info.Files, metainfo.FileInfo{Path: []string{"..", "victim.mkv"}})
	_, magnet := seedInfo(t, root, info, 0)

	dir, err := os.MkdirTemp("", "kl-bt-escape-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	victim := filepath.Join(dir, "victim.mkv")
	writeFile(t, victim)

	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(15 * time.Second)
	const id = "escape-1"
	e.Start(Job{TaskID: id, URL: magnet, Dir: dir})
	t.Cleanup(func() { e.Remove(id, false) })

	waitFor(t, "the torrent to be refused", 45*time.Second, func() bool {
		return sink.snapshot().Status == core.StatusError
	})
	if b, err := os.ReadFile(victim); err != nil || string(b) != "already here" {
		t.Fatalf("the file beside the torrent's folder holds %q, %v", b, err)
	}
}
