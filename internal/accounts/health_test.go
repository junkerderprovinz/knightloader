package accounts

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestClassifyReasonNotApplicable is the guard against benching a good key
// over a bad link: a failure about the LINK or about this machine must never
// reach the account at all, or one dead link would hold back every other
// link routed through a perfectly healthy account.
func TestClassifyReasonNotApplicable(t *testing.T) {
	for _, reason := range []core.Reason{
		core.ReasonGone, core.ReasonUnsupported, core.ReasonCaptcha,
		core.ReasonDiskFull, core.ReasonCancelled,
	} {
		if _, applicable := ClassifyReason(reason); applicable {
			t.Errorf("ClassifyReason(%q) applicable = true, want false - this reason is about the link or the machine, not the account", reason)
		}
	}
}

// TestClassifyReasonDefaultsToTempDisabled is row 4 of package 14: every
// reason this layer cannot tell apart from a global outage - an auth
// rejection, a rate limit, the service down, unreachable, or entirely
// unclassified - must default to HealthTempDisabled, never HealthInvalid.
// Only app_health.go's service-specific refineState is allowed to promote
// past this, and only on a verified code.
func TestClassifyReasonDefaultsToTempDisabled(t *testing.T) {
	for _, reason := range []core.Reason{
		core.ReasonAuth, core.ReasonLimit, core.ReasonUnavailable,
		core.ReasonNetwork, core.ReasonUnknown,
	} {
		state, applicable := ClassifyReason(reason)
		if !applicable {
			t.Errorf("ClassifyReason(%q) applicable = false, want true", reason)
		}
		if state != HealthTempDisabled {
			t.Errorf("ClassifyReason(%q) = %q, want %q (the safe default, never %q)", reason, state, HealthTempDisabled, HealthInvalid)
		}
	}
}

// TestHealthStateUsable pins which states route and which do not: only OK
// (and the zero value, an account nothing has ever reported on) may still be
// dispatched to.
func TestHealthStateUsable(t *testing.T) {
	cases := []struct {
		state HealthState
		want  bool
	}{
		{HealthOK, true},
		{"", true},
		{HealthInvalid, false},
		{HealthExpired, false},
		{HealthTempDisabled, false},
		{HealthError, false},
	}
	for _, c := range cases {
		if got := c.state.Usable(); got != c.want {
			t.Errorf("HealthState(%q).Usable() = %v, want %v", c.state, got, c.want)
		}
	}
}

// TestTrackerGetDefaultsOK is the load-bearing default for every account
// that has simply never failed: it must read as OK without needing an entry
// in the store at all, the same "absence defaults safe" shape
// app_accounts.go's accountEnabled already relies on for Enabled.
func TestTrackerGetDefaultsOK(t *testing.T) {
	tr := OpenTracker(t.TempDir())
	rec := tr.Get("alldebrid", "")
	if rec.State != HealthOK {
		t.Fatalf("Get on a never-reported account = %q, want %q", rec.State, HealthOK)
	}
	if !tr.Usable("alldebrid", "") {
		t.Fatal("Usable on a never-reported account = false, want true")
	}
}

// TestReportFailureStartsBenchOnce is row 3, at the state-machine level: the
// first failure that tips an account into HealthTempDisabled reports
// started=true and stamps BenchedUntil; every failure that lands on an
// account already benched reports started=false and leaves BenchedUntil
// exactly where it was - which is what lets the caller schedule exactly one
// probe per bench instead of one per failure.
func TestReportFailureStartsBenchOnce(t *testing.T) {
	tr := OpenTracker(t.TempDir())

	rec, started := tr.ReportFailure("torbox", "", HealthTempDisabled, "503 service unavailable", 15*time.Minute)
	if !started {
		t.Fatal("first ReportFailure into TempDisabled: started = false, want true")
	}
	firstUntil := rec.BenchedUntil
	if firstUntil.IsZero() {
		t.Fatal("BenchedUntil is zero after the bench started")
	}
	if rec.BenchCount != 1 {
		t.Fatalf("BenchCount = %d after the first bench, want 1", rec.BenchCount)
	}

	// A second, third, fourth failure while still within the same bench -
	// exactly what forty queued tasks sharing one dead key produce within the
	// same millisecond (see internal/app's dispatch-level test for the
	// end-to-end version of this).
	for i := 0; i < 3; i++ {
		rec2, started2 := tr.ReportFailure("torbox", "", HealthTempDisabled, "503 service unavailable", 30*time.Minute)
		if started2 {
			t.Fatalf("failure #%d against an already-benched account: started = true, want false", i+2)
		}
		if !rec2.BenchedUntil.Equal(firstUntil) {
			t.Fatalf("failure #%d changed BenchedUntil from %v to %v; piling on must not push the expiry out", i+2, firstUntil, rec2.BenchedUntil)
		}
		if rec2.BenchCount != 1 {
			t.Fatalf("failure #%d changed BenchCount to %d; piling on must not count as a second episode", i+2, rec2.BenchCount)
		}
	}

	if tr.Usable("torbox", "") {
		t.Fatal("a benched account reads as Usable")
	}
}

