package app

// The backend chosen while COLLECTING a link has to be the backend dispatch
// would choose. Two places answered that question and they disagreed.
//
// stage() asked Registry.For, which walks the list the registry sorted ONCE at
// Register time, by the static Info().Prio. Dispatch asks rankedChain, which
// re-ranks per URL through jd.PriorityFor - and that is where a host JD knows
// how to reach earns its boost from 10 to 41, past Direct's 40.
//
// The gap is not academic, because the collected answer STICKS:
// resolverForTaskLocked returns t.Resolver unchanged whenever the recorded
// backend is routable at all, and "direct" always is - it has no account to be
// locked out of. So a link that came in through the collector kept the backend
// the frozen order gave it, and never reached the ranked chain that was built
// to correct exactly this.
//
// What it looks like from outside: a hoster link whose path ends in a filename
// (nitroflare.com/view/ABC/File.rar) is claimed by Direct as well as JD, goes
// out as a plain anonymous GET, and its row shows no mode at all - because
// modeForLocked reads the resolver it is handed, and answers ModeUnknown for
// "direct". The "Free" badge that says "this is going through JD without a
// login" can therefore never appear on the links it was written for.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// TestCollectingPicksTheBackendDispatchWouldPick is the join of the two halves.
//
// Each half already had a test and each half passed: jd's own resolver_test
// pins PriorityFor at 41 for a host it knows, and priority_test pins that
// dispatch reads that number. Nobody asked whether the link ARRIVES with the
// answer those two agree on, and it did not.
func TestCollectingPicksTheBackendDispatchWouldPick(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})

	const host = "staging-resolver-test.example"
	const url = "https://" + host + "/pack/File.rar"
	jd.SetKnownHosts([]string{host})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })

	// The fixture has to have BOTH bidding, or "JD wins" would be true for the
	// boring reason that nothing else claimed the link. priority_test.go guards
	// its own fixture the same way, and this is the file where it matters most:
	// Direct only claims a path that ends like a filename, so a URL without one
	// would make this test pass no matter which side of the bug is in place.
	if !(resolver.Direct{}).Match(url) {
		t.Fatal("fixture broken: resolver.Direct does not claim a .rar path")
	}
	if !(jd.Resolver{}).Match(url) {
		t.Fatal("fixture broken: jd.Resolver does not claim an ordinary http(s) URL")
	}
	if !jd.HostKnown(host) {
		t.Fatal("fixture broken: SetKnownHosts did not take")
	}

	collecting := a.stagingResolverFor(url)
	if collecting == nil {
		t.Fatal("stagingResolverFor returned nil for a URL two resolvers claim")
	}

	a.mu.Lock()
	dispatching := a.resolverForTaskLocked(&core.Task{URL: url})
	a.mu.Unlock()
	if dispatching == nil {
		t.Fatal("resolverForTaskLocked returned nil for a URL two resolvers claim")
	}

	if collecting.Info().ID != dispatching.Info().ID {
		t.Fatalf(
			"collecting chose %q, dispatch would choose %q - a link staged now keeps the collected answer "+
				"(resolverForTaskLocked returns t.Resolver whenever it is routable, and %q always is), so the "+
				"ranked chain never gets to correct it",
			collecting.Info().ID, dispatching.Info().ID, collecting.Info().ID,
		)
	}
	if collecting.Info().ID != "jd" {
		t.Fatalf("both agreed on %q, want %q: JD knows this host, so PriorityFor lifts it to 41 over Direct's 40",
			collecting.Info().ID, "jd")
	}
}

// TestCollectingLeavesAnUnknownHostWithDirect is the other half, and it is the
// one that says the fix is not too wide. A host JD has never heard of earns no
// boost, so a plain file URL must still be an ordinary GET. routing_test.go
// pins the same expectation for the dispatch side; this pins it for the door
// links actually come in through.
func TestCollectingLeavesAnUnknownHostWithDirect(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts(nil)

	const url = "https://staging-resolver-unknown.example/movie.mp4"
	got := a.stagingResolverFor(url)
	if got == nil || got.Info().ID != "direct" {
		id := "<nil>"
		if got != nil {
			id = got.Info().ID
		}
		t.Fatalf("stagingResolverFor = %q, want %q for a host JD does not know", id, "direct")
	}
}
