package app

// The disk readout, driven against a fake volume for the reason the guard's own
// tests are: a reading that comes from the machine underneath only says
// something on a machine that happens to be nearly full, and says nothing at
// all about the two answers this feature exists for - a platform that cannot
// measure, and a folder that is not there yet.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeUsage is the volume reading every row in this file gets, and it keeps the
// folders it was asked about - which is half of what is being tested here,
// since the folder that gets measured is not always the folder that was
// configured.
type fakeUsage struct {
	space diskspace.Space
	known bool

	// before runs inside the call, before anything is recorded. Set once, from
	// the test's own goroutine, before the first report is asked for.
	before func(path string)

	mu    sync.Mutex
	asked []string
	calls int
}

func (f *fakeUsage) read(path string) (diskspace.Space, bool) {
	if f.before != nil {
		f.before(path)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, path)
	f.calls++
	return f.space, f.known
}

func (f *fakeUsage) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeUsage) wasAsked(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.asked {
		if p == filepath.Clean(path) {
			return true
		}
	}
	return false
}

// installUsage swaps the package's own reading for this one and puts the real
// implementation back afterwards, so a test that fails does not leave every
// later test in this package looking at an invented disk.
func installUsage(t *testing.T, sp diskspace.Space, known bool) *fakeUsage {
	t.Helper()
	f := &fakeUsage{space: sp, known: known}
	prev := diskUsage
	t.Cleanup(func() { diskUsage = prev })
	diskUsage = f.read
	return f
}

// aVolume is a comfortable disk: four terabytes, a quarter of it occupied, and
// a little of the rest held back from us so that free plus used deliberately
// does not add up to the total.
func aVolume() diskspace.Space {
	return diskspace.Space{Free: 2900 * gib, Used: 1000 * gib, Total: 4000 * gib}
}

// reportApp is an app whose settings the test writes and whose queue it fills
// by hand. Nothing here dispatches: the readout reads the queue, it does not
// run it.
func reportApp(t *testing.T, mutate func(*settings.Settings)) *App {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a
}

// owe stages one task that is owed bytes: an announced size, what has already
// arrived, and optionally a folder of its own.
func owe(a *App, id, dir string, status core.Status, size, loaded int64) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://disk.example/" + id + ".bin", Name: id + ".bin",
		Dir: dir, Status: status, Enabled: true, Size: size, Loaded: loaded,
	}
	a.mu.Unlock()
}

// rowFor is the report's row for one folder, or a failure naming what it did
// report - a missing row is otherwise indistinguishable from a wrong one.
func rowFor(t *testing.T, rep DiskReport, dir string) VolumeReport {
	t.Helper()
	want := filepath.Clean(dir)
	var named []string
	for _, v := range rep.Volumes {
		if v.Dir == want {
			return v
		}
		named = append(named, v.Dir)
	}
	t.Fatalf("no row for %q; the report named %v", want, named)
	return VolumeReport{}
}

// TestAPlatformThatCannotMeasureSaysSoAndNotThatNothingIsLeft is the fail-open
// rule in a readout's own currency. internal/diskspace answers "I do not know"
// on any kernel it has no call for, and the row for such a platform must carry
// that as its own answer: three zeroes and Known false. A row that reported the
// zeroes as figures would draw an empty bar on a full disk, and the person
// looking at it has no way to tell that from a volume with nothing left.
func TestAPlatformThatCannotMeasureSaysSoAndNotThatNothingIsLeft(t *testing.T) {
	installUsage(t, diskspace.Space{}, false)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })

	v := rowFor(t, a.DiskReport(), dl)
	if v.Known {
		t.Error("the row says the platform answered, on a build where it did not: an unanswerable question has to stay unanswered, never become a zero")
	}
	if v.Free != 0 || v.Used != 0 || v.Total != 0 {
		t.Errorf("the row carries figures (%d free, %d used, %d total) from a platform that cannot measure", v.Free, v.Used, v.Total)
	}
	if v.Role != roleDownloads {
		t.Errorf("role = %q for the download folder, want %q", v.Role, roleDownloads)
	}
}

