// Package schedule answers one question: at this moment, should the queue be
// running, and how fast. It is KnightLoader's reading of JDownloader's Scheduler
// extension, where a nightly window throttles or pauses downloads and the
// morning hands the line back.
//
// The evaluator is pure: a rule set plus a time.Time is the whole input. That is
// what makes Next possible — the answer can be asked for instants that have not
// happened yet, so the caller learns when the state will change and sleeps until
// then instead of waking every second to compare clocks. Runner is the thin loop
// that does exactly that.
//
// Everything here is wall-clock time in the location of the time.Time it is
// handed, which for a Runner is time.Local. That makes the zone database part of
// the answer: an image built without one has no transitions to read, so the
// windows silently stop following the clocks in spring and autumn. A binary that
// ships without /usr/share/zoneinfo has to import time/tzdata to get them back.
package schedule

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// horizonDays is how far Next looks ahead before it reports that the answer
// never changes again, which is what lets the caller stop asking rather than
// re-arm a timer for the same non-event forever.
//
// Coverage depends on nothing but the weekday and the minute of the day, so the
// timetable repeats every week and a week of lookahead looks like enough. It is
// not. A window whose clock times fall in the stretch a spring-forward jump
// deletes does not run that day at all, and it has no edges that day either —
// there is no instant to report. A weekday that appears only once then
// contributes nothing, Next says "never", and a nightly window that would have
// run again the following week is instead dropped for good: the runner parks on
// a nil channel until someone saves the settings page.
//
// Two weeks gives every weekday two occurrences ahead of the caller, and a zone
// does not move its clocks twice inside a fortnight, so at most one of them can
// be deleted.
const horizonDays = 14

// Action is what an entry does to the queue for as long as it is in force.
type Action string

const (
	// ActionPause holds the queue: running downloads are left alone, nothing new
	// starts. Killing a transfer mid-file would throw away bytes nobody asked to
	// lose, so this is the queue switch, not a stop button.
	ActionPause Action = "pause"

	// ActionResume releases the queue again. It exists so a narrow exception can
	// be layered over a wide window ("pause all day, except at lunch") instead of
	// forcing the user to describe the gap as two separate windows.
	ActionResume Action = "resume"

	// ActionLimit caps the combined download speed at Entry.Limit bytes per
	// second.
	ActionLimit Action = "limit"
)

// Entry is one row of the user's timetable.
type Entry struct {
	// Name is what the UI shows for the row. It carries no meaning here.
	Name string `json:"name,omitempty"`

	// Days are the weekdays the window opens on, as time.Weekday values with 0 =
	// Sunday. For a window that runs past midnight these name the day it starts:
	// "Fri 22:00-06:00" ends on Saturday morning without Saturday being ticked.
	Days []time.Weekday `json:"days"`

	// Start and End are local wall-clock times written "HH:MM". An End before
	// Start means the window runs past midnight, which is the normal shape of a
	// nightly window and not an error. An End equal to Start is refused rather
	// than read as a whole day, because "no time at all" is the equally fair
	// reading and a caller that wants the whole day can say so with two rows.
	Start string `json:"start"`
	End   string `json:"end"`

	// Action is what happens while the window is open.
	Action Action `json:"action"`

	// Limit is the cap in bytes per second for ActionLimit, where 0 means
	// unlimited — that is how a window says "take the brakes off". Ignored by the
	// other actions.
	Limit int64 `json:"limit,omitempty"`

	// Disabled parks a row without deleting it. The flag is negative on purpose:
	// a settings file written by an older build carries no such key, and a
	// positive "enabled" would decode to false there and silently retire every
	// window the user already had.
	Disabled bool `json:"disabled,omitempty"`
}

// State is what the queue should be doing.
type State struct {
	Paused bool `json:"paused"`
	// Limit is the total allowance in bytes per second; 0 means unlimited.
	Limit int64 `json:"limit"`
}

// rule is a validated entry in the shape the evaluator wants: a weekday bitmap
// and minutes since midnight, so answering costs no parsing and no allocation.
type rule struct {
	days   [7]bool
	start  int // minutes since local midnight
	end    int // minutes since local midnight
	wrap   bool
	action Action
	limit  int64
}

