package startupcheck

import (
	"fmt"
	"os"
	"time"
)

// clockFacts is everything the running process can observe about its own idea
// of the time, gathered in one place so the JUDGEMENT below is a pure function
// of them.
//
// Split this way for one reason, and it is not tidiness. Go decides time.Local
// exactly once, on first use, from the environment the process started in;
// t.Setenv("TZ", …) inside a test therefore changes nothing at all, and a test
// written against the environment would pass on a CI runner that happens to be
// on UTC and fail on the developer's machine in Europe/Berlin - or, far worse,
// pass on both while checking nothing. With the facts injected, every one of
// the four verdicts below is reachable from a table and each of them can be
// made to fail on purpose.
type clockFacts struct {
	// TZ is the raw environment variable, exactly as it was set.
	TZ string
	// ZoneName and Offset are what time.Now().Zone() answers: the abbreviation
	// in force right now ("CEST") and its offset in seconds east of UTC.
	ZoneName string
	Offset   int
	// LocalName is time.Local.String(), and it is deliberately NOT the thing
	// any verdict is raised on. Go's initLocal overwrites the name whenever the
	// zone came from /etc/localtime rather than from TZ, so a container with
	// /etc/localtime bind-mounted - a completely ordinary setup - reports the
	// literal string "Local" and looks broken to anything that reads it as a
	// zone name. It travels as supporting text and nothing else.
	LocalName string
	// Now is the moment, formatted by the caller into the detail line.
	Now time.Time
	// ZoneDBOK is whether this system has a zone database at all, probed by
	// loading one zone that certainly exists in a complete one.
	ZoneDBOK bool
	// TZLoadOK is whether the zone TZ names could be loaded. Meaningless and
	// true when TZ is empty.
	TZLoadOK bool
}

// Clock reports which clock a schedule window is actually being read against.
//
// THE WHOLE FINDING IS THAT GO FALLS BACK TO UTC IN SILENCE. TZ=Europe/Berln (a
// typo) and TZ unset are indistinguishable from inside the process except that
// local time is UTC in both, and no error is raised anywhere, ever. Nothing in
// this app notices; the first symptom is a nightly window that ran an hour or
// two off, weeks later, on somebody else's schedule. internal/schedule's own
// header names that dependency and nothing detects it today.
func Clock() Check {
	f := observeClock()
	return clockCheck(f)
}

func observeClock() clockFacts {
	now := time.Now()
	name, offset := now.Zone()
	f := clockFacts{
		TZ:        os.Getenv("TZ"),
		ZoneName:  name,
		Offset:    offset,
		LocalName: time.Local.String(),
		Now:       now,
	}
	// Europe/Berlin rather than UTC: UTC is built into the binary and loads on
	// a system with no zone database at all, so probing it would answer "fine"
	// on exactly the image this is meant to catch. A named zone with daylight
	// saving transitions can only come from a real database.
	//
	// This binary does NOT import time/tzdata - only internal/schedule's own
	// test does - so the database is the container image's tzdata package and
	// nothing else. A rebuild onto scratch or distroless removes every DST
	// transition in the app with no error anywhere.
	_, err := time.LoadLocation("Europe/Berlin")
	f.ZoneDBOK = err == nil
	f.TZLoadOK = true
	if f.TZ != "" {
		_, err := time.LoadLocation(f.TZ)
		f.TZLoadOK = err == nil
	}
	return f
}

// clockCheck is the judgement, and every branch of it rests on something that
// can be PROVEN from inside the process rather than guessed at.
func clockCheck(f clockFacts) Check {
	c := Check{
		ID:      IDClock,
		Subject: f.TZ,
		Detail:  clockDetail(f),
	}

	switch {
	case !f.ZoneDBOK:
		// Checked FIRST, and the order is the whole difference between a
		// useful row and a misleading one. With no zone database even a
		// perfectly spelled TZ fails to load, so testing the spelling first
		// would report "check your spelling" at somebody whose spelling is
		// fine and whose image simply has no zone data in it.
		c.Verdict = VerdictWarn
		c.Code = CodeNoZoneDatabase

	case f.TZ != "" && !f.TZLoadOK && f.Offset == 0 && f.ZoneName == "UTC":
		// BOTH halves are required. That TZ names a zone which will not load
		// is one fact; that the process is nevertheless running on UTC is the
		// other, and only the pair proves the silent fallback happened. On
		// Windows the pair never occurs, because Go ignores TZ there and takes
		// the zone from the system - so a desktop install with a stray TZ is
		// not accused of something that did not happen to it.
		c.Verdict = VerdictFail
		c.Code = CodeUTCFallback

	case f.TZ == "" && f.Offset == 0 && f.ZoneName == "UTC":
		// Warn and not fail: running on UTC is a working configuration, and on
		// a container with no TZ set it is the expected one. It is here because
		// it is worth KNOWING - a window written as 23:00 means 23:00 UTC, not
		// 23:00 where the person who typed it lives.
		c.Verdict = VerdictWarn
		c.Code = CodeTZUnset

	default:
		c.Verdict = VerdictOK
	}
	return c
}

// clockDetail is the fact worth reading: the abbreviation in force, its offset,
// the local time right now, and time.Local's own name last, as supporting text
// nothing is decided on.
func clockDetail(f clockFacts) string {
	sign := "+"
	off := f.Offset
	if off < 0 {
		sign, off = "-", -off
	}
	return fmt.Sprintf("%s %s%02d:%02d, %s (%s)",
		f.ZoneName, sign, off/3600, (off%3600)/60,
		f.Now.Format(time.RFC3339), f.LocalName)
}
