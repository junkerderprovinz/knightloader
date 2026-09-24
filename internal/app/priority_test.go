package app

// Host priority order where it decides routing: resolverForTaskLocked and
// nextResolverLocked consulting jd.PriorityFor through dynamicPrio and
// rankedChain in app_dispatch.go, rather than the registry's frozen
// Info().Prio. jd's resolver_test.go pins PriorityFor in isolation; this file
// pins that dispatch reads it.

import (
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
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
// numbers, JD's ActiveLoginPrio would still win.
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
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: "priority-app-test-report.example"})

	// Automatically torrent (50) comes before the debrid (45); the hand order
	// turns that round.
	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"fakedebrid", "torrent"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	got := a.ResolverPriority("")
	if len(got) < 2 {
		t.Fatalf("ResolverPriority returned %d entries, want at least fakedebrid and torrent", len(got))
	}
	if got[0].ID != "fakedebrid" || got[1].ID != "torrent" {
		t.Fatalf("ResolverPriority = %q, %q, want fakedebrid then torrent; the card shows the hand-arranged order, not the registry's",
			got[0].ID, got[1].ID)
	}

	// For a concrete host the answer is the whole chain that host walks, the
	// per-link resolvers included.
	cfg.ResolverOrder = []string{"jd", "direct"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	perHost := a.ResolverPriority("priority-app-test-report.example")
	if len(perHost) == 0 || perHost[0].ID != "jd" {
		t.Fatalf("ResolverPriority(host) = %+v, want jd first, as the stored order says", perHost)
	}
}

// JD, yt-dlp, the plain GET and the HTTP fallback all claim nearly any link,
// and which of them fits depends on the host, so the card leaves them out
// rather than letting a drag freeze one order for every host.
func TestPriorityCardLeavesOutTheResolversThatDependOnTheLink(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})

	for _, r := range a.ResolverPriority("") {
		switch r.ID {
		case "direct", "jd", "ytdlp", "http":
			t.Errorf("the priority card lists %q, whose place depends on the link", r.ID)
		}
	}
}

// A header profile and the user's own servers take only the links they were
// set up for, so a place on the card would rank nothing, and a stale list that
// still names them is not stored.
func TestPriorityCardLeavesOutHeaderProfilesAndOwnServers(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(hostheaders.Resolver{})
	a.Registry.Register(remotefs.Resolver{})

	for _, r := range a.ResolverPriority("") {
		if r.ID == hostheaders.ResolverID || r.ID == remotefs.ResolverID {
			t.Errorf("the priority card lists %q", r.ID)
		}
	}
	if _, err := a.SaveResolverOrder([]string{hostheaders.ResolverID, "torrent", remotefs.ResolverID}); err != nil {
		t.Fatal(err)
	}
	if order := a.Settings.Get().ResolverOrder; !slices.Equal(order, []string{"torrent"}) {
		t.Errorf("stored order = %q, want only torrent", order)
	}
}

// The order below is what a card listing every resolver sends after any drag,
// direct above JD. Stored as sent, it would take every link to a known hoster
// down a plain GET that saves the hoster's landing page; saved through the
// card, JD's free mode keeps the hoster and direct keeps the ordinary file.
func TestASavedCardOrderLeavesHosterLinksToJD(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	jd.SetKnownHosts([]string{"rapidgator.net"})

	if _, err := a.SaveResolverOrder([]string{"torrent", "direct", "jd", "http"}); err != nil {
		t.Fatal(err)
	}

	hoster := &core.Task{URL: "https://rapidgator.net/file/0a1b2c/movie.mkv"}
	if got := a.resolverForTaskLocked(hoster); got == nil || got.Info().ID != "jd" {
		t.Errorf("a known hoster's link goes to %+v, want jd", got)
	}
	plain := &core.Task{URL: "https://files.priority-app-test-plain.example/movie.mkv"}
	if got := a.resolverForTaskLocked(plain); got == nil || got.Info().ID != "direct" {
		t.Errorf("an ordinary file goes to %+v, want direct", got)
	}
	if order := a.Settings.Get().ResolverOrder; slices.Contains(order, "direct") || slices.Contains(order, "jd") {
		t.Errorf("stored order = %q, want the per-link resolvers left out", order)
	}
}

// Each switched-on hoster login is a row of its own on the priority card.
func TestPriorityCardListsEachConnectedHosterLogin(t *testing.T) {
	a := newQueueApp(t)
	if err := a.SetHosterLogin("ddownload.com", "user", "secret"); err != nil {
		t.Fatal(err)
	}

	rows := a.ResolverPriority("")
	if !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == "login:ddownload.com" }) {
		t.Fatalf("ResolverPriority = %+v, want a row for the ddownload.com login", rows)
	}
}

// Where the login row sits against a debrid service that carries the same host
// decides which of the two a link to that host goes to.
func TestALoginRowDecidesBetweenTheOwnAccountAndADebrid(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 50, host: "ddownload.com"})
	t.Cleanup(func() { jd.SetHostActive("ddownload.com", false) })
	jd.SetHostActive("ddownload.com", true)
	link := &core.Task{URL: "https://ddownload.com/abc123/movie.mkv"}

	route := func(order ...string) string {
		t.Helper()
		if _, err := a.SaveResolverOrder(order); err != nil {
			t.Fatal(err)
		}
		got := a.resolverForTaskLocked(link)
		if got == nil {
			return "<nil>"
		}
		return got.Info().ID
	}
	if got := route("login:ddownload.com", "fakedebrid"); got != "jd" {
		t.Errorf("login above the debrid: link goes to %q, want jd", got)
	}
	if got := route("fakedebrid", "login:ddownload.com"); got != "fakedebrid" {
		t.Errorf("debrid above the login: link goes to %q, want fakedebrid", got)
	}
}

// profileFor is a header profile store covering one origin.
type profileFor string

func (p profileFor) Covers(raw string) bool { return strings.HasPrefix(raw, string(p)) }
func (p profileFor) ForURL(string) (string, hostheaders.Set) {
	return "own", hostheaders.Set{Origin: string(p)}
}
func (profileFor) Get(string) hostheaders.Set { return hostheaders.Set{} }

// A header profile is the user's own setup for one origin, usually a premium
// cookie, so it goes ahead of a debrid service that carries the same host,
// even one placed first by hand.
func TestAHeaderProfileGoesFirstForItsOrigin(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: "katfile.com"})
	a.Registry.Register(hostheaders.Resolver{Profiles: profileFor("https://katfile.com")})
	link := &core.Task{URL: "https://katfile.com/abc/movie.mkv"}

	if got := a.resolverForTaskLocked(link); got == nil || got.Info().ID != hostheaders.ResolverID {
		t.Fatalf("resolver = %+v, want the header profile", got)
	}
	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"fakedebrid", hostheaders.ResolverID}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.resolverForTaskLocked(link); got == nil || got.Info().ID != hostheaders.ResolverID {
		t.Errorf("with the debrid ordered first: resolver = %+v, want the header profile", got)
	}
}
