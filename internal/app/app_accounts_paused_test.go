package app

import (
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

func TestABenchedAccountIsListedByItsServiceAlone(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "work", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	a.SetAccountLabel("alldebrid", "work", "SECRETLABEL")
	if got := a.PausedAccounts(); len(got.Benches) != 0 || len(got.On) != 1 {
		t.Fatalf("an account nobody benched = %+v, want it on and without a bench", got)
	}
	if _, started := a.acctHealthTracker().ReportFailure("alldebrid", "work", accounts.HealthTempDisabled, "DETAILLEAK", time.Hour); !started {
		t.Fatal("setup: expected the bench to start")
	}

	got := a.PausedAccounts()
	if len(got.Benches) != 1 || len(got.On) != 1 {
		t.Fatalf("PausedAccounts() = %+v, want the one bench of the one account", got)
	}
	p := got.Benches[0]
	if p.Service != "AllDebrid" || p.Host != "" || p.Volume || p.ID == "" || p.Account != got.On[0] {
		t.Errorf("PausedAccounts()[0] = %+v, want AllDebrid for every hoster with an id", p)
	}
	if until, err := time.Parse(time.RFC3339, p.Until); err != nil || time.Until(until) < 50*time.Minute {
		t.Errorf("Until = %q, want about an hour from now", p.Until)
	}
	for _, leak := range []string{"work", "SECRETLABEL", "DETAILLEAK", "fake-key"} {
		if strings.Contains(p.ID+p.Account+p.Service+p.Host+p.Until, leak) {
			t.Errorf("the bench says %q: %+v", leak, p)
		}
	}
	if again := a.PausedAccounts(); again.Benches[0].ID != p.ID {
		t.Errorf("the same bench changed its id from %q to %q", p.ID, again.Benches[0].ID)
	}

	a.reportAccountSuccess("alldebrid", "work")
	if left := a.PausedAccounts(); len(left.Benches) != 0 || len(left.On) != 1 {
		t.Errorf("an account back in use = %+v, want it on without a bench", left)
	}
}

func TestABenchedAccountSwitchedOffLeavesTheAccountsInUse(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", time.Hour)
	if got := a.PausedAccounts(); len(got.Benches) != 1 || len(got.On) != 1 {
		t.Fatalf("PausedAccounts() = %+v, want the bench of the account in use", got)
	}
	a.SetAccountEnabled("alldebrid", "", false)
	if got := a.PausedAccounts(); len(got.Benches) != 0 || len(got.On) != 0 {
		t.Errorf("PausedAccounts() = %+v, want neither the bench nor the account once it is off", got)
	}
}
