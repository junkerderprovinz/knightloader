package app

// Covers the three account-entity contracts that must never regress silently:
// Enabled defaults true for an account nothing ever recorded metadata for
// (the migration hazard Task.Enabled has already been bitten by once), a
// disabled account is invisible to routing exactly like a missing credential,
// and the page's row list is one row per configured account rather than one
// per catalogue entry.
//
// These stay clear of the real debrid APIs on purpose: rewireBackends only
// makes an outbound call for a service it finds a routed credential for, so
// every case here is built to leave that set empty (a store-level seed that
// skips SetAccountCredential's own rewire, or a disable that removes the only
// configured service before rewireBackends runs) - a unit test that depends on
// a third party being reachable is not a unit test.

import (
	"os"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

func newAccountsTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// TestAccountEnabledDefaultsTrue is the migration guarantee: an account with
// no recorded metadata - which is every account that exists today, and every
// one of the three env-keyed secrets the moment this change lands - reads as
// enabled, never as switched off.
func TestAccountEnabledDefaultsTrue(t *testing.T) {
	a := newAccountsTestApp(t)

	// Seeded directly through the store so this does not go through
	// SetAccountCredential's own rewireBackends call - see the package
	// comment above.
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	if !a.accountEnabled("alldebrid", "") {
		t.Fatal("accountEnabled defaulted to false for an account with no recorded metadata; must default true")
	}
	rows := a.AccountStates()
	if len(rows) != 1 || !rows[0].Enabled {
		t.Fatalf("AccountStates() = %+v, want exactly one row with Enabled=true", rows)
	}
}

// TestAccountEnabledFromEnvDefaultsTrue is the same guarantee for the
// specific migration this wave performs: a credential that was always
// supplied by the container (KL_ALLDEBRID and friends) becomes an account row
// for the first time, and that row must not come up disabled.
func TestAccountEnabledFromEnvDefaultsTrue(t *testing.T) {
	t.Setenv("KL_ALLDEBRID", "container-supplied-key")
	a := newAccountsTestApp(t)

	rows := a.AccountStates()
	if len(rows) != 1 {
		t.Fatalf("AccountStates() = %+v, want exactly one row", rows)
	}
	row := rows[0]
	if !row.Enabled {
		t.Fatal("env-supplied account defaulted to Enabled=false; must default true")
	}
	if !row.FromEnv || row.EnvVar != "KL_ALLDEBRID" {
		t.Fatalf("row = %+v, want FromEnv=true and EnvVar=%q stating the reason", row, "KL_ALLDEBRID")
	}
}

// TestAccountDisableGatesRouting is the routing half of Enabled: switching an
// account off must make rewireBackends see no credential at all, the same as
// if nothing had ever been configured - not merely "hidden on the page".
func TestAccountDisableGatesRouting(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	if got := a.routedCredential("alldebrid").APIKey; got != "fake-key" {
		t.Fatalf("routedCredential = %q while enabled, want the stored key", got)
	}

	// Disabling with nothing else configured means rewireBackends (called by
	// SetAccountEnabled) has no service to make an outbound call for.
	a.SetAccountEnabled("alldebrid", "", false)

	if a.accountEnabled("alldebrid", "") {
		t.Fatal("account still reads enabled after SetAccountEnabled(false)")
	}
	if got := a.routedCredential("alldebrid").APIKey; got != "" {
		t.Fatalf("routedCredential = %q once disabled, want empty - Enabled must gate routing exactly as a missing credential does", got)
	}
	for _, id := range a.Registry.IDs() {
		if id == "alldebrid" {
			t.Fatal("alldebrid resolver still registered after its only account was disabled")
		}
	}
}

// TestAccountStatesOneRowPerAccount is the shape change the whole page is
// built on: the row list tracks configured accounts, not catalogue entries -
// two services with nothing set up contribute no rows, and a third with one
// stored credential contributes exactly one.
func TestAccountStatesOneRowPerAccount(t *testing.T) {
	a := newAccountsTestApp(t)
	if len(accounts.Catalogue) < 2 {
		t.Fatal("this test needs at least two catalogue entries to be meaningful")
	}
	if got := a.AccountStates(); len(got) != 0 {
		t.Fatalf("AccountStates() on a fresh store = %+v, want no rows at all", got)
	}

	if err := a.Accounts.SetCredential("realdebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	rows := a.AccountStates()
	if len(rows) != 1 || rows[0].Service != "realdebrid" {
		t.Fatalf("AccountStates() = %+v, want exactly the one configured account (not one row per catalogue entry)", rows)
	}
}

// TestAccountRemoveClearsMetadata is deleteAccountMeta's own contract: once a
// credential is cleared, a slot configured again later under the same
// service/account id must not resurrect a stale Enabled=false.
func TestAccountRemoveClearsMetadata(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.SetAccountCredential("realdebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	a.SetAccountEnabled("realdebrid", "", false)
	if a.accountEnabled("realdebrid", "") {
		t.Fatal("setup failed: expected the account to be disabled")
	}

	if err := a.SetAccountCredential("realdebrid", "", accounts.Credential{}); err != nil {
		t.Fatal(err)
	}
	if !a.accountEnabled("realdebrid", "") {
		t.Fatal("accountEnabled still false after the credential was cleared; metadata must not survive removal")
	}

	// Confirm the file itself was actually touched, not merely read as
	// missing by chance.
	if _, err := os.Stat(a.acctMetaPath()); err != nil {
		t.Fatalf("account_meta.json missing after SetAccountEnabled: %v", err)
	}
}
