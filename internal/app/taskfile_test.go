package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// delegatedFileApp wires the "elsewhere" backend (see collision_policy_test.go)
// and returns the app, the backend and the download folder.
func delegatedFileApp(t *testing.T) (*App, *stubBackend, string) {
	t.Helper()
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { s.MaxConcurrent, s.MaxPerHost = 4, 4 })
	stub := &stubBackend{got: make(chan string, 4)}
	a.bmu.Lock()
	a.debrid["elsewhere"] = stub
	a.bmu.Unlock()
	a.Registry.Register(elsewhereResolver{})
	return a, stub, dir
}

// queueTask puts a task in the wait queue and runs one dispatch pass.
func queueTask(a *App, task *core.Task) {
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
}

// fileBytes writes n bytes of zeros, which is what the download library leaves
// of a transfer that died before its first byte: the whole length reserved.
func fileBytes(t *testing.T, path string, n int) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// The library has forgotten the transfer after a restart, so the file it
// half-wrote is still under the task's name. Left there, the new attempt is
// written beside it as "set.part3 (1).rar" and the set is read from the wrong
// file.
func TestANewAttemptDeletesTheFileItsLastAttemptLeft(t *testing.T) {
	a, stub, dir := delegatedFileApp(t)
	partial := fileBytes(t, filepath.Join(dir, "set.part3.rar"), 2048)

	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/set.part3.rar", Name: "set.part3.rar",
		Resolver: "elsewhere", Status: core.StatusQueued, Enabled: true, Size: 2048, File: partial,
	})
	collect(t, stub.got, 1)

	if fileExists(partial) {
		t.Error("the last attempt's file is still there, so the new one will be written beside it")
	}
	if got := liveTask(a, "1").File; got != "" {
		t.Errorf("the task still records %q; the new attempt reports its own file", got)
	}
}

// A path another task recorded is that task's file, however the two came to
// share a name.
func TestAFileAnotherTaskRecordedIsNotDeleted(t *testing.T) {
	a, stub, dir := delegatedFileApp(t)
	shared := fileBytes(t, filepath.Join(dir, "film.mkv"), 2048)
	a.mu.Lock()
	a.tasks["2"] = &core.Task{
		ID: "2", URL: "https://elsewhere.example/mirror/film.mkv", Name: "film.mkv",
		Status: core.StatusDone, Enabled: true, Size: 2048, File: shared,
	}
	a.mu.Unlock()

	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv",
		Resolver: "elsewhere", Status: core.StatusQueued, Enabled: true, Size: 2048, File: shared,
	})
	collect(t, stub.got, 1)

	if !fileExists(shared) {
		t.Error("a file another task recorded was deleted")
	}
}

// Skip turns a download down when somebody else's file has its name. The
// task's own leftover is not that, or a download interrupted once could never
// run again.
func TestSkipDoesNotRefuseATaskItsOwnLeftover(t *testing.T) {
	a, stub, dir := delegated(t, collide.Skip)
	own := filepath.Join(dir, "clash.bin")

	queueTask(a, &core.Task{
		ID: "d1", URL: "https://elsewhere.example/clash.bin", Name: "clash.bin", Resolver: "elsewhere",
		Status: core.StatusQueued, Enabled: true, Size: int64(len("already here")), File: own,
	})
	collect(t, stub.got, 1)

	if live := liveTask(a, "d1"); live.Status == core.StatusError {
		t.Fatalf("the task was refused over its own leftover: %s", live.Error)
	}
	if fileExists(own) {
		t.Error("the leftover is still there, so the new attempt will be written beside it")
	}
}

