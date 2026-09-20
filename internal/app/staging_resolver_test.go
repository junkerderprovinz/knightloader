package app

// The backend chosen while a link is collected has to be the one dispatch would
// choose. Registry.For walks the list the registry sorted at Register time by
// the static Info().Prio, while dispatch asks rankedChain, which re-ranks per
// URL through jd.PriorityFor and lifts a host JD can reach from 10 to 41, past
// Direct's 40.
//
// The collected answer sticks: resolverForTaskLocked returns t.Resolver whenever
// the recorded backend is routable, and "direct" always is, since it has no
// account to be locked out of. A hoster link whose path ends in a filename is
// then sent as a plain anonymous GET and its row shows no mode, because
// modeForLocked answers ModeUnknown for "direct".

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// The join of the two halves: jd's resolver_test pins PriorityFor at 41 for a
// host it knows and priority_test pins that dispatch reads that number, while
// this asks whether a link arrives with the answer those two agree on.
func TestCollectingPicksTheBackendDispatchWouldPick(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})

	const host = "staging-resolver-test.example"
	const url = "https://" + host + "/pack/File.rar"
	jd.SetKnownHosts([]string{host})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })

	// Both resolvers have to bid, or "JD wins" would be true because nothing
	// else claimed the link. Direct only claims a path that ends like a
	// filename, so a URL without one would pass either way.
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
			"collecting chose %q, dispatch would choose %q; a staged link keeps the collected answer "+
				"(resolverForTaskLocked returns t.Resolver whenever it is routable, and %q always is), "+
				"so the ranked chain never corrects it",
			collecting.Info().ID, dispatching.Info().ID, collecting.Info().ID,
		)
	}
	if collecting.Info().ID != "jd" {
		t.Fatalf("both agreed on %q, want %q: JD knows this host, so PriorityFor lifts it to 41 over Direct's 40",
			collecting.Info().ID, "jd")
	}
}

// The other half, which keeps the rule narrow: a host JD has never heard of
// earns no boost, so a plain file URL is still an ordinary GET. routing_test.go
// pins the same for the dispatch side.
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
