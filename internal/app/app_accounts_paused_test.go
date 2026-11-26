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
	if len(a.PausedAccounts()) != 0 {
		t.Fatalf("an account nobody benched is listed: %+v", a.PausedAccounts())
	}
	if _, started := a.acctHealthTracker().ReportFailure("alldebrid", "work", accounts.HealthTempDisabled, "DETAILLEAK", time.Hour); !started {
		t.Fatal("setup: expected the bench to start")
	}

	got := a.PausedAccounts()
	if len(got) != 1 {
		t.Fatalf("PausedAccounts() = %+v, want the one bench", got)
	}
	p := got[0]
	if p.Service != "AllDebrid" || p.Host != "" || p.Volume || p.ID == "" {
		t.Errorf("PausedAccounts()[0] = %+v, want AllDebrid for every hoster with an id", p)
	}
	if until, err := time.Parse(time.RFC3339, p.Until); err != nil || time.Until(until) < 50*time.Minute {
		t.Errorf("Until = %q, want about an hour from now", p.Until)
	}
	for _, leak := range []string{"work", "SECRETLABEL", "DETAILLEAK", "fake-key"} {
		if strings.Contains(p.ID+p.Service+p.Host+p.Until, leak) {
			t.Errorf("the bench says %q: %+v", leak, p)
		}
	}
	if again := a.PausedAccounts(); again[0].ID != p.ID {
		t.Errorf("the same bench changed its id from %q to %q", p.ID, again[0].ID)
	}

	a.reportAccountSuccess("alldebrid", "work")
	if left := a.PausedAccounts(); len(left) != 0 {
		t.Errorf("an account back in use is still listed: %+v", left)
	}
}

func TestASwitchedOffAccountIsNotListedAsBenched(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	a.SetAccountEnabled("alldebrid", "", false)
	a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", time.Hour)
	if got := a.PausedAccounts(); len(got) != 0 {
		t.Errorf("PausedAccounts() = %+v, want nothing for an account that is off", got)
	}
}
