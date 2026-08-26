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