// A file at the recorded path that is not the size the task was writing is
// not its leftover, so Skip treats it like any other file in the way.
func TestSkipRefusesAFileAtTheRecordedPathOfAnotherSize(t *testing.T) {
	a, stub, dir := delegated(t, collide.Skip)
	theirs := filepath.Join(dir, "clash.bin")

	queueTask(a, &core.Task{
		ID: "d1", URL: "https://elsewhere.example/clash.bin", Name: "clash.bin", Resolver: "elsewhere",
		Status: core.StatusQueued, Enabled: true, Size: 2048, File: theirs,
	})

	expectNone(t, stub.got)
	if live := liveTask(a, "d1"); live.Status != core.StatusError || !strings.Contains(live.Error, "already exists") {
		t.Errorf("the task ended %q (%s), want it refused", live.Status, live.Error)
	}
	if got, err := os.ReadFile(theirs); err != nil || string(got) != "already here" {
		t.Errorf("clash.bin = %q, %v; it was never the task's", got, err)
	}
}

// The library reserves the whole length up front, so a file of any other size
// at the recorded path is not the one this task was writing.
func TestAFileOfAnotherSizeAtTheRecordedPathIsNotDeleted(t *testing.T) {
	a, stub, dir := delegatedFileApp(t)
	theirs := fileBytes(t, filepath.Join(dir, "film.mkv"), 100)

	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv",
		Resolver: "elsewhere", Status: core.StatusQueued, Enabled: true, Size: 2048, File: theirs,
	})
	collect(t, stub.got, 1)

	if !fileExists(theirs) {
		t.Error("a file that is not the size this task was writing was deleted")
	}
}

// The task keeps the name it resolved, which set detection keys on, and learns
// where its bytes really are, and both survive the next restart.
func TestTheFileABackendReportsIsRecordedAndSaved(t *testing.T) {
	a := newQueueApp(t)
	written := filepath.Join(t.TempDir(), "set.part3 (1).rar")
	a.mu.Lock()
	a.tasks["1"] = &core.Task{ID: "1", URL: "https://host.example/set.part3.rar", Name: "set.part3.rar", Status: core.StatusRunning, Enabled: true}
	a.active["1"] = true
	a.mu.Unlock()

	a.onUpdate("1", core.Update{Status: core.StatusRunning, Loaded: 10, File: written})

	live := liveTask(a, "1")
	if live.File != written || live.Name != "set.part3.rar" {
		t.Fatalf("the task reads name %q, file %q; want set.part3.rar and %q", live.Name, live.File, written)
	}
	all, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range all {
		if saved.ID == "1" && saved.File != written {
			t.Errorf("the stored task records %q, want %q", saved.File, written)
		}
	}
}

// Removing a download together with its files has to reach the file after a
// restart too, when the library has forgotten the transfer.
func TestRemovingWithFilesDeletesTheFileTheTaskRecorded(t *testing.T) {
	a, _, dir := delegatedFileApp(t)
	mine := fileBytes(t, filepath.Join(dir, "film (1).mkv"), 2048)
	theirs := fileBytes(t, filepath.Join(dir, "film.mkv"), 2048)
	a.mu.Lock()
	a.tasks["1"] = &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv", Resolver: "elsewhere",
		Status: core.StatusDone, Enabled: true, Size: 2048, File: mine,
	}
	a.mu.Unlock()

	a.Remove("1", true)

	if fileExists(mine) {
		t.Error("the task's own file survived a removal with files")
	}
	if !fileExists(theirs) {
		t.Error("the file under the task's name was deleted, and it was never the task's")
	}
}

// A single archive written beside a file of its name is unpacked from where it
// was written.
func TestASingleArchiveIsUnpackedFromTheFileItsDownloadWrote(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = false, false })
	if err := os.WriteFile(filepath.Join(base, "release.zip"), []byte("not the download"), 0o644); err != nil {
		t.Fatal(err)
	}
	written := filepath.Join(base, "release (1).zip")
	writeZip(t, written, "inside.txt", "unpacked")
	stageDone(t, a, "1", "release.zip")
	editTask(a, "1", func(task *core.Task) { task.File = written })

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, "1")
		return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
	})
	if j, _ := jobFor(a, "1"); j.Status != ExtractDone {
		t.Fatalf("the job ended %q: %s", j.Status, j.Error)
	}
	if _, err := os.Stat(filepath.Join(base, "release (1)", "inside.txt")); err != nil {
		t.Errorf("the archive the download wrote was not unpacked: %v", err)
	}
}

