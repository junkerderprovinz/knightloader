package settings

// The destination volume: how much room is kept free, and the two marks at
// which the queue stops filling it. The three fields themselves are documented
// on the Settings struct; what is here is the defaults, the clamps, and the one
// rule the two thresholds have to obey with respect to each other.

// DefaultDiskReserve is the headroom kept free beyond what a download still
// needs, in bytes.
//
// Half a gigabyte, and on by default. The check it feeds compares a task's own
// remaining bytes against what is free, so it can only refuse a download that
// would not have fitted, and such a download ends as core.ReasonDiskFull a few
// minutes later anyway, having spent the line time and left a part file behind.
// Refusing it up front turns "failed, with rubbish on the disk" into "waiting,
// with the disk as it was", and it never touches a queue on a healthy machine.
//
// The two thresholds beside it are off by default because a threshold is an
// absolute number of bytes with no download to measure it against, so its right
// value depends on how big the volume is and what else lives on it. This one
// needs no such knowledge: half a gigabyte keeps a filesystem out of the state
// where it cannot write its own metadata, and goes unnoticed on any volume
// worth downloading to.
const DefaultDiskReserve = 512 << 20

// maxDiskBytes is a petabyte, a typo guard rather than a policy. A threshold
// larger than any volume this app will be pointed at is a queue that never
// starts anything again, and the likeliest way to get one is typing bytes where
// gigabytes were meant.
const maxDiskBytes = 1 << 50

func sanitizeDiskSpace(n Settings) Settings {
	n.DiskReserve = clampDiskBytes(n.DiskReserve)
	n.DiskLowSpace = clampDiskBytes(n.DiskLowSpace)
	n.DiskCriticalSpace = clampDiskBytes(n.DiskCriticalSpace)
	// The pause floor may not sit above the start floor, or the two make a
	// loop: the guard stops a running transfer below DiskCriticalSpace and puts
	// it back in the wait queue, and the dispatcher starts it again, because
	// DiskLowSpace is the only mark it consults and that one is lower. The
	// transfer would be stopped and restarted once per watcher tick for as long
	// as the disk stayed low, throwing away the bytes of every non-resumable
	// one each time round.
	//
	// The start floor is raised rather than the pause floor lowered, so that
	// the instruction that survives is the one somebody typed: "stop everything
	// below X" already says not to start anything new below X.
	if n.DiskCriticalSpace > n.DiskLowSpace {
		n.DiskLowSpace = n.DiskCriticalSpace
	}
	return n
}

// clampDiskBytes reads a negative figure as off rather than refusing it. A byte
// count is typed into a box, and every numeric setting in this package treats a
// value it cannot use as the absence of one instead of an error to dismiss
// before the other edits save.
func clampDiskBytes(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > maxDiskBytes {
		return maxDiskBytes
	}
	return v
}
