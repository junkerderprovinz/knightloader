package app

// A finished file the app moves leaves its backend's record behind. Another
// download can land on that path afterwards, and removing or restarting the
// moved one must not take that download's file.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// recordKeeper deletes a removed download's file where it wrote it, as the
// download library and the FTP backend do: by their own record, wherever the
// app has moved the file since.
type recordKeeper struct{ wrote map[string]string }

func (recordKeeper) Download(string, string, map[string]string, int) {}
func (recordKeeper) Pause(string)                                    {}
func (recordKeeper) Resume(string)                                   {}
func (k recordKeeper) Remove(id string, deleteFiles bool) {
	if deleteFiles {
		_ = os.Remove(k.wrote[id])
	}
}

// finishedAt puts download "moved" in the list as finished at path, where its
// backend wrote it, and gives download "other" the same backend and path.
func finishedAt(t *testing.T, a *App, path, pkg string) {
	t.Helper()
	withBackend(a, "elsewhere", recordKeeper{wrote: map[string]string{"moved": path, "other": path}})
	a.Registry.Register(elsewhereResolver{})
	fileBytes(t, path, 4)
	putTask(t, a, core.Task{ID: "moved", URL: "https://elsewhere.example/nocd", Name: "nocd", Package: pkg,
		Resolver: "elsewhere", Status: core.StatusDone, Enabled: true, Size: 4, File: path})
}

func noUnpacking(s *settings.Settings) { s.Extract, s.VerifyChecksums = false, false }

// appMoves are the ways the app moves a finished file. Each returns the app,
// the path download "moved" was written to and the one its file went to.
var appMoves = []struct {
	name string
	move func(t *testing.T) (a *App, from, to string)
}{
	{"renamed", func(t *testing.T) (*App, string, string) {
		a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
		from := filepath.Join(dir, "nocd")
		finishedAt(t, a, from, "")
		name := "Report.pdf"
		if err := a.SetTaskOptions([]string{"moved"}, TaskOptions{Name: &name}); err != nil {
			t.Fatal(err)
		}
		return a, from, filepath.Join(dir, name)
	}},
	{"delivered out of the working folder", func(t *testing.T) (*App, string, string) {
		work := t.TempDir()
		a, dir := newRuleApp(t, func(s *settings.Settings, _ string) {
			noUnpacking(s)
			s.WorkDir = work
		})
		from := filepath.Join(workdir.For(work, dir), "nocd")
		finishedAt(t, a, from, "")
		a.deliverDownload("moved")
		return a, from, filepath.Join(dir, "nocd")
	}},
	{"moved with its package's folder", func(t *testing.T) (*App, string, string) {
		a, dir := packageAppWith(t, noUnpacking)
		from := filepath.Join(dir, "Old", "nocd")
		finishedAt(t, a, from, "Old")
		if _, err := a.RenamePackage([]string{"moved"}, "New"); err != nil {
			t.Fatal(err)
		}
		return a, from, filepath.Join(dir, "New", "nocd")
	}},
}

