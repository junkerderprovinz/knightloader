package app

// A renamed package's folder follows the name on disk, with what is already in
// it and what is still being written there.

import (
	"bytes"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// packageAppWith is newPackageApp with more of the settings changed.
func packageAppWith(t *testing.T, mutate func(s *settings.Settings)) (*App, string) {
	t.Helper()
	return newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.SubfolderByPackage = true
		mutate(s)
	})
}

// oldPackage puts a finished part and a waiting one in package "Old", with the
// finished one on disk in the package's folder, and returns that folder.
func oldPackage(t *testing.T, a *App, base string) string {
	t.Helper()
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	finishedTask(t, a, old, "part1", "film.part1.rar")
	editTask(a, "part1", func(x *core.Task) {
		x.Package = "Old"
		x.File = filepath.Join(old, "film.part1.rar")
	})
	putTask(t, a, core.Task{ID: "part2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})
	return old
}

// The folder takes the new name with everything in it, a file no task names
// included, and the rows follow their files. The part still waiting follows
// too, so the set is one folder again once it arrives.
func TestRenamingAPackageMovesItsFilesToTheNewFolder(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	if err := os.WriteFile(filepath.Join(old, "film.sfv"), []byte("sums"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.RenamePackage([]string{"part1", "part2"}, "Film"); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, "Film")
	for _, id := range []string{"part1", "part2"} {
		live := liveTask(a, id)
		if live.Package != "Film" {
			t.Errorf("%s is in package %q, want the new name", id, live.Package)
		}
		if live.Dir != "" {
			t.Errorf("%s had its folder written down as %q; it should go on following the name", id, live.Dir)
		}
		if got := a.TaskFolder(id); got != want {
			t.Errorf("%s downloads to %q, want %q", id, got, want)
		}
	}
	for _, name := range []string{"film.part1.rar", "film.sfv"} {
		if _, err := os.Stat(filepath.Join(want, name)); err != nil {
			t.Errorf("%s did not move with the folder: %v", name, err)
		}
	}
	if got := liveTask(a, "part1").File; got != filepath.Join(want, "film.part1.rar") {
		t.Errorf("the finished part records %q, where its file is not", got)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old folder is still there: %v", err)
	}
}

// With a working folder, what is still in it moves to the working folder of
// the new destination, where the delivery then looks for it.
func TestRenamingAPackageMovesItsShareOfTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, base := packageAppWith(t, func(s *settings.Settings) { s.WorkDir = work })
	staged := stagedIn(t, workdir.For(work, filepath.Join(base, "Old")), "film.mkv", "the whole film")
	stageDone(t, a, "1", "film.mkv")
	editTask(a, "1", func(x *core.Task) { x.Package, x.File = "Old", staged })

	if _, err := a.RenamePackage([]string{"1"}, "New"); err != nil {
		t.Fatal(err)
	}

	delivered := filepath.Join(base, "New", "film.mkv")
	waitFor(t, "the delivery into the new folder", func() bool { return liveTask(a, "1").File == delivered })
	if body, err := os.ReadFile(delivered); err != nil || string(body) != "the whole film" {
		t.Errorf("%s holds %q, %v; want the file from the working folder", delivered, body, err)
	}
	if !gone(t, workdir.For(work, filepath.Join(base, "Old"))) {
		t.Error("the old working folder is still there")
	}
}

// A download running while its package is renamed goes on in the new folder
// from where it was, and finishes there.
func TestRenamingAPackageWhileItDownloadsFinishesInTheNewFolder(t *testing.T) {
	if raceEnabled {
		// A real transfer through gopeed v1.9.3; see startSlow in
		// internal/engine.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	for _, tc := range []struct {
		name string
		work bool
	}{{"straight into the folder", false}, {"through a working folder", true}} {
		t.Run(tc.name, func(t *testing.T) {
			work := ""
			if tc.work {
				work = t.TempDir()
			}
			a, base := packageAppWith(t, func(s *settings.Settings) {
				s.WorkDir = work
				s.MaxConcurrent, s.MaxPerHost = 4, 4
			})
			o := newHeldOrigin(t, 2<<20)
			queueTask(a, &core.Task{ID: "1", URL: o.srv.URL + "/film.mkv", Name: "film.mkv", Package: "Old",
				Status: core.StatusQueued, Enabled: true, CreatedAt: time.Now()})
			waitFor(t, "the first bytes on disk", func() bool {
				live := liveTask(a, "1")
				return live.Status == core.StatusRunning && live.Loaded > 0 && live.File != ""
			})
			before := o.requests()

			if _, err := a.RenamePackage([]string{"1"}, "New"); err != nil {
				t.Fatal(err)
			}
			o.let()

			want := filepath.Join(base, "New", "film.mkv")
			waitFor(t, "the download finishing in the new folder", func() bool {
				live := liveTask(a, "1")
				return live.Status == core.StatusDone && live.File == want
			})
			if got, err := os.ReadFile(want); err != nil || !bytes.Equal(got, o.data) {
				t.Errorf("%s holds %d bytes, %v; want the %d served", want, len(got), err, len(o.data))
			}
			for _, left := range []string{filepath.Join(base, "Old"), workdir.For(work, filepath.Join(base, "Old"))} {
				if _, err := os.Stat(left); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s is still there: %v", left, err)
				}
			}
			after := o.rangesSince(before)
			if len(after) == 0 {
				t.Error("nothing was asked for after the rename, so the transfer was never stopped for the move")
			}
			for _, rg := range after {
				if rg == "" || strings.HasPrefix(rg, "bytes=0-") {
					t.Errorf("after the rename the file was asked for from the start (Range %q)", rg)
				}
			}
		})
	}
}

// When the new name is already a folder with something in it, the rule the
// app applies to a folder a download lands on decides: rename counts the name
// up, skip leaves the files where they are, and ask and overwrite turn the
// rename down with nothing changed.
func TestRenamingAPackageOntoATakenFolderFollowsTheCollisionPolicy(t *testing.T) {
	for _, tc := range []struct {
		policy  string
		refused bool
		folder  string
	}{
		{policy: "rename", folder: "New (2)"},
		{policy: "skip", folder: "Old"},
		{policy: "ask", refused: true, folder: "Old"},
		{policy: "overwrite", refused: true, folder: "Old"},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			a, base := packageAppWith(t, func(s *settings.Settings) { s.CollisionPolicy = tc.policy })
			oldPackage(t, a, base)
			taken := filepath.Join(base, "New")
			if err := os.MkdirAll(taken, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(taken, "theirs.bin"), []byte("theirs"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := a.RenamePackage([]string{"part1", "part2"}, "New")

			var refusal *RenameRefusal
			if tc.refused != errors.As(err, &refusal) {
				t.Fatalf("RenamePackage = %v, want refused: %v", err, tc.refused)
			}
			if tc.refused && refusal.Code != "folderExists" {
				t.Errorf("refused with code %q, want folderExists", refusal.Code)
			}
			wantPkg := "New"
			if tc.refused {
				wantPkg = "Old"
			}
			want := filepath.Join(base, tc.folder)
			for _, id := range []string{"part1", "part2"} {
				if live := liveTask(a, id); live.Package != wantPkg {
					t.Errorf("%s is in package %q, want %q", id, live.Package, wantPkg)
				}
				if got := a.TaskFolder(id); got != want {
					t.Errorf("%s downloads to %q, want %q", id, got, want)
				}
			}
			if _, err := os.Stat(filepath.Join(want, "film.part1.rar")); err != nil {
				t.Errorf("the finished part is not in %s: %v", want, err)
			}
			if body, err := os.ReadFile(filepath.Join(taken, "theirs.bin")); err != nil || string(body) != "theirs" {
				t.Errorf("the file already in the taken folder was touched: %q, %v", body, err)
			}
		})
	}
}

// An archive being unpacked is read from the folder, so the rename waits for
// another go rather than move the volumes out from under it.
func TestAPackageBeingUnpackedIsNotRenamed(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	editTask(a, "part1", func(x *core.Task) { x.Status = core.StatusExtracting })

	_, err := a.RenamePackage([]string{"part1", "part2"}, "Film")

	var refusal *RenameRefusal
	if !errors.As(err, &refusal) || refusal.Code != "busy" {
		t.Fatalf("RenamePackage = %v, want the busy refusal", err)
	}
	if live := liveTask(a, "part2"); live.Package != "Old" {
		t.Errorf("the package became %q despite the refusal", live.Package)
	}
	if _, err := os.Stat(filepath.Join(old, "film.part1.rar")); err != nil {
		t.Errorf("the archive left its folder: %v", err)
	}
}

// JDownloader writes where it was told when the link was handed over, and
// nothing here can stop it or point it elsewhere, so a folder it is still
// writing into keeps its name, and the package keeps the folder.
func TestAFolderJDownloaderIsStillWritingIntoKeepsItsName(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	putTask(t, a, core.Task{ID: "jd", URL: "https://hoster.example/film.part3.rar", Name: "film.part3.rar",
		Package: "Old", Resolver: "jd", Status: core.StatusRunning, Loaded: 100, Enabled: true})

	if _, err := a.RenamePackage([]string{"part1", "part2", "jd"}, "Film"); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"part1", "part2", "jd"} {
		if live := liveTask(a, id); live.Package != "Film" {
			t.Errorf("%s is in package %q, want the new name", id, live.Package)
		}
		if got := a.TaskFolder(id); got != old {
			t.Errorf("%s downloads to %q, want the folder JDownloader writes to, %q", id, got, old)
		}
	}
	if _, err := os.Stat(filepath.Join(old, "film.part1.rar")); err != nil {
		t.Errorf("the finished part left the folder: %v", err)
	}
}

