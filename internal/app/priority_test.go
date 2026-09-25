package app

// Host priority order where it decides routing: resolverForTaskLocked and
// nextResolverLocked consulting jd.PriorityFor through dynamicPrio and
// rankedChain in app_dispatch.go, rather than the registry's frozen
// Info().Prio. jd's resolver_test.go pins PriorityFor in isolation; this file
// pins that dispatch reads it, and that the hosts the direct download, the HTTP
// fallback and yt-dlp leave alone (app_claims.go) stay left alone whatever
// order the priority card saves.

import (
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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
	// URL before the login exists, or "JD wins" would be true for the wrong
	// reason (Direct not claiming it at all).
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
// order, a debrid service that carries the host, rather than to what the
// frozen registry order says. The direct download is not in that chain at all:
// a host with a login is a file hoster, and a plain GET there saves its page.
func TestNextResolverFallsBackThroughTheSameRankedOrder(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-fallback.example"
	const url = "https://" + host + "/movie.mkv"
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: host})
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	task := &core.Task{URL: url, Resolver: "jd"}
	if next := a.nextResolverLocked(task); next != "fakedebrid" {
		t.Errorf("nextResolverLocked after jd = %q, want %q (the next entry in the promoted order jd was picked from)", next, "fakedebrid")
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "fakedebrid"}); next != "" {
		t.Errorf("nextResolverLocked after fakedebrid = %q, want the chain to end there", next)
	}
}

// settings.ResolverOrder is the answer, not a hint the automatic ranking may
// overrule. The fixture takes the hardest case, a host with a confirmed-active
// native JD login, and puts JD below a debrid service by hand: folded in
// beside the automatic numbers, JD's ActiveLoginPrio would still win.
func TestHandArrangedOrderOutranksTheAutomaticOne(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-handorder.example"
	const url = "https://" + host + "/movie.mkv"
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: host})
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	// Without an order this host goes to JD, which is the state the assertion
	// below is a change from.
	if got := a.resolverForTaskLocked(&core.Task{URL: url}); got == nil || got.Info().ID != "jd" {
		t.Fatalf("fixture broken: an active native login should route to jd, got %+v", got)
	}

	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"fakedebrid", "jd"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	got := a.resolverForTaskLocked(&core.Task{URL: url})
	if got == nil || got.Info().ID != "fakedebrid" {
		t.Fatalf("resolverForTaskLocked = %+v, want fakedebrid; a hand-arranged order beats even an active native login", got)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "fakedebrid"}); next != "jd" {
		t.Errorf("nextResolverLocked after fakedebrid = %q, want %q; the fallback walks the hand-arranged order too", next, "jd")
	}
}

