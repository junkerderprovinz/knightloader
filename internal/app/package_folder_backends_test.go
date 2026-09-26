package app

// What a package rename asks of each kind of backend that writes into the
// folder: stop, and carry on in the new one.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// pausingBackend writes nowhere and stands in for a backend that has nothing
// on disk yet, such as a debrid service still unlocking a link: its Pause
// reports a pause, and Resume a run again. Each call is recorded with the
// folder the task derived at that moment.
type pausingBackend struct {
	a     *App
	mu    sync.Mutex
	calls []string
}

func (b *pausingBackend) record(op, id string) {
	dir := filepath.Base(b.a.taskDir(id))
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, op+" "+dir)
}

func (b *pausingBackend) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

func (b *pausingBackend) Download(string, string, map[string]string, int) {}
func (b *pausingBackend) Remove(string, bool)                             {}

func (b *pausingBackend) Pause(id string) {
	b.record("pause", id)
	b.a.onUpdate(id, core.Update{Status: core.StatusPaused})
}

func (b *pausingBackend) Resume(id string) {
	b.record("resume", id)
	b.a.onUpdate(id, core.Update{Status: core.StatusRunning})
}

// haltingBackend is one that can stop a transfer and wait for it, as yt-dlp
// and the FTP/SFTP backend can.
type haltingBackend struct{ pausingBackend }

func (b *haltingBackend) Halt(id string) bool {
	b.record("halt", id)
	return true
}

// recordingBackend is recording a live stream for every task it has.
type recordingBackend struct{ pausingBackend }

func (b *recordingBackend) Recording(string) bool { return true }

// withBackend wires be under id, the way a debrid account slot is wired.
func withBackend(a *App, id string, be backend) {
	a.bmu.Lock()
	a.debrid[id] = be
	a.bmu.Unlock()
}

// runningIn puts a running task of backend resolver into package Old.
func runningIn(t *testing.T, a *App, id, resolver string) {
	t.Helper()
	putTask(t, a, core.Task{ID: id, URL: "https://host.example/" + id + ".bin", Name: id + ".bin",
		Package: "Old", Resolver: resolver, Status: core.StatusRunning, Loaded: 10, Enabled: true})
	a.mu.Lock()
	a.active[id], a.started[id] = true, true
	a.mu.Unlock()
}

// A backend that can halt its transfer is halted while the folder still has
// its old name, and started again once the task derives the new one; the app
// hears of no pause.
func TestAHaltedTransferGoesOnInTheNewFolder(t *testing.T) {
	a, base := newPackageApp(t)
	oldPackage(t, a, base)
	be := &haltingBackend{pausingBackend{a: a}}
	withBackend(a, "halting", be)
	runningIn(t, a, "run", "halting")

	if _, err := a.RenamePackage([]string{"part1", "part2", "run"}, "Film"); err != nil {
		t.Fatal(err)
	}

	if got, want := be.seen(), []string{"halt Old", "resume Film"}; !slices.Equal(got, want) {
		t.Errorf("the backend was asked %v, want %v", got, want)
	}
	if live := liveTask(a, "run"); live.Status != core.StatusRunning {
		t.Errorf("the transfer is %q after the rename, want it running", live.Status)
	}
}

// A backend that can only pause, because it has nothing on disk yet, is paused
// for the move and resumed afterwards, so the row does not stay paused.
func TestAPausedTransferIsResumedAfterTheMove(t *testing.T) {
	a, base := newPackageApp(t)
	oldPackage(t, a, base)
	be := &pausingBackend{a: a}
	withBackend(a, "pausing", be)
	runningIn(t, a, "run", "pausing")

	if _, err := a.RenamePackage([]string{"part1", "part2", "run"}, "Film"); err != nil {
		t.Fatal(err)
	}

	if got, want := be.seen(), []string{"pause Old", "resume Film"}; !slices.Equal(got, want) {
		t.Errorf("the backend was asked %v, want %v", got, want)
	}
	if live := liveTask(a, "run"); live.Status != core.StatusRunning {
		t.Errorf("the transfer is %q after the rename, want it running again", live.Status)
	}
}

// Stopping a live recording ends it, so a package with one running is not
// renamed until the recording is over, and nothing moves.
func TestAPackageRecordingALiveStreamIsNotRenamed(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	be := &recordingBackend{pausingBackend{a: a}}
	withBackend(a, "recording", be)
	runningIn(t, a, "live", "recording")

	_, err := a.RenamePackage([]string{"part1", "part2", "live"}, "Film")

	var refusal *RenameRefusal
	if !errors.As(err, &refusal) || refusal.Code != "busy" {
		t.Fatalf("RenamePackage = %v, want the busy refusal", err)
	}
	if calls := be.seen(); len(calls) != 0 {
		t.Errorf("the recording was touched: %v", calls)
	}
	if _, err := os.Stat(filepath.Join(old, "film.part1.rar")); err != nil {
		t.Errorf("the folder moved anyway: %v", err)
	}
}

// A delivery out of the working folder is under way, so the rename waits.
func TestAPackageBeingMovedIntoPlaceIsNotRenamed(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	a.mu.Lock()
	a.beginPlacingLocked("part1")
	a.mu.Unlock()

	_, err := a.RenamePackage([]string{"part1", "part2"}, "Film")

	var refusal *RenameRefusal
	if !errors.As(err, &refusal) || refusal.Code != "busy" {
		t.Fatalf("RenamePackage = %v, want the busy refusal", err)
	}
	if _, err := os.Stat(filepath.Join(old, "film.part1.rar")); err != nil {
		t.Errorf("the folder moved anyway: %v", err)
	}
}