// A download saved as "release (1).rar" and renamed back to its own name by
// hand: the file it recorded is gone, and the archive is where its name puts
// it, so that is what is unpacked.
func TestASingleArchiveRenamedBackByHandIsUnpackedUnderItsOwnName(t *testing.T) {
	for _, name := range []string{"release.rar", "release.zip"} {
		t.Run(name, func(t *testing.T) {
			a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = false, false })
			own := filepath.Join(base, name)
			if filepath.Ext(name) == ".zip" {
				writeZip(t, own, "inside.txt", "unpacked")
			} else {
				vols := extracttest.RarSet(1<<20, extracttest.File{Name: "inside.txt", Body: []byte("unpacked")})
				if err := os.WriteFile(own, vols[0], 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stageDone(t, a, "1", name)
			editTask(a, "1", func(task *core.Task) { task.File = filepath.Join(base, "release (1)"+filepath.Ext(name)) })

			if err := a.StartExtraction([]string{"1"}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the job settling", func() bool {
				j, ok := jobFor(a, "1")
				return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
			})
			if j, _ := jobFor(a, "1"); j.Status != ExtractDone {
				t.Fatalf("the job ended %q: %s", j.Status, j.Error)
			}
			if got, err := os.ReadFile(filepath.Join(base, "release", "inside.txt")); err != nil || string(got) != "unpacked" {
				t.Errorf("inside.txt = %q, %v; the archive under its own name was not unpacked", got, err)
			}
		})
	}
}

// The reader finds every part after the first by name. A part whose download
// had to be written under another name would be read from whatever holds the
// name, and the failure would read as a damaged archive.
func TestAPartWrittenUnderAnotherNameStopsTheExtractionNamingBothFiles(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = false, false })
	paths := extracttest.WriteRarSet(t, base, "set", extracttest.RarSet(1024,
		extracttest.File{Name: "movie.bin", Body: bytes.Repeat([]byte("m"), 1500)},
	))
	written := filepath.Join(base, "set.part2 (1).rar")
	if err := os.Rename(paths[1], written); err != nil {
		t.Fatal(err)
	}
	theirs := []byte("somebody else's file")
	if err := os.WriteFile(paths[1], theirs, 0o644); err != nil {
		t.Fatal(err)
	}
	stageDone(t, a, "1", "set.part1.rar")
	stageDone(t, a, "2", "set.part2.rar")
	editTask(a, "2", func(task *core.Task) { task.File = written })

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, "1")
		return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
	})
	j, _ := jobFor(a, "1")
	if j.Status != ExtractFailed {
		t.Fatalf("the job ended %q, want it refused", j.Status)
	}
	if !strings.Contains(j.Error, paths[1]) || !strings.Contains(j.Error, written) {
		t.Errorf("the refusal reads %q, want both %s and %s named", j.Error, paths[1], written)
	}
	if fileExists(filepath.Join(base, "set")) {
		t.Error("something was unpacked from the wrong file")
	}
	if got, err := os.ReadFile(paths[1]); err != nil || !bytes.Equal(got, theirs) {
		t.Errorf("the file under the part's name was touched: %q, %v", got, err)
	}
}