// Schedule is a compiled timetable. The zero value is a valid empty schedule
// that changes nothing, which is what a fresh install has.
//
// A compiled schedule is immutable, so it is a value the caller may copy and
// read from several goroutines at once without a lock of its own; Set installs a
// replacement rather than editing one in place.
type Schedule struct {
	rules []rule
}

// Compile turns stored rows into an evaluator, keeping their order.
//
// Rows that Validate rejects are skipped instead of failing the whole timetable:
// these entries come out of a settings file a human may have edited by hand, and
// one unreadable line must not take the other windows down with it. Disabled
// rows are skipped for the same reason they exist.
func Compile(entries []Entry) Schedule {
	var s Schedule
	for _, e := range entries {
		if e.Disabled {
			continue
		}
		r, err := e.compile()
		if err != nil {
			continue
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// Validate reports why an entry cannot be used, so the API can refuse a bad row
// with a reason the user can act on instead of storing a window that silently
// never fires. A row it accepts is a row Compile keeps, unless the user has
// parked it with Disabled — that is a choice, not a defect, so it is not
// something to refuse a save over.
func (e Entry) Validate() error {
	_, err := e.compile()
	return err
}

func (e Entry) compile() (rule, error) {
	start, err := parseClock(e.Start)
	if err != nil {
		return rule{}, fmt.Errorf("start time: %w", err)
	}
	end, err := parseClock(e.End)
	if err != nil {
		return rule{}, fmt.Errorf("end time: %w", err)
	}
	if start == end {
		// Read one way this is a whole day, read the other it is nothing. Guessing
		// is the dangerous option: an accidental 00:00 to 00:00 pause taken as a
		// whole day would hold the queue down forever with nothing on screen to
		// explain it.
		return rule{}, errors.New("start and end are the same minute; a window needs a length")
	}
	if len(e.Days) == 0 {
		// "Every day" is deliberately not the meaning of an empty list. A pause
		// window that quietly applies to all seven days because a checkbox was
		// missed is a worse surprise than a row that refuses to save.
		return rule{}, errors.New("pick at least one weekday")
	}
	r := rule{start: start, end: end, wrap: end < start, action: e.Action, limit: e.Limit}
	for _, d := range e.Days {
		if d < time.Sunday || d > time.Saturday {
			return rule{}, fmt.Errorf("weekday %d is not a day (0 = Sunday .. 6 = Saturday)", int(d))
		}
		r.days[int(d)] = true
	}
	switch e.Action {
	case ActionPause, ActionResume:
	case ActionLimit:
		if e.Limit < 0 {
			return rule{}, errors.New("a speed limit cannot be negative (0 means unlimited)")
		}
	default:
		return rule{}, fmt.Errorf("unknown action %q", string(e.Action))
	}
	return r, nil
}

// covers reports whether the rule is in force at the wall clock of t. The window
// is half-open, [start, end), so back-to-back windows tile the day without
// overlapping: 12:00-16:00 picks up exactly where 08:00-12:00 stops, and nobody
// has to write 11:59 to keep the pair from both claiming noon.
func (r rule) covers(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	d := int(t.Weekday())
	if !r.wrap {
		return r.days[d] && m >= r.start && m < r.end
	}
	// A window that runs past midnight belongs to the day it opened on. Testing
	// the current day for the tail as well is the classic bug here: "Mon
	// 22:00-06:00" would then also cover Monday 03:00, nineteen hours before it
	// was meant to start, and a nightly pause would sit on the queue every
	// morning.
	if r.days[d] && m >= r.start {
		return true
	}
	return r.days[(d+6)%7] && m < r.end
}

// At reports the state in force at t. It reads t's own location, because the
// entries are wall-clock times: they follow the host's zone, including across the
// days it moves its clocks. On those days a window can run twice or not at all,
// and that is the chosen behaviour rather than an oversight:
//
//   - when the clocks go back, a stretch of wall clock happens twice, so a window
//     inside it is in force twice. Next reports all four of its edges.
//   - when they go forward, a stretch never happens, so a window inside it does
//     not run that day and one that starts inside it opens the moment the clock
//     jumps, short by the length of the jump. It is back to its normal length the
//     following week.
//
// The stretch is not always an hour — Lord Howe moves by thirty minutes, Troll by
// two, and Kiritimati once moved by a whole day — which is why nothing here
// reasons about how far a clock jumped, only about which wall clocks existed.
//
// The alternative is to pin windows to elapsed time and serve the missing hour
// anyway, which drags every following night off the wall clock the user typed.
//
// base is the state where no entry covers t — the speed limit and pause switch
// the user set by hand. It is a parameter rather than an assumed zero because "no
// window applies" and "a window says unlimited" are different answers, and
// folding them together would let an unrelated pause window quietly wipe the
// limit the user configured.
//
// Entries are applied in order and the last one wins, so a broad rule can be laid
// down first and an exception carved out of it afterwards. Each action touches
// only the field it names: a speed window layered inside a pause window leaves
// the pause alone, and vice versa.
func (s Schedule) At(t time.Time, base State) State {
	out := base
	for _, r := range s.rules {
		if !r.covers(t) {
			continue
		}
		switch r.action {
		case ActionPause:
			out.Paused = true
		case ActionResume:
			out.Paused = false
		case ActionLimit:
			out.Limit = r.limit
		}
	}
	return out
}

// Next returns the first instant after t at which At would answer differently,
// so a caller can sleep exactly that long instead of waking every second to ask.
// Sleeping through it is safe: nothing changes in between, which is the property
// the package exists for.
//
// The second result is false when the answer never changes again — an empty
// timetable, or one whose windows all agree with what is already in force.
//
// base matters here too: when a window only asserts what the user already set by
// hand, its edges are not changes at all and Next steps straight over them.
func (s Schedule) Next(t time.Time, base State) (time.Time, bool) {
	if len(s.rules) == 0 {
		return time.Time{}, false
	}
	cur := s.At(t, base)
	for _, c := range s.boundaries(t) {
		if s.At(c, base) != cur {
			return c, true
		}
	}
	return time.Time{}, false
}

// boundaries lists every instant in the horizon at which the answer could change,
// in order and strictly after t. Those are the window edges, plus every zone
// transition: a clock that jumps moves the windows relative to real time, and a
// transition is the one moment a window can open or close without any edge of its
// own being reached.
func (s Schedule) boundaries(t time.Time) []time.Time {
	loc := t.Location()
	y, mo, d := t.Date()

	var out []time.Time
	add := func(c time.Time) {
		if c.After(t) {
			out = append(out, c)
		}
	}
	// The walk starts a day early: a window that opened yesterday evening still
	// has its closing edge ahead of us, and generating only from today would
	// leave the caller asleep through the end of the night it is already in.
	for off := -1; off <= horizonDays; off++ {
		// The weekday comes from a fixed midday in UTC. A zone whose clocks move
		// at midnight makes local midnight a time that does not exist, and
		// time.Date would normalise that into a neighbouring day.
		wd := int(time.Date(y, mo, d+off, 12, 0, 0, 0, time.UTC).Weekday())
		for _, r := range s.rules {
			if !r.days[wd] {
				continue
			}
			// Each edge is built from its own wall-clock fields rather than by
			// adding a duration to midnight: on a day that gains or loses an hour,
			// "midnight plus eight hours" is not 08:00. An edge can also happen
			// twice or not at all, which is why this is a list.
			for _, c := range wallInstants(y, mo, d+off, r.start/60, r.start%60, loc) {
				add(c)
			}
			endDay := d + off
			if r.wrap {
				endDay++
			}
			for _, c := range wallInstants(y, mo, endDay, r.end/60, r.end%60, loc) {
				add(c)
			}
		}
	}

	// The span is measured in elapsed time rather than in calendar days so that a
	// zone whose clocks move at midnight cannot turn the end of the horizon into a
	// wall clock that does not exist.
	for _, c := range zoneTransitions(t, t.Add(time.Duration(horizonDays+2)*24*time.Hour)) {
		add(c)
	}

	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

// wallInstants returns every instant whose local clock reads h:m on the given
// calendar day. Normally there is exactly one. On the day the clocks go back
// there are two and both are real, because the window inside the repeated hour
// genuinely runs twice; drop one and the window looks as if it opened and then
// never closed. On the day they go forward there are none, which is how a window
// lying in the skipped hour comes to be skipped with it.
//
// The candidates are spelled out rather than left to time.Date because time.Date
// has to guess at an ambiguous wall clock, and which of the two passes it lands
// on depends on the sign of the zone's offset: east of Greenwich it answers with
// the second pass, west of it with the first. Building on that guess made the
// repeated edges visible in Berlin and invisible in Santiago, so every user in
// the Americas kept a nightly window an hour past its end once a year.
func wallInstants(y int, mo time.Month, d, h, m int, loc *time.Location) []time.Time {
	// The wall clock read as though it were UTC. Every real instant with this
	// clock face is that value minus one of the offsets the zone actually used
	// nearby, so the candidates are exactly what offsetsAround yields.
	naive := time.Date(y, mo, d, h, m, 0, 0, time.UTC)
	wy, wmo, wd := naive.Date()

	var out []time.Time
	for _, off := range offsetsAround(naive, loc) {
		c := naive.Add(-time.Duration(off) * time.Second).In(loc)
		// Reading the clock back is what separates a real instant from a skipped
		// one. Without this check a window in the missing hour would silently move
		// to a time the user never typed instead of not running.
		cy, cmo, cd := c.Date()
		if cy != wy || cmo != wmo || cd != wd || c.Hour() != h || c.Minute() != m {
			continue
		}
		out = append(out, c)
	}
	return out
}

// offsetsAround lists, in seconds and without repeats, every UTC offset loc used
// within fifteen hours either side of naive. Fifteen is past the largest offset
// in the zone database, so a candidate instant cannot lie outside the scanned
// span and be missed.
func offsetsAround(naive time.Time, loc *time.Location) []int {
	end := naive.Add(15 * time.Hour)
	var out []int
	for cur := naive.Add(-15 * time.Hour).In(loc); ; {
		if _, off := cur.Zone(); !slices.Contains(out, off) {
			out = append(out, off)
		}
		_, zend := cur.ZoneBounds()
		if zend.IsZero() || !zend.After(cur) || !zend.Before(end) {
			return out
		}
		cur = zend
	}
}

// zoneTransitions walks the offset changes of t's location from t up to end. A
// location with no transitions at all (UTC, a fixed zone) yields none, and the
// loop is bounded by end rather than by a count so a database with dense
// historical entries cannot make it spin.
func zoneTransitions(t, end time.Time) []time.Time {
	var out []time.Time
	for cur := t; cur.Before(end); {
		_, zend := cur.ZoneBounds()
		if zend.IsZero() || !zend.After(cur) || !zend.Before(end) {
			return out
		}
		out = append(out, zend)
		cur = zend
	}
	return out
}

// parseClock reads "HH:MM" into minutes since local midnight. A browser time
// input may append seconds; they are checked and then dropped, because rejecting
// a row over a trailing ":00" nobody typed is a mystery from the outside, while
// waving the field through unread would accept "22:00:banana" as a time and
// store a row the user has no reason to doubt. The schedule's resolution is the
// minute, so a seconds value that is there and valid is deliberately ignored.
// 24:00 is not accepted: a window that ends at midnight is written "00:00" and
// wraps, and having two spellings for one edge invites them to drift apart.
func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("missing (want HH:MM)")
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	h, okH := twoDigits(parts[0])
	m, okM := twoDigits(parts[1])
	if !okH || !okM || h > 23 || m > 59 {
		return 0, fmt.Errorf("%q is not a time of day", s)
	}
	if len(parts) == 3 {
		if sec, ok := twoDigits(parts[2]); !ok || sec > 59 {
			return 0, fmt.Errorf("%q is not a time of day", s)
		}
	}
	return h*60 + m, nil
}

// twoDigits reads one or two ASCII digits. strconv would also take "+8" and
// " 8", which have no business in a stored time and would make two different
// spellings of the same row compare unequal.
func twoDigits(s string) (int, bool) {
	if len(s) == 0 || len(s) > 2 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}