// slowStart takes its time over the first start it is given, as a backend
// does that answers only once it has registered the transfer, and can halt a
// transfer it has.
type slowStart struct {
	a       *App
	release chan struct{}
	once    sync.Once

	mu      sync.Mutex
	running bool
	halts   []bool
	resumes []string
}

func (s *slowStart) Download(string, string, map[string]string, int) {}
func (s *slowStart) Pause(string)                                    {}
func (s *slowStart) Remove(string, bool)                             {}

func (s *slowStart) Resume(id string) {
	s.once.Do(func() { <-s.release })
	dir := filepath.Base(s.a.taskDir(id))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = true
	s.resumes = append(s.resumes, dir)
}

func (s *slowStart) Halt(string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := s.running
	s.running = false
	s.halts = append(s.halts, found)
	return found
}

// A start the dispatcher has handed to its backend a moment before the rename
// is waited for, so the rename finds the transfer and stops it, rather than
// moving the folder while it starts writing into the old one.
func TestARenameWaitsForAStartOnItsWayToTheBackend(t *testing.T) {
	a, base := newPackageApp(t)
	oldPackage(t, a, base)
	be := &slowStart{a: a, release: make(chan struct{})}
	withBackend(a, "slow", be)
	a.mu.Lock()
	a.started["slow"] = true
	a.mu.Unlock()
	queueTask(a, &core.Task{ID: "slow", URL: "https://host.example/slow.bin", Name: "slow.bin",
		Package: "Old", Resolver: "slow", Status: core.StatusQueued, Enabled: true, CreatedAt: time.Now()})

	done := make(chan error, 1)
	go func() {
		_, err := a.RenamePackage([]string{"part1", "part2", "slow"}, "Film")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the rename went ahead (%v) while a start was still on its way to the backend", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(be.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	be.mu.Lock()
	defer be.mu.Unlock()
	if !slices.Equal(be.halts, []bool{true}) {
		t.Errorf("Halt found %v, want the transfer the start had just registered", be.halts)
	}
	if !slices.Equal(be.resumes, []string{"Old", "Film"}) {
		t.Errorf("the transfer started in %v, want Old and then, after the move, Film", be.resumes)
	}
}

// renameJD answers what a rename asks of JDownloader for the task jd, whose
// one link runs until it is disabled. It records the calls that change
// something, and whether the task's partial file had already reached the new
// folder when JD was told it.
type renameJD struct {
	moved string

	mu         sync.Mutex
	running    bool
	calls      []string
	movedFirst bool
}

func (f *renameJD) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/downloadsV2/queryPackages":
			_, _ = w.Write([]byte(`{"data":[{"uuid":9,"name":"KL-jd"}]}`))
		case "/downloadsV2/queryLinks":
			flag := ""
			if f.running {
				flag = `,"running":true`
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":1,"name":"film.part3.rar","bytesTotal":100,"bytesLoaded":40` + flag + `}]}`))
		case "/downloadsV2/setEnabled":
			f.running = strings.HasPrefix(r.URL.RawQuery, "true")
			f.calls = append(f.calls, "enable "+strings.SplitN(r.URL.RawQuery, "&", 2)[0])
			_, _ = w.Write([]byte(`{"data":""}`))
		case "/downloadsV2/setDownloadDirectory":
			first, _ := url.QueryUnescape(strings.SplitN(r.URL.RawQuery, "&", 2)[0])
			var dir string
			_ = json.Unmarshal([]byte(first), &dir)
			f.calls = append(f.calls, "move "+filepath.Base(dir))
			if filepath.Base(dir) == "Film" {
				_, err := os.Stat(f.moved)
				f.movedFirst = err == nil
			}
			_, _ = w.Write([]byte(`{"data":""}`))
		case "/downloadcontroller/start":
			f.calls = append(f.calls, "start")
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			_, _ = w.Write([]byte(`{"data":""}`))
		}
	})
}

func (f *renameJD) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// JDownloader keeps its own record of where a package's files are. A rename
// stops its download, moves the folder with JD's partial file in it, tells JD
// the new folder and only then lets it go on, so JD resumes from the bytes it
// has instead of starting over in the old folder.
func TestRenamingAPackageTakesJDownloadersDownloadsAlong(t *testing.T) {
	a, base := newPackageApp(t)
	old := oldPackage(t, a, base)
	partial := filepath.Join(old, "film.part3.rar.part")
	if err := os.WriteFile(partial, []byte("forty bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &renameJD{moved: filepath.Join(base, "Film", "film.part3.rar.part")}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	jb := jd.NewBackend(srv.URL, a.onUpdate)
	jb.Dir = a.taskDir
	a.bmu.Lock()
	a.jd = jb
	a.bmu.Unlock()
	runningIn(t, a, "jd", "jd")
	jb.Resume("jd")
	defer jb.Remove("jd", false)
	waitFor(t, "JD pinning the package to its folder", func() bool { return slices.Contains(fake.seen(), "move Old") })
	before := len(fake.seen())

	if _, err := a.RenamePackage([]string{"part1", "part2", "jd"}, "Film"); err != nil {
		t.Fatal(err)
	}

	got := fake.seen()[before:]
	want := []string{"enable false", "move Film", "enable true", "start"}
	if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Errorf("JD was asked %v, want %v first", got, want)
	}
	fake.mu.Lock()
	movedFirst := fake.movedFirst
	fake.mu.Unlock()
	if !movedFirst {
		t.Error("JD was told the new folder before its partial file had moved there")
	}
	if _, err := os.Stat(fake.moved); err != nil {
		t.Errorf("JD's partial file did not move with the folder: %v", err)
	}
	for _, id := range []string{"part1", "part2", "jd"} {
		if got := a.TaskFolder(id); got != filepath.Join(base, "Film") {
			t.Errorf("%s downloads to %q, want the new folder", id, got)
		}
	}
}
