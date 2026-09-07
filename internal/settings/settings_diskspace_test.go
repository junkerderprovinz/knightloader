package settings

import "testing"

// TestThePauseFloorNeverSitsAboveTheStartFloor is the one rule the two
// thresholds have to obey with respect to each other, and it is not a
// tidiness rule - it is a loop. Below the pause mark the guard stops a running
// transfer and puts it back in the wait queue; the dispatcher then looks at
// that queue and consults the START mark only. With the start mark lower, or
// switched off entirely, it hands the slot straight back, and the transfer is
// stopped and restarted once per watcher tick for as long as the volume stays
// low - throwing away the bytes of every non-resumable one each time round.
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

// TestTheDiskFiguresAreClamped keeps a typo out of a queue that would never
// start anything again, and reads a negative figure as "off" rather than
// refusing the whole save - every other numeric field in this package treats a
// value it cannot use as the absence of one.
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

// TestAFreshInstallShipsTheReserveAndNeitherThreshold is the shipping decision
// written down. The reserve can only ever refuse a download that provably
// would not have fitted, so it costs a healthy machine nothing; a threshold is
// an absolute byte figure whose right value depends entirely on the volume, so
// inventing one would either do nothing or stop somebody's queue after an
// update they did not read.
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
