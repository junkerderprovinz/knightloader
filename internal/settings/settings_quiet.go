package settings

// Quiet mode: the second set of limits, and the one rule about which of the two
// numbers wins.
//
// The control it belongs to is the turtle every torrent client has - Transmission
// calls it alternative speed, qBittorrent alternative rate limits - and in both
// of them it is the most-pressed button in the interface. The reason is worth
// writing down, because it is what this type has to be shaped by: the answer to
// "the box is eating the line" has to be ONE press. A trip to a settings page to
// type a smaller number, and a second trip that evening to type the old one back,
// is a thing people do twice and then stop doing.

// QuietLimits is what the queue is allowed to do while quiet mode is on.
//
// Zero means "leave this one alone" in both fields, NOT "unlimited" - which is
// the opposite of what zero means in Settings.SpeedLimit, and is the reason this
// is a type of its own rather than two more int fields on Settings. Two things
// force it:
//
//   - the two halves are configured one at a time. Somebody who wants only the
//     slot count cut at night has to be able to leave the speed alone, and with
//     zero reading as "unlimited" there would be no way to say that: clearing
//     the speed box would hand the whole line over the moment the mode came on;
//   - a settings.json edited by hand, or written by a build that had only half
//     of this key, decodes the missing half as zero. Read as "unlimited" that is
//     a turtle that takes the brakes OFF, which is the one thing it can never do.
type QuietLimits struct {
	// SpeedLimit is the combined cap in bytes per second while the mode is on.
	// It replaces whatever else was in force, a timetable window's own limit
	// included - see internal/app.speedInForce for that decision and for why it
	// is not simply the smaller of the two.
	SpeedLimit int64 `json:"speedLimit"`

	// MaxConcurrent is how many downloads may run at once while the mode is on.
	//
	// Lowering it never stops a transfer that is already running, exactly as
	// lowering the ordinary MaxConcurrent does not: the dispatcher hands slots
	// out and never takes them back. A quiet mode that killed transfers would be
	// the hard stop wearing a turtle, and the hard stop is a separate button with
	// its own warning for a reason.
	MaxConcurrent int `json:"maxConcurrent"`
}

// UnderQuiet returns these settings as they read while quiet mode is in force:
// the second set of numbers laid over the first.
//
// limit is the speed limit that applies with the mode OFF. It is a parameter
// rather than s.SpeedLimit because those are not the same number: a timetable
// window carries its own limit, and reading the stored figure here would hand a
// nightly 2 MB/s window back to the daytime setting the moment somebody pressed
// the turtle - a window silently lifted by the control that was supposed to
// tighten it.
//
// MaxPerHost is pulled down with MaxConcurrent for the same reason sanitizeQueue
// pulls it down: a per-host figure above the global one is a limit the dispatcher
// can never reach, so leaving it standing would make the quiet numbers read as
// "one at a time, four per host".
func (s Settings) UnderQuiet(limit int64) Settings {
	s.SpeedLimit = limit
	if s.Quiet.SpeedLimit > 0 {
		s.SpeedLimit = s.Quiet.SpeedLimit
	}
	if s.Quiet.MaxConcurrent > 0 {
		s.MaxConcurrent = s.Quiet.MaxConcurrent
		if s.MaxPerHost > s.MaxConcurrent {
			s.MaxPerHost = s.MaxConcurrent
		}
	}
	return s
}

func sanitizeQuiet(n Settings) Settings {
	if n.Quiet.SpeedLimit < 0 {
		n.Quiet.SpeedLimit = 0
	}
	if n.Quiet.MaxConcurrent < 0 {
		n.Quiet.MaxConcurrent = 0
	}
	// The same ceiling the ordinary MaxConcurrent has, because this figure
	// reaches the same dispatcher. Deliberately NOT floored at 1 the way
	// sanitizeQueue floors that one: zero is the "not configured" answer here,
	// and a floor would turn a cleared field into "run exactly one download",
	// which is a limit the user did not ask for and cannot switch off again.
	if n.Quiet.MaxConcurrent > maxConcurrentCeiling {
		n.Quiet.MaxConcurrent = maxConcurrentCeiling
	}
	// The quiet figure is NOT clamped to be smaller than the ordinary one. It is
	// the number the person typed on the page that says what quiet means, and a
	// value quietly cut to something else would be stored, shown back, and then
	// not honoured - the failure routes_controls.go refuses to build for the
	// chunk count, for the same reason.
	return n
}
