package settings

// The destination volume: how much room is kept free, and the two marks at
// which the queue stops filling it. The three fields themselves are documented
// on the Settings struct; what is here is the defaults, the clamps, and the one
// rule the two thresholds have to obey with respect to each other.

// DefaultDiskReserve is the headroom kept free beyond what a download still
// needs, in bytes.
//
// HALF A GIGABYTE, AND ON BY DEFAULT, which is the one shipping decision in
// this file worth arguing about. The check it feeds compares a task's own
// remaining bytes against what is actually free, so it can only ever refuse a
// download that would not have fitted - and a download that does not fit ends
// as core.ReasonDiskFull a few minutes later anyway, having spent the line
// time and left a part file behind. Refusing it up front changes the outcome
// from "failed, with rubbish on the disk" to "waiting, with the disk as it
// was", and it does so without ever touching a queue on a healthy machine.
//
// The two THRESHOLDS beside it are off by default, and the reason they differ
// from this one is not caution for its own sake: a threshold is an absolute
// number of bytes with no download to measure it against, so its right value
// depends entirely on how big the volume is and what else lives on it. This
// one needs no such knowledge. Half a gigabyte is enough to keep a filesystem
// out of the state where it cannot write its own metadata, and small enough
// that nobody notices it on a volume worth downloading to.
const DefaultDiskReserve = 512 << 20

// maxDiskBytes is a petabyte, and it is a typo guard rather than a policy.
// A threshold larger than any volume this app will be pointed at is a queue
// that never starts anything again, which is indistinguishable from the app
// being broken - and the likeliest way to get one is a person typing bytes
// where they meant gigabytes.
const maxDiskBytes = 1 << 50

func sanitizeDiskSpace(n Settings) Settings {
	n.DiskReserve = clampDiskBytes(n.DiskReserve)
	n.DiskLowSpace = clampDiskBytes(n.DiskLowSpace)
	n.DiskCriticalSpace = clampDiskBytes(n.DiskCriticalSpace)
	// THE PAUSE FLOOR MAY NOT SIT ABOVE THE START FLOOR, and the fix is to
	// raise the start floor rather than to lower the pause floor.
	//
	// It is not tidiness, it is a loop. The guard stops a running transfer
	// below DiskCriticalSpace and puts it back in the wait queue; the
	// dispatcher then looks at the same queue and starts it again, because
	// DiskLowSpace - the only mark the dispatcher consults - is lower and
	// nothing is stopping it. The transfer would be stopped and restarted once
	// per watcher tick for as long as the disk stayed low, throwing away the
	// bytes of every non-resumable one each time round.
	//
	// Raising the start floor rather than lowering the pause floor because the
	// person's own instruction is the one that survives: "stop everything below
	// X" already says "and obviously do not start anything new below X". The
	// other repair would quietly weaken the number they typed.
	if n.DiskCriticalSpace > n.DiskLowSpace {
		n.DiskLowSpace = n.DiskCriticalSpace
	}
	return n
}

// clampDiskBytes reads a negative figure as "off" rather than refusing it. A
// byte count is typed into a box, and every other numeric setting in this
// package treats a value it cannot use as the absence of one instead of an
// error message the user has to dismiss before their other edits save.
func clampDiskBytes(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > maxDiskBytes {
		return maxDiskBytes
	}
	return v
}
