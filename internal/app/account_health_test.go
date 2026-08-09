package app

// Covers the three account-health contracts docs/build-plan.md 6B commits to:
//
//   - Tier defaults to "unknown" and never "free" for an account nothing has
//     read yet.
//   - Unlimited traffic is its own case - the cache never fabricates a Limit
//     for it, and the one place it ships as plain text never reads as "0 B".
//   - The read path every AccountState row goes through
//     (accountRow/TestAccount -> fillHealth -> accountHealth) never reaches
//     the live per-service call. Only the ticker does, and only on its own
//     schedule - see accountHealthLoop's doc comment for why that schedule
//     is what keeps every test in this package clear of the real debrid
//     APIs, the same discipline accounts_test.go's package comment states.

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// TestAccountTierDefaultsUnknownNotFree is the guarantee item 4 of
// docs/build-plan.md 6B exists for: an account the health ticker has not
// read yet must never be shown as "free" - there is no way to tell "not
// checked" from "confirmed free" once the two are conflated, and the
// complaint that follows is a paying user watching this column call their
// account "Free".
func TestAccountTierDefaultsUnknownNotFree(t *testing.T) {
	a := newAccountsTestApp(t)
	// Seeded directly through the store, the same pattern accounts_test.go's
	// package comment documents: this keeps rewireBackends from ever seeing a
	// routed credential, so nothing here depends on a third party, and (new
	// this file) it also means the account-health ticker starts with nothing
	// to read - exactly the state this test wants to observe.
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

	// TestAccount's own live hosts-check reads the same cache through the
	// same fillHealth call (see that function's doc comment: it must not
	// touch the ticker's cache, only report it) - it must not smuggle a
	// different default in.
	if got := a.TestAccount("alldebrid", "").Tier; got != "unknown" {
		t.Fatalf("TestAccount(...).Tier = %q, want %q", got, "unknown")
	}
}

// TestUnlimitedTrafficNeverFabricatesALimit is item 3: unlimited is checked
// before anything divides Used by Limit, at every layer between a debrid
// client's answer and the one place this ships as plain text.
func TestUnlimitedTrafficNeverFabricatesALimit(t *testing.T) {
	// The true shape both AllDebrid.Account and RealDebrid.Account return for
	// a premium tier - see their doc comments: neither service's API exposes
	// an overall byte cap for one, so Traffic carries no figures at all
	// alongside Unlimited.
	h := healthFromDebrid(debrid.AccountInfo{Tier: "premium", Traffic: debrid.TrafficInfo{Unlimited: true}})
	if !h.Traffic.Unlimited {
		t.Fatal("Unlimited did not survive the fold from debrid.AccountInfo into AccountHealth")
	}
	if h.Traffic.Limit != 0 || h.Traffic.Used != 0 {
		t.Fatalf("Traffic = %+v for an unlimited account, want Used and Limit at zero - "+
			"either nonzero is exactly the fabricated figure this type exists to rule out", h.Traffic)
	}

	// The one place this ships as plain, untranslated text (Accounts.tsx's
	// TrafficLeft column - see fmtTrafficLeft's doc comment) must read as "no
	// limit", never as "0 B": a progress bar fed a zero maximum reads as
	// "out of traffic", and an empty-looking byte figure in a plain-text
	// column is the same lie with no bar to blame.
	if got := fmtTrafficLeft(h.Traffic); got != "∞" {
		t.Fatalf("fmtTrafficLeft(unlimited) = %q, want the unlimited symbol, not a byte figure or empty string", got)
	}

	// The contrasting case: the unlimited guard must not swallow an ordinary,
	// finite reading too.
	finite := TrafficState{Used: 300, Limit: 1000}
	if got := fmtTrafficLeft(finite); got != "700 B" {
		t.Fatalf("fmtTrafficLeft(finite) = %q, want %q", got, "700 B")
	}
	// Below 10 units carries one decimal, matching web/src/lib/format.ts's
	// fmtBytes exactly - the two must agree, or the same figure reads
	// differently depending on which formatter happened to render it.
	if got := fmtTrafficLeft(TrafficState{Used: 0, Limit: 5*1024*1024 + 512*1024}); got != "5.5 MiB" {
		t.Fatalf("fmtTrafficLeft(under 10 MiB) = %q, want %q", got, "5.5 MiB")
	}
	// At or above 10 units the decimal drops, again matching fmtBytes.
	if got := fmtTrafficLeft(TrafficState{Used: 0, Limit: 200 * 1024 * 1024}); got != "200 MiB" {
		t.Fatalf("fmtTrafficLeft(200 MiB) = %q, want %q", got, "200 MiB")
	}

	// A row nothing has fetched yet - the cache-miss default - must not
	// render as "0 B" either: that would be indistinguishable from "confirmed
	// zero traffic left", the TrafficState-shaped version of the tier
	// default trap above.
	if got := fmtTrafficLeft(TrafficState{}); got != "" {
		t.Fatalf("fmtTrafficLeft(never fetched) = %q, want empty - Accounts.tsx renders that as a dash, not as a real answer", got)
	}
}

// TestAccountHealthStripNeverBlocksOnLiveCall is item 2, proved rather than
// assumed: it swaps the one seam a live per-service call goes through for a
// stub that blocks forever, then confirms the read path every AccountState
// row is built from - AccountStates, which GET /api/accounts and so the
// shell-bar strip both read - returns immediately regardless. If a future
// change ever made that path reach the live call inline (the exact mistake
// this whole cache exists to prevent), this test hangs instead of passing.
func TestAccountHealthStripNeverBlocksOnLiveCall(t *testing.T) {
	prev := accountInfoFetcher
	block := make(chan struct{})
	t.Cleanup(func() {
		accountInfoFetcher = prev
		close(block) // lets anything that DID reach the stub exit, rather than leak it
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
			t.Fatalf("AccountStates() = %+v, want one row reading %q - no ticker sweep has run in this test", rows, "unknown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccountStates() did not return within 2s - the strip's read path must never reach the live per-service call, only the cache")
	}

	// Confirms the assertion above is not vacuously true for some unrelated
	// reason: the stub really was live and reachable, and the read path
	// above simply never called it.
	select {
	case <-reached:
		t.Fatal("the live fetcher WAS invoked on the read path - AccountStates must only ever read the cache, never fetch")
	default:
	}
}
