package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

func TestATorrentNeverLandsWhereAnotherDownloadIs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Somebody's folder, and a file still being written.
	if err := os.Mkdir(filepath.Join(dir, "Show.S01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Movie.mkv.part"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{roots: map[string]torrentRoot{}}
	n := 0
	place := func(e *Engine, name string, j Job) string {
		t.Helper()
		n++
		j.TaskID = fmt.Sprint(n)
		j.URL = fmt.Sprintf("magnet:?xt=urn:btih:%040x", n)
		if name != "" {
			j.URL += "&dn=" + name
		}
		opts := &base.Options{Path: dir}
		if err := e.placeTorrent(j, opts); err != nil {
			t.Fatal(err)
		}
		if got := e.roots[j.TaskID].dir; got != opts.Path {
			t.Fatalf("%s is noted in %s but resolved in %s", name, got, opts.Path)
		}
		return opts.Path
	}
	in := func(sub string) string { return filepath.Join(dir, sub) }

	for _, c := range []struct {
		what, name string
		job        Job
		want       string
	}{
		{"a name somebody's folder has", "Show.S01", Job{}, in("Show.S01.1")},
		{"the same name again", "Show.S01", Job{}, in("Show.S01.2")},
		{"a free name", "Other", Job{}, dir},
		{"a name a torrent is about to take", "Other", Job{}, in("Other.1")},
		{"a name a file being written has", "Movie.mkv", Job{}, in("Movie.mkv.1")},
		{"a name the task knows from an earlier start", "", Job{TorrentName: "Show.S01"}, in("Show.S01.3")},
		{"a task name that leaves the folder", "Free", Job{TorrentName: "../Show.S01"}, dir},
	} {
		if got := place(e, c.name, c.job); got != c.want {
			t.Errorf("%s: resolved in %s, want %s", c.what, got, c.want)
		}
	}

	// After a restart the task takes up the place it had, although its name
	// is taken by then, by its own files.
	again := &Engine{roots: map[string]torrentRoot{}}
	for _, prev := range []string{in("Show.S01.1/Show.S01"), in("Other")} {
		if got, want := place(again, filepath.Base(prev), Job{TorrentRoot: prev}), filepath.Dir(prev); got != want {
			t.Errorf("a torrent that landed at %s starts again in %s, want %s", prev, got, want)
		}
	}
	// Unless the task's folder has changed since.
	if got := place(again, "Show.S01", Job{TorrentRoot: filepath.Join(t.TempDir(), "Show.S01")}); got != in("Show.S01.4") {
		t.Errorf("a torrent whose folder changed resolved in %s, want a place of its own in the new one", got)
	}
}

func TestDeletingATorrentTakesOnlyItsOwnFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"Show.S01/Show.S01E01.mkv", "Show.S01/Subs/Show.S01E01.srt", "Show.S01/Show.S01E02.mkv.part",
		// What the torrent does not name: an unpacked file, and another
		// download that ended up inside.
		"Show.S01/unpacked.mkv", "Show.S01/Extras/other.mkv",
		"Nest.1/Nest/a.bin",
	} {
		write(rel)
	}
	torrentRoot{
		dir: dir, path: filepath.Join(dir, "Show.S01"),
		files: []string{"Show.S01/Show.S01E01.mkv", "Show.S01/Subs/Show.S01E01.srt", "Show.S01/Show.S01E02.mkv", "Show.S01/Extras/Show.S01E00.mkv"},
	}.remove()
	nest := filepath.Join(dir, "Nest.1")
	torrentRoot{dir: nest, path: filepath.Join(nest, "Nest"), nest: nest, files: []string{"Nest/a.bin"}}.remove()

	for rel, want := range map[string]bool{
		"Show.S01/Show.S01E01.mkv":      false,
		"Show.S01/Subs":                 false,
		"Show.S01/Show.S01E02.mkv.part": false,
		"Show.S01/unpacked.mkv":         true,
		"Show.S01/Extras/other.mkv":     true,
		"Nest.1":                        false,
	} {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if got := err == nil; got != want {
			t.Errorf("%s is there after the delete: %v, want %v", rel, got, want)
		}
	}
}

// byTask keeps every update, per task.
type byTask struct {
	mu sync.Mutex
	m  map[string][]core.Update
}

func (b *byTask) add(id string, u core.Update) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string][]core.Update{}
	}
	b.m[id] = append(b.m[id], u)
}

// last is the latest status of the task and the file it reported.
func (b *byTask) last(id string) (status core.Status, file, errText string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, u := range b.m[id] {
		if u.Status != "" {
			status, errText = u.Status, u.Err
		}
		if u.File != "" {
			file = u.File
		}
	}
	return status, file, errText
}

func waitDone(t *testing.T, b *byTask, id string) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, file, errText := b.last(id)
		switch {
		case status == core.StatusDone:
			return file
		case status == core.StatusError:
			t.Fatalf("%s failed: %s", id, errText)
		case time.Now().After(deadline):
			t.Fatalf("%s is still %q after a minute", id, status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTwoTorrentsOfOneNameLandApartAndAreDeletedApart(t *testing.T) {
	requireTorrentClient(t)
	_, first := testenv.SeedTorrent(t, "Show.S01", map[string]int{"Show.S01E01.mkv": 40 << 10})
	_, other := testenv.SeedTorrent(t, "Show.S01", map[string]int{"Show.S01E02.mkv": 50 << 10})
	dir, err := os.MkdirTemp("", "kl-bt-apart-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := &byTask{}
	e, err := New(dir, b.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(30 * time.Second)

	firstRoot, otherRoot := filepath.Join(dir, "Show.S01"), filepath.Join(dir, "Show.S01.1", "Show.S01")
	e.Start(Job{TaskID: "first", URL: first, Dir: dir})
	if got := waitDone(t, b, "first"); got != firstRoot {
		t.Errorf("the first torrent reports it landed at %q, want %s", got, firstRoot)
	}
	e.Start(Job{TaskID: "other", URL: other, Dir: dir})
	if got := waitDone(t, b, "other"); got != otherRoot {
		t.Errorf("the other torrent reports it landed at %q, want %s", got, otherRoot)
	}
	for _, f := range []string{filepath.Join(firstRoot, "Show.S01E01.mkv"), filepath.Join(otherRoot, "Show.S01E02.mkv")} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("a torrent's file is not where it should be: %v", err)
		}
	}
	stray := filepath.Join(firstRoot, "notes.txt")
	if err := os.WriteFile(stray, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.Remove("first", true)
	if _, err := os.Stat(filepath.Join(firstRoot, "Show.S01E01.mkv")); !os.IsNotExist(err) {
		t.Errorf("the deleted torrent's file is still there (%v)", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("a file the torrent does not name went with it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(otherRoot, "Show.S01E02.mkv")); err != nil {
		t.Errorf("the other torrent's file went with the first: %v", err)
	}
}
