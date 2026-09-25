package schedule

import "time"

// Suspension sets the whole timetable aside for a while and leaves its rows as
// they are. While it covers a moment the queue does what the user set by hand,
// the base every answer starts from, and afterwards the rows apply again with
// nobody having to put them back.
//
// The zero value is no suspension.
type Suspension struct {
	On bool
	// Until is when the timetable applies again by itself. The zero time keeps
	// it aside until somebody lifts the suspension.
	Until time.Time
}

// Covers reports whether sp holds the timetable aside at t.
func (sp Suspension) Covers(t time.Time) bool {
	return sp.On && (sp.Until.IsZero() || t.Before(sp.Until))
}

// At is s.At with the suspension laid over it: the base while sp covers t.
func (sp Suspension) At(s Schedule, t time.Time, base State) State {
	if sp.Covers(t) {
		return base
	}
	return s.At(t, base)
}

// Next is s.Next with the suspension laid over it. An open-ended suspension
// never changes the answer by itself. One with an end changes it there only if
// a window is open at that moment; otherwise the first change is the
// timetable's own next edge after the end.
func (sp Suspension) Next(s Schedule, t time.Time, base State) (time.Time, bool) {
	if !sp.Covers(t) {
		return s.Next(t, base)
	}
	if sp.Until.IsZero() {
		return time.Time{}, false
	}
	// The rows are read in t's zone. An end that arrived as UTC from a browser
	// would otherwise be matched against the timetable in UTC.
	end := sp.Until.In(t.Location())
	if s.At(end, base) != base {
		return end, true
	}
	return s.Next(end, base)
}
