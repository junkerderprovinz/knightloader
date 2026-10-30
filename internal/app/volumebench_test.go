package app

// A spent download volume benches a debrid service until the volume is back.
// The key check that ends an ordinary bench passes on a spent account, so it
// would send the next batch of links into the same refusal every 15 minutes.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// spentVolumeApp has a Debrid-Link key stored, a key check that always passes and
// counts its calls, and an account reading the test sets.
func spentVolumeApp(t *testing.T) (*App, *atomic.Int32, *atomic.Value) {
	t.Helper()
	a := quotaApp(t)
	if err := a.Accounts.SetCredential("debridlink", "", accounts.Credential{APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	origProbe, origFetch := probeCredential, accountInfoFetcher
	probeCredential = func(context.Context, string, accounts.Credential) (bool, int, error) {
		probes.Add(1)
		return true, 1, nil
	}
	var reading atomic.Value
	reading.Store(TrafficState{UsedPercent: 100, PercentKnown: true})
	accountInfoFetcher = func(context.Context, string, accounts.Credential) (AccountHealth, bool, error) {
		return AccountHealth{Tier: "premium", Traffic: reading.Load().(TrafficState), FetchedAt: time.Now()}, true, nil
	}
	t.Cleanup(func() { probeCredential, accountInfoFetcher = origProbe, origFetch })
	return a, &probes, &reading
}

func TestAVolumeBenchOutlastsTheKeyCheck(t *testing.T) {
	a, probes, reading := spentVolumeApp(t)
	tr := a.acctHealthTracker()

	a.reportAccountFailure("debridlink", "", core.ReasonLimit, debridLinkSpent)
	rec := tr.Get("debridlink", "")
	if rec.State != accounts.HealthTempDisabled || !rec.Volume {
		t.Fatalf("record %+v, want a volume bench", rec)
	}
	if d := time.Until(rec.BenchedUntil); d < volumeRecheck-time.Minute {
		t.Fatalf("benched for %s, want the hour a volume without a reset time waits", d)
	}

	a.probeBenchExpiry("debridlink", "", rec.BenchedUntil)

	if tr.Usable("debridlink", "") {
		t.Fatal("the bench ended while the reading still shows the volume used up")
	}
	if probes.Load() != 0 {
		t.Error("the key was checked, which a spent account passes")
	}
	again := tr.Get("debridlink", "")
	if !again.Volume || !again.BenchedUntil.After(rec.BenchedUntil) {
		t.Errorf("record %+v, want the volume bench carried on past %s", again, rec.BenchedUntil)
	}

	reading.Store(TrafficState{UsedPercent: 40, PercentKnown: true})
	a.probeBenchExpiry("debridlink", "", again.BenchedUntil)

	if !tr.Usable("debridlink", "") {
		t.Error("the bench outlived a reading with volume left")
	}
}

func TestTheResetTimeEndsAVolumeBench(t *testing.T) {
	a, _, reading := spentVolumeApp(t)
	reset := time.Now().Add(300 * time.Millisecond)
	reading.Store(TrafficState{UsedPercent: 100, PercentKnown: true, ResetsAt: reset.UTC().Format(time.RFC3339Nano)})
	st := a.healthState()
	st.mu.Lock()
	st.rows[metaKey("debridlink", "")] = AccountHealth{Tier: "premium", Traffic: reading.Load().(TrafficState)}
	st.mu.Unlock()

	a.reportAccountFailure("debridlink", "", core.ReasonLimit, debridLinkSpent)
	if got := a.acctHealthTracker().Get("debridlink", "").BenchedUntil; !got.Equal(reset) {
		t.Fatalf("benched until %s, want the reset at %s", got, reset)
	}

	// The service has refilled the volume by the time the bench is over.
	reading.Store(TrafficState{UsedPercent: 0, PercentKnown: true})
	waitFor(t, "the reset to end the bench", func() bool { return a.acctHealthTracker().Usable("debridlink", "") })
}

// A volume used up for one hoster takes the service off that hoster alone.
func TestAHosterVolumeBenchLeavesTheOtherHostersAlone(t *testing.T) {
	a, events := siteBenchApp(t)
	isolateResolvers(a,
		sitesResolver{id: "debridlink", prio: 50, sites: []string{"down.example", "up.example"}},
		sitesResolver{id: "torbox", prio: 47, sites: []string{"down.example"}},
	)
	queueForSite(a, "d1", "down.example", "")
	dispatchNow(a)
	wantEvents(t, events, "debridlink download d1")

	a.onUpdate("d1", core.Update{Status: core.StatusError,
		Err: "debridlink: debrid-link /downloader/add: the daily traffic limit for this hoster is used up"})

	got := settle(events)
	if !got["debridlink remove d1"] || !got["torbox download d1"] {
		t.Fatalf("backend calls %v, want the link handed from Debrid-Link to TorBox", got)
	}
	if !a.acctHealthTracker().Usable("debridlink", "") {
		t.Fatal("Debrid-Link's account was benched over the volume of one hoster")
	}

	queueForSite(a, "u1", "up.example", "")
	dispatchNow(a)
	wantEvents(t, events, "debridlink download u1")

	queueForSite(a, "d2", "down.example", "")
	dispatchNow(a)
	wantEvents(t, events, "torbox download d2")

	pauses := a.accountPauses("debridlink", "")
	if len(pauses) != 1 || !pauses[0].Volume || pauses[0].Host != "down.example" || pauses[0].Until == "" {
		t.Errorf("accounts page reads %+v, want one volume pause for down.example with its end", pauses)
	}
}

// A refresh on the accounts page is how a person tries a benched account
// before its time is up.
func TestARefreshEndsAVolumeBench(t *testing.T) {
	a, _, _ := spentVolumeApp(t)
	a.reportAccountFailure("debridlink", "", core.ReasonLimit, debridLinkSpent)
	a.mu.Lock()
	a.benchSiteLocked("debridlink", "https://rapidgator.net/file/x", time.Now().Add(time.Hour), true)
	a.mu.Unlock()
	if len(a.accountPauses("debridlink", "")) != 2 {
		t.Fatalf("pauses %+v, want the account's and the hoster's", a.accountPauses("debridlink", ""))
	}

	st := a.TestAccount("debridlink", "")

	if !a.acctHealthTracker().Usable("debridlink", "") {
		t.Error("the account is still benched after a refresh that passed")
	}
	if len(st.Paused) != 0 {
		t.Errorf("the row still shows %+v", st.Paused)
	}
}
