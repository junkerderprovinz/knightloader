package app

// The disk guard at the two places it acts: the dispatch pass that declines to
// start a download, and the watcher pass that stops a running one. Readings
// come from a fake volume so the result does not depend on the machine.

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

// fakeVolume is the free-space reading every check here sees. It can change
// mid-test, like a volume that fills while a transfer runs.
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

// installVolume swaps in the fake reading and restores the real one afterwards.
func installVolume(t *testing.T, free uint64, known bool) *fakeVolume {
	t.Helper()
	v := &fakeVolume{free: free, known: known}
	prev := freeSpace
	t.Cleanup(func() { freeSpace = prev })
	freeSpace = v.read
	return v
}

// diskApp wires one host to a fake resolver and a fake backend, so a dispatch
// ends in a channel.
func diskApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	// Defaults() turns the reserve on; each test starts fully off and turns on
	// only what it tests.
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

// queueSized stages one queued task of a given announced size; 0 means
// unknown, as for most links.
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

// Under the floor nothing starts, and the row says it is waiting for disk
// space rather than for a slot.
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

// A task of unknown size starts on a healthy volume; most tasks have no size.
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

// A task of unknown size is exempt from the per-file check but not from the
// floor.
func TestAnUnknownSizeStillWaitsUnderTheFloor(t *testing.T) {
	installVolume(t, 500*mib, true)
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskReserve, s.DiskLowSpace = gib, 2*gib
	})
	queueSized(a, "d1", 0)

	running, waiting := dispatchNow(a)
	if running["d1"] {
		t.Error("a task of unknown size started on a volume under the floor")
	}
	if waiting["d1"] != core.WaitingDisk {
		t.Errorf("d1 waits with %q, want %q", waiting["d1"], core.WaitingDisk)
	}
}

// The reserve, on by default: a download that cannot fit is not started, rather
// than failing with a part file left behind.
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
	// The check is per file, so a small task behind it still starts.
	if !running["small"] {
		t.Error("a 100 MiB download was refused too; the check is per file, not a blanket stop")
	}
}

// The reading is taken once per pass, so the pass subtracts what it has
// already promised.
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

// The guard fails open: a volume that cannot be measured blocks nothing, or
// every platform without a free-space call would stop downloading.
func TestAVolumeThatCannotBeMeasuredBlocksNothing(t *testing.T) {
	installVolume(t, 0, false)
	a, _ := diskApp(t, func(s *settings.Settings) {
		s.DiskReserve, s.DiskLowSpace = 100*gib, 100*gib
	})
	queueSized(a, "d1", 50*gib)

	running, _ := dispatchNow(a)
	if !running["d1"] {
		t.Error("a download was refused on a platform that cannot measure free space; no reading must mean no opinion, not a full disk")
	}
}

// Below the pause mark the watcher stops running transfers, which produce no
// event the dispatcher would see.
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
		t.Errorf("status = %q after the guard stopped it, want %q; a stopped transfer must go back into the wait queue",
			status, core.StatusQueued)
	}
	if waiting != core.WaitingDisk {
		t.Errorf("d1 waits with %q, want %q", waiting, core.WaitingDisk)
	}
}

// The dispatcher runs right after the guard stops a transfer (inside StopBack)
// and must not restart it on the same full volume; sanitizeDiskSpace keeps the
// start floor at least at the pause mark.
func TestAStoppedTransferIsNotRestartedIntoTheSameFullVolume(t *testing.T) {
	v := installVolume(t, 100*gib, true)
	// The pause mark above the start floor, which sanitize has to correct.
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
		t.Fatal("the transfer was stopped and immediately started again; a pause mark above the start floor must raise the start floor")
	}
}