// Once the part is moved to where the refusal said, the set unpacks, although
// the download still records the name it was saved under.
func TestAPartMovedToWhereTheArchiveLooksIsUnpacked(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = false, false })
	movie := bytes.Repeat([]byte("m"), 1500)
	paths := extracttest.WriteRarSet(t, base, "set", extracttest.RarSet(1024,
		extracttest.File{Name: "movie.bin", Body: movie},
	))
	stageDone(t, a, "1", filepath.Base(paths[0]))
	stageDone(t, a, "2", filepath.Base(paths[1]))
	editTask(a, "2", func(task *core.Task) { task.File = filepath.Join(base, "set.part2 (1).rar") })

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, "1")
		return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
	})
	if j, _ := jobFor(a, "1"); j.Status != ExtractDone {
		t.Fatalf("the job ended %q: %s", j.Status, j.Error)
	}
	if got, err := os.ReadFile(filepath.Join(base, "set", "movie.bin")); err != nil || !bytes.Equal(got, movie) {
		t.Errorf("movie.bin came out as %d bytes, %v; want %d", len(got), err, len(movie))
	}
}

// A part downloaded into another folder is not beside the first one, where
// the reader looks for it.
func TestAPartDownloadedIntoAnotherFolderStopsTheExtraction(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = false, false })
	paths := extracttest.WriteRarSet(t, base, "set", extracttest.RarSet(1024,
		extracttest.File{Name: "movie.bin", Body: bytes.Repeat([]byte("m"), 1500)},
	))
	elsewhere := filepath.Join(t.TempDir(), "set.part2.rar")
	if err := os.Rename(paths[1], elsewhere); err != nil {
		t.Fatal(err)
	}
	stageDone(t, a, "1", "set.part1.rar")
	stageDone(t, a, "2", "set.part2.rar")
	editTask(a, "2", func(task *core.Task) { task.File = elsewhere })

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, "1")
		return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
	})
	j, _ := jobFor(a, "1")
	if j.Status != ExtractFailed {
		t.Fatalf("the job ended %q, want it refused", j.Status)
	}
	if !strings.Contains(j.Error, filepath.Dir(elsewhere)) || !strings.Contains(j.Error, base) {
		t.Errorf("the refusal reads %q, want both folders named", j.Error)
	}
}

// Deleting the archive after unpacking has to reach every volume of a rar
// set, not only a lone archive.
func TestAnUnpackedRarSetIsDeletedWhenTheSettingSaysSo(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.ArchiveDisposal = "delete"
	})
	paths := extracttest.WriteRarSet(t, base, "set", extracttest.RarSet(1024,
		extracttest.File{Name: "movie.bin", Body: bytes.Repeat([]byte("m"), 2500)},
	))
	for i, p := range paths {
		stageDone(t, a, string(rune('1'+i)), filepath.Base(p))
	}

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, "1")
		return ok && (j.Status == ExtractDone || j.Status == ExtractFailed)
	})
	if j, _ := jobFor(a, "1"); j.Status != ExtractDone {
		t.Fatalf("the job ended %q: %s", j.Status, j.Error)
	}
	for _, p := range paths {
		if fileExists(p) {
			t.Errorf("%s is still there after the set was unpacked and the archive was to be deleted", filepath.Base(p))
		}
	}
}

// A rename rule moves the file the download wrote, not a file that merely has
// the name the download resolved.
func TestARenameRuleMovesTheFileTheDownloadWrote(t *testing.T) {
	a, dir := newRuleApp(t, func(*settings.Settings, string) {})
	theirs := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(theirs, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "movie (1).mkv")
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks["1"] = &core.Task{
		ID: "1", URL: "https://host.example/movie.mkv", Name: "movie.mkv", Filename: "Film.mkv",
		Status: core.StatusRunning, Enabled: true, File: mine,
	}
	a.active["1"] = true
	a.mu.Unlock()

	a.onUpdate("1", core.Update{Status: core.StatusDone})

	if got, err := os.ReadFile(filepath.Join(dir, "Film.mkv")); err != nil || string(got) != "mine" {
		t.Errorf("Film.mkv = %q, %v; want the download", got, err)
	}
	if got, err := os.ReadFile(theirs); err != nil || string(got) != "theirs" {
		t.Errorf("movie.mkv = %q, %v; it was never the download's", got, err)
	}
	if live := liveTask(a, "1"); live.File != filepath.Join(dir, "Film.mkv") {
		t.Errorf("the task records %q after the rename", live.File)
	}
}

