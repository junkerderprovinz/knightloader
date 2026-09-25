package schedule

import (
	"testing"
	"time"
)

// nightlyPause pauses the queue every night from 22:00 to 06:00.
func nightlyPause() Schedule {
	return Compile([]Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}})
}

func TestSuspensionAnswersTheBaseInsideAWindow(t *testing.T) {
	base := State{Limit: 5000}
	sp := Suspension{On: true, Until: ts(3, 1, 0)}
	if got := sp.At(nightlyPause(), ts(2, 23, 0), base); got != base {
		t.Errorf("At inside a suspended pause window = %+v, want the base %+v", got, base)
	}
}

func TestSuspensionEndsByItself(t *testing.T) {
	sp := Suspension{On: true, Until: ts(3, 1, 0)}
	if sp.Covers(ts(3, 1, 0)) {
		t.Error("a suspension still covers the moment it ends")
	}
	if got := sp.At(nightlyPause(), ts(3, 1, 0), State{}); got != (State{Paused: true}) {
		t.Errorf("At once the suspension ended = %+v, want the window's pause back", got)
	}
}

func TestOpenEndedSuspensionNeverChangesTheAnswer(t *testing.T) {
	sp := Suspension{On: true}
	if !sp.Covers(ts(30, 12, 0)) {
		t.Error("an open-ended suspension ran out")
	}
	if next, ok := sp.Next(nightlyPause(), ts(2, 12, 0), State{}); ok {
		t.Errorf("Next = %s, want no change while the timetable is set aside until lifted", next)
	}
}

func TestSuspensionEndingInsideAWindowIsTheNextChange(t *testing.T) {
	sp := Suspension{On: true, Until: ts(3, 1, 0)}
	next, ok := sp.Next(nightlyPause(), ts(2, 23, 0), State{})
	if !ok || !next.Equal(ts(3, 1, 0)) {
		t.Errorf("Next = %s, %v; want the end of the suspension, where the pause comes back", next, ok)
	}
}

func TestSuspensionEndingOutsideAWindowSkipsToTheTimetable(t *testing.T) {
	sp := Suspension{On: true, Until: ts(2, 15, 0)}
	next, ok := sp.Next(nightlyPause(), ts(2, 12, 0), State{})
	if !ok || !next.Equal(ts(2, 22, 0)) {
		t.Errorf("Next = %s, %v; want 22:00, since nothing changes when the suspension ends at 15:00", next, ok)
	}
}

// The browser sends "until midnight" as a UTC instant, and the rows are wall
// clock times where the server runs.
func TestSuspensionEndSentInUTCIsReadInTheServersZone(t *testing.T) {
	vienna := time.FixedZone("CEST", 2*60*60)
	s := Compile([]Entry{{Days: everyDay(), Start: "00:00", End: "01:00", Action: ActionPause}})
	now := time.Date(2026, time.September, 25, 20, 0, 0, 0, vienna)
	midnight := time.Date(2026, time.September, 26, 0, 0, 0, 0, vienna)
	sp := Suspension{On: true, Until: midnight.UTC()}

	next, ok := sp.Next(s, now, State{})
	if !ok || !next.Equal(midnight) {
		t.Errorf("Next = %s, %v; want local midnight, where the suspension ends inside the pause window", next, ok)
	}
}

func TestZeroSuspensionLeavesTheTimetableAlone(t *testing.T) {
	var sp Suspension
	if got := sp.At(nightlyPause(), ts(2, 23, 0), State{}); got != (State{Paused: true}) {
		t.Errorf("At = %+v, want the window's pause", got)
	}
	next, ok := sp.Next(nightlyPause(), ts(2, 23, 0), State{})
	if !ok || !next.Equal(ts(3, 6, 0)) {
		t.Errorf("Next = %s, %v; want the window's end at 06:00", next, ok)
	}
}

func TestRunnerSuspendLiftsAnOpenWindowAtOnce(t *testing.T) {
	clock := newFakeClock(ts(2, 23, 0))
	applied := make(chan State, 8)
	r, err := NewRunner(Options{
		Entries: []Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}},
		Apply:   func(s State) { applied <- s },
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	r.Start()

	if got := <-applied; got != (State{Paused: true}) {
		t.Fatalf("first apply = %+v, want the nightly pause", got)
	}
	clock.waited(t)

	r.Suspend(Suspension{On: true, Until: ts(3, 0, 0)})
	if got := <-applied; got != (State{}) {
		t.Errorf("apply after Suspend = %+v, want the queue running", got)
	}
	if got := clock.waited(t); got != time.Hour {
		t.Errorf("waited %s, want 1h; the pause comes back at midnight", got)
	}

	clock.advance(ts(3, 0, 0))
	if got := <-applied; got != (State{Paused: true}) {
		t.Errorf("apply at the end of the suspension = %+v, want the pause back", got)
	}
}

func TestRunnerStartsUnderACarriedOverSuspension(t *testing.T) {
	clock := newFakeClock(ts(2, 23, 0))
	applied := make(chan State, 8)
	r, err := NewRunner(Options{
		Entries:    []Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}},
		Apply:      func(s State) { applied <- s },
		Suspension: Suspension{On: true},
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	r.Start()

	if got := <-applied; got != (State{}) {
		t.Errorf("first apply = %+v, want the queue running while the timetable is set aside", got)
	}

	r.Suspend(Suspension{})
	if got := <-applied; got != (State{Paused: true}) {
		t.Errorf("apply after lifting the suspension = %+v, want the nightly pause", got)
	}
}
