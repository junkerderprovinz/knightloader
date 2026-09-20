package settings

// Quiet mode: the second set of limits, and the one rule about which of the two
// numbers wins.
//
// It is the turtle every torrent client has, Transmission's alternative speed
// and qBittorrent's alternative rate limits, and in both it is the most-pressed
// button in the interface. That is what shapes this type: the answer to "the
// box is eating the line" has to be one press, not a trip to a settings page to
// type a smaller number and a second trip that evening to type the old one
// back.

// QuietLimits is what the queue may do while quiet mode is on.
//
// Zero means "leave this one alone" in both fields rather than "unlimited",
// which is the opposite of what zero means in Settings.SpeedLimit, and the
// reason this is a type of its own rather than two more int fields. Two things
// force it:
//
//   - the two halves are configured one at a time. Somebody who wants only the
//     slot count cut at night has to be able to leave the speed alone, and with
//     zero reading as unlimited there is no way to say that: clearing the speed
//     box would hand the whole line over the moment the mode came on;
//   - a settings.json edited by hand, or written by a build that had only half
//     of this key, decodes the missing half as zero. Read as unlimited, that is
//     a turtle that takes the brakes off.
type QuietLimits struct {
	// SpeedLimit is the combined cap in bytes per second while the mode is on.
	// It replaces whatever else was in force, a timetable window's own limit
	// included. See internal/app.speedInForce for why it is not the smaller of
	// the two.
	SpeedLimit int64 `json:"speedLimit"`

	// MaxConcurrent is how many downloads may run at once while the mode is on.
	//
	// Lowering it never stops a transfer already running, as lowering the
	// ordinary MaxConcurrent does not: the dispatcher hands slots out and never
	// takes them back. A quiet mode that killed transfers would be the hard stop
	// wearing a turtle, and the hard stop is a separate button with its own
	// warning.
	MaxConcurrent int `json:"maxConcurrent"`
}

// UnderQuiet returns these settings as they read while quiet mode is in force:
// the second set of numbers laid over the first.
//
// limit is the speed limit that applies with the mode off. A parameter rather
// than s.SpeedLimit, because those are not the same number: a timetable window
// carries its own limit, and reading the stored figure would hand a nightly
// 2 MB/s window back to the daytime setting the moment somebody pressed the
// turtle, a window lifted by the control that was meant to tighten it.
//
// MaxPerHost is pulled down with MaxConcurrent for the reason sanitizeQueue
// pulls it down: a per-host figure above the global one is a limit the
// dispatcher can never reach, so leaving it standing would make the quiet
// numbers read as "one at a time, four per host".
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
	// reaches the same dispatcher. Not floored at 1 the way sanitizeQueue floors
	// that one: zero is the not-configured answer here, and a floor would turn a
	// cleared field into "run exactly one download", a limit nobody asked for
	// and nobody can switch off again.
	if n.Quiet.MaxConcurrent > maxConcurrentCeiling {
		n.Quiet.MaxConcurrent = maxConcurrentCeiling
	}
	// The quiet figure is not clamped to be smaller than the ordinary one. It is
	// the number typed on the page that says what quiet means, and a value cut
	// to something else would be stored, shown back and then not honoured, the
	// failure routes_controls.go refuses to build for the chunk count.
	return n
}