// otherOnTheOldPath moves download "moved" and lets download "other" finish on
// the path it left. It returns the app, that path and the moved file's.
func otherOnTheOldPath(t *testing.T, move func(*testing.T) (*App, string, string)) (*App, string, string) {
	t.Helper()
	a, from, to := move(t)
	if got := liveTask(a, "moved").File; got != to {
		t.Fatalf("the moved download records %q, want %q", got, to)
	}
	if err := os.MkdirAll(filepath.Dir(from), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(from, []byte("the other download"), 0o644); err != nil {
		t.Fatal(err)
	}
	putTask(t, a, core.Task{ID: "other", URL: "https://elsewhere.example/mirror/nocd", Name: "nocd", Resolver: "elsewhere",
		Status: core.StatusDone, Enabled: true, Size: int64(len("the other download")), File: from})
	return a, from, to
}

// Two rows can record one path when the first one's file was deleted by hand
// before the second finished there. The backend of the first still holds the
// path, and only the last row that records it may have the backend delete it.
func TestABackendDeletesARemovedDownloadsFileOnlyWhenNoOtherRowRecordsIt(t *testing.T) {
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
	path := filepath.Join(dir, "report.pdf")
	withBackend(a, "elsewhere", recordKeeper{wrote: map[string]string{"first": path, "second": path}})
	a.Registry.Register(elsewhereResolver{})
	fileBytes(t, path, 8)
	for id, size := range map[string]int64{"first": 4, "second": 8} {
		putTask(t, a, core.Task{ID: id, URL: "https://elsewhere.example/" + id + "/report.pdf", Name: "report.pdf",
			Resolver: "elsewhere", Status: core.StatusDone, Enabled: true, Size: size, File: path})
	}

	a.Remove("first", true)

	if !fileExists(path) {
		t.Fatal("removing the first row with its files deleted the file the second row records")
	}

	a.Remove("second", true)

	if fileExists(path) {
		t.Error("the second row's own file survived a removal with files")
	}
}

func TestRestartingADownloadSparesTheFileAnotherRowRecordsAtItsPath(t *testing.T) {
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
	path := filepath.Join(dir, "report.pdf")
	withBackend(a, "elsewhere", recordKeeper{wrote: map[string]string{"first": path, "second": path}})
	a.Registry.Register(elsewhereResolver{})
	fileBytes(t, path, 8)
	for id, size := range map[string]int64{"first": 4, "second": 8} {
		putTask(t, a, core.Task{ID: id, URL: "https://elsewhere.example/" + id + "/report.pdf", Name: "report.pdf",
			Resolver: "elsewhere", Status: core.StatusDone, Enabled: true, Size: size, File: path})
	}

	a.RestartTasks([]string{"first"})

	if !fileExists(path) {
		t.Fatal("restarting the first row deleted the file the second row records")
	}
}

func TestRemovingAMovedDownloadWithItsFilesSparesTheFileNowAtItsOldPath(t *testing.T) {
	for _, m := range appMoves {
		t.Run(m.name, func(t *testing.T) {
			a, from, to := otherOnTheOldPath(t, m.move)

			a.Remove("moved", true)

			if got, err := os.ReadFile(from); err != nil || string(got) != "the other download" {
				t.Errorf("the other download's file reads %q, %v", got, err)
			}
			if fileExists(to) {
				t.Error("the removed download's own file is still there")
			}
		})
	}
}

func TestRestartingAMovedDownloadSparesTheFileNowAtItsOldPath(t *testing.T) {
	for _, m := range appMoves {
		t.Run(m.name, func(t *testing.T) {
			a, from, to := otherOnTheOldPath(t, m.move)

			a.RestartTasks([]string{"moved"})

			waitFor(t, "the new attempt clearing the moved file", func() bool { return !fileExists(to) })
			if got, err := os.ReadFile(from); err != nil || string(got) != "the other download" {
				t.Errorf("the other download's file reads %q, %v", got, err)
			}
		})
	}
}

// The download library deletes in the folder it wrote to, which for a download
// fetched through the working folder is that folder, long after the file was
// delivered.
func TestRemovingADeliveredDownloadSparesTheNextOneInTheWorkingFolder(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	work := t.TempDir()
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) {
		noUnpacking(s)
		s.WorkDir = work
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "nocd", time.Time{}, strings.NewReader("the first download"))
	}))
	t.Cleanup(srv.Close)
	queueTask(a, &core.Task{ID: "moved", URL: srv.URL + "/nocd", Name: "nocd", Status: core.StatusQueued,
		Enabled: true, CreatedAt: time.Now()})
	delivered := filepath.Join(dir, "nocd")
	waitFor(t, "the delivery", func() bool {
		live := liveTask(a, "moved")
		return live.Status == core.StatusDone && live.File == delivered
	})
	staged := filepath.Join(workdir.For(work, dir), "nocd")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("the other download"), 0o644); err != nil {
		t.Fatal(err)
	}
	putTask(t, a, core.Task{ID: "other", URL: srv.URL + "/mirror/nocd", Name: "nocd", Status: core.StatusDone,
		Enabled: true, Size: int64(len("the other download")), File: staged})

	a.Remove("moved", true)

	if got, err := os.ReadFile(staged); err != nil || string(got) != "the other download" {
		t.Errorf("the other download's file reads %q, %v", got, err)
	}
	if fileExists(delivered) {
		t.Error("the removed download's own file is still there")
	}
}
