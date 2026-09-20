package app

// Host priority order where it decides routing: resolverForTaskLocked and
// nextResolverLocked consulting jd.PriorityFor through dynamicPrio and
// rankedChain in app_dispatch.go, rather than the registry's frozen
// Info().Prio. jd's resolver_test.go pins PriorityFor in isolation; this file
// pins that dispatch reads it.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// With no native login active for a host, the re-rank changes nothing:
// resolverForTaskLocked picks Direct (Prio 40) over JD (basePrio 10) as the
// frozen registry order would.
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

// Once a native login is confirmed for this host (through jd.SetHostActive, the
// seam internal/hosterauth's reconciler uses), JD is asked before Direct, so a
// filename match does not send a premium-backed link out anonymously.
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
		t.Errorf("an unrelated host's resolverForTaskLocked = %+v, want direct; activating one host must not promote JD everywhere", got)
	}
}

// A task that started on a promoted JD falls back to what came next in that
// order, Direct, rather than to what the frozen registry order says.
func TestNextResolverFallsBackThroughTheSameRankedOrder(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-fallback.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	task := &core.Task{URL: url, Resolver: "jd"}
	if next := a.nextResolverLocked(task); next != "direct" {
		t.Errorf("nextResolverLocked after jd = %q, want %q (the next entry in the promoted order jd was picked from)", next, "direct")
	}
}

// settings.ResolverOrder is the answer, not a hint the automatic ranking may
// overrule. The fixture takes the hardest case, a host with a confirmed-active
// native JD login, and puts JD last by hand: folded in beside the automatic
// numbers, JD's activeLoginPrio would still win.
func TestHandArrangedOrderOutranksTheAutomaticOne(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-handorder.example"
	const url = "https://" + host + "/movie.mkv"
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	// Without an order this host goes to JD, which is the state the assertion
	// below is a change from.
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
		t.Fatalf("resolverForTaskLocked = %+v, want direct; a hand-arranged order beats even an active native login", got)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "direct"}); next != "jd" {
		t.Errorf("nextResolverLocked after direct = %q, want %q; the fallback walks the hand-arranged order too", next, "jd")
	}
}

// The reset the "Automatisch" button sends: an empty order means there is no
// hand order, not that everything goes last, so the automatic ranking returns.
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
		t.Fatalf("resolverForTaskLocked = %+v, want jd; clearing the order brings the automatic ranking back", got)
	}
}

// The Prioritätsreihenfolge card reads /api/resolvers/priority, so that route
// has to answer from the same re-ranked order dispatch walks rather than from
// the registry's frozen one.
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
		t.Fatalf("ResolverPriority = %q, %q, want jd then direct; the card shows the hand-arranged order, not the registry's",
			got[0].ID, got[1].ID)
	}

	// And the same answer for a concrete host, narrowed to what matches it.
	perHost := a.ResolverPriority("priority-app-test-report.example")
	if len(perHost) == 0 || perHost[0].ID != "jd" {
		t.Fatalf("ResolverPriority(host) = %+v, want jd first for the same reason", perHost)
	}
}
