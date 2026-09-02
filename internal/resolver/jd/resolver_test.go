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
// on activeHostPrio for why.
func TestPriorityForRisesOnceHostIsActive(t *testing.T) {
	const host = "priority-test-rapidgator.example"
	t.Cleanup(func() { SetHostActive(host, false) })

	SetHostActive(host, true)
	if got := PriorityFor("https://" + host + "/file/123"); got != activeHostPrio {
		t.Errorf("PriorityFor = %d, want %d (above resolver.Direct's 40) once the host is active", got, activeHostPrio)
	}
	if activeHostPrio <= 40 {
		t.Errorf("activeHostPrio = %d must exceed resolver.Direct's Prio (40) or the nudge does nothing", activeHostPrio)
	}

	// www. and case must not matter - the same host arrives differently from a
	// browser paste and from JD's own account list.
	if got := PriorityFor("HTTPS://WWW." + host + "/x.zip"); got != activeHostPrio {
		t.Errorf("PriorityFor = %d, want %d for a www./case variant of the same host", got, activeHostPrio)
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
		if got := PriorityFor(raw); got != activeHostPrio {
			t.Errorf("PriorityFor(%q) = %d, want %d - a host JD has a plugin for must outrank a blind GET", raw, got, activeHostPrio)
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
