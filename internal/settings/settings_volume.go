package settings

// The volume cap: how much may finish downloading before the queue is held
// back, when that allowance starts over, and what "held back" means. What each
// of the four fields MEANS is on the Settings struct; what is here is the
// defaults, the clamps, and the one value that deliberately does not exist.
//
// THERE IS NO "off" ACTION, and that is the shipping decision in this file. The
// off state is VolumeCap == 0, so there is exactly one place to look to find out
// whether anything is capped at all. With an "off" action there would be two,
// they would be free to disagree, and the question "why is my queue paused"
// would have two answers to check instead of one.
//
// A cap of 0 is a real answer and not an unfinished setting: it says "count, and
// never hold anything back", which is what every install that has not been told
// otherwise wants. Nothing here changes what a queue does because an update
// shipped a default - the same argument DefaultDiskReserve's neighbours make for
// the two disk thresholds being off.

// DefaultVolumeCapResetDay is the day of the month the counter starts over on.
//
// The 1st, because a period that runs with the calendar month is the one a
// person can check against anything else - an invoice, a provider's own usage
// page, the chart on the overview. Somebody whose allowance renews on the 17th
// changes one number; somebody who never looks gets the reading everyone else's
// month agrees with.
const DefaultVolumeCapResetDay = 1

// The three answers to "the cap is reached, now what".
//
// Report is first and is the default: the counter is the whole feature until
// somebody asks for more than that. Pause holds the QUEUE back and never
// touches a transfer that is already running, and Throttle keeps everything
// going at VolumeCapThrottle. Neither of the two acting values ever aborts a
// running download: stopping mid file throws away bytes that have already been
// spent, which is the opposite of what somebody watching an allowance wants.
const (
	VolumeCapReport   = "report"
	VolumeCapPause    = "pause"
	VolumeCapThrottle = "throttle"
)

// maxVolumeCapBytes is a petabyte, and like maxDiskBytes next door it is a typo
// guard rather than a policy. A cap nothing could ever reach is the same as no
// cap, so it costs nobody anything; a cap of "500" typed in the box a byte count
// is stored in would be reached by one download and pause a queue with no
// explanation on screen that the person would recognise.
const maxVolumeCapBytes = 1 << 50

func sanitizeVolume(n Settings) Settings {
	n.VolumeCap = clampVolumeBytes(n.VolumeCap)
	// Not clamped against the cap or against anything else: it is a speed, and
	// the only number it is ever compared with is the OTHER speed limits in
	// force, which happens where those are decided (internal/app/app_budget.go)
	// and not in a settings file that cannot see a schedule window.
	n.VolumeCapThrottle = clampVolumeBytes(n.VolumeCapThrottle)
	n.VolumeCapResetDay = clampResetDay(n.VolumeCapResetDay)
	switch n.VolumeCapAction {
	case VolumeCapPause, VolumeCapThrottle:
		// Left as it is. Note that neither is read at all while VolumeCap is 0,
		// which is why an action is never rewritten to match the cap: somebody
		// who sets the cap back to 0 for a month and then types it in again gets
		// the answer they chose the first time.
	default:
		// Anything else, the empty string of an install that upgraded into these
		// keys included, is Report. Folded rather than refused for the reason
		// every numeric setting in this package is clamped rather than refused:
		// a value this cannot use must not be an error message somebody has to
		// dismiss before the rest of their edits will save.
		n.VolumeCapAction = VolumeCapReport
	}
	return n
}

// clampResetDay keeps the day inside a month, at both ends and for different
// reasons.
//
// Below 1 is the zero an install that has never seen this key carries, and the
// answer is the default rather than an error. Above 31 is a typo, and the answer
// is 31 rather than the default: somebody who typed 45 meant the end of the
// month, and handing them the 1st would move their allowance to the other end of
// it.
//
// 31 is allowed even though most months are shorter. What that means in
// February is the period arithmetic's problem and it is solved there, once, in
// internal/app/app_volumecap.go: the day is clamped to the month's own length at
// the moment a period is built, so a cap set to the 31st restarts on the 28th in
// February and never rolls into March.
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