// The reset the "Automatisch" button sends: an empty order means there is no
// hand order, not that everything goes last, so the automatic ranking returns.
func TestEmptyHandOrderRestoresTheAutomaticOne(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "priority-app-test-handreset.example"
	const url = "https://" + host + "/movie.mkv"
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: host})
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	jd.SetHostActive(host, true)

	if _, err := a.SaveResolverOrder([]string{"fakedebrid", "jd"}); err != nil {
		t.Fatal(err)
	}
	if got := a.resolverForTaskLocked(&core.Task{URL: url}); got == nil || got.Info().ID != "fakedebrid" {
		t.Fatalf("fixture broken: the hand order should route to fakedebrid first, got %+v", got)
	}

	if _, err := a.SaveResolverOrder([]string{}); err != nil {
		t.Fatal(err)
	}
	if order := a.Settings.Get().ResolverOrder; order != nil {
		t.Errorf("stored order after the reset = %q, want none", order)
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
	// HTTP fallback included.
	cfg.ResolverOrder = []string{"jd", "direct"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	perHost := a.ResolverPriority("priority-app-test-report.example")
	if len(perHost) == 0 || perHost[0].ID != "jd" {
		t.Fatalf("ResolverPriority(host) = %+v, want jd first, as the stored order says", perHost)
	}
	if last := perHost[len(perHost)-1].ID; last != "http" {
		t.Errorf("ResolverPriority(host) ends with %q, want the HTTP fallback last", last)
	}
}

func cardIDs(rows []resolver.Info) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// JDownloader, yt-dlp and the direct download are rows of the card like the
// services above them. The HTTP fallback is not: it is what a link reaches
// after everything else, so a place on the card would mean nothing.
func TestPriorityCardListsJDYtdlpAndTheDirectDownload(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(ytdlp.Resolver{})

	ids := cardIDs(a.ResolverPriority(""))
	for _, want := range []string{"jd", "ytdlp", "direct"} {
		if !slices.Contains(ids, want) {
			t.Errorf("the priority card leaves out %q: %q", want, ids)
		}
	}
	if slices.Contains(ids, "http") {
		t.Errorf("the priority card lists the HTTP fallback: %q", ids)
	}
	// With nothing arranged by hand the card shows the order an ordinary link
	// walks, so the first drag keeps it: a file goes to the direct download
	// before yt-dlp or JDownloader.
	if d, y, j := slices.Index(ids, "direct"), slices.Index(ids, "ytdlp"), slices.Index(ids, "jd"); !(d < y && y < j) {
		t.Errorf("automatic card order = %q, want direct, then ytdlp, then jd", ids)
	}
}

// What the card sends after a drag is stored as sent, JD, yt-dlp and the
// direct download included, and the card reads it back in the same order.
func TestADraggedOrderWithJDYtdlpAndDirectRoundTrips(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(ytdlp.Resolver{})

	sent := []string{"jd", "direct", "torrent", "ytdlp"}
	rows, err := a.SaveResolverOrder(sent)
	if err != nil {
		t.Fatal(err)
	}
	if order := a.Settings.Get().ResolverOrder; !slices.Equal(order, sent) {
		t.Errorf("stored order = %q, want %q", order, sent)
	}
	startsWithSent := func(ids []string) bool { return len(ids) >= len(sent) && slices.Equal(ids[:len(sent)], sent) }
	if ids := cardIDs(rows); !startsWithSent(ids) {
		t.Errorf("the card reads back %q, want it to start with %q", ids, sent)
	}
	if ids := cardIDs(a.ResolverPriority("")); !startsWithSent(ids) {
		t.Errorf("a fresh read of the card = %q, want it to start with %q", ids, sent)
	}
}

// A header profile, the user's own servers and the HTTP fallback have no row,
// so a list that still names them is stored without them.
func TestPriorityCardLeavesOutHeaderProfilesOwnServersAndTheFallback(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(hostheaders.Resolver{})
	a.Registry.Register(remotefs.Resolver{})

	for _, r := range a.ResolverPriority("") {
		if r.ID == hostheaders.ResolverID || r.ID == remotefs.ResolverID {
			t.Errorf("the priority card lists %q", r.ID)
		}
	}
	if _, err := a.SaveResolverOrder([]string{hostheaders.ResolverID, "torrent", remotefs.ResolverID, "http"}); err != nil {
		t.Fatal(err)
	}
	if order := a.Settings.Get().ResolverOrder; !slices.Equal(order, []string{"torrent"}) {
		t.Errorf("stored order = %q, want only torrent", order)
	}
}

// The case the claim rule exists for: direct saved above JD, as a drag of the
// direct download to the top leaves it. A link to a hoster JD knows still goes
// to JD's free mode, since a plain GET would save the hoster's landing page,
// while an ordinary file keeps going to the direct download.
func TestDirectAboveJDLeavesFilehosterLinksToJD(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	jd.SetKnownHosts([]string{"filehoster.priority-app-test.example"})

	if _, err := a.SaveResolverOrder([]string{"direct", "jd", "torrent"}); err != nil {
		t.Fatal(err)
	}
	if order := a.Settings.Get().ResolverOrder; !slices.Equal(order, []string{"direct", "jd", "torrent"}) {
		t.Fatalf("stored order = %q, want direct above jd as sent", order)
	}

	hoster := &core.Task{URL: "https://filehoster.priority-app-test.example/file/0a1b2c/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(hoster)); got != "jd" {
		t.Errorf("a known hoster's link goes to %q, want jd", got)
	}
	// Nor is the direct download or the fallback where it goes once JD has
	// had it.
	hoster.Resolver = "jd"
	if next := a.nextResolverLocked(hoster); next == "direct" || next == "http" {
		t.Errorf("after jd a known hoster's link falls back to %q", next)
	}
	plain := &core.Task{URL: "https://files.priority-app-test-plain.example/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(plain)); got != "direct" {
		t.Errorf("an ordinary file goes to %q, want direct", got)
	}
}

// The same with the direct download at the very top and no JD list at all: the
// curated hosters and the hosts a debrid service lists are file hosters too, so
// neither goes out as a plain GET, and a task a fallback already recorded on
// the direct download is routed again rather than started there.
func TestDirectOnTopLeavesCuratedAndDebridListedHosters(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const listed = "listed.priority-app-test.example"
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 45, host: listed})
	a.claims.set(map[string]bool{listed: true}, nil)
	jd.SetKnownHosts(nil)

	if _, err := a.SaveResolverOrder([]string{"direct", "fakedebrid", "jd"}); err != nil {
		t.Fatal(err)
	}

	curated := &core.Task{URL: "https://rapidgator.net/file/0a1b2c/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(curated)); got != "jd" {
		t.Errorf("a curated hoster's link goes to %q, want jd", got)
	}
	debridListed := &core.Task{URL: "https://" + listed + "/file/abc/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(debridListed)); got != "fakedebrid" {
		t.Errorf("a debrid-listed hoster's link goes to %q, want fakedebrid", got)
	}
	recorded := &core.Task{URL: "https://rapidgator.net/file/0a1b2c/movie.mkv", Resolver: "direct"}
	if got := resolverIDOf(a.resolverForTaskLocked(recorded)); got != "jd" {
		t.Errorf("a hoster link recorded on direct goes to %q, want jd", got)
	}
}

