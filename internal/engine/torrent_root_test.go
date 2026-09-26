package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// A magnet can carry another name than the torrent it turns out to be, and
// the library creates the torrent's empty files, with its folder, while it
// resolves. A folder of the torrent's name that was there before, or that
// another torrent lands in, is not the magnet's own: it would write into it,
// and removing it with its files would take what is in there.
func TestAMagnetOfAnotherNameStaysOutOfAFolderThatIsNotItsOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var foreign []string
	for _, season := range []string{"Show.S01", "Show.S02", "Show.S03"} {
		p := filepath.Join(dir, season, season+"E02.mkv")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		foreign = append(foreign, p)
	}
	cases := []struct {
		what, dn, name string
		refused        bool
	}{
		{"another name in its link", "Show.S01.PROPER", "Show.S01", true},
		{"no name in its link", "", "Show.S02", true},
		{"a name that differs from the folder's in case only", "Show", "SHOW.S03", true},
		{"a free name", "Other.PROPER", "Other", false},
		{"the name another torrent has just taken, in other case", "Other.REPACK", "OTHER", true},
	}
	// Every magnet is placed before any has resolved, as when several are
	// added at once.
	e := &Engine{roots: map[string]torrentRoot{}}
	for i, c := range cases {
		link := fmt.Sprintf("magnet:?xt=urn:btih:%040x", i+1)
		if c.dn != "" {
			link += "&dn=" + c.dn
		}
		if err := e.placeTorrent(Job{TaskID: fmt.Sprint(i), URL: link}, &base.Options{Path: dir}); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range cases {
		id := fmt.Sprint(i)
		nfo := filepath.Join(e.roots[id].dir, c.name, c.name+".nfo")
		if err := os.MkdirAll(filepath.Dir(nfo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(nfo, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		root, err := e.settleTorrent(id, &base.Resource{Name: c.name, Files: []*base.FileInfo{
			{Name: c.name + "E01.mkv", Size: 10}, {Name: c.name + "E02.mkv", Size: 10}, {Name: c.name + ".nfo"},
		}})
		switch {
		case c.refused && err == nil:
			t.Errorf("%s: the magnet lands at %s", c.what, root)
		case !c.refused && err != nil:
			t.Errorf("%s: the magnet was refused: %v", c.what, err)
		case !c.refused && root != filepath.Join(dir, c.name):
			t.Errorf("%s: the magnet lands at %s, want %s", c.what, root, filepath.Join(dir, c.name))
		}
	}
	for _, p := range foreign {
		if b, err := os.ReadFile(p); err != nil || string(b) != "mine" {
			t.Errorf("%s, in a folder that is not a magnet's, reads %q (%v)", p, b, err)
		}
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

// A magnet reports its file list with the place it lands. Once the engine is
// gone, as after a restart, that list deletes the magnet's files, the empty
// one too, and then its folder.
func TestAMagnetReportsTheFileListItIsDeletedByAfterARestart(t *testing.T) {
	requireTorrentClient(t)
	files := []seedFile{{"Show.S01E01.mkv", 40 << 10}, {"Subs/Show.S01E01.srt", 2 << 10}, {"Show.S01.nfo", 0}}
	_, magnet := seedTorrent(t, "Show.S01", files)
	dir, err := os.MkdirTemp("", "kl-bt-list-*")
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

	e.Start(Job{TaskID: "magnet", URL: magnet, Dir: dir})
	root := waitDone(t, b, "magnet")
	var listed []string
	b.mu.Lock()
	for _, u := range b.m["magnet"] {
		if u.MagnetFiles != nil {
			listed = slices.Clone(u.MagnetFiles)
		}
	}
	b.mu.Unlock()
	want := []string{"Show.S01.nfo", "Show.S01E01.mkv", "Subs/Show.S01E01.srt"}
	if got := slices.Sorted(slices.Values(listed)); !slices.Equal(got, want) {
		t.Errorf("the magnet reported the file list %v, want %v", got, want)
	}

	e.Close()
	DeleteTorrentFiles(dir, root, listed)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("the magnet's folder is still there after its listed files were deleted (%v)", err)
	}
}

// A magnet whose link names it otherwise, or not at all, is only named by the
// swarm, after the library has created its empty files in the folder of its
// real name. When somebody else's folder has that name, the magnet is not
// downloaded, and removing it with its files leaves that folder as it was. A
// free name is the magnet's own, and removing it takes its folder.
func TestAMagnetOfAnotherNameIsNotDownloadedIntoAFolderThatIsNotItsOwn(t *testing.T) {
	requireTorrentClient(t)
	dir, err := os.MkdirTemp("", "kl-bt-dn-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	foreign := filepath.Join(dir, "Show.S01", "Show.S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Episodes of different sizes, so each magnet is a torrent of its own.
	season := func(e01 int) map[string]int {
		return map[string]int{"Show.S01E01.mkv": e01, "Show.S01E02.mkv": 20 << 10, "Show.S01.nfo": 0}
	}
	link := func(magnet, from, to string) string {
		t.Helper()
		out := strings.Replace(magnet, from, to, 1)
		if out == magnet {
			t.Fatalf("%s carries no %s", magnet, from)
		}
		return out
	}
	_, proper := testenv.SeedTorrent(t, "Show.S01", season(40<<10))
	_, nameless := testenv.SeedTorrent(t, "Show.S01", season(41<<10))
	_, film := testenv.SeedTorrent(t, "Film", map[string]int{"Film.mkv": 40 << 10, "Film.nfo": 0})

	b := &byTask{}
	e, err := New(dir, b.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(30 * time.Second)

	for id, uri := range map[string]string{
		"proper":   link(proper, "&dn=Show.S01&", "&dn=Show.S01.PROPER&"),
		"nameless": link(nameless, "&dn=Show.S01&", "&"),
	} {
		e.Start(Job{TaskID: id, URL: uri, Dir: dir})
		deadline := time.Now().Add(45 * time.Second)
		for {
			status, _, errText := b.last(id)
			if status == core.StatusError {
				if !strings.Contains(errText, "already exists") {
					t.Errorf("%s failed with %q, want the folder of its name named as taken", id, errText)
				}
				break
			}
			if status == core.StatusDone || time.Now().After(deadline) {
				t.Fatalf("%s is %q, want it refused", id, status)
			}
			time.Sleep(100 * time.Millisecond)
		}
		e.Remove(id, true)
	}
	// As the file list arrives, before its name can be checked, the library
	// renames a file at one of the torrent's paths to .part when its size is
	// not the torrent's.
	got, err := os.ReadFile(foreign)
	if err != nil {
		got, err = os.ReadFile(foreign + ".part")
	}
	if err != nil || string(got) != "mine" {
		t.Errorf("the file in the folder that is not the magnets' reads %q (%v)", got, err)
	}

	e.Start(Job{TaskID: "film", URL: link(film, "&dn=Film&", "&dn=Film.PROPER&"), Dir: dir})
	root := waitDone(t, b, "film")
	if want := filepath.Join(dir, "Film"); root != want {
		t.Fatalf("the film landed at %s, want %s", root, want)
	}
	e.Remove("film", true)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("the film's folder is still there after it was removed with its files (%v)", err)
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
