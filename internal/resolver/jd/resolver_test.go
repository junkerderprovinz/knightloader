package jd

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestPriorityForDefaultsWithNoNativeLogin(t *testing.T) {
	if got := PriorityFor("https://never-activated.example/file/123"); got != basePrio {
		t.Errorf("PriorityFor = %d, want the default %d for a host with no native login", got, basePrio)
	}
}

func TestPriorityForRisesOnceHostIsActive(t *testing.T) {
	const host = "priority-test-rapidgator.example"
	t.Cleanup(func() { SetHostActive(host, false) })

	SetHostActive(host, true)
	if got := PriorityFor("https://" + host + "/file/123"); got != ActiveLoginPrio {
		t.Errorf("PriorityFor = %d, want %d (above resolver.Direct's 40) once the host is active", got, ActiveLoginPrio)
	}
	if ActiveLoginPrio <= 40 {
		t.Errorf("ActiveLoginPrio = %d must exceed resolver.Direct's Prio (40) or the nudge does nothing", ActiveLoginPrio)
	}

	// A browser paste and JD's account list spell the same host differently.
	if got := PriorityFor("HTTPS://WWW." + host + "/x.zip"); got != ActiveLoginPrio {
		t.Errorf("PriorityFor = %d, want %d for a www./case variant of the same host", got, ActiveLoginPrio)
	}

	SetHostActive(host, false)
	if got := PriorityFor("https://" + host + "/file/123"); got != basePrio {
		t.Errorf("PriorityFor = %d, want the default %d once the host is deactivated again", got, basePrio)
	}
}

func TestPriorityForUnrelatedHostUnaffected(t *testing.T) {
	const activeHost = "priority-test-active.example"
	const otherHost = "priority-test-other.example"
	t.Cleanup(func() { SetHostActive(activeHost, false) })

	SetHostActive(activeHost, true)
	if got := PriorityFor("https://" + otherHost + "/file/123"); got != basePrio {
		t.Errorf("PriorityFor(%s) = %d, want the default %d - activating %s must not raise it", otherHost, got, basePrio, activeHost)
	}
}

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

// A hoster link without a login goes to JD's free mode rather than to a plain
// GET that would save the hoster's landing page.
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
	if got := PriorityFor("https://not-a-hoster.example/file"); got != basePrio {
		t.Errorf("PriorityFor(unknown host) = %d, want the unchanged default %d", got, basePrio)
	}
}

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

// JD has a plugin for YouTube too, but only yt-dlp offers formats and quality.
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
	// A confirmed login still wins on a media site.
	SetHostActive("youtube.com", true)
	t.Cleanup(func() { SetHostActive("youtube.com", false) })
	if got := PriorityFor("https://youtube.com/watch?v=x"); got != ActiveLoginPrio {
		t.Errorf("PriorityFor(media site with a login) = %d, want %d", got, ActiveLoginPrio)
	}
}

// FileHoster answers for exactly the hosts PriorityFor lifts above a plain GET,
// so the app can keep that GET off them whatever the order says.
func TestFileHosterMatchesTheHostsJDIsLiftedFor(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil); SetFileHosts(nil); SetHostActive("login-only.example", false) })
	SetKnownHosts([]string{"rapidgator.net", "youtube.com"})
	SetFileHosts(map[string]bool{"rapidgator.net": true})
	SetHostActive("login-only.example", true)

	for host, want := range map[string]bool{
		"rapidgator.net":     true,
		"WWW.Rapidgator.net": true,
		"login-only.example": true,
		"youtube.com":        false,
		"cdn.rapidgator.net": false,
		"files.example":      false,
	} {
		if got := FileHoster(host); got != want {
			t.Errorf("FileHoster(%q) = %v, want %v", host, got, want)
		}
		lifted := PriorityFor("https://"+host+"/f/1") > basePrio
		if lifted != want {
			t.Errorf("PriorityFor lifts %q: %v, FileHoster says %v; the two must agree", host, lifted, want)
		}
	}
}

// Without a debrid account or TorBox key nothing classifies hosts, and JD is
// the only way to fetch from a hoster.
func TestNoClassificationKeepsTheKnownHostBoost(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil); SetFileHosts(nil) })
	SetKnownHosts([]string{"rapidgator.net"})
	SetFileHosts(nil)

	if got := PriorityFor("https://rapidgator.net/file/abc"); got != knownHostPrio {
		t.Errorf("PriorityFor = %d, want %d with no classification available", got, knownHostPrio)
	}
}

// A premium account at the hoster beats a multihoster unlock, which beats JD's
// free mode, which beats a plain GET. The numbers live in three packages.
func TestTheLadderIsOrderedAsIntended(t *testing.T) {
	const directPrio = 40 // resolver.Direct's own Info().Prio
	const lowestDebrid = 44
	if !(ActiveLoginPrio > lowestDebrid) {
		t.Errorf("a confirmed login (%d) must outrank every debrid service (lowest %d)", ActiveLoginPrio, lowestDebrid)
	}
	if !(lowestDebrid > knownHostPrio) {
		t.Errorf("every debrid service (lowest %d) must outrank JD's free mode (%d)", lowestDebrid, knownHostPrio)
	}
	if !(knownHostPrio > directPrio) {
		t.Errorf("JD's free mode (%d) must outrank a blind GET (%d)", knownHostPrio, directPrio)
	}
}