// Moving a finished download out of the working folder moves the file it
// wrote, and the task follows it.
func TestDeliveryMovesTheFileTheDownloadWrote(t *testing.T) {
	work := t.TempDir()
	a, dest := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = work })
	task := &core.Task{ID: "1", URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Enabled: true}
	staged := a.workDirFor(task)
	task.File = filepath.Join(staged, "movie (1).mkv")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(task.File, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks["1"] = task
	a.mu.Unlock()

	a.deliverDownload("1")

	moved := filepath.Join(dest, "movie (1).mkv")
	if got, err := os.ReadFile(moved); err != nil || string(got) != "mine" {
		t.Fatalf("%s = %q, %v; want the download moved there", moved, got, err)
	}
	if live := liveTask(a, "1"); live.File != moved {
		t.Errorf("the task records %q, want %q", live.File, moved)
	}
}

// A download renamed back to its own name by hand in the working folder is
// moved from there, and the task follows it to where it landed.
func TestDeliveryMovesAFileRenamedBackByHand(t *testing.T) {
	work := t.TempDir()
	a, dest := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = work })
	task := &core.Task{ID: "1", URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Enabled: true}
	staged := a.workDirFor(task)
	task.File = filepath.Join(staged, "movie (1).mkv")
	renamed := filepath.Join(staged, "movie.mkv")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamed, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks["1"] = task
	a.mu.Unlock()

	a.deliverDownload("1")

	moved := filepath.Join(dest, "movie.mkv")
	if got, err := os.ReadFile(moved); err != nil || string(got) != "mine" {
		t.Fatalf("%s = %q, %v; want the download moved there", moved, got, err)
	}
	if live := liveTask(a, "1"); live.File != moved {
		t.Errorf("the task records %q, want %q", live.File, moved)
	}
}

// fakeDebrid unlocks every link to url, the way a debrid service hands back a
// direct link on its own server. With fresh set, every unlock after the first
// hands out fresh instead.
type fakeDebrid struct {
	url   string
	fresh string
	size  int64

	unlocks *atomic.Int32
}

func (fakeDebrid) ID() string    { return "fakedebrid" }
func (fakeDebrid) Label() string { return "Fake Debrid" }

func (fakeDebrid) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{"hoster.example": true}, nil
}

func (f fakeDebrid) Unlock(context.Context, string) (debrid.Direct, error) {
	url := f.url
	if f.unlocks.Add(1) > 1 && f.fresh != "" {
		url = f.fresh
	}
	return debrid.Direct{URL: url, Name: "set.part3.rar", Size: f.size}, nil
}

// wireDebrid puts svc in the app the way rewireBackends does, handing its links
// to the engine through engineHandoff.
func wireDebrid(a *App, svc fakeDebrid) {
	a.bmu.Lock()
	a.debrid[svc.ID()] = debrid.NewBackend(svc, engineHandoff{a.Engine, a}, a.onUpdate)
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: svc.ID(), Prio: 90, Hosts: map[string]bool{"hoster.example": true}, Svc: svc})
}

// debridTask queues one task for the fake debrid service and waits for it to
// settle.
func debridTask(t *testing.T, a *App, task core.Task) core.Task {
	t.Helper()
	task.ID, task.URL, task.Resolver = "1", "https://hoster.example/file/abc", "fakedebrid"
	task.Status, task.Enabled = core.StatusQueued, true
	queueTask(a, &task)
	waitFor(t, "the download finishing", func() bool {
		s := liveTask(a, "1").Status
		return s == core.StatusDone || s == core.StatusError
	})
	return liveTask(a, "1")
}