// A torrent is taken out of the engine for the move, since the library opens
// its files in the folder it was added in. One that was downloading waits in
// the queue to start again and take up its files in the new place; one that
// was seeding has stopped.
func TestRenamingAPackageTakesItsTorrentsAlong(t *testing.T) {
	a, base := newPackageApp(t)
	a.SetHalted(true)
	old := filepath.Join(base, "Old")
	for _, name := range []string{"Show", "Film"} {
		if err := os.MkdirAll(filepath.Join(old, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, name, "part.mkv"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	putTask(t, a, core.Task{ID: "seeding", URL: "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a",
		Name: "Show", Package: "Old", Resolver: "torrent", InfoHash: "c12fe1c06bba254a9dc9f519b335aa7c1367a88a",
		Status: core.StatusDone, Seeding: true, File: filepath.Join(old, "Show"), Enabled: true})
	putTask(t, a, core.Task{ID: "fetching", URL: "magnet:?xt=urn:btih:d12fe1c06bba254a9dc9f519b335aa7c1367a88a",
		Name: "Film", Package: "Old", Resolver: "torrent", InfoHash: "d12fe1c06bba254a9dc9f519b335aa7c1367a88a",
		Status: core.StatusRunning, Loaded: 1, File: filepath.Join(old, "Film"), Enabled: true})
	a.mu.Lock()
	a.active["fetching"], a.started["fetching"] = true, true
	a.mu.Unlock()

	if _, err := a.RenamePackage([]string{"seeding", "fetching"}, "New"); err != nil {
		t.Fatal(err)
	}

	seeding, fetching := liveTask(a, "seeding"), liveTask(a, "fetching")
	if seeding.Seeding || seeding.SeedingEnded.IsZero() {
		t.Errorf("the seeding torrent reads Seeding %v, ended %v; it stopped for the move", seeding.Seeding, seeding.SeedingEnded)
	}
	if fetching.Status != core.StatusQueued {
		t.Errorf("the downloading torrent is %q, want it queued to start again", fetching.Status)
	}
	a.mu.Lock()
	active, started, queued := a.active["fetching"], a.started["fetching"], slices.Contains(a.queue, "fetching")
	a.mu.Unlock()
	if active || started || !queued {
		t.Errorf("active %v, started %v, queued %v; want it waiting for a fresh start", active, started, queued)
	}
	for id, name := range map[string]string{"seeding": "Show", "fetching": "Film"} {
		want := filepath.Join(base, "New", name)
		if got := liveTask(a, id).File; got != want {
			t.Errorf("%s records %q, want %q", id, got, want)
		}
		if _, err := os.Stat(filepath.Join(want, "part.mkv")); err != nil {
			t.Errorf("%s's files did not move: %v", id, err)
		}
	}
}

// heldOrigin serves one file with ranges. An answer from the middle of the file
// sends its first 128 KiB and then waits for let, so a download stays part way
// through until the test lets it go on. One from the start is sent slowly
// instead, since the engine reads it while it resolves the link and would sit
// out its read timeout on a held one. It records the Range of every request.
type heldOrigin struct {
	srv  *httptest.Server
	data []byte
	open chan struct{}
	once sync.Once

	mu     sync.Mutex
	ranges []string
}

func newHeldOrigin(t *testing.T, size int) *heldOrigin {
	t.Helper()
	o := &heldOrigin{data: make([]byte, size), open: make(chan struct{})}
	_, _ = cryptorand.Read(o.data)
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(func() {
		o.let()
		o.srv.Close()
	})
	return o
}

func (o *heldOrigin) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.ranges = append(o.ranges, r.Header.Get("Range"))
	o.mu.Unlock()
	lo, hi := 0, len(o.data)-1
	if rg, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		from, to, _ := strings.Cut(rg, "-")
		lo, _ = strconv.Atoi(from)
		if to != "" {
			hi, _ = strconv.Atoi(to)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi, len(o.data)))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(hi-lo+1))
	if r.Header.Get("Range") != "" {
		w.WriteHeader(http.StatusPartialContent)
	}
	const chunk = 32 << 10
	for off := lo; off <= hi; off += chunk {
		if lo == 0 {
			time.Sleep(20 * time.Millisecond)
		} else if off-lo >= 4*chunk {
			select {
			case <-o.open:
			case <-r.Context().Done():
				return
			}
		}
		if _, err := w.Write(o.data[off:min(off+chunk, hi+1)]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
	}
}

// let lets every answer, held or still to come, run to its end.
func (o *heldOrigin) let() { o.once.Do(func() { close(o.open) }) }

func (o *heldOrigin) requests() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ranges)
}

func (o *heldOrigin) rangesSince(n int) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.ranges[n:]...)
}