// A hoster's alias domain is left alone like its main domain, whether the
// hoster is on the curated list or in JD's host list.
func TestDirectOnTopLeavesAHosterAliasDomain(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	if _, err := a.SaveResolverOrder([]string{"direct", "jd"}); err != nil {
		t.Fatal(err)
	}
	link := &core.Task{URL: "https://rg.to/file/0a1b2c/movie.mkv"}

	jd.SetKnownHosts(nil)
	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != "jd" {
		t.Errorf("with no JD list an rg.to link goes to %q, want jd", got)
	}
	jd.SetKnownHosts([]string{"rapidgator.net"})
	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != "jd" {
		t.Errorf("with rapidgator.net on JD's list an rg.to link goes to %q, want jd", got)
	}
}

// A login saved under an alias domain keeps its row's place for links to the
// hoster's main domain.
func TestALoginRowSavedUnderAnAliasRoutesTheMainDomain(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 50, host: "rapidgator.net"})
	t.Cleanup(func() { jd.SetHostActive("rg.to", false) })
	jd.SetHostActive("rg.to", true)

	if _, err := a.SaveResolverOrder([]string{"login:rg.to", "fakedebrid"}); err != nil {
		t.Fatal(err)
	}
	link := &core.Task{URL: "https://rapidgator.net/file/0a1b2c/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != "jd" {
		t.Errorf("login row above the debrid: link goes to %q, want jd", got)
	}
}

// While yt-dlp runs, a video site is its business wherever the direct download
// stands, and a file hoster is never yt-dlp's even when yt-dlp is placed above
// JD.
func TestVideoSitesStayWithYtdlpAndFilehostersWithJD(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(ytdlp.Resolver{Leave: a.claims.fileHoster})
	a.claims.set(nil, map[string]bool{"videosite.priority-app-test.example": true})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	jd.SetKnownHosts([]string{"filehoster.priority-app-test.example"})

	if _, err := a.SaveResolverOrder([]string{"direct", "ytdlp", "jd"}); err != nil {
		t.Fatal(err)
	}

	video := &core.Task{URL: "https://www.videosite.priority-app-test.example/clips/trailer.mp4"}
	if got := resolverIDOf(a.resolverForTaskLocked(video)); got != "ytdlp" {
		t.Errorf("a video site's link goes to %q, want ytdlp", got)
	}
	hoster := &core.Task{URL: "https://filehoster.priority-app-test.example/file/abc"}
	if got := resolverIDOf(a.resolverForTaskLocked(hoster)); got != "jd" {
		t.Errorf("a known hoster's link goes to %q, want jd", got)
	}
	plain := &core.Task{URL: "https://files.priority-app-test-plain.example/trailer.mp4"}
	if got := resolverIDOf(a.resolverForTaskLocked(plain)); got != "direct" {
		t.Errorf("an ordinary file goes to %q, want direct", got)
	}

	// Switched off, yt-dlp takes no link, and a video site's file is a file.
	switchModulesOff(t, a, "ytdlp")
	if got := resolverIDOf(a.resolverForTaskLocked(video)); got != "direct" {
		t.Errorf("with yt-dlp switched off a video site's file goes to %q, want direct", got)
	}
	switchModulesOff(t, a)

	// Without yt-dlp a video site's file is a file like any other.
	a.claims.set(nil, nil)
	if got := resolverIDOf(a.resolverForTaskLocked(video)); got != "direct" {
		t.Errorf("with yt-dlp gone a video site's file goes to %q, want direct", got)
	}
}

