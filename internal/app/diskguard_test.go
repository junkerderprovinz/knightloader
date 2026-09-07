package app

// The disk guard, driven at the two places it acts: the dispatch pass that
// declines to START a download, and the watcher pass that STOPS one already
// running. Every reading comes from a fake volume, because a test that only
// says something on a machine that happens to be nearly full is a test that
// says nothing.

import (
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	gib = 1 << 30
	mib = 1 << 20
)

const (
	diskHost     = "disk.example"
	diskResolver = "diskbe"
)

// fakeVolume is the free-space reading every check in this file sees. It is
// mutable mid-test on purpose: the case the watcher exists for is a volume
// that was comfortable when the transfer started and is not any more.
type fakeVolume struct {
	mu    sync.Mutex
	free  uint64
	known bool
}

func (v *fakeVolume) set(free uint64, known bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.free, v.known = free, known
}

func (v *fakeVolume) read(string) (uint64, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.free, v.known
}

// installVolume swaps the package's own reading for this one and puts the real
// implementation back afterwards, so a test that fails does not leave every
// later test in this package looking at an invented disk.
func installVolume(t *testing.T, free uint64, known bool) *fakeVolume {
	t.Helper()
	v := &fakeVolume{free: free, known: known}
	prev := freeSpace
	t.Cleanup(func() { freeSpace = prev })
	freeSpace = v.read
	return v
}

// diskApp wires one host to a fake resolver and a fake backend, so a dispatch
// pass ends in a channel rather than on somebody's server.
func diskApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	// Defaults() ships the per-file reserve switched on. Every test below says
	// what it wants explicitly, so they all start from a guard that is fully
	// off and only the field under test is turned back on.
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be := &capBackend{got: make(chan int, 8)}
	a.bmu.Lock()
	a.debrid[diskResolver] = be
	a.bmu.Unlock()
	a.Registry.Register(hostResolver{id: diskResolver, host: diskHost})
	return a, be
}

// queueSized stages one queued task of a given announced size. A size of 0 is
// the "nobody has checked this link" case, which is most of them.
func queueSized(a *App, id string, size int64) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + diskHost + "/" + id + ".bin", Name: id + ".bin",
		Resolver: diskResolver, Status: core.StatusQueued, Enabled: true, Size: size,
	}
	a.queue = append(a.queue, id)
	a.mu.Unlock()
}

// dispatchNow runs one pass and reports what is running plus each task's
// reason for not being.
func dispatchNow(a *App) (running map[string]bool, waiting map[string]core.Waiting) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dispatchLocked()
	running = map[string]bool{}
	for id := range a.active {
		running[id] = true
	}
	waiting = map[string]core.Waiting{}
	for id, t := range a.tasks {
		waiting[id] = t.Waiting
	}
	return running, waiting
}

// TestAVolumeUnderTheFloorStartsNothingAndSaysSo is the first threshold. The
// reason on the row is half the test: "all slots busy" would be a true
// sentence about the wrong problem, and it sends the reader to raise
// MaxConcurrent, which frees not one byte.
func TestAVolumeUnderTheFloorStartsNothingAndSaysSo(t *testing.T) {
	installVolume(t, 800*mib, true)
	a, _ := diskApp(t, func(s *settings.Settings) { s.DiskLowSpace = gib })
	queueSized(a, "d1", 0)

	running, waiting := dispatchNow(a)
	if running["d1"] {
		t.Error("a download started on a volume under the configured floor")
	}
	if waiting["d1"] != core.WaitingDisk {
		t.Errorf("d1 waits with %q, want %q", waiting["d1"], core.WaitingDisk)
	}
}

// TestAnUnknownSizeStartsOnAHealthyVolume is the first half of the answer to
// "what about a task nobody checked". Most tasks carry Size 0, and refusing
// them would be an app that stops downloading on a machine with two terabytes
// free because it could not prove a file fits.
func TestAnUnknownSizeStartsOnAHealthyVolume(t *testing.T) {
	installVolume(t, 100*gib, true)
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskReserve, s.DiskLowSpace = gib, 2*gib
	})
	queueSized(a, "d1", 0)

	running, _ := dispatchNow(a)
	if !running["d1"] {
		t.Error("a task of unknown size was refused on a volume with 100 GiB free")
	}
}

// TestAnUnknownSizeStillWaitsUnderTheFloor is the other half, and the two
// together are the whole policy: a task with no size is exempt from the
// per-file arithmetic and NOT from the floor. Without this the exemption would
// be a hole big enough to drive the entire queue through, since a task's size
// is unknown at exactly the moment it is about to be started.
func TestAnUnknownSizeStillWaitsUnderTheFloor(t *testing.T) {
	installVolume(t, 500*mib, true)
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskReserve, s.DiskLowSpace = gib, 2*gib
	})
	queueSized(a, "d1", 0)

	running, waiting := dispatchNow(a)
	if running["d1"] {
		t.Error("a task of unknown size started on a volume under the floor: the exemption from the per-file check must not exempt it from the floor as well")
	}
	if waiting["d1"] != core.WaitingDisk {
		t.Errorf("d1 waits with %q, want %q", waiting["d1"], core.WaitingDisk)
	}
}

