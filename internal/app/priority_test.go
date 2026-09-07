package app

// Host priority order, at the point it actually decides routing:
// resolverForTaskLocked / nextResolverLocked consulting jd.PriorityFor
// through dynamicPrio/rankedChain (app_dispatch.go), rather than trusting
// the registry's frozen Info().Prio alone. jd's own resolver_test.go already
// pins PriorityFor in isolation; this file pins that dispatch actually reads
// it - the wiring 6D's own doc comment on jd.PriorityFor named as missing.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// TestResolverForTaskPrefersDirectUntilJDIsPromoted is the DEFAULT half: with
// no native login active for a host, resolverForTaskLocked must still pick
// Direct (Prio 40) over JD (basePrio 10) exactly as the frozen registry order
// already would - the dynamic re-rank changes nothing for a host that never
// activated one.
func TestResolverForTaskPrefersDirectUntilJDIsPromoted(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const url = "https://priority-app-test-default.example/movie.mkv"

	got := a.resolverForTaskLocked(&core.Task{URL: url})
	if got == nil || got.Info().ID != "direct" {
		id := "<nil>"
		if got != nil {
			id = got.Info().ID
		}
		t.Fatalf("resolverForTaskLocked = %q, want %q (no native login has been activated for this host)", id, "direct")
	}
}

// TestResolverForTaskPromotesJDForAnActiveHostedLogin is the row this file
// exists for: once internal/hosterauth's reconciler (stood in for here by a
// direct jd.SetHostActive call, the same seam it uses) confirms a native
// login for this exact host, JD must be asked BEFORE Direct - a plain
// filename match must not keep sending a premium-backed link out anonymously.
func TestResolverForTaskPromotesJDForAnActiveHostedLogin(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-active.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })

	// Sanity check on the fixture itself: both resolvers really do match this
	// URL, or "JD wins" would be true for the wrong reason (Direct not
	// claiming it at all).
	if !(resolver.Direct{}).Match(url) {
		t.Fatal("test fixture broken: resolver.Direct does not match a .mkv URL")
	}
	if !(jd.Resolver{}).Match(url) {
		t.Fatal("test fixture broken: jd.Resolver does not match an ordinary http(s) URL")
	}

	jd.SetHostActive(host, true)
	got := a.resolverForTaskLocked(&core.Task{URL: url})
	if got == nil || got.Info().ID != "jd" {
		id := "<nil>"
		if got != nil {
			id = got.Info().ID
		}
		t.Fatalf("resolverForTaskLocked = %q, want %q once the host has a confirmed-active native login", id, "jd")
	}

	// And the promotion is per-host, not a global bump: an unrelated host
	// must still prefer Direct.
	other := "https://priority-app-test-unaffected.example/movie.mkv"
	got = a.resolverForTaskLocked(&core.Task{URL: other})
	if got == nil || got.Info().ID != "direct" {
		t.Errorf("an unrelated host's resolverForTaskLocked = %+v, want direct - activating one host must not promote JD everywhere", got)
	}
}

// TestNextResolverFallsBackThroughTheSameRankedOrder pins the fallback half:
// a task that started on a dynamically-promoted JD (u.Unsupported) must fall
// back to whatever actually came next in THAT order - Direct - not to
// whatever the frozen registry order would have said next.
func TestNextResolverFallsBackThroughTheSameRankedOrder(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-fallback.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	task := &core.Task{URL: url, Resolver: "jd"}
	if next := a.nextResolverLocked(task); next != "direct" {
		t.Errorf("nextResolverLocked after jd = %q, want %q (the next entry in the SAME promoted order jd was picked from)", next, "direct")
	}
}

// TestHandArrangedOrderOutranksTheAutomaticOne pins the half added on
// 2026-09-07: settings.ResolverOrder is not a hint the automatic ranking may
// overrule, it is the answer. The fixture picks the hardest case on purpose -
// a host with a CONFIRMED-ACTIVE native JD login, which is the one thing that
// outranks Direct automatically - and puts JD last by hand. If the hand order
// were merely folded in beside the automatic numbers, JD's activeLoginPrio
// would still win here and this would fail.
func TestHandArrangedOrderOutranksTheAutomaticOne(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-handorder.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	// Without an order, this host goes to JD - the state the next assertion
	// is a change FROM, checked rather than assumed.
	if got := a.resolverForTaskLocked(&core.Task{URL: url}); got == nil || got.Info().ID != "jd" {
		t.Fatalf("fixture broken: an active native login should route to jd, got %+v", got)
	}

	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"direct", "jd"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	got := a.resolverForTaskLocked(&core.Task{URL: url})
	if got == nil || got.Info().ID != "direct" {
		t.Fatalf("resolverForTaskLocked = %+v, want direct - a hand-arranged order has to beat even an active native login", got)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "direct"}); next != "jd" {
		t.Errorf("nextResolverLocked after direct = %q, want %q - the fallback walks the SAME hand-arranged order", next, "jd")
	}
}

// TestEmptyHandOrderRestoresTheAutomaticOne is the reset the "Automatisch"
// button sends: an empty order is not "put everything last", it is "there is
// no hand order", and the automatic ranking must come back untouched.
func TestEmptyHandOrderRestoresTheAutomaticOne(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-handreset.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"direct", "jd"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.resolverForTaskLocked(&core.Task{URL: url}); got == nil || got.Info().ID != "direct" {
		t.Fatalf("fixture broken: the hand order should route to direct first, got %+v", got)
	}

	cfg = a.Settings.Get()
	cfg.ResolverOrder = nil
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.resolverForTaskLocked(&core.Task{URL: url}); got == nil || got.Info().ID != "jd" {
		t.Fatalf("resolverForTaskLocked = %+v, want jd - clearing the order has to bring the automatic ranking back", got)
	}
}

// TestResolverPriorityReportsWhatDispatchWalks pins the card's own read path
// against the routing it claims to describe. The two used to be different
// functions answering different questions: /api/resolvers/priority read the
// registry's frozen order while dispatch read the re-ranked one, so the
// Prioritätsreihenfolge card could show a ladder the downloader did not use.
func TestResolverPriorityReportsWhatDispatchWalks(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})

	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"jd", "direct"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	got := a.ResolverPriority("")
	if len(got) < 2 {
		t.Fatalf("ResolverPriority returned %d entries, want at least the two registered here", len(got))
	}
	if got[0].ID != "jd" || got[1].ID != "direct" {
		t.Fatalf("ResolverPriority = %q, %q, want jd then direct - the card has to show the hand-arranged order, not the registry's own",
			got[0].ID, got[1].ID)
	}

	// And the same answer for a concrete host, narrowed to what matches it.
	perHost := a.ResolverPriority("priority-app-test-report.example")
	if len(perHost) == 0 || perHost[0].ID != "jd" {
		t.Fatalf("ResolverPriority(host) = %+v, want jd first for the same reason", perHost)
	}
}
