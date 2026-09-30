package app

import (
	"context"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Every solver the catalogue offers has a client, built from the credential
// its Kind asks for, and a credential missing a part builds none.
func TestEveryCatalogueCaptchaSolverHasAClient(t *testing.T) {
	for _, svc := range accounts.Catalogue {
		if svc.Group != accounts.GroupCaptchaSolver {
			continue
		}
		full := accounts.Credential{APIKey: "key"}
		partial := accounts.Credential{Username: "user"}
		if svc.Kind == accounts.KindUsernamePassword {
			full = accounts.Credential{Username: "user", Password: "pass"}
		}
		if captchaSolverFor(svc.ID, full) == nil {
			t.Errorf("%s: no client for a complete credential", svc.ID)
		}
		if captchaSolverFor(svc.ID, partial) != nil {
			t.Errorf("%s: a client for an incomplete credential", svc.ID)
		}
	}
}

// A solver whose account is switched off on the Accounts page is not tried,
// whatever the order says, and comes back when it is switched on again.
func TestCaptchaSolverSwitchedOffOnTheAccountsPageIsNotTried(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.SetAccountCredential("anticaptcha", "", accounts.Credential{APIKey: "key"}); err != nil {
		t.Fatal(err)
	}
	cfg := a.Settings.Get()
	cfg.CaptchaSolverOrder = []string{"anticaptcha"}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	if got := len(a.captchaSolvers()); got != 1 {
		t.Fatalf("with its key stored the order yields %d solvers, want 1", got)
	}
	a.SetAccountEnabled("anticaptcha", "", false)
	if got := len(a.captchaSolvers()); got != 0 {
		t.Fatalf("switched off, the order still yields %d solvers", got)
	}
	a.SetAccountEnabled("anticaptcha", "", true)
	if got := len(a.captchaSolvers()); got != 1 {
		t.Fatalf("switched on again, the order yields %d solvers, want 1", got)
	}
}

// An incomplete solver credential is refused before any network call.
func TestCheckCredentialRefusesAnIncompleteSolverLogin(t *testing.T) {
	ok, _, err := checkCredential(context.Background(), "deathbycaptcha", accounts.Credential{Username: "user"})
	if ok || err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("checkCredential = %v, %v, want an incomplete credential", ok, err)
	}
}