// TestAFolderThatDoesNotExistYetIsMeasuredAboveItAndSaysWhere is the case a
// download folder is in nine times out of ten, and the one that can hand a
// reader a confident wrong number: the walk up is silent, so a folder whose
// mount did not come up is measured at the volume root and reported under the
// name of the folder that is missing. Both paths have to travel.
func TestAFolderThatDoesNotExistYetIsMeasuredAboveItAndSaysWhere(t *testing.T) {
	f := installUsage(t, aVolume(), true)
	base := t.TempDir()
	missing := filepath.Join(base, "not", "created", "yet")
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = missing })

	v := rowFor(t, a.DiskReport(), missing)
	if v.Exists {
		t.Error("the row says the folder is there; it has never been created")
	}
	if v.Measured != base {
		t.Errorf("measured %q, want the nearest existing folder above it, %q", v.Measured, base)
	}
	if !f.wasAsked(base) {
		t.Errorf("the volume was never asked about %q; it was asked about %v", base, f.asked)
	}
	if !v.Known || v.Total != aVolume().Total {
		t.Error("the figures were dropped for a folder that does not exist yet, which is the folder the question is normally about")
	}
}

// TestAFolderThatIsThereSaysSo is the other half of the pair, and it is what
// stops "not created yet" being drawn on every row: a folder that exists is
// measured at itself and nowhere else.
func TestAFolderThatIsThereSaysSo(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })

	v := rowFor(t, a.DiskReport(), dl)
	if !v.Exists {
		t.Error("a folder the test framework had just created is reported as missing")
	}
	if v.Measured != v.Dir {
		t.Errorf("measured %q for a folder that is there (%q)", v.Measured, v.Dir)
	}
}

// TestTwoFoldersKeepTheirOwnDemand is the whole reason the figure is per folder
// rather than one instance-wide total. The queue counters under the list
// already say what the box owes altogether; what nobody can see is which of two
// destinations is the one that will not fit, and a report that added them
// together would put the same overcommitted-looking number on a folder with
// four terabytes free.
func TestTwoFoldersKeepTheirOwnDemand(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl, other := t.TempDir(), t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "here", "", core.StatusQueued, 10*gib, 0)
	owe(a, "there", other, core.StatusQueued, 3*gib, 0)

	rep := a.DiskReport()
	if v := rowFor(t, rep, dl); v.Queued != 10*gib || v.Tasks != 1 {
		t.Errorf("the download folder is owed %d bytes by %d downloads, want %d by 1", v.Queued, v.Tasks, int64(10*gib))
	}
	v := rowFor(t, rep, other)
	if v.Queued != 3*gib || v.Tasks != 1 {
		t.Errorf("the task's own folder is owed %d bytes by %d downloads, want %d by 1", v.Queued, v.Tasks, int64(3*gib))
	}
	if v.Role != roleTask {
		t.Errorf("role = %q for a folder set on the download itself, want %q", v.Role, roleTask)
	}
}

// TestARunningDownloadIsStillOwedItsRemainingBytes pins the half of the
// arithmetic that the interface's own wording promises. A transfer that has
// started has usually had its room taken out of the volume already - the engine
// creates the file at its full length before the first byte arrives - so these
// bytes are counted here AND are already missing from the free figure. That is
// why the two stand side by side and are never subtracted from one another, and
// why what is counted is what is still to fetch rather than the announced size.
func TestARunningDownloadIsStillOwedItsRemainingBytes(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "running", "", core.StatusRunning, 10*gib, 4*gib)

	v := rowFor(t, a.DiskReport(), dl)
	if v.Queued != 6*gib {
		t.Errorf("owed %d bytes for a 10 GiB download that has already fetched 4, want %d", v.Queued, int64(6*gib))
	}
	if v.Tasks != 1 {
		t.Errorf("%d downloads owe it, want 1", v.Tasks)
	}
}

// TestAnUncheckedLinkCountsAsADownloadAndNotAsBytes is the floor the wording on
// the interface promises, seen from both sides. Most of a fresh queue has no
// size at all, so those links add nothing to the byte figure - but a row
// reading "0 B, 0 downloads" in front of two hundred files that are about to be
// written is the one thing it must not say.
func TestAnUncheckedLinkCountsAsADownloadAndNotAsBytes(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "unchecked", "", core.StatusQueued, 0, 0)

	v := rowFor(t, a.DiskReport(), dl)
	if v.Queued != 0 {
		t.Errorf("owed %d bytes for a link whose size nobody knows, want 0: a guess here is a promise the queue cannot keep", v.Queued)
	}
	if v.Tasks != 1 {
		t.Errorf("%d downloads owe it, want 1: the link is still going to be written, and a row that hides it hides the whole queue on a fresh install", v.Tasks)
	}
}

