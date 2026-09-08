package selftest

// The zone reading.
//
// WHAT THIS FILE DELIBERATELY DOES NOT ASSERT: that setting TZ changes the
// offset. It does not, and a test that expected it to would be testing a state
// the program cannot reach - Go resolves time.Local once, during package
// initialisation, and no amount of os.Setenv afterwards moves it. That is
// exactly why ZoneReport reads the environment separately from time.Local
// instead of trusting one to describe the other.
//
// AND IT DOES NOT ASSERT THAT A REAL ZONE RESOLVES. "Europe/Berlin" resolving
// depends on the zone database being installed on whatever machine runs the
// suite, which is a fact about the test runner rather than about this code -
// on a stripped container it is legitimately absent, and a green suite must
// not require anybody to install tzdata to run it. The direction that CAN be
// asserted everywhere is the failing one: a name no database will ever have.

import (
	"testing"
	"time"
)

// TestAnUnsetTZNeverReportsTheZoneDatabaseAsUnreadable is the one that keeps
// the ordinary container quiet. Nothing was asked of the database, so nothing
// failed, and reporting otherwise would put "the zone database could not be
// read" in front of every install that simply never set TZ - which is most of
// them, and for which the sentence is meaningless.
func TestAnUnsetTZNeverReportsTheZoneDatabaseAsUnreadable(t *testing.T) {
	t.Setenv("TZ", "")
	z := ZoneReport()
	if z.TZ != "" {
		t.Fatalf("TZ = %q with the variable unset, want empty", z.TZ)
	}
	if !z.TZResolved {
		t.Fatal("TZResolved is false with no TZ set at all; nothing was asked of the zone database, " +
			"so nothing can have failed - this would warn every ordinary install about a problem it does not have")
	}
	if z.Name == "" {
		t.Error("the zone came back with no name at all; the row has nothing to show")
	}
}

// TestATZTheDatabaseCannotSupplyIsReportedAsUnresolved is the finding that is
// invisible from every other angle: Go falls back to UTC silently when TZ
// names a zone the database does not have, so the configuration insists on one
// zone while every schedule in the process runs in another, with nothing
// anywhere saying so.
func TestATZTheDatabaseCannotSupplyIsReportedAsUnresolved(t *testing.T) {
	t.Setenv("TZ", "Definitely/NotAZone")
	z := ZoneReport()
	if z.TZ != "Definitely/NotAZone" {
		t.Fatalf("TZ = %q, want the value the environment actually holds", z.TZ)
	}
	if z.TZResolved {
		t.Fatal("TZResolved is true for a zone name no database has; this is the exact case the field exists " +
			"for - Go falls back to UTC without a word and the timetable then runs at the wrong hour")
	}
	if z.Name != "Definitely/NotAZone" {
		t.Errorf("Name = %q, want what TZ asked for - the row has to show the operator their own value back", z.Name)
	}
}

// TestTheOffsetMatchesTheClockItIsReportedBeside stops the two halves drifting
// apart: the offset is what makes the reported time readable, and one taken
// from a different call than the abbreviation would disagree with it across a
// daylight-saving boundary.
func TestTheOffsetMatchesTheClockItIsReportedBeside(t *testing.T) {
	z := ZoneReport()
	_, want := time.Now().Zone()
	if z.OffsetSeconds != want {
		t.Fatalf("OffsetSeconds = %d, want %d - the offset in force right now", z.OffsetSeconds, want)
	}
	if z.UTC && want != 0 {
		t.Fatalf("the zone reports itself as UTC at an offset of %d seconds; UTC is not merely an offset of zero "+
			"and it is certainly not a non-zero one", want)
	}
}