// The whole path a debrid download takes after a restart: the leftover of the
// attempt before it goes, the engine writes the file under the task's own name,
// and the task records that file.
func TestARestartedDebridDownloadLandsUnderItsOwnName(t *testing.T) {
	if raceEnabled {
		// A real transfer through gopeed v1.9.3, which races on the task's
		// status when it starts; see internal/engine's settle.
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { s.MaxConcurrent, s.MaxPerHost = 4, 4 })
	body := bytes.Repeat([]byte("volume three "), 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "set.part3.rar", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()
	wireDebrid(a, fakeDebrid{url: srv.URL + "/set.part3.rar", size: int64(len(body)), unlocks: new(atomic.Int32)})

	canonical := fileBytes(t, filepath.Join(dir, "set.part3.rar"), len(body))
	live := debridTask(t, a, core.Task{Name: "set.part3.rar", Size: int64(len(body)), File: canonical})

	if live.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", live.Status, live.Error)
	}
	if live.File != canonical {
		t.Errorf("the task records %q, want %q", live.File, canonical)
	}
	if got, err := os.ReadFile(canonical); err != nil || !bytes.Equal(got, body) {
		t.Errorf("%s holds %d bytes, %v; want the download", canonical, len(got), err)
	}
	if fileExists(filepath.Join(dir, "set.part3 (1).rar")) {
		t.Error("the download was written beside its own leftover")
	}
}

// A debrid download lands in the folder the list shows for it, here its
// category's, and not in the engine's own folder.
func TestADebridDownloadLandsInTheFolderTheListShows(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	films := filepath.Join(t.TempDir(), "filme")
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.MaxConcurrent, s.MaxPerHost = 4, 4
		s.Categories = []settings.Category{{ID: "filme", Dir: films}}
	})
	body := bytes.Repeat([]byte("a film "), 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "film.mkv", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()
	wireDebrid(a, fakeDebrid{url: srv.URL + "/film.mkv", size: int64(len(body)), unlocks: new(atomic.Int32)})

	live := debridTask(t, a, core.Task{Category: "filme"})

	if live.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", live.Status, live.Error)
	}
	want := filepath.Join(films, "film.mkv")
	if live.File != want {
		t.Errorf("the download was written to %s, want %s in the category's folder", live.File, want)
	}
	if got, err := os.ReadFile(want); err != nil || !bytes.Equal(got, body) {
		t.Errorf("%s holds %d bytes, %v; want the download", want, len(got), err)
	}
}

// A debrid link that stops answering part way through: the part it refused is
// fetched from a fresh unlock, and only then is the download done.
func TestADebridDownloadWhoseLinkExpiresIsFinishedFromAFreshUnlock(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { s.MaxConcurrent, s.MaxPerHost = 4, 4 })
	body := make([]byte, 4<<20)
	for i := range body {
		body[i] = byte(i * 7)
	}
	var refused atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first link refuses the second half of the file, as an expired
		// one does part way through.
		if r.URL.Path == "/old/set.part3.rar" {
			if rg, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
				start, _, _ := strings.Cut(rg, "-")
				if n, _ := strconv.Atoi(start); n >= len(body)/2 {
					refused.Add(1)
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}
		http.ServeContent(w, r, "set.part3.rar", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()
	svc := fakeDebrid{url: srv.URL + "/old/set.part3.rar", fresh: srv.URL + "/fresh/set.part3.rar", size: int64(len(body)), unlocks: new(atomic.Int32)}
	wireDebrid(a, svc)

	// Two connections, so the refused range is the second one's.
	live := debridTask(t, a, core.Task{Name: "set.part3.rar", Size: int64(len(body)), Chunks: 2})

	if live.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", live.Status, live.Error)
	}
	if refused.Load() == 0 {
		t.Fatal("no range was refused, so this proves nothing")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "set.part3.rar")); err != nil || !bytes.Equal(got, body) {
		t.Errorf("the download holds %d bytes, %v; want the whole file", len(got), err)
	}
	if svc.unlocks.Load() < 2 {
		t.Error("the link was never unlocked again")
	}
}
