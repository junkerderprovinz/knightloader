package selftest

// What zone this process is actually running in, and whether the zone it was
// asked to run in could be found at all.
//
// THE ZONE IS THE FINDING, NOT THE SKEW. internal/schedule's own package
// comment says the timetable evaluator works in time.Local, and the Dockerfile
// installs tzdata and sets no TZ - so a container started without -e TZ=...
// runs every window in UTC. Somebody who put their nightly window at 22:00 has
// been starting it at 22:00 UTC, which in Berlin is 23:00 in winter and
// midnight in summer, and nothing in this app has ever mentioned it. That is a
// year-old silent misbehaviour that costs one os.Getenv to surface.
//
// THE SECOND HALF IS THE ONE NOBODY EXPECTS. time.LoadLocation on a name the
// zone database does not have returns an error, and Go's own initialisation
// of time.Local does the same thing SILENTLY: a TZ naming a zone that is not
// installed leaves the process in UTC with no message anywhere. So "TZ says
// Europe/Berlin" is not evidence that anything is running in Europe/Berlin,
// and the only way to tell is to ask the database the same question and see
// whether it answers.
//
// WHAT IS NOT IN THIS FILE: any verdict about the difference between this
// machine's clock and the reader's. That difference cannot be computed here -
// it needs the browser's own clock and the round-trip time to discount - so it
// is computed in the browser (web/src/lib/selftest.ts) and nowhere else. A Go
// function for it would have no caller in Go, and a function no Go code calls
// is one no Go test can protect from drifting away from the copy that actually
// decides.

import (
	"os"
	"time"
)

// Zone is this process's time zone as three separate facts, because they fail
// separately.
type Zone struct {
	// Name is the zone as best it can be named: what $TZ says when it says
	// anything, otherwise time.Local's own name, otherwise the abbreviation
	// currently in force. It is for showing, never for comparing.
	Name string
	// OffsetSeconds is the offset from UTC in force right now - "right now"
	// because a zone with a summer-time rule has two, and the one worth
	// reporting is the one a schedule is being evaluated against today.
	OffsetSeconds int
	// UTC reports whether this process is effectively running in UTC. It is
	// the condition the clock check raises on when a timetable exists, and it
	// is deliberately not "OffsetSeconds == 0": London in January is at zero
	// and is not UTC, and telling somebody in London to set TZ would be
	// advice that changes nothing.
	UTC bool
	// TZ is what the environment asked for, "" when nothing did.
	TZ string
	// TZResolved reports whether TZ names a zone the database could actually
	// supply. TRUE WHEN TZ IS EMPTY: nothing was asked of the database, so
	// nothing failed, and reporting false there would put a scary sentence in
	// front of every ordinary container.
	TZResolved bool
}

// ZoneReport reads this process's zone.
//
// It reads os.Getenv("TZ") rather than trusting time.Local's name for the
// answer to "what was asked for", because the two are genuinely different
// questions and only their disagreement is interesting. time.Local is what Go
// resolved at startup - already fallen back to UTC if the resolution failed -
// so on its own it can never show a failed resolution. TZ is the instruction;
// LoadLocation is whether the instruction could be carried out.
func ZoneReport() Zone {
	now := time.Now()
	abbrev, offset := now.Zone()

	z := Zone{
		Name:          abbrev,
		OffsetSeconds: offset,
		TZ:            os.Getenv("TZ"),
	}

	// time.Local.String() is "Local" on a build that read /etc/localtime with
	// no name attached, which tells a reader nothing at all - so it is used
	// only when it is something better than that.
	if n := time.Local.String(); n != "" && n != "Local" {
		z.Name = n
	}
	if z.TZ != "" {
		z.Name = z.TZ
	}

	// Effectively UTC: the offset is zero AND the zone calls itself UTC. Both
	// halves are needed - see the field's own comment for the London case, and
	// a zone named "UTC" with a non-zero offset would be a broken database
	// rather than UTC.
	z.UTC = offset == 0 && (abbrev == "UTC" || abbrev == "" || time.Local == time.UTC)

	if z.TZ == "" {
		z.TZResolved = true
		return z
	}
	_, err := time.LoadLocation(z.TZ)
	z.TZResolved = err == nil
	return z
}
