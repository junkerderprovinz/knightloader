package jd

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestPriorityForDefaultsWithNoNativeLogin pins the DEFAULT answer: a host
// nothing has activated must route exactly as it always has, at the same
// value Info().Prio already gives (basePrio) - a per-host nudge that changed
// the answer for every host it had never heard of would not be a nudge.
func TestPriorityForDefaultsWithNoNativeLogin(t *testing.T) {
	if got := PriorityFor("https://never-activated.example/file/123"); got != basePrio {
		t.Errorf("PriorityFor = %d, want the default %d for a host with no native login", got, basePrio)
	}
}

// TestPriorityForRisesOnceHostIsActive pins the other half: once
// internal/hosterauth's reconciler calls SetHostActive for a host, that
// host's links must outrank resolver.Direct's fixed 40 - see the doc comment
// on activeLoginPrio for why. Since 2026-09-07 it must outrank the debrid
// services too: a premium account at the hoster itself is what a multihoster
// unlock approximates, so it wins over one.
func TestPriorityForRisesOnceHostIsActive(t *testing.T) {
	const host = "priority-test-rapidgator.example"
	t.Cleanup(func() { SetHostActive(host, false) })

	SetHostActive(host, true)
	if got := PriorityFor("https://" + host + "/file/123"); got != activeLoginPrio {
		t.Errorf("PriorityFor = %d, want %d (above resolver.Direct's 40) once the host is active", got, activeLoginPrio)
	}
	if activeLoginPrio <= 40 {
		t.Errorf("activeLoginPrio = %d must exceed resolver.Direct's Prio (40) or the nudge does nothing", activeLoginPrio)
	}

	// www. and case must not matter - the same host arrives differently from a
	// browser paste and from JD's own account list.
	if got := PriorityFor("HTTPS://WWW." + host + "/x.zip"); got != activeLoginPrio {
		t.Errorf("PriorityFor = %d, want %d for a www./case variant of the same host", got, activeLoginPrio)
	}

	SetHostActive(host, false)
	if got := PriorityFor("https://" + host + "/file/123"); got != basePrio {
		t.Errorf("PriorityFor = %d, want the default %d once the host is deactivated again", got, basePrio)
	}
}

// TestPriorityForUnrelatedHostUnaffected is the "not a global bump" guard: one
// active host must not raise the answer for a different one, or the nudge
// would quietly send every plain file link through JD the moment any single
// hoster login is confirmed active.
func TestPriorityForUnrelatedHostUnaffected(t *testing.T) {
	const activeHost = "priority-test-active.example"
	const otherHost = "priority-test-other.example"
	t.Cleanup(func() { SetHostActive(activeHost, false) })

	SetHostActive(activeHost, true)
	if got := PriorityFor("https://" + otherHost + "/file/123"); got != basePrio {
		t.Errorf("PriorityFor(%s) = %d, want the default %d - activating %s must not raise it", otherHost, got, basePrio, activeHost)
	}
}

// TestCheckWithNoBackendIsUncheckable pins the same nil-safety
// debrid.Resolver.Svc already established: a bare jd.Resolver{}, the shape
// every routing test in this package constructs, must answer every link
// uncheckable rather than panic on a nil Backend.
func TestCheckWithNoBackendIsUncheckable(t *testing.T) {
	r := Resolver{}
	got, err := r.Check(context.Background(), []string{"https://host.example/a", "https://host.example/b"})
	if err != nil {
		t.Fatalf("Check returned an error with no backend: %v", err)
	}
	if len(got) != 2 || got[0] != core.AvailUncheckable || got[1] != core.AvailUncheckable {
		t.Errorf("Check = %v, want two uncheckable verdicts", got)
	}
}

// TestCheckDelegatesToBackend pins the other half: once a Backend is wired
// in, Check hands the call straight through rather than adding its own
// interpretation on top.
func TestCheckDelegatesToBackend(t *testing.T) {
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = orig }()

	const url = "https://host.example/a"
	fake := newFakeJDCheck(t, map[string]string{url: "ONLINE"})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	r := Resolver{Backend: NewBackend(srv.URL, func(string, core.Update) {})}
	got, err := r.Check(context.Background(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != core.AvailOnline {
		t.Errorf("Check = %v, want [online]", got)
	}
}

// TestPriorityForRisesForAHostJDKnowsWithNoLogin is the "free mode, like
// JDownloader" requirement, and it is a genuine change of behaviour rather than
// a tidy-up (jdp, 2026-09-02: "Wenn man links runterladen möchte für die kein
// premium account hinterlegt ist muss das angezeigt werden un der link im free
// modus heruntergeladen werden. wie in JD").
//
// Before this, a hoster link with no login went to resolver.Direct, whose fetch
// is a plain HTTP GET that knows nothing about hosters: for most of them that
// saves the landing PAGE under the real file name and calls it a successful
// download. JD's own plugin for that host is the only thing in this app that can
// do the free-mode dance, so it has to outrank a blind GET.
func TestPriorityForRisesForAHostJDKnowsWithNoLogin(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil) })
	SetKnownHosts([]string{"Rapidgator.NET", "www.example-hoster.com"})

	for _, raw := range []string{
		"https://rapidgator.net/file/abc",
		"https://www.rapidgator.net/file/abc",
		"https://example-hoster.com/f/1",
	} {
		if got := PriorityFor(raw); got != knownHostPrio {
			t.Errorf("PriorityFor(%q) = %d, want %d - a host JD has a plugin for must outrank a blind GET", raw, got, knownHostPrio)
		}
	}
	// A host JD does not know is unchanged: nothing here may quietly promote
	// every link to the catch-all.
	if got := PriorityFor("https://not-a-hoster.example/file"); got != basePrio {
		t.Errorf("PriorityFor(unknown host) = %d, want the unchanged default %d", got, basePrio)
	}
}