// TestADownloadThatIsSwitchedOffIsNotOwedAnything keeps this figure and the
// counters under the list in step. A disabled link is not going to be written
// anywhere, and counting it would put bytes in front of somebody that no amount
// of waiting ever works off.
func TestADownloadThatIsSwitchedOffIsNotOwedAnything(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "off", "", core.StatusQueued, 9*gib, 0)
	a.mu.Lock()
	a.tasks["off"].Enabled = false
	a.mu.Unlock()

	if v := rowFor(t, a.DiskReport(), dl); v.Queued != 0 || v.Tasks != 0 {
		t.Errorf("a switched-off download is owed %d bytes by %d downloads, want 0 by 0", v.Queued, v.Tasks)
	}
}

// TestAFinishedDownloadIsNotOwedAnything is the same rule for the other three
// statuses Counters excludes: nothing is owed on a download that is done or has
// failed, and a link still in the collector has not been added to the queue at
// all.
func TestAFinishedDownloadIsNotOwedAnything(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "done", "", core.StatusDone, 9*gib, 9*gib)
	owe(a, "failed", "", core.StatusError, 9*gib, 1*gib)
	owe(a, "staged", "", core.StatusCollected, 9*gib, 0)

	if v := rowFor(t, a.DiskReport(), dl); v.Queued != 0 || v.Tasks != 0 {
		t.Errorf("finished, failed and collected downloads are owed %d bytes by %d downloads, want 0 by 0", v.Queued, v.Tasks)
	}
}

// TestAPerPackageSubfolderIsCountedAgainstTheFolderItIsIn is the grouping
// decision written down. With the per-package level switched on, every package
// resolves to a directory of its own, and a row each would be thirty rows and
// thirty syscalls describing one disk thirty times - none of which exists yet,
// so all thirty would be measured at the same parent anyway.
func TestAPerPackageSubfolderIsCountedAgainstTheFolderItIsIn(t *testing.T) {
	f := installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) {
		s.DownloadDir = dl
		s.SubfolderByPackage = true
	})
	a.mu.Lock()
	a.tasks["p1"] = &core.Task{ID: "p1", Package: "Alpha", Status: core.StatusQueued, Enabled: true, Size: 2 * gib}
	a.tasks["p2"] = &core.Task{ID: "p2", Package: "Beta", Status: core.StatusQueued, Enabled: true, Size: 3 * gib}
	a.mu.Unlock()

	rep := a.DiskReport()
	if v := rowFor(t, rep, dl); v.Queued != 5*gib || v.Tasks != 2 {
		t.Errorf("the download folder is owed %d bytes by %d downloads, want %d by 2", v.Queued, v.Tasks, int64(5*gib))
	}
	if len(rep.Volumes) != 1 {
		var named []string
		for _, v := range rep.Volumes {
			named = append(named, v.Dir)
		}
		t.Errorf("the report has %d rows for two packages under one download folder: %v", len(rep.Volumes), named)
	}
	if f.count() != 1 {
		t.Errorf("%d volume readings were taken for one folder", f.count())
	}
}

// TestAConfiguredTemplateIsMeasuredAtItsFixedHead is the trap a folder template
// sets. "/downloads/<jd:date>/<jd:packagename>" is never a directory, so
// measuring it as written walks up past the download folder and reports
// whatever it lands on - on a fresh container, the image's own filesystem. The
// cut is settings' own rule, exported rather than copied for the third time.
func TestAConfiguredTemplateIsMeasuredAtItsFixedHead(t *testing.T) {
	f := installUsage(t, aVolume(), true)
	base := t.TempDir()
	head := filepath.Join(base, "downloads")
	if err := os.MkdirAll(head, 0o755); err != nil {
		t.Fatal(err)
	}
	a := reportApp(t, func(s *settings.Settings) {
		s.DownloadDir = filepath.Join(head, "<jd:date>", "<jd:packagename>")
	})

	v := rowFor(t, a.DiskReport(), head)
	if !v.Exists {
		t.Error("the fixed head of the template is reported as missing, though it is on disk")
	}
	if !f.wasAsked(head) {
		t.Errorf("the volume was asked about %v rather than about the folder the app actually creates, %q", f.asked, head)
	}
}

