package settings

import "testing"

// TestUnderQuietLaysTheSecondSetOverTheFirst covers the substitution itself,
// including the case the whole parameter exists for: the limit that applies with
// the mode off is NOT the stored SpeedLimit while a timetable window is open, so
// a quiet mode with no speed of its own has to hand the WINDOW's figure back and
// not the daytime one.
func TestUnderQuietLaysTheSecondSetOverTheFirst(t *testing.T) {
	const (
		daytime = 8 << 20 // what the settings page says
		window  = 2 << 20 // what a nightly window says instead
		turtle  = 512 << 10
	)
	base := Settings{MaxConcurrent: 6, MaxPerHost: 4, SpeedLimit: daytime}

	cases := []struct {
		name           string
		quiet          QuietLimits
		limit          int64
		wantSpeed      int64
		wantConcurrent int
		wantPerHost    int
	}{
		{
			name:  "both numbers set",
			quiet: QuietLimits{SpeedLimit: turtle, MaxConcurrent: 1},
			limit: daytime, wantSpeed: turtle, wantConcurrent: 1, wantPerHost: 1,
		},
		{
			name:  "no speed of its own leaves the speed alone",
			quiet: QuietLimits{MaxConcurrent: 2},
			limit: daytime, wantSpeed: daytime, wantConcurrent: 2, wantPerHost: 2,
		},
		{
			name:  "and that is the window's number, not the stored one",
			quiet: QuietLimits{MaxConcurrent: 2},
			limit: window, wantSpeed: window, wantConcurrent: 2, wantPerHost: 2,
		},
		{
			name:  "the turtle beats a window that was already throttling",
			quiet: QuietLimits{SpeedLimit: turtle},
			limit: window, wantSpeed: turtle, wantConcurrent: 6, wantPerHost: 4,
		},
		{
			name:  "nothing configured changes nothing",
			quiet: QuietLimits{},
			limit: window, wantSpeed: window, wantConcurrent: 6, wantPerHost: 4,
		},
		{
			// The per-host figure is only pulled down, never up: a mode that
			// RAISED it would be a quiet mode opening more connections to one
			// host than the loud one did.
			name:  "a quiet slot count above the per-host figure leaves it where it was",
			quiet: QuietLimits{MaxConcurrent: 10},
			limit: daytime, wantSpeed: daytime, wantConcurrent: 10, wantPerHost: 4,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base
			s.Quiet = c.quiet
			got := s.UnderQuiet(c.limit)
			if got.SpeedLimit != c.wantSpeed {
				t.Errorf("SpeedLimit = %d, want %d", got.SpeedLimit, c.wantSpeed)
			}
			if got.MaxConcurrent != c.wantConcurrent {
				t.Errorf("MaxConcurrent = %d, want %d", got.MaxConcurrent, c.wantConcurrent)
			}
			if got.MaxPerHost != c.wantPerHost {
				t.Errorf("MaxPerHost = %d, want %d", got.MaxPerHost, c.wantPerHost)
			}
		})
	}
}

// TestUnderQuietLeavesTheStoredNumbersAlone: the substitution is a reading of the
// settings and never an edit of them. If it wrote through, one pass of the
// dispatcher with the mode on would persist the quiet numbers as the user's own,
// and switching the mode off would hand back the figures it had just replaced.
func TestUnderQuietLeavesTheStoredNumbersAlone(t *testing.T) {
	s := Settings{MaxConcurrent: 6, MaxPerHost: 4, SpeedLimit: 8 << 20,
		Quiet: QuietLimits{SpeedLimit: 512 << 10, MaxConcurrent: 1}}
	_ = s.UnderQuiet(8 << 20)
	if s.MaxConcurrent != 6 || s.MaxPerHost != 4 || s.SpeedLimit != 8<<20 {
		t.Errorf("the stored settings were changed by reading them: %+v", s)
	}
}

// TestSanitizeQuiet pins the bounds, and one thing that must NOT be bounded: zero
// stays zero. A floor of 1 on the slot count would turn a cleared field into "run
// exactly one download at a time" - a limit nobody typed and, because zero is how
// you say "leave it alone", one they could not switch off again.
func TestSanitizeQuiet(t *testing.T) {
	cases := []struct {
		name           string
		in             QuietLimits
		wantSpeed      int64
		wantConcurrent int
	}{
		{"a configured pair is left as typed", QuietLimits{SpeedLimit: 512 << 10, MaxConcurrent: 2}, 512 << 10, 2},
		{"zero is not floored", QuietLimits{}, 0, 0},
		{"negatives become the not-configured zero", QuietLimits{SpeedLimit: -1, MaxConcurrent: -3}, 0, 0},
		{"the slot count has the same ceiling as the ordinary one", QuietLimits{MaxConcurrent: 999}, 0, maxConcurrentCeiling},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeQuiet(Settings{Quiet: c.in}).Quiet
			if got.SpeedLimit != c.wantSpeed {
				t.Errorf("SpeedLimit = %d, want %d", got.SpeedLimit, c.wantSpeed)
			}
			if got.MaxConcurrent != c.wantConcurrent {
				t.Errorf("MaxConcurrent = %d, want %d", got.MaxConcurrent, c.wantConcurrent)
			}
		})
	}

	// The quiet figure is deliberately not clamped to be smaller than the loud
	// one. It is what the person typed on the page that defines what quiet means,
	// and a number silently cut to something else is one that is stored, shown
	// back, and then not honoured.
	got := sanitizeQuiet(Settings{MaxConcurrent: 2, Quiet: QuietLimits{MaxConcurrent: 8}}).Quiet
	if got.MaxConcurrent != 8 {
		t.Errorf("a quiet slot count above the ordinary one = %d, want it kept at 8", got.MaxConcurrent)
	}
}

// TestDefaultsGiveQuietModeSomethingToDo: the mode ships off, so this default
// only ever takes effect on a deliberate press - and a button labelled "quiet
// mode" whose first press changes nothing at all is how people learn a feature is
// broken. The speed stays at zero because the box cannot guess how fast the line
// is; the slot count it can.
func TestDefaultsGiveQuietModeSomethingToDo(t *testing.T) {
	d := Defaults()
	if d.Quiet.MaxConcurrent < 1 || d.Quiet.MaxConcurrent >= d.MaxConcurrent {
		t.Errorf("default quiet slot count = %d, want something between 1 and the ordinary %d",
			d.Quiet.MaxConcurrent, d.MaxConcurrent)
	}
	if d.Quiet.SpeedLimit != 0 {
		t.Errorf("default quiet speed = %d, want 0: a speed limit cannot be guessed for somebody else's line", d.Quiet.SpeedLimit)
	}
}