// TestSetKnownHostsReplacesRatherThanAccumulates: a host JD stops supporting has
// to stop outranking Direct on the very next pass, not linger until a restart.
func TestSetKnownHostsReplacesRatherThanAccumulates(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil) })
	SetKnownHosts([]string{"one.example"})
	SetKnownHosts([]string{"two.example"})

	if HostKnown("one.example") {
		t.Error("a host dropped from JD's list is still known; SetKnownHosts accumulated instead of replacing")
	}
	if !HostKnown("two.example") {
		t.Error("the host JD still lists is not known")
	}
}

// TestPriorityForLeavesMediaSitesToYtdlp is the 2026-09-07 half of a problem
// that first arrived through TorBox on 2026-09-06 and came back through a
// different door: JD has a plugin for YouTube too, its plugin list carries 714
// entries on jdp's own instance, and the known-host boost would put a YouTube
// link in JD's hands and past yt-dlp - the one backend that turns such a link
// into the five keepable rows with a quality to pick.
func TestPriorityForLeavesMediaSitesToYtdlp(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil); SetFileHosts(nil) })
	SetKnownHosts([]string{"rapidgator.net", "youtube.com"})
	SetFileHosts(map[string]bool{"rapidgator.net": true})

	if got := PriorityFor("https://rapidgator.net/file/abc"); got != knownHostPrio {
		t.Errorf("PriorityFor(file hoster) = %d, want %d - JD is still what fetches from a hoster", got, knownHostPrio)
	}
	if got := PriorityFor("https://youtube.com/watch?v=x"); got != basePrio {
		t.Errorf("PriorityFor(media site) = %d, want the unboosted %d so yt-dlp gets it", got, basePrio)
	}
	// A confirmed login still wins, media site or not: if somebody really has a
	// premium account at that host, using it is not this rule's business.
	SetHostActive("youtube.com", true)
	t.Cleanup(func() { SetHostActive("youtube.com", false) })
	if got := PriorityFor("https://youtube.com/watch?v=x"); got != activeLoginPrio {
		t.Errorf("PriorityFor(media site with a login) = %d, want %d", got, activeLoginPrio)
	}
}

// TestNoClassificationKeepsTheOldBoost is the degenerate case this must not
// break: an install with no debrid account and no TorBox key has nothing that
// could classify a host, and JD is then the only thing that can fetch from a
// hoster at all. An empty set means "nobody has classified anything", never
// "everything is a media site".
func TestNoClassificationKeepsTheOldBoost(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil); SetFileHosts(nil) })
	SetKnownHosts([]string{"rapidgator.net"})
	SetFileHosts(nil)

	if got := PriorityFor("https://rapidgator.net/file/abc"); got != knownHostPrio {
		t.Errorf("PriorityFor = %d, want %d with no classification available", got, knownHostPrio)
	}
}

// TestTheLadderIsOrderedAsIntended states the whole ranking in one place, in
// the terms it was decided in: a premium account at the hoster beats a
// multihoster unlock, a multihoster beats JD's free mode, and JD's free mode
// beats a blind GET. The numbers themselves are in three packages, so this is
// the only place the ORDER between them is written down.
func TestTheLadderIsOrderedAsIntended(t *testing.T) {
	const directPrio = 40 // resolver.Direct's own Info().Prio
	const lowestDebrid = 44
	if !(activeLoginPrio > lowestDebrid) {
		t.Errorf("a confirmed login (%d) must outrank every debrid service (lowest %d)", activeLoginPrio, lowestDebrid)
	}
	if !(lowestDebrid > knownHostPrio) {
		t.Errorf("every debrid service (lowest %d) must outrank JD's free mode (%d)", lowestDebrid, knownHostPrio)
	}
	if !(knownHostPrio > directPrio) {
		t.Errorf("JD's free mode (%d) must outrank a blind GET (%d)", knownHostPrio, directPrio)
	}
}
