package schedule

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	// The DST tests need a real zone with real transitions, and a bare container
	// (or a Windows box) has no system zone database to read one from.
	_ "time/tzdata"
)

// The reference week is 2026-03-02 (a Monday) through 2026-03-08 (a Sunday), so
// every weekday in a table below can be read off the day number directly.
func ts(day, hour, min int) time.Time {
	return time.Date(2026, time.March, day, hour, min, 0, 0, time.UTC)
}

func everyDay() []time.Weekday {
	return []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
}

func workdays() []time.Weekday {
	return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
}

// TestAtWindowRunsPastMidnight is the bug this package is most likely to have:
// a nightly window belongs to the day it opened on, so "Fri 22:00-06:00" must
// reach into Saturday morning and must not touch Friday morning. Getting it
// wrong pauses the queue nineteen hours early, every single week.
func TestAtWindowRunsPastMidnight(t *testing.T) {
	s := Compile([]Entry{{
		Days: []time.Weekday{time.Friday}, Start: "22:00", End: "06:00", Action: ActionPause,
	}})
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"friday morning is not the tail of friday night", ts(6, 3, 0), false},
		{"just before it opens", ts(6, 21, 59), false},
		{"the opening minute", ts(6, 22, 0), true},
		{"before midnight", ts(6, 23, 59), true},
		{"after midnight", ts(7, 0, 0), true},
		{"last minute", ts(7, 5, 59), true},
		{"the closing minute is already out", ts(7, 6, 0), false},
		{"saturday night is not ticked", ts(7, 22, 0), false},
		{"so sunday morning is free", ts(8, 3, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, State{}).Paused; got != c.want {
				t.Errorf("At(%s).Paused = %v, want %v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestAtPlainWindowStaysInsideItsDay is the same check for the non-wrapping
// case, including the half-open edge: a window ends the minute it says it does,
// so two windows that meet cannot both own the shared minute.
func TestAtPlainWindowStaysInsideItsDay(t *testing.T) {
	s := Compile([]Entry{
		{Days: workdays(), Start: "08:00", End: "12:00", Action: ActionLimit, Limit: 100},
		{Days: workdays(), Start: "12:00", End: "16:00", Action: ActionLimit, Limit: 200},
	})
	cases := []struct {
		name string
		when time.Time
		want int64
	}{
		{"before the first", ts(2, 7, 59), 0},
		{"inside the first", ts(2, 8, 0), 100},
		{"last minute of the first", ts(2, 11, 59), 100},
		{"the shared minute belongs to the second", ts(2, 12, 0), 200},
		{"inside the second", ts(2, 15, 59), 200},
		{"after the second", ts(2, 16, 0), 0},
		{"saturday is not a workday", ts(7, 9, 0), 0},
		{"sunday neither", ts(8, 9, 0), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, State{}).Limit; got != c.want {
				t.Errorf("At(%s).Limit = %d, want %d", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestAtLaterEntriesWin pins the layering rule the docs promise: a broad window
// laid down first, an exception carved out of it afterwards. If order stopped
// mattering, the lunch break in the middle of an office-hours pause would either
// never open or never close, depending on which way the tie broke.
func TestAtLaterEntriesWin(t *testing.T) {
	broad := Entry{Days: workdays(), Start: "09:00", End: "17:00", Action: ActionPause}
	exception := Entry{Days: workdays(), Start: "12:00", End: "13:00", Action: ActionResume}

	if got := Compile([]Entry{broad, exception}).At(ts(2, 12, 30), State{}).Paused; got {
		t.Error("the exception is listed last, so it must win at 12:30")
	}
	if got := Compile([]Entry{broad, exception}).At(ts(2, 11, 0), State{}).Paused; !got {
		t.Error("outside the exception the broad window still holds")
	}
	// The same two rows in the other order describe something else entirely, and
	// that has to be visible: it is the only thing that makes the order a feature
	// rather than an accident.
	if got := Compile([]Entry{exception, broad}).At(ts(2, 12, 30), State{}).Paused; !got {
		t.Error("with the broad window last it must swallow the exception at 12:30")
	}
}

// TestAtActionsTouchOnlyTheirOwnField covers a speed window layered inside a
// pause window. If an action reset the whole state instead of its own field, the
// overlapping half of the night would lose one of the two.
func TestAtActionsTouchOnlyTheirOwnField(t *testing.T) {
	s := Compile([]Entry{
		{Days: everyDay(), Start: "00:00", End: "12:00", Action: ActionPause},
		{Days: everyDay(), Start: "06:00", End: "18:00", Action: ActionLimit, Limit: 100},
	})
	cases := []struct {
		name string
		when time.Time
		want State
	}{
		{"pause only", ts(2, 3, 0), State{Paused: true}},
		{"both", ts(2, 7, 0), State{Paused: true, Limit: 100}},
		{"limit only", ts(2, 13, 0), State{Limit: 100}},
		{"neither", ts(2, 19, 0), State{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, State{}); got != c.want {
				t.Errorf("At(%s) = %+v, want %+v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestAtKeepsTheBaseWhereNothingApplies is why At takes a base at all: the user's
// own speed limit has to survive a window that says nothing about speed. If the
// base were assumed to be the zero state, an unrelated pause window would hand
// the whole line back every night.
func TestAtKeepsTheBaseWhereNothingApplies(t *testing.T) {
	base := State{Limit: 5000}
	s := Compile([]Entry{
		{Days: everyDay(), Start: "01:00", End: "02:00", Action: ActionPause},
		{Days: everyDay(), Start: "03:00", End: "04:00", Action: ActionLimit, Limit: 100},
	})
	cases := []struct {
		name string
		when time.Time
		want State
	}{
		{"outside every window", ts(2, 12, 0), State{Limit: 5000}},
		{"a pause window leaves the limit alone", ts(2, 1, 30), State{Paused: true, Limit: 5000}},
		{"a speed window replaces it", ts(2, 3, 30), State{Limit: 100}},
		{"and hands it back afterwards", ts(2, 4, 0), State{Limit: 5000}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.At(c.when, base); got != c.want {
				t.Errorf("At(%s) = %+v, want %+v", c.when.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestNextLandsOnTheChange checks that Next reports the edge that matters and
// steps over the ones that do not. A boundary between two windows that agree is
// not a change, and waking the caller for it would be a wake-up with nothing to
// do at the end of it.
func TestNextLandsOnTheChange(t *testing.T) {
	nightly := Compile([]Entry{{
		Days: workdays(), Start: "22:00", End: "06:00", Action: ActionLimit, Limit: 200,
	}})
	touching := Compile([]Entry{
		{Days: everyDay(), Start: "08:00", End: "12:00", Action: ActionLimit, Limit: 100},
		{Days: everyDay(), Start: "12:00", End: "16:00", Action: ActionLimit, Limit: 100},
	})
	agrees := Compile([]Entry{{
		Days: everyDay(), Start: "08:00", End: "12:00", Action: ActionLimit, Limit: 500,
	}})

	cases := []struct {
		name string
		s    Schedule
		base State
		from time.Time
		want time.Time
		ok   bool
	}{
		{"up to the opening edge", nightly, State{}, ts(2, 12, 0), ts(2, 22, 0), true},
		{"then over midnight to the closing edge", nightly, State{}, ts(2, 23, 0), ts(3, 6, 0), true},
		{"the closing edge itself is behind us", nightly, State{}, ts(3, 6, 0), ts(3, 22, 0), true},
		// Asked from inside the tail, the change is the end of a window that
		// opened yesterday. Looking only at today's edges would skip it and sleep
		// straight through the morning.
		{"inside the tail of yesterday's window", nightly, State{}, ts(3, 2, 0), ts(3, 6, 0), true},
		{"across the weekend gap", nightly, State{}, ts(7, 7, 0), ts(9, 22, 0), true},
		{"the friday tail still ends on saturday", nightly, State{}, ts(6, 23, 0), ts(7, 6, 0), true},
		{"a boundary that changes nothing is skipped", touching, State{}, ts(2, 9, 0), ts(2, 16, 0), true},
		{"a window that only restates the base never changes anything", agrees, State{Limit: 500}, ts(2, 9, 0), time.Time{}, false},
		{"the same window against a different base does", agrees, State{Limit: 900}, ts(2, 7, 0), ts(2, 8, 0), true},
		{"an empty timetable never changes", Schedule{}, State{}, ts(2, 9, 0), time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.s.Next(c.from, c.base)
			if ok != c.ok {
				t.Fatalf("Next(%s) ok = %v, want %v", c.from.Format(time.RFC3339), ok, c.ok)
			}
			if ok && !got.Equal(c.want) {
				t.Errorf("Next(%s) = %s, want %s", c.from.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
			}
		})
	}
}

// TestNextIsSafeToSleepThrough is the promise the package is built on: a caller
// may sleep from now until Next and miss nothing. Every minute in between is
// walked and must still answer what Next was asked about, and the instant itself
// must answer differently — otherwise "sleep until Next" would be silently
// skipping a window, which is exactly the failure that polling hides.
//
// The probes deliberately straddle both days on which the clocks move, because
// that is where an implementation built on wall-clock arithmetic drifts away
// from real elapsed time.
func TestNextIsSafeToSleepThrough(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	s := Compile([]Entry{
		{Days: workdays(), Start: "09:00", End: "17:00", Action: ActionPause},
		{Days: workdays(), Start: "12:00", End: "13:00", Action: ActionResume},
		{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionLimit, Limit: 500000},
		{Days: []time.Weekday{time.Sunday}, Start: "02:00", End: "06:00", Action: ActionLimit},
	})
	base := State{Limit: 1000000}

	probes := []time.Time{
		time.Date(2026, time.March, 2, 0, 0, 0, 0, berlin),
		time.Date(2026, time.March, 2, 11, 30, 0, 0, berlin),
		time.Date(2026, time.March, 6, 23, 15, 0, 0, berlin),
		time.Date(2026, time.March, 28, 20, 0, 0, 0, berlin), // the evening before the clocks go forward
		time.Date(2026, time.March, 29, 0, 0, 0, 0, berlin),  // the night they do
		time.Date(2026, time.October, 24, 23, 0, 0, 0, berlin),
		time.Date(2026, time.October, 25, 0, 0, 0, 0, berlin), // the night they go back
	}
	for _, from := range probes {
		next, ok := s.Next(from, base)
		if !ok {
			t.Fatalf("Next(%s) found no change at all", from.Format(time.RFC3339))
		}
		want := s.At(from, base)
		for cur := from; cur.Before(next); cur = cur.Add(time.Minute) {
			if got := s.At(cur, base); got != want {
				t.Fatalf("from %s: state changed to %+v at %s, but Next said the first change was %s",
					from.Format(time.RFC3339), got, cur.Format(time.RFC3339), next.Format(time.RFC3339))
			}
		}
		if got := s.At(next, base); got == want {
			t.Errorf("from %s: Next returned %s, where the state is still %+v",
				from.Format(time.RFC3339), next.Format(time.RFC3339), got)
		}
	}
}

// TestSpringForward documents what happens to a window on the day an hour is
// skipped. The evaluator reads the wall clock, so:
//
//   - a window lying entirely inside the missing hour does not run that day at
//     all. There is no instant whose clock reads 02:15, so nothing can be in
//     force at 02:15. It runs again a week later, unchanged.
//   - a window that starts inside the missing hour and ends after it opens the
//     moment the clock jumps, and is therefore an hour shorter that one night.
//
// The alternative — pinning windows to elapsed time so the missing hour is
// served anyway — would drag every following night off the wall clock the user
// typed, which is the worse surprise for a timetable.
//
// Both zones below skip an hour, but Santiago skips the hour that contains local
// midnight: on 2026-09-06 the clock runs from Saturday 23:59 straight to Sunday
// 01:00. Anything that reaches a day by building local midnight and adding to it
// lands on a time that does not exist there, so a European-only test would leave
// the whole class untested.
func TestSpringForward(t *testing.T) {
	cases := []struct {
		zone string
		day  time.Weekday
		// jump is the first instant of the new offset, given in UTC because the
		// wall clock it replaced cannot be written down.
		jump time.Time
		// inside lies wholly within the missing hour; straddle starts inside it
		// and ends well after.
		insideStart, insideEnd     string
		straddleStart, straddleEnd string
		// insideNextWeek is when the skipped window next runs, straddleShut when
		// the straddling one closes on the day itself.
		insideNextWeek, straddleShut time.Time
	}{
		{
			zone: "Europe/Berlin", day: time.Sunday,
			jump:        time.Date(2026, time.March, 29, 1, 0, 0, 0, time.UTC), // 03:00 CEST
			insideStart: "02:00", insideEnd: "02:30",
			straddleStart: "02:00", straddleEnd: "06:00",
			insideNextWeek: time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC),  // 02:00 CEST
			straddleShut:   time.Date(2026, time.March, 29, 4, 0, 0, 0, time.UTC), // 06:00 CEST
		},
		{
			zone: "America/Santiago", day: time.Sunday,
			jump:        time.Date(2026, time.September, 6, 4, 0, 0, 0, time.UTC), // 01:00 -03
			insideStart: "00:15", insideEnd: "00:45",
			straddleStart: "00:15", straddleEnd: "06:00",
			insideNextWeek: time.Date(2026, time.September, 13, 3, 15, 0, 0, time.UTC), // 00:15 -03
			straddleShut:   time.Date(2026, time.September, 6, 9, 0, 0, 0, time.UTC),   // 06:00 -03
		},
	}

	for _, c := range cases {
		t.Run(c.zone, func(t *testing.T) {
			loc := mustLoad(t, c.zone)
			jump := c.jump.In(loc)
			// An hour before the jump is a wall clock that exists in both zones and
			// sits ahead of both windows, so it is a fair starting point for Next.
			from := jump.Add(-time.Hour)

			t.Run("a window inside the missing hour is skipped for that day", func(t *testing.T) {
				s := Compile([]Entry{{
					Days: []time.Weekday{c.day}, Start: c.insideStart, End: c.insideEnd, Action: ActionPause,
				}})
				for _, when := range []time.Time{from, jump.Add(-time.Minute), jump, jump.Add(15 * time.Minute), jump.Add(30 * time.Minute)} {
					if s.At(when, State{}).Paused {
						t.Errorf("at %s the queue is paused, but that window's clock times never happen today", when.Format(time.RFC3339))
					}
				}
				// A week later there is nothing special about that clock time, so the
				// window is back and Next has to say so rather than give up.
				want := c.insideNextWeek.In(loc)
				got, ok := s.Next(from, State{})
				if !ok || !got.Equal(want) {
					t.Errorf("Next = %s (ok=%v), want the same window next week at %s", got.Format(time.RFC3339), ok, want.Format(time.RFC3339))
				}
			})

			t.Run("a window that starts inside it opens when the clock jumps", func(t *testing.T) {
				s := Compile([]Entry{{
					Days: []time.Weekday{c.day}, Start: c.straddleStart, End: c.straddleEnd, Action: ActionPause,
				}})
				if s.At(jump.Add(-time.Minute), State{}).Paused {
					t.Error("the window must not open before its start time")
				}
				if !s.At(jump, State{}).Paused {
					t.Error("the first instant of the new offset is past the start time, so the window is open")
				}
				if got, ok := s.Next(from, State{}); !ok || !got.Equal(jump) {
					t.Errorf("Next = %s (ok=%v), want the transition instant %s", got.Format(time.RFC3339), ok, jump.Format(time.RFC3339))
				}
				// More hours on the clock than in real time. Next has to agree with At
				// about that or the caller wakes an hour late.
				want := c.straddleShut.In(loc)
				if got, ok := s.Next(jump, State{}); !ok || !got.Equal(want) {
					t.Errorf("Next from the jump = %s (ok=%v), want %s", got.Format(time.RFC3339), ok, want.Format(time.RFC3339))
				}
			})
		})
	}
}

// TestFallBack is the other half of that day: an hour of wall clock runs twice,
// so a window inside it is in force twice and Next has to find all four edges.
//
// Both hemispheres are here on purpose. time.Date cannot answer an ambiguous
// wall clock — there are two instants and it returns one — and which one it
// picks depends on the sign of the zone's offset: east of Greenwich it lands on
// the second pass, west of it on the first. An implementation that trusts that
// guess and reconstructs the other pass from it passes in Berlin and loses the
// second pass entirely in Santiago, where the window then looks like it opened
// once and never closed.
func TestFallBack(t *testing.T) {
	cases := []struct {
		zone  string
		day   time.Weekday
		start string
		end   string
		// back is the instant the clocks go back, in UTC. The window opens on the
		// first minute of the hour that is about to repeat, so its four edges are
		// back-1h, back-30m, back and back+30m.
		back time.Time
	}{
		{"Europe/Berlin", time.Sunday, "02:00", "02:30",
			time.Date(2026, time.October, 25, 1, 0, 0, 0, time.UTC)},
		// Santiago repeats the last hour of Saturday, so the window is a Saturday
		// window and its second pass falls on the same calendar day as its first.
		{"America/Santiago", time.Saturday, "23:00", "23:30",
			time.Date(2026, time.April, 5, 3, 0, 0, 0, time.UTC)},
	}

	for _, c := range cases {
		t.Run(c.zone, func(t *testing.T) {
			loc := mustLoad(t, c.zone)
			s := Compile([]Entry{{
				Days: []time.Weekday{c.day}, Start: c.start, End: c.end, Action: ActionPause,
			}})

			firstOpen := c.back.Add(-time.Hour).In(loc)
			firstShut := c.back.Add(-30 * time.Minute).In(loc)
			againOpen := c.back.In(loc)
			againShut := c.back.Add(30 * time.Minute).In(loc)

			for _, p := range []struct {
				when time.Time
				want bool
			}{
				{firstOpen, true},
				{firstShut, false},
				{againOpen, true},
				{againShut, false},
			} {
				if got := s.At(p.when, State{}).Paused; got != p.want {
					t.Errorf("At(%s).Paused = %v, want %v", p.when.Format(time.RFC3339), got, p.want)
				}
			}

			from := c.back.Add(-2 * time.Hour).In(loc)
			for _, want := range []time.Time{firstOpen, firstShut, againOpen, againShut} {
				got, ok := s.Next(from, State{})
				if !ok || !got.Equal(want) {
					t.Fatalf("Next(%s) = %s (ok=%v), want %s", from.Format(time.RFC3339), got.Format(time.RFC3339), ok, want.Format(time.RFC3339))
				}
				from = got
			}
		})
	}
}

// TestNextSurvivesAWeekdayTheClocksDelete is the bug a week of lookahead has.
// Coverage depends on nothing but the weekday and the minute, so the pattern
// repeats weekly and a week reads as enough — until the single occurrence of a
// weekday inside that week is the day a clock jump deletes the window's times.
// The rule then produces no edges at all, Next answers "the state never changes
// again", and the caller stops asking: a nightly window that would run perfectly
// well the following week is dropped for good, with nothing on screen to say so.
//
// Asking from the transition day itself hides all of this, because that weekday
// then appears twice however short the horizon is. Every probe below is on some
// other day for exactly that reason.
func TestNextSurvivesAWeekdayTheClocksDelete(t *testing.T) {
	cases := []struct {
		name  string
		zone  string
		entry Entry
		// from and want are given in UTC because the wall clocks in play either
		// do not exist or belong to an offset that is about to change.
		from time.Time
		want time.Time
	}{
		{
			name:  "berlin loses the 02:00 hour, so the sunday window runs a week later",
			zone:  "Europe/Berlin",
			entry: Entry{Days: []time.Weekday{time.Sunday}, Start: "02:00", End: "02:30", Action: ActionPause},
			from:  time.Date(2026, time.March, 24, 11, 0, 0, 0, time.UTC), // Tuesday 12:00 CET
			want:  time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC),   // 02:00 CEST
		},
		{
			name:  "and from the friday before it too",
			zone:  "Europe/Berlin",
			entry: Entry{Days: []time.Weekday{time.Sunday}, Start: "02:00", End: "02:30", Action: ActionPause},
			from:  time.Date(2026, time.March, 27, 11, 0, 0, 0, time.UTC), // Friday 12:00 CET
			want:  time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			// Santiago skips the hour that contains local midnight, so the deleted
			// window sits at the very start of the day rather than in the middle.
			name:  "santiago loses the midnight hour",
			zone:  "America/Santiago",
			entry: Entry{Days: []time.Weekday{time.Sunday}, Start: "00:15", End: "00:45", Action: ActionPause},
			from:  time.Date(2026, time.September, 2, 16, 0, 0, 0, time.UTC), // Wednesday 12:00 -04
			want:  time.Date(2026, time.September, 13, 3, 15, 0, 0, time.UTC),
		},
		{
			// Kiritimati jumped from -10:00 to +14:00 on 1994-12-31, so Saturday
			// 1994-12-31 never happened there at all. A whole weekday can go missing,
			// not just an hour of one.
			name:  "kiritimati deletes an entire saturday",
			zone:  "Pacific/Kiritimati",
			entry: Entry{Days: []time.Weekday{time.Saturday}, Start: "12:00", End: "13:00", Action: ActionPause},
			from:  time.Date(1994, time.December, 30, 9, 0, 0, 0, time.UTC), // Thursday 23:00 -10
			want:  time.Date(1995, time.January, 6, 22, 0, 0, 0, time.UTC),  // 12:00 +14 on the 7th
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loc := mustLoad(t, c.zone)
			s := Compile([]Entry{c.entry})
			from := c.from.In(loc)
			want := c.want.In(loc)

			got, ok := s.Next(from, State{})
			if !ok {
				t.Fatalf("Next(%s) reported that the state never changes again, but the window runs at %s",
					from.Format(time.RFC3339), want.Format(time.RFC3339))
			}
			if !got.Equal(want) {
				t.Errorf("Next(%s) = %s, want %s", from.Format(time.RFC3339), got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
			// The window really is gone in between: if it ran somewhere in there the
			// answer above would be wrong for a different reason.
			for cur := from; cur.Before(want); cur = cur.Add(time.Minute) {
				if s.At(cur, State{}).Paused {
					t.Fatalf("the window was in force at %s, so %s was not the next change",
						cur.Format(time.RFC3339), want.Format(time.RFC3339))
				}
			}
		})
	}
}

// TestEmptyScheduleIsWhateverTheUserSet: a timetable with no usable rows has
// nothing to say, so the answer is the state the user set by hand. A fresh
// install is running and unlimited, and a queue the user paused stays paused. An
// evaluator that answered State{} here would release the brakes on every install
// that has no schedule yet and restart a queue somebody had deliberately stopped.
func TestEmptyScheduleIsWhateverTheUserSet(t *testing.T) {
	cases := []struct {
		name string
		s    Schedule
	}{
		{"the zero value", Schedule{}},
		{"compiled from nil", Compile(nil)},
		{"compiled from an empty list", Compile([]Entry{})},
		{"every row parked", Compile([]Entry{
			{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause, Disabled: true},
		})},
		{"every row unreadable", Compile([]Entry{
			{Days: everyDay(), Start: "half past ten", End: "06:00", Action: ActionPause},
		})},
	}
	bases := []State{{}, {Paused: true}, {Limit: 5000}, {Paused: true, Limit: 5000}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, base := range bases {
				for _, when := range []time.Time{ts(2, 0, 0), ts(2, 12, 0), ts(7, 23, 59)} {
					if got := c.s.At(when, base); got != base {
						t.Errorf("At(%s, %+v) = %+v, want the base back unchanged", when.Format(time.RFC3339), base, got)
					}
				}
				if got, ok := c.s.Next(ts(2, 12, 0), base); ok {
					t.Errorf("Next = %s, want no next change: there is nothing to change into",
						got.Format(time.RFC3339))
				}
			}
		})
	}
}

// TestAtUnlimitedWindowLiftsTheUsersOwnLimit is the distinction At's base
// parameter exists for. "No window applies" and "a window says unlimited" both
// come out as Limit 0, and folding them together would leave the user's daytime
// cap sitting on the queue all night — the one thing the window was written to
// remove. It would also blind Next, because the state either side of the edge
// would then look identical.
func TestAtUnlimitedWindowLiftsTheUsersOwnLimit(t *testing.T) {
	s := Compile([]Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionLimit}})
	base := State{Limit: 5000}

	if got := s.At(ts(2, 23, 0), base).Limit; got != 0 {
		t.Errorf("inside the window Limit = %d, want 0: the window says unlimited", got)
	}
	if got := s.At(ts(2, 12, 0), base).Limit; got != 5000 {
		t.Errorf("outside the window Limit = %d, want the user's own 5000", got)
	}
	if got, ok := s.Next(ts(2, 12, 0), base); !ok || !got.Equal(ts(2, 22, 0)) {
		t.Errorf("Next = %s (ok=%v), want the opening edge %s: lifting the cap is a change",
			got.Format(time.RFC3339), ok, ts(2, 22, 0).Format(time.RFC3339))
	}
}

// TestNightlyWindowHasNoSeamAtMidnight: a window that runs past midnight is one
// window, not two. covers answers it from a different branch either side of
// 00:00, and a split that did not line up would show either as a flicker at
// midnight or as a wake-up Next hands the caller with nothing to do at the end
// of it, every single night.
func TestNightlyWindowHasNoSeamAtMidnight(t *testing.T) {
	s := Compile([]Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}})
	for _, when := range []time.Time{ts(2, 23, 58), ts(2, 23, 59), ts(3, 0, 0), ts(3, 0, 1)} {
		if !s.At(when, State{}).Paused {
			t.Errorf("At(%s) is not paused, but the window runs straight through midnight", when.Format(time.RFC3339))
		}
	}
	if got, ok := s.Next(ts(2, 23, 0), State{}); !ok || !got.Equal(ts(3, 6, 0)) {
		t.Errorf("Next = %s (ok=%v), want the morning edge %s: midnight is not a change",
			got.Format(time.RFC3339), ok, ts(3, 6, 0).Format(time.RFC3339))
	}
}

// TestNextIsStrictlyAfterTheInstantAsked: the caller sleeps for Next minus now,
// so a Next that answered with now — or with the edge it is already standing on
// — would ask for a wait of nothing and spin the loop against a core. The
// seconds below are not round because a real clock reading almost never is.
func TestNextIsStrictlyAfterTheInstantAsked(t *testing.T) {
	s := Compile([]Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}})
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"a second before the opening edge", ts(2, 21, 59).Add(59 * time.Second), ts(2, 22, 0)},
		{"standing exactly on it", ts(2, 22, 0), ts(3, 6, 0)},
		{"a moment past it", ts(2, 22, 0).Add(time.Second), ts(3, 6, 0)},
		{"mid-minute in the last minute of the window", ts(3, 5, 59).Add(37 * time.Second), ts(3, 6, 0)},
		{"standing exactly on the closing edge", ts(3, 6, 0), ts(3, 22, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := s.Next(c.from, State{})
			if !ok {
				t.Fatalf("Next(%s) found no change at all", c.from.Format(time.RFC3339))
			}
			if !got.After(c.from) {
				t.Fatalf("Next(%s) = %s, which the caller would sleep no time at all for",
					c.from.Format(time.RFC3339), got.Format(time.RFC3339))
			}
			if !got.Equal(c.want) {
				t.Errorf("Next(%s) = %s, want %s", c.from.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
			}
		})
	}
}

// TestValidate pins the reasons a row is refused. Each of these is a row that
// would otherwise be stored and then quietly never fire, which is the failure
// mode a user cannot debug from the outside.
func TestValidate(t *testing.T) {
	ok := Entry{Days: []time.Weekday{time.Monday}, Start: "22:00", End: "06:00", Action: ActionPause}
	cases := []struct {
		name  string
		entry Entry
		want  string // substring of the message, empty means the row is fine
	}{
		{"a nightly window is not an error", ok, ""},
		{"seconds from a browser time input are tolerated", set(ok, func(e *Entry) { e.Start = "22:00:00" }), ""},
		// Tolerating the seconds field is not the same as not reading it. Waving it
		// through unchecked stores a row whose time the user has no reason to doubt.
		{"but the seconds field is still read", set(ok, func(e *Entry) { e.Start = "22:00:banana" }), "start time"},
		{"seconds out of range", set(ok, func(e *Entry) { e.End = "06:00:60" }), "end time"},
		{"an empty seconds field is not a time", set(ok, func(e *Entry) { e.End = "06:00:" }), "end time"},
		{"a speed window with no limit means unlimited", set(ok, func(e *Entry) { e.Action = ActionLimit }), ""},
		{"missing start", set(ok, func(e *Entry) { e.Start = "" }), "start time"},
		{"start is not a time", set(ok, func(e *Entry) { e.Start = "half past ten" }), "start time"},
		{"hour out of range", set(ok, func(e *Entry) { e.End = "24:00" }), "end time"},
		{"minute out of range", set(ok, func(e *Entry) { e.End = "06:60" }), "end time"},
		{"signed numbers are not times", set(ok, func(e *Entry) { e.Start = "+8:00" }), "start time"},
		{"zero length window", set(ok, func(e *Entry) { e.End = "22:00" }), "same minute"},
		{"no days", set(ok, func(e *Entry) { e.Days = nil }), "weekday"},
		{"a day that is not a day", set(ok, func(e *Entry) { e.Days = []time.Weekday{9} }), "not a day"},
		{"unknown action", set(ok, func(e *Entry) { e.Action = "reboot" }), "unknown action"},
		{"negative limit", set(ok, func(e *Entry) { e.Action, e.Limit = ActionLimit, -1 }), "negative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.entry.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Errorf("Validate() = %v, want the row accepted", err)
			case c.want != "" && err == nil:
				t.Errorf("Validate() accepted the row, want a complaint about %q", c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Errorf("Validate() = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestCompileKeepsTheUsableRows is the defensive half: settings.json can be
// hand-edited, and one broken line must not take the rest of the timetable with
// it. A disabled row is skipped the same way, which is the whole point of the
// flag.
func TestCompileKeepsTheUsableRows(t *testing.T) {
	s := Compile([]Entry{
		{Days: everyDay(), Start: "01:00", End: "02:00", Action: ActionPause},
		{Days: everyDay(), Start: "nonsense", End: "04:00", Action: ActionPause},
		{Days: everyDay(), Start: "05:00", End: "06:00", Action: ActionPause, Disabled: true},
		{Days: everyDay(), Start: "07:00", End: "08:00", Action: ActionLimit, Limit: 100},
	})
	if len(s.rules) != 2 {
		t.Fatalf("compiled %d rules, want the 2 usable ones", len(s.rules))
	}
	if !s.At(ts(2, 1, 30), State{}).Paused {
		t.Error("the first row must survive the broken one that follows it")
	}
	if s.At(ts(2, 5, 30), State{}).Paused {
		t.Error("a disabled row must not apply")
	}
	if got := s.At(ts(2, 7, 30), State{}).Limit; got != 100 {
		t.Errorf("the last row = %d, want 100: a broken row in the middle must not shift the rest", got)
	}
}

// TestNextMatchesAMinuteWalk cross-checks Next against the only definition of it
// that cannot itself be wrong: walk every minute and see where At first answers
// differently. The named tests above each pin one shape of window in one zone,
// which is how the fall-back handling came to be correct in Berlin and broken in
// Santiago — nobody had written down the case. This walks generated timetables
// through the awkward zones instead of the ones somebody thought of, so the next
// such gap fails here rather than in production once a year.
//
// The generator is seeded, so a failure is reproducible and prints the timetable
// that produced it.
func TestNextMatchesAMinuteWalk(t *testing.T) {
	// One zone per shape of transition, because the shape is what breaks: an
	// offset that goes up rather than down, a step that is not a whole hour, a
	// jump that swallows local midnight, and a zone that never moves at all.
	zones := []string{
		"Europe/Berlin",       // east of Greenwich, moves at 02:00
		"America/Santiago",    // west of Greenwich, and its jump swallows midnight
		"Australia/Lord_Howe", // moves by thirty minutes, southern hemisphere
		"America/St_Johns",    // half hour offset from UTC
		"Asia/Kolkata",        // no transitions at all
	}
	actions := []Action{ActionPause, ActionResume, ActionLimit}
	rnd := rand.New(rand.NewSource(20260329))

	for _, zn := range zones {
		loc := mustLoad(t, zn)

		// Probing around every transition of the year is the point; a handful of
		// ordinary instants come along to keep the plain weekly case honest.
		var probes []time.Time
		for cur := time.Date(2026, time.January, 1, 0, 0, 0, 0, loc); ; {
			_, zend := cur.ZoneBounds()
			if zend.IsZero() || !zend.After(cur) || zend.Year() > 2026 {
				break
			}
			probes = append(probes, zend.Add(-90*time.Minute), zend.Add(-time.Minute), zend, zend.Add(time.Minute), zend.Add(90*time.Minute))
			cur = zend
		}
		probes = append(probes,
			time.Date(2026, time.June, 10, 0, 0, 0, 0, loc),
			time.Date(2026, time.June, 13, 23, 30, 0, 0, loc))

		for i := 0; i < 12; i++ {
			var entries []Entry
			for n := 1 + rnd.Intn(3); n > 0; n-- {
				var days []time.Weekday
				for d := time.Sunday; d <= time.Saturday; d++ {
					if rnd.Intn(2) == 0 {
						days = append(days, d)
					}
				}
				if len(days) == 0 {
					days = append(days, time.Weekday(rnd.Intn(7)))
				}
				start, end := rnd.Intn(24*60), rnd.Intn(24*60)
				if start == end {
					end = (end + 1 + rnd.Intn(60)) % (24 * 60)
				}
				entries = append(entries, Entry{
					Days:   days,
					Start:  hhmm(start),
					End:    hhmm(end),
					Action: actions[rnd.Intn(len(actions))],
					Limit:  int64(rnd.Intn(3)) * 1000,
				})
			}
			s := Compile(entries)
			base := State{Limit: int64(rnd.Intn(2)) * 1000}

			for _, from := range probes {
				next, ok := s.Next(from, base)
				want := s.At(from, base)
				// With no next change the claim is that nothing changes at all. The
				// pattern repeats weekly, but the week that loses an hour can delete
				// every edge a weekday had, so the walk has to run well past the
				// horizon Next looks at or it would only confirm its own blind spot.
				stop := from.Add(22 * 24 * time.Hour)
				if ok {
					if !next.After(from) {
						t.Fatalf("%s %+v: Next(%s) = %s, which is not in the future",
							zn, entries, from.Format(time.RFC3339), next.Format(time.RFC3339))
					}
					stop = next
				}
				for cur := from; cur.Before(stop); cur = cur.Add(time.Minute) {
					if got := s.At(cur, base); got != want {
						t.Fatalf("%s %+v base %+v: from %s the state became %+v at %s, but Next said %s (ok=%v)",
							zn, entries, base, from.Format(time.RFC3339), got, cur.Format(time.RFC3339), next.Format(time.RFC3339), ok)
					}
				}
				if ok {
					if got := s.At(next, base); got == want {
						t.Fatalf("%s %+v base %+v: Next(%s) = %s, where the state is still %+v",
							zn, entries, base, from.Format(time.RFC3339), next.Format(time.RFC3339), got)
					}
				}
			}
		}
	}
}

// hhmm writes minutes since midnight the way an Entry stores them.
func hhmm(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func set(e Entry, f func(*Entry)) Entry {
	f(&e)
	return e
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}
