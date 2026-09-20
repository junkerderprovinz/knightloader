package app

// Account health: an unread account's tier is "unknown", never "free";
// unlimited traffic never gets a made-up limit; and the read path behind every
// AccountState row never reaches the live per-service call, only the cache the
// ticker fills.

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// An unread account must not be shown as "free"; "not checked" and "confirmed
// free" would be indistinguishable.
func TestAccountTierDefaultsUnknownNotFree(t *testing.T) {
	a := newAccountsTestApp(t)
	// Seeded through the store, so rewireBackends sees no routed credential
	// and the health ticker has nothing to read.
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	rows := a.AccountStates()
	if len(rows) != 1 {
		t.Fatalf("AccountStates() = %+v, want exactly one row", rows)
	}
	if got := rows[0].Tier; got != "unknown" {
		t.Fatalf("Tier = %q for an account the health ticker has not read yet, want %q (never %q)", got, "unknown", "free")
	}
	if got := rows[0].Traffic; got != (TrafficState{}) {
		t.Fatalf("Traffic = %+v for an unread account, want the zero value", got)
	}

	// TestAccount reads the same cache through fillHealth and must report the
	// same default.
	if got := a.TestAccount("alldebrid", "").Tier; got != "unknown" {
		t.Fatalf("TestAccount(...).Tier = %q, want %q", got, "unknown")
	}
}

// Unlimited is checked before anything divides Used by Limit, at every layer
// up to the plain-text column.
func TestUnlimitedTrafficNeverFabricatesALimit(t *testing.T) {
	// What AllDebrid and Real-Debrid return for a premium tier: neither API
	// exposes a byte cap, so there are no figures beside Unlimited.
	h := healthFromDebrid(debrid.AccountInfo{Tier: "premium", Traffic: debrid.TrafficInfo{Unlimited: true}})
	if !h.Traffic.Unlimited {
		t.Fatal("Unlimited did not survive the fold from debrid.AccountInfo into AccountHealth")
	}
	if h.Traffic.Limit != 0 || h.Traffic.Used != 0 {
		t.Fatalf("Traffic = %+v for an unlimited account, want Used and Limit at zero", h.Traffic)
	}

	// The plain-text column (fmtTrafficLeft) must read as unlimited, never
	// as "0 B", which would look like no traffic left.
	if got := fmtTrafficLeft(h.Traffic); got != "∞" {
		t.Fatalf("fmtTrafficLeft(unlimited) = %q, want the unlimited symbol, not a byte figure or empty string", got)
	}

	// A finite reading still shows its figure.
	finite := TrafficState{Used: 300, Limit: 1000}
	if got := fmtTrafficLeft(finite); got != "700 B" {
		t.Fatalf("fmtTrafficLeft(finite) = %q, want %q", got, "700 B")
	}
	// Below 10 units one decimal, matching fmtBytes in web/src/lib/format.ts.
	if got := fmtTrafficLeft(TrafficState{Used: 0, Limit: 5*1024*1024 + 512*1024}); got != "5.5 MiB" {
		t.Fatalf("fmtTrafficLeft(under 10 MiB) = %q, want %q", got, "5.5 MiB")
	}
	// At or above 10 units the decimal drops, again matching fmtBytes.
	if got := fmtTrafficLeft(TrafficState{Used: 0, Limit: 200 * 1024 * 1024}); got != "200 MiB" {
		t.Fatalf("fmtTrafficLeft(200 MiB) = %q, want %q", got, "200 MiB")
	}

	// A row never fetched must not read as "0 B" either, which would look like
	// confirmed zero traffic.
	if got := fmtTrafficLeft(TrafficState{}); got != "" {
		t.Fatalf("fmtTrafficLeft(never fetched) = %q, want empty; Accounts.tsx renders that as a dash", got)
	}
}

// The live per-service call is replaced by one that blocks forever, and
// AccountStates, behind GET /api/accounts and the shell-bar strip, must still
// return at once.
func TestAccountHealthStripNeverBlocksOnLiveCall(t *testing.T) {
	prev := accountInfoFetcher
	block := make(chan struct{})
	t.Cleanup(func() {
		accountInfoFetcher = prev
		close(block) // lets anything that did reach the stub exit
	})
	reached := make(chan struct{}, 1)
	accountInfoFetcher = func(context.Context, string, accounts.Credential) (AccountHealth, bool, error) {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-block
		return AccountHealth{}, false, nil
	}

	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	done := make(chan []AccountState, 1)
	go func() { done <- a.AccountStates() }()

	select {
	case rows := <-done:
		if len(rows) != 1 || rows[0].Tier != "unknown" {
			t.Fatalf("AccountStates() = %+v, want one row reading %q; no ticker sweep has run in this test", rows, "unknown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccountStates() did not return within 2s; the strip's read path must only read the cache")
	}

	// And the stub was never called.
	select {
	case <-reached:
		t.Fatal("the live fetcher was invoked on the read path; AccountStates must only read the cache")
	default:
	}
}