// TestReportFailureBenchEpisodesCount is the backoff half: once a bench
// clears (ReportSuccess) and the account fails again, that is a NEW episode
// and BenchCount continues from where it left off - what app_health.go's
// benchDelay grows against.
func TestReportFailureBenchEpisodesCount(t *testing.T) {
	tr := OpenTracker(t.TempDir())

	if _, started := tr.ReportFailure("alldebrid", "", HealthTempDisabled, "first", time.Minute); !started {
		t.Fatal("first bench: started = false, want true")
	}
	tr.ReportSuccess("alldebrid", "")
	if !tr.Usable("alldebrid", "") {
		t.Fatal("account not Usable after ReportSuccess")
	}

	rec, started := tr.ReportFailure("alldebrid", "", HealthTempDisabled, "second", time.Minute)
	if !started {
		t.Fatal("bench after a recovered success: started = false, want true (a new episode)")
	}
	if rec.BenchCount != 1 {
		t.Fatalf("BenchCount after a fresh episode following a recovery = %d, want 1 (ReportSuccess resets it)", rec.BenchCount)
	}
}

// TestReportFailureInvalidCarriesNoBench is State-specific: HealthInvalid
// (and, by the same construction, HealthExpired/HealthError) never gets a
// BenchedUntil - only HealthTempDisabled is ever auto-probed (row 1: "with a
// benchedUntil" names TempDisabled alone).
func TestReportFailureInvalidCarriesNoBench(t *testing.T) {
	tr := OpenTracker(t.TempDir())
	rec, started := tr.ReportFailure("alldebrid", "", HealthInvalid, "bad key", time.Hour)
	if started {
		t.Fatal("ReportFailure(HealthInvalid): started = true, want false - only TempDisabled schedules a probe")
	}
	if !rec.BenchedUntil.IsZero() {
		t.Fatalf("BenchedUntil = %v for an Invalid account, want zero", rec.BenchedUntil)
	}
	if tr.Usable("alldebrid", "") {
		t.Fatal("an Invalid account reads as Usable")
	}
}

// TestResetClearsRecord is what SetAccountCredential leans on: a fresh
// credential must not inherit the old one's verdict.
func TestResetClearsRecord(t *testing.T) {
	tr := OpenTracker(t.TempDir())
	tr.ReportFailure("realdebrid", "", HealthInvalid, "bad token", 0)
	if tr.Usable("realdebrid", "") {
		t.Fatal("setup failed: expected the account to read unusable before Reset")
	}
	tr.Reset("realdebrid", "")
	if !tr.Usable("realdebrid", "") {
		t.Fatal("account still unusable after Reset")
	}
}

// TestTrackerPersistsAcrossOpen is the "persisted status" half of package 14:
// health must survive a restart, the same guarantee accounts.json and
// account_meta.json already give the credential and its metadata.
func TestTrackerPersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	tr := OpenTracker(dir)
	tr.ReportFailure("torbox", "", HealthExpired, "must be premium", 0)

	reopened := OpenTracker(dir)
	rec := reopened.Get("torbox", "")
	if rec.State != HealthExpired {
		t.Fatalf("state after reopening = %q, want %q", rec.State, HealthExpired)
	}
	if rec.Detail != "must be premium" {
		t.Fatalf("detail after reopening = %q, want %q", rec.Detail, "must be premium")
	}
}

// TestTrackerNamedAccountsAreIndependent: a second login on the same service
// (accountKey's "named account" shape) must not share health with the
// default one - benching one AllDebrid key must not silently bench a second,
// perfectly good AllDebrid key configured beside it.
func TestTrackerNamedAccountsAreIndependent(t *testing.T) {
	tr := OpenTracker(t.TempDir())
	tr.ReportFailure("alldebrid", "work", HealthInvalid, "bad key", 0)

	if tr.Usable("alldebrid", "work") {
		t.Fatal("the named account that failed reads as Usable")
	}
	if !tr.Usable("alldebrid", "") {
		t.Fatal("the default account was affected by a different named account's failure")
	}
	if !tr.Usable("alldebrid", "personal") {
		t.Fatal("a third, unrelated named account was affected by a different one's failure")
	}
}