// A WebDAV login makes https links on that host the user's own server, which
// no row of the card may take from it, the direct download on top included.
func TestTheUsersOwnServerStaysAboveEveryRow(t *testing.T) {
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	const host = "dav.priority-app-test.example"
	a.Registry.Register(remotefs.Resolver{Accounts: remotefs.Logins{host: {Username: "u", Password: "p"}}})

	if _, err := a.SaveResolverOrder([]string{"direct", "jd"}); err != nil {
		t.Fatal(err)
	}
	link := &core.Task{URL: "https://" + host + "/share/movie.mkv"}
	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != remotefs.ResolverID {
		t.Errorf("a link on the user's WebDAV server goes to %q, want %s", got, remotefs.ResolverID)
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
// decides which of the two a link to that host goes to, and JD's own row, which
// ranks JD for every other host, does not overrule it.
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
		return resolverIDOf(a.resolverForTaskLocked(link))
	}
	if got := route("login:ddownload.com", "fakedebrid"); got != "jd" {
		t.Errorf("login above the debrid: link goes to %q, want jd", got)
	}
	if got := route("fakedebrid", "login:ddownload.com"); got != "fakedebrid" {
		t.Errorf("debrid above the login: link goes to %q, want fakedebrid", got)
	}
	if got := route("login:ddownload.com", "fakedebrid", "jd"); got != "jd" {
		t.Errorf("login above the debrid, JD's own row below it: link goes to %q, want jd", got)
	}
	if got := route("jd", "fakedebrid", "login:ddownload.com"); got != "fakedebrid" {
		t.Errorf("JD's own row on top, the login below the debrid: link goes to %q, want fakedebrid", got)
	}
}

// A login the saved order does not name yet goes out at JD's own row, and the
// card shows it right below JD, so saving the card as it stands keeps the
// login's links where they go.
func TestALoginRowTheOrderDoesNotNameSitsBelowJD(t *testing.T) {
	t.Setenv("KL_JD", "")
	a := newQueueApp(t)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(fakeResolver{id: "fakedebrid", prio: 50, host: "ddownload.com"})
	if _, err := a.SaveResolverOrder([]string{"jd", "fakedebrid", "direct"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHosterLogin("ddownload.com", "user", "secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jd.SetHostActive("ddownload.com", false) })
	jd.SetHostActive("ddownload.com", true)
	link := &core.Task{URL: "https://ddownload.com/abc123/movie.mkv"}

	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != "jd" {
		t.Fatalf("fixture broken: the link goes to %q, want jd", got)
	}
	card := cardIDs(a.ResolverPriority(""))
	if j, l, d := slices.Index(card, "jd"), slices.Index(card, "login:ddownload.com"), slices.Index(card, "fakedebrid"); j < 0 || l != j+1 || d < l {
		t.Errorf("card = %q, want the login right below jd and above fakedebrid", card)
	}
	if _, err := a.SaveResolverOrder(card); err != nil {
		t.Fatal(err)
	}
	if got := resolverIDOf(a.resolverForTaskLocked(link)); got != "jd" {
		t.Errorf("after saving the card as shown the link goes to %q, want jd", got)
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
