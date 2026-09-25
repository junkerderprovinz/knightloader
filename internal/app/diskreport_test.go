package app

// The disk readout against a fake volume, so a platform that cannot measure and
// a folder that does not exist yet can be tested anywhere.

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

// fakeUsage is the volume reading every row gets. It records the folders it was
// asked about, since the measured folder is not always the configured one.
type fakeUsage struct {
	space diskspace.Space
	known bool

	// before runs inside the call, before anything is recorded. It is set
	// before the first report is asked for.
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

// installUsage swaps in the fake reading and restores the real one afterwards.
func installUsage(t *testing.T, sp diskspace.Space, known bool) *fakeUsage {
	t.Helper()
	f := &fakeUsage{space: sp, known: known}
	prev := diskUsage
	t.Cleanup(func() { diskUsage = prev })
	diskUsage = f.read
	return f
}

// aVolume is a comfortable disk: four terabytes, a quarter used, and some held
// back so that free plus used does not add up to the total.
func aVolume() diskspace.Space {
	return diskspace.Space{Free: 2900 * gib, Used: 1000 * gib, Total: 4000 * gib}
}

// reportApp is an app whose settings and queue the test fills by hand. Nothing
// dispatches.
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

// rowFor is the report's row for one folder, or a failure naming the rows it
// did report.
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

// A platform that cannot measure reports Known false with no figures, which the
// page must not draw as an empty volume.
func TestAPlatformThatCannotMeasureSaysSoAndNotThatNothingIsLeft(t *testing.T) {
	installUsage(t, diskspace.Space{}, false)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })

	v := rowFor(t, a.DiskReport(), dl)
	if v.Known {
		t.Error("the row says the platform answered, on a build where it did not")
	}
	if v.Free != 0 || v.Used != 0 || v.Total != 0 {
		t.Errorf("the row carries figures (%d free, %d used, %d total) from a platform that cannot measure", v.Free, v.Used, v.Total)
	}
	if v.Role != roleDownloads {
		t.Errorf("role = %q for the download folder, want %q", v.Role, roleDownloads)
	}
}

// A missing folder is measured at the nearest existing parent, and the row
// carries both paths, since a mount that did not come up would otherwise be
// reported at the volume root under its own name.
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
		t.Error("the figures were dropped for a folder that does not exist yet")
	}
}

// An existing folder is measured at itself.
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

// Demand is per folder, so the report shows which destination will not fit.
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

// A running download is owed what it still has to fetch, not its announced
// size. The engine preallocates, so these bytes may already be missing from the
// free figure, which is why the two are shown side by side and never
// subtracted.
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

// A link of unknown size adds no bytes but still counts as a download, or a
// fresh queue would read "0 downloads".
func TestAnUncheckedLinkCountsAsADownloadAndNotAsBytes(t *testing.T) {
	installUsage(t, aVolume(), true)
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	owe(a, "unchecked", "", core.StatusQueued, 0, 0)

	v := rowFor(t, a.DiskReport(), dl)
	if v.Queued != 0 {
		t.Errorf("owed %d bytes for a link whose size nobody knows, want 0", v.Queued)
	}
	if v.Tasks != 1 {
		t.Errorf("%d downloads owe it, want 1; the link will still be written", v.Tasks)
	}
}

// A disabled link is owed nothing, matching the counters under the list.
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

// Done, failed and collected tasks are owed nothing, as in Counters.
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

// Per-package subfolders count against the folder they are in, rather than a
// row and a syscall each for the same disk.
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

// A template like "/downloads/<jd:date>/<jd:packagename>" is measured at its
// fixed head, or the walk up would land on the container's own filesystem.
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

// Measuring creates and writes nothing, not even the probe settings.Validate
// drops into a folder.
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

// The working folder is reported too, since that is where bytes are written
// first.
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

// Callers share one recent reading, so polling tabs do not each stat every
// folder.
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
		t.Error("the second call carries a fresher timestamp than the reading it was handed")
	}
}

// No volume is measured while a.mu is held: a stat on a dead network mount
// blocks until its timeout, and everything else queues behind that lock.
func TestNothingIsMeasuredWhileTheAppLockIsHeld(t *testing.T) {
	dl := t.TempDir()
	a := reportApp(t, func(s *settings.Settings) { s.DownloadDir = dl })
	f := installUsage(t, aVolume(), true)
	var held bool
	f.before = func(string) {
		// Retried, since other goroutines hold a.mu briefly; holding it for the
		// whole walk never lets go.
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
		t.Error("a volume was measured while a.mu was held; the lock must be released before any syscall")
	}
}

// Before anything has been measured, the volumes encode as [] rather than
// null.
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