// TestMeasuringCreatesNothingAndWritesNothing is the one rule in this file with
// a consequence outside the report. settings.Validate is the obvious-looking way
// to find out whether a folder is usable, and it MkdirAll's the path and drops a
// probe file in it - so wiring it in here would mean that opening a dashboard
// creates every configured folder on disk and litters each one, on a page
// nobody thinks of as a write.
func TestMeasuringCreatesNothingAndWritesNothing(t *testing.T) {
	installUsage(t, aVolume(), true)
	base := t.TempDir()
	missing := filepath.Join(base, "downloads", "not", "there")
	a := reportApp(t, func(s *settings.Settings) {
		s.DownloadDir = missing
		s.WorkDir = filepath.Join(base, "work")
		s.Categories = []settings.Category{{ID: "series", Name: "Series", Dir: filepath.Join(base, "series")}}
	})

	a.DiskReport()

	if _, err := os.Stat(missing); err == nil {
		t.Errorf("%q was created by a readout", missing)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("measuring left %v behind in %q; it may create nothing and write nothing", names, base)
	}
}

// TestTheWorkingFolderIsReported is the second folder the shipped interface
// asks about by name. It is a folder downloads land in - bytes are written
// there first and moved when they are finished - so a readout that only knew
// about the download folder would be silent about the volume that actually
// fills up on an install that has one.
func TestTheWorkingFolderIsReported(t *testing.T) {
	installUsage(t, aVolume(), true)
	base := t.TempDir()
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	a := reportApp(t, func(s *settings.Settings) { s.WorkDir = work })

	if v := rowFor(t, a.DiskReport(), work); v.Role != roleWork {
		t.Errorf("role = %q for the working folder, want %q", v.Role, roleWork)
	}
}

// TestTheReadingIsSharedRatherThanTakenPerCaller is what keeps a route every
// open browser tab polls from being a stat per folder per tab. The figures move
// at disk speed and the guard that acts on them looks every fifteen seconds, so
// a second reading taken within the same few seconds costs syscalls and tells
// nobody anything new.
func TestTheReadingIsSharedRatherThanTakenPerCaller(t *testing.T) {
	f := installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })

	first := a.DiskReport()
	took := f.count()
	if took == 0 {
		t.Fatal("the first report measured nothing at all")
	}
	second := a.DiskReport()
	if f.count() != took {
		t.Errorf("%d readings were taken for two calls in the same second, want %d", f.count(), took)
	}
	if !second.SampledAt.Equal(first.SampledAt) {
		t.Error("the second call carries a fresher timestamp than the reading it was actually handed, which is the one thing a stale figure must not do")
	}
}

// TestNothingIsMeasuredWhileTheAppLockIsHeld is the rule that keeps one tired
// mount from stopping the whole app. A stat on an unresponsive network mount
// blocks for as long as that mount takes to time out; the dispatcher already
// pays that once per pass with a.mu in its hand, and a route every open tab
// polls may not join in - every download, every settings save and every list
// queues behind that lock.
func TestNothingIsMeasuredWhileTheAppLockIsHeld(t *testing.T) {
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	f := installUsage(t, aVolume(), true)
	var held bool
	f.before = func(string) {
		// Retried rather than asked once: another goroutine of a running app
		// holds a.mu for microseconds at a time, and a single failed attempt
		// would report that as this call holding it. The bug being guarded
		// against holds the lock for the whole walk, so it never lets go.
		for i := 0; i < 200; i++ {
			if a.mu.TryLock() {
				a.mu.Unlock()
				return
			}
			time.Sleep(time.Millisecond)
		}
		held = true
	}

	a.DiskReport()
	if held {
		t.Error("a volume was measured while a.mu was held; the lock is for building the demand map and has to be released before any syscall")
	}
}

// TestAReportThatHasMeasuredNothingIsAnEmptyListAndNotNull covers the reading a
// caller is handed before anything has been measured - the first ask, while
// somebody else's walk is still out. A nil slice encodes as JSON null, and the
// page that walks over it throws rather than drawing an empty list. The
// neighbouring StopCost initialises its own slice for exactly this reason.
func TestAReportThatHasMeasuredNothingIsAnEmptyListAndNotNull(t *testing.T) {
	a := reportApp(t, func(*settings.Settings) {})
	b, err := json.Marshal(a.diskReportStateFor().report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"volumes":null`) {
		t.Errorf("a report that has measured nothing encodes as %s", b)
	}
}
