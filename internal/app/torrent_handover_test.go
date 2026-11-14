package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestTheBuiltInClientsTorrentsAreReadiedForQBittorrentOnce(t *testing.T) {
	data, dir := t.TempDir(), t.TempDir()
	root := filepath.Join(dir, "Pack")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{"a.bin.part": "half", "sub/b.bin": "whole", "sub/b.bin.part": "stale"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := []core.TorrentFile{{Path: "a.bin", Size: 8}, {Path: "sub/b.bin", Size: 5}}
	running := &core.Task{ID: "run", Resolver: "torrent", Status: core.StatusRunning, File: root, TorrentFiles: files}
	done := &core.Task{ID: "done", Resolver: "torrent", Status: core.StatusDone, File: filepath.Join(dir, "movie.mkv")}
	http := &core.Task{ID: "web", Resolver: "direct", Status: core.StatusDone, File: filepath.Join(dir, "page")}

	check := handOverTorrents(data, []*core.Task{running, done, http})
	if len(check) != 1 || !check["done"] {
		t.Errorf("checked on seeding: %v", check)
	}
	if b, err := os.ReadFile(filepath.Join(root, "a.bin")); err != nil || string(b) != "half" {
		t.Errorf("a.bin = %q, %v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "sub", "b.bin")); string(b) != "whole" {
		t.Errorf("b.bin was replaced by its old part file: %q", b)
	}
	if again := handOverTorrents(data, []*core.Task{running, done}); again != nil {
		t.Errorf("handed over twice: %v", again)
	}
}
