package settings

// The volume cap: how much may finish downloading before the queue is held
// back, when that allowance starts over, and what "held back" means. What each
// of the four fields means is on the Settings struct; here are the defaults,
// the clamps, and the one value that does not exist.
//
// There is no "off" action. The off state is VolumeCap == 0, so there is one
// place to look to find out whether anything is capped at all. With an "off"
// action there would be two, free to disagree, and "why is my queue paused"
// would have two answers to check.
//
// A cap of 0 is a real answer rather than an unfinished setting: count, and
// never hold anything back.

// DefaultVolumeCapResetDay is the day of the month the counter starts over on.
//
// The 1st, because a period that runs with the calendar month is the one that
// can be checked against an invoice, a provider's usage page or the chart on
// the overview. Somebody whose allowance renews on the 17th changes one number.
const DefaultVolumeCapResetDay = 1

// The three answers to "the cap is reached, now what".
//
// Report is the default: the counter is the whole feature until somebody asks
// for more. Pause holds the queue back and never touches a transfer already
// running, and Throttle keeps everything going at VolumeCapThrottle. Neither
// acting value aborts a running download, since stopping mid file throws away
// bytes that have already been spent.
const (
	VolumeCapReport   = "report"
	VolumeCapPause    = "pause"
	VolumeCapThrottle = "throttle"
)

// maxVolumeCapBytes is a petabyte, a typo guard rather than a policy like
// maxDiskBytes next door. A cap nothing could reach is the same as no cap, and
// costs nobody anything; a cap of "500" typed into a box that stores a byte
// count is reached by one download and pauses a queue with no explanation on
// screen anybody would recognise.
const maxVolumeCapBytes = 1 << 50

func sanitizeVolume(n Settings) Settings {
	n.VolumeCap = clampVolumeBytes(n.VolumeCap)
	// Not clamped against the cap or anything else: it is a speed, and the only
	// numbers it is compared with are the other speed limits in force, which
	// happens in internal/app/app_budget.go and not in a settings file that
	// cannot see a schedule window.
	n.VolumeCapThrottle = clampVolumeBytes(n.VolumeCapThrottle)
	n.VolumeCapResetDay = clampResetDay(n.VolumeCapResetDay)
	switch n.VolumeCapAction {
	case VolumeCapPause, VolumeCapThrottle:
		// Left as it is. Neither is read while VolumeCap is 0, which is why an
		// action is never rewritten to match the cap: somebody who sets the cap
		// back to 0 for a month and types it in again gets the answer they chose
		// the first time.
	default:
		// Anything else, including the empty string of an install that upgraded
		// into these keys, is Report. Folded rather than refused for the reason
		// every numeric setting here is clamped: a value this cannot use must
		// not be an error to dismiss before the rest of the edits will save.
		n.VolumeCapAction = VolumeCapReport
	}
	return n
}

// clampResetDay keeps the day inside a month, at both ends and for different
// reasons.
//
// Below 1 is the zero an install that has never seen this key carries, and the
// answer is the default. Above 31 is a typo, and the answer is 31 rather than
// the default: somebody who typed 45 meant the end of the month, and the 1st
// would move their allowance to the other end of it.
//
// 31 is allowed even though most months are shorter. The period arithmetic in
// internal/app/app_volumecap.go clamps the day to the month's own length when a
// period is built, so a cap set to the 31st restarts on the 28th in February
// and never rolls into March.
func clampResetDay(day int) int {
	if day < 1 {
		return DefaultVolumeCapResetDay
	}
	if day > 31 {
		return 31
	}
	return day
}

// clampVolumeBytes reads a negative figure as "off" rather than refusing it,
// exactly as clampDiskBytes does for the volume's own thresholds.
func clampVolumeBytes(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > maxVolumeCapBytes {
		return maxVolumeCapBytes
	}
	return v
}