// TestAFileThatWillNotFitIsNotStarted is the reserve, which is the half that
// ships switched on. A download that cannot fit ends as ReasonDiskFull with a
// part file behind it; refusing it costs the queue nothing and leaves the
// volume as it was.
func TestAFileThatWillNotFitIsNotStarted(t *testing.T) {
	installVolume(t, 10*gib, true)
	a, _ := diskApp(t, func(s *settings.Settings) { s.DiskReserve = gib })
	queueSized(a, "big", 20*gib)
	queueSized(a, "small", 100*mib)

	running, waiting := dispatchNow(a)
	if running["big"] {
		t.Error("a 20 GiB download started against 10 GiB of free space")
	}
	if waiting["big"] != core.WaitingDisk {
		t.Errorf("big waits with %q, want %q", waiting["big"], core.WaitingDisk)
	}
	// The other half of the same assertion: the check is per file, so one
	// oversized task must not hold up a small one behind it.
	if !running["small"] {
		t.Error("a 100 MiB download was refused too; the check is per file, not a blanket stop")
	}
}

// TestOnePassDoesNotPromiseTheSameBytesTwice pins what the per-pass
// bookkeeping is for. The reading is taken once for the whole pass, so without
// it four twenty-gigabyte downloads on a thirty-gigabyte volume would each be
// told, truthfully and uselessly, that thirty gigabytes were free.
func TestOnePassDoesNotPromiseTheSameBytesTwice(t *testing.T) {
	installVolume(t, 30*gib, true)
	a, _ := diskApp(t, func(s *settings.Settings) { s.DiskReserve = gib })
	queueSized(a, "a1", 20*gib)
	queueSized(a, "a2", 20*gib)

	running, waiting := dispatchNow(a)
	started := 0
	for _, id := range []string{"a1", "a2"} {
		if running[id] {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("%d of two 20 GiB downloads started against 30 GiB free, want exactly 1", started)
	}
	for _, id := range []string{"a1", "a2"} {
		if !running[id] && waiting[id] != core.WaitingDisk {
			t.Errorf("%s did not start and waits with %q, want %q", id, waiting[id], core.WaitingDisk)
		}
	}
}

// TestAVolumeThatCannotBeMeasuredBlocksNothing is the fail-open rule, and it
// is the one that will be tempting to "tidy up" into a zero one day. A guard
// that stops the queue whenever it is ignorant halts a perfectly healthy
// machine on any platform internal/diskspace has no call for, and the person
// it happens to has nothing on screen to work out why.
func TestAVolumeThatCannotBeMeasuredBlocksNothing(t *testing.T) {
	installVolume(t, 0, false)
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskReserve, s.DiskLowSpace = 100*gib, 100*gib
	})
	queueSized(a, "d1", 50*gib)

	running, _ := dispatchNow(a)
	if !running["d1"] {
		t.Error("a download was refused on a platform that cannot measure free space; an unanswerable question must be no opinion, never a zero that reads as a full disk")
	}
}

// TestTheGuardStopsTransfersOnAVolumeGoneCritical is the second threshold and
// the reason there is a watcher at all. A running download produces no event
// that reaches the dispatcher, so a queue quietly filling the last gigabyte
// over twenty minutes would be noticed only once the writes had already
// failed.
func TestTheGuardStopsTransfersOnAVolumeGoneCritical(t *testing.T) {
	v := installVolume(t, 100*gib, true)
	a, be := diskApp(t, func(s *settings.Settings) {
		s.DiskLowSpace, s.DiskCriticalSpace = 2*gib, gib
	})
	queueSized(a, "d1", 0)

	if running, _ := dispatchNow(a); !running["d1"] {
		t.Fatal("the download did not start on a volume with 100 GiB free")
	}
	select {
	case <-be.got:
	case <-time.After(2 * time.Second):
		t.Fatal("the backend was never handed the download")
	}

	v.set(500*mib, true)
	a.diskPass()

	a.mu.Lock()
	stillRunning := a.active["d1"]
	status := a.tasks["d1"].Status
	waiting := a.tasks["d1"].Waiting
	a.mu.Unlock()

	if stillRunning {
		t.Error("a transfer kept running on a volume under the pause mark")
	}
	if status != core.StatusQueued {
		t.Errorf("status = %q after the guard stopped it, want %q - a stopped transfer has to go BACK into the wait queue, or nothing ever starts it again",
			status, core.StatusQueued)
	}
	if waiting != core.WaitingDisk {
		t.Errorf("d1 waits with %q, want %q", waiting, core.WaitingDisk)
	}
}

// TestAStoppedTransferIsNotRestartedIntoTheSameFullVolume is the loop that
// sanitizeDiskSpace's own invariant exists to prevent, checked from this side:
// the guard stops a transfer below the pause mark, the dispatcher runs
// immediately afterwards (inside StopBack), and it must not hand the slot
// straight back. Every fifteen seconds, for as long as the disk stayed low,
// and each round throwing away whatever a non-resumable transfer had fetched.
func TestAStoppedTransferIsNotRestartedIntoTheSameFullVolume(t *testing.T) {
	v := installVolume(t, 100*gib, true)
	// The pause mark ABOVE the start floor, which is the shape that would
	// loop. sanitize is expected to raise the start floor to match it.
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskLowSpace, s.DiskCriticalSpace = 0, gib
	})
	queueSized(a, "d1", 0)
	if running, _ := dispatchNow(a); !running["d1"] {
		t.Fatal("the download did not start on a volume with 100 GiB free")
	}

	v.set(500*mib, true)
	a.diskPass()

	a.mu.Lock()
	stillRunning := a.active["d1"]
	a.mu.Unlock()
	if stillRunning {
		t.Fatal("the transfer was stopped and immediately started again: a pause mark above the start floor has to raise the start floor, or the guard fights the dispatcher once per tick")
	}
}
