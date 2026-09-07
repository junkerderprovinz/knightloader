package schedule

import (
	"strings"
	"testing"
	"time"
)

// TestAtQuietWindowOnlyCoversItsOwnHours is the fourth action's version of the
// check every other action already has: a window that turns quiet mode on has to
// turn it off again at its own end, and must not reach into the day around it.
// Getting this wrong leaves the box on the slow set of limits all day, with a
// timetable on screen that says the window closed hours ago.
func TestAtQuietWindowOnlyCoversItsOwnHours(t *testing.T) {
	s := Compile([]Entry{{
		Days: []time.Weekday{time.Monday}, Start: "20:00", End: "23:00", Action: ActionQuiet,
	}})
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"before it opens", ts(2, 19, 59), false},
		{"the opening minute", ts(2, 20, 0), true},
		{"inside", ts(2, 21, 30), true},
		{"last minute", ts(2, 22, 59), true},
		{"the closing minute is already out", ts(2, 23, 0), false},
		{"tuesday is not ticked", ts(3, 21, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, State{}).Quiet; got != c.want {
				t.Errorf("At(%s).Quiet = %v, want %v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestAtQuietTouchesOnlyItsOwnField is the same promise the other three actions
// keep. A quiet window laid over a pause window must not release the pause, and
// it must not wipe the speed limit the user configured - the fields are three
// separate answers and an action that reset the state would lose two of them.
func TestAtQuietTouchesOnlyItsOwnField(t *testing.T) {
	s := Compile([]Entry{
		{Days: everyDay(), Start: "00:00", End: "12:00", Action: ActionPause},
		{Days: everyDay(), Start: "06:00", End: "18:00", Action: ActionQuiet},
	})
	base := State{Limit: 5000}
	cases := []struct {
		name string
		when time.Time
		want State
	}{
		{"pause only", ts(2, 3, 0), State{Paused: true, Limit: 5000}},
		{"both", ts(2, 7, 0), State{Paused: true, Limit: 5000, Quiet: true}},
		{"quiet only", ts(2, 13, 0), State{Limit: 5000, Quiet: true}},
		{"neither", ts(2, 19, 0), State{Limit: 5000}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, base); got != c.want {
				t.Errorf("At(%s) = %+v, want %+v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestAtQuietKeepsTheSwitchWhereNoWindowApplies is why the flag is part of the
// base at all. The switch beside the queue is what quiet mode falls back to, so a
// pause window that says nothing about it has to hand it back untouched - and a
// quiet window has to hand back the switch's own answer at its end, not a false
// it invented.
func TestAtQuietKeepsTheSwitchWhereNoWindowApplies(t *testing.T) {
	pressed := State{Quiet: true}
	s := Compile([]Entry{
		{Days: everyDay(), Start: "01:00", End: "02:00", Action: ActionPause},
		{Days: everyDay(), Start: "03:00", End: "04:00", Action: ActionQuiet},
	})
	cases := []struct {
		name string
		when time.Time
		want State
	}{
		{"outside every window the switch decides", ts(2, 12, 0), State{Quiet: true}},
		{"a pause window leaves it alone", ts(2, 1, 30), State{Paused: true, Quiet: true}},
		{"a quiet window agrees with it", ts(2, 3, 30), State{Quiet: true}},
		{"and its end does not switch it off", ts(2, 4, 0), State{Quiet: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, pressed); got != c.want {
				t.Errorf("At(%s) = %+v, want %+v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestNextReportsAQuietEdge: the runner sleeps until Next and applies nothing in
// between, so an edge Next does not report is a window that never opens. The
// field was added to State after Next was written, and a State comparison that
// had been spelled out field by field would have skipped it silently.
func TestNextReportsAQuietEdge(t *testing.T) {
	s := Compile([]Entry{{
		Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionQuiet,
	}})
	got, ok := s.Next(ts(2, 12, 0), State{})
	if !ok {
		t.Fatal("Next reported no change at all, so the runner would sleep through the window opening")
	}
	if want := ts(2, 22, 0); !got.Equal(want) {
		t.Errorf("Next = %s, want the opening edge %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	// And the other direction: with the switch already on, the window asserts
	// nothing new, so its edges are not changes and Next must step over them.
	if _, ok := s.Next(ts(2, 12, 0), State{Quiet: true}); ok {
		t.Error("a window that only repeats what the switch already says has no edges worth waking for")
	}
}

// TestValidateAcceptsQuiet: a row whose action Compile does not know is dropped
// without a word, so an action the evaluator handles and Validate refuses (or the
// reverse) is a window that saves and then never fires.
func TestValidateAcceptsQuiet(t *testing.T) {
	e := Entry{Days: []time.Weekday{time.Monday}, Start: "20:00", End: "23:00", Action: ActionQuiet}
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a quiet window accepted", err)
	}
	if len(Compile([]Entry{e}).rules) != 1 {
		t.Error("Validate accepted the row and Compile dropped it, which is a window that saves and never fires")
	}
	// The limit field belongs to ActionLimit alone, so a negative one on a quiet
	// row is not a reason to refuse it - it is a leftover from an action the user
	// changed their mind about.
	e.Limit = -1
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() = %v, want the limit field ignored on a quiet row", err)
	}
	if err := (Entry{Days: []time.Weekday{time.Monday}, Start: "20:00", End: "23:00", Action: "quieter"}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "unknown action") {
		t.Errorf("a near miss on the action name = %v, want it refused by name", err)
	}
}
