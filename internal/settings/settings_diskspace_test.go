package settings

import "testing"

// The other arrangement is a loop. Below the pause mark the guard stops a
// running transfer and puts it back in the wait queue, and the dispatcher
// consults the start mark only, so with the start mark lower it hands the slot
// straight back. The transfer is stopped and restarted once per watcher tick
// for as long as the volume stays low, throwing away the bytes of every
// non-resumable one each time round.
func TestThePauseFloorNeverSitsAboveTheStartFloor(t *testing.T) {
	// The shape somebody reaches for first: "stop everything below a
	// gigabyte", with the other box left alone.
	got := sanitize(Settings{DiskCriticalSpace: gigabyte})
	if got.DiskLowSpace != gigabyte {
		t.Errorf("DiskLowSpace = %d with a pause floor of %d, want the start floor raised to match it",
			got.DiskLowSpace, gigabyte)
	}
	if got.DiskCriticalSpace != gigabyte {
		t.Errorf("DiskCriticalSpace = %d, want the typed %d kept - the repair raises the start floor, it does not weaken the number the person wrote",
			got.DiskCriticalSpace, gigabyte)
	}

	// A start floor already above the pause floor is left exactly as written:
	// that is the ordinary arrangement and nothing needs repairing.
	got = sanitize(Settings{DiskLowSpace: 4 * gigabyte, DiskCriticalSpace: gigabyte})
	if got.DiskLowSpace != 4*gigabyte || got.DiskCriticalSpace != gigabyte {
		t.Errorf("sanitize moved a pair that was already in order: low=%d critical=%d",
			got.DiskLowSpace, got.DiskCriticalSpace)
	}
}

// A typo would leave a queue that never starts anything again. A negative
// figure reads as off rather than refusing the whole save, the way every
// numeric field in this package treats a value it cannot use.
func TestTheDiskFiguresAreClamped(t *testing.T) {
	got := sanitize(Settings{DiskReserve: -1, DiskLowSpace: -1, DiskCriticalSpace: -1})
	if got.DiskReserve != 0 || got.DiskLowSpace != 0 || got.DiskCriticalSpace != 0 {
		t.Errorf("negatives survived sanitize: %+v", got)
	}
	got = sanitize(Settings{DiskLowSpace: maxDiskBytes * 10})
	if got.DiskLowSpace != maxDiskBytes {
		t.Errorf("DiskLowSpace = %d, want it clamped to %d", got.DiskLowSpace, maxDiskBytes)
	}
}

// The reserve can only refuse a download that provably would not have fitted,
// so it costs a healthy machine nothing. A threshold is an absolute byte figure
// whose right value depends on the volume, so an invented one would either do
// nothing or stop somebody's queue after an update they did not read.
func TestAFreshInstallShipsTheReserveAndNeitherThreshold(t *testing.T) {
	d := sanitize(Defaults())
	if d.DiskReserve != DefaultDiskReserve {
		t.Errorf("DiskReserve = %d on a fresh install, want %d", d.DiskReserve, DefaultDiskReserve)
	}
	if d.DiskLowSpace != 0 || d.DiskCriticalSpace != 0 {
		t.Errorf("a fresh install ships a threshold: low=%d critical=%d, want both off",
			d.DiskLowSpace, d.DiskCriticalSpace)
	}
}

const gigabyte = 1 << 30
