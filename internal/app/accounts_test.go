package app

// Accounts: Enabled defaults to true without recorded metadata, a disabled
// account is invisible to routing like a missing credential, and the page lists
// one row per configured account.
//
// rewireBackends only calls out for a routed credential, so every case leaves
// that set empty (a store-level seed, or a disable before the rewire) to stay
// clear of the real debrid APIs.

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

// An account without recorded metadata reads as enabled.
func TestAccountEnabledDefaultsTrue(t *testing.T) {
	a := newAccountsTestApp(t)

	// Seeded through the store, skipping SetAccountCredential's rewire.
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

// A credential supplied by the container (KL_ALLDEBRID and friends) appears as
// an enabled row.
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

// A disabled account gives rewireBackends no credential at all, as if nothing
// were configured.
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
		t.Fatalf("routedCredential = %q once disabled, want empty", got)
	}
	for _, id := range a.Registry.IDs() {
		if id == "alldebrid" {
			t.Fatal("alldebrid resolver still registered after its only account was disabled")
		}
	}
}

// Rows track configured accounts, not catalogue entries.
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

// Clearing a credential clears its metadata, so a slot configured again later
// does not come back disabled.
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

	// The file was written, not merely missing.
	if _, err := os.Stat(a.acctMetaPath()); err != nil {
		t.Fatalf("account_meta.json missing after SetAccountEnabled: %v", err)
	}
}
