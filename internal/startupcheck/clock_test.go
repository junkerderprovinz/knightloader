package startupcheck

import (
	"testing"
	"time"
)

// TestClockVerdicts is a table over clockFacts rather than over the environment,
// and that is the only way this can be tested at all: Go decides time.Local
// exactly once, on first use, from the environment the process started in, so
// t.Setenv("TZ", …) changes nothing. A test written against the environment
// would pass on a CI runner that happens to be on UTC, fail on a machine in
// Europe/Berlin, and - worst of the three - could pass on both while exercising
// none of the branches it claims to.
func TestClockVerdicts(t *testing.T) {
	berlin := clockFacts{TZ: "Europe/Berlin", ZoneName: "CEST", Offset: 7200, LocalName: "Europe/Berlin", ZoneDBOK: true, TZLoadOK: true}

	cases := []struct {
		name    string
		facts   clockFacts
		verdict Verdict
		code    string
	}{
		{
			name:    "a configured zone that loaded is simply fine",
			facts:   berlin,
			verdict: VerdictOK,
		},
		{
			// The typo case, and the whole reason this row exists. Go raises no
			// error anywhere; the only trace is that local time is UTC while TZ
			// names something else.
			name:    "TZ names a zone that will not load and the process is on UTC",
			facts:   clockFacts{TZ: "Europe/Berln", ZoneName: "UTC", Offset: 0, LocalName: "UTC", ZoneDBOK: true, TZLoadOK: false},
			verdict: VerdictFail,
			code:    CodeUTCFallback,
		},
		{
			// Windows: Go ignores TZ there and takes the zone from the system,
			// so the fallback never happened and accusing the install of it
			// would be the false alarm that empties the page of readers.
			name:    "TZ will not load but local time is not UTC, so nothing fell back",
			facts:   clockFacts{TZ: "Europe/Berln", ZoneName: "CET", Offset: 3600, LocalName: "Local", ZoneDBOK: true, TZLoadOK: false},
			verdict: VerdictOK,
		},
		{
			// Checked before the spelling, or somebody whose spelling is fine
			// is told to go and fix it.
			name:    "no zone database at all beats a spelling complaint",
			facts:   clockFacts{TZ: "Europe/Berln", ZoneName: "UTC", Offset: 0, LocalName: "UTC", ZoneDBOK: false, TZLoadOK: false},
			verdict: VerdictWarn,
			code:    CodeNoZoneDatabase,
		},
		{
			name:    "no zone database even with TZ unset",
			facts:   clockFacts{TZ: "", ZoneName: "UTC", Offset: 0, LocalName: "UTC", ZoneDBOK: false, TZLoadOK: true},
			verdict: VerdictWarn,
			code:    CodeNoZoneDatabase,
		},
		{
			name:    "nothing set and running on UTC is worth knowing, not worth a red row",
			facts:   clockFacts{TZ: "", ZoneName: "UTC", Offset: 0, LocalName: "UTC", ZoneDBOK: true, TZLoadOK: true},
			verdict: VerdictWarn,
			code:    CodeTZUnset,
		},
		{
			// /etc/localtime bind-mounted: Go's initLocal overwrites the name,
			// so time.Local.String() is the literal "Local". Raising anything
			// on that string would call a perfectly ordinary container broken.
			name:    "a zone from /etc/localtime reports the name \"Local\" and is fine",
			facts:   clockFacts{TZ: "", ZoneName: "CET", Offset: 3600, LocalName: "Local", ZoneDBOK: true, TZLoadOK: true},
			verdict: VerdictOK,
		},
		{
			// TZ=UTC is somebody saying UTC on purpose, and Go loads it.
			name:    "UTC asked for by name is not a fallback",
			facts:   clockFacts{TZ: "UTC", ZoneName: "UTC", Offset: 0, LocalName: "UTC", ZoneDBOK: true, TZLoadOK: true},
			verdict: VerdictOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.facts.Now = time.Date(2026, 9, 8, 21, 30, 0, 0, time.UTC)
			c := clockCheck(tc.facts)
			if c.ID != IDClock {
				t.Errorf("id = %q, want %q", c.ID, IDClock)
			}
			if c.Verdict != tc.verdict {
				t.Errorf("verdict = %q, want %q", c.Verdict, tc.verdict)
			}
			if c.Code != tc.code {
				t.Errorf("code = %q, want %q", c.Code, tc.code)
			}
			if c.Subject != tc.facts.TZ {
				t.Errorf("subject = %q, want the raw TZ value %q", c.Subject, tc.facts.TZ)
			}
			if c.Detail == "" {
				t.Error("detail is empty; the zone in force and the local time are the whole reading")
			}
		})
	}
}

// TestClockDetailCarriesTheOffsetBothWays. The sign is worked out by hand here,
// so a box west of Greenwich has to be exercised or half the arithmetic is
// never run.
func TestClockDetailCarriesTheOffsetBothWays(t *testing.T) {
	now := time.Date(2026, 9, 8, 21, 30, 0, 0, time.UTC)
	if got := clockDetail(clockFacts{ZoneName: "CEST", Offset: 7200, LocalName: "Europe/Berlin", Now: now}); got[:12] != "CEST +02:00," {
		t.Errorf("detail = %q, want it to start with the zone and a positive offset", got)
	}
	if got := clockDetail(clockFacts{ZoneName: "EDT", Offset: -14400, LocalName: "America/New_York", Now: now}); got[:11] != "EDT -04:00," {
		t.Errorf("detail = %q, want a negative offset spelled with a minus", got)
	}
}

// TestClockObservesTheRunningProcess is the one case the table cannot cover:
// that observeClock actually fills the facts in from this process rather than
// answering with a zero value. It asserts only what is true on every machine.
func TestClockObservesTheRunningProcess(t *testing.T) {
	c := Clock()
	if c.ID != IDClock {
		t.Errorf("id = %q, want %q", c.ID, IDClock)
	}
	if c.Verdict == "" {
		t.Error("verdict is empty")
	}
	if c.Detail == "" {
		t.Error("detail is empty, so nothing was read off the running process at all")
	}
}
