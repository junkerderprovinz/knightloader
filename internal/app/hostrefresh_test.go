package app

// The routing host-list cache, at the app-integration level: fetchDebridHosts
// keeping the last good list on a transient failure (the fix this row is
// about), it surviving a fresh rewireBackends call the way a real process
// restart or a later account change would, and refreshHostListsIfDue's own
// once-per-interval gate. internal/resolver's own hostcache_test.go pins the
// pure mechanism; this file pins that app_accounts.go actually uses it the
// way it is documented to.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// flakyDebridService answers Hosts() with whatever script says next, in
// order - the shape a real timeout-then-recovery-then-timeout-again service
// takes across several rewireBackends calls.
type flakyDebridService struct {
	id     string
	script []func() (map[string]bool, error)
	calls  int
}

func (f *flakyDebridService) ID() string    { return f.id }
func (f *flakyDebridService) Label() string { return f.id }
func (f *flakyDebridService) Hosts(context.Context) (map[string]bool, error) {
	i := f.calls
	f.calls++
	if i >= len(f.script) {
		i = len(f.script) - 1
	}
	return f.script[i]()
}
func (f *flakyDebridService) Unlock(context.Context, string) (debrid.Direct, error) {
	return debrid.Direct{}, nil
}

func succeed(hosts ...string) func() (map[string]bool, error) {
	return func() (map[string]bool, error) {
		set := map[string]bool{}
		for _, h := range hosts {
			set[h] = true
		}
		return set, nil
	}
}

func fail() func() (map[string]bool, error) {
	return func() (map[string]bool, error) { return nil, errBoom }
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom: transient service error" }

// TestFetchDebridHostsKeepsLastGoodOnFailure is THE fix row 3 exists for, at
// the level rewireBackends actually calls: a transient Hosts() error must
// leave the previously fetched set in place, never nil - which HostInSet
// (internal/resolver/debrid) reads as "this service claims nothing at all".
//
// Construct the failure, assert the list is unchanged.
func TestFetchDebridHostsKeepsLastGoodOnFailure(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	svc := &flakyDebridService{id: "flaky-rd", script: []func() (map[string]bool, error){
		succeed("a.example", "b.example"),
		fail(),
	}}

	first := a.fetchDebridHosts(svc)
	if len(first) != 2 || !first["a.example"] || !first["b.example"] {
		t.Fatalf("first fetch = %v, want the two seeded hosts", first)
	}

	second := a.fetchDebridHosts(svc)
	if len(second) != 2 || !second["a.example"] || !second["b.example"] {
		t.Errorf("after a failed fetch, fetchDebridHosts = %v, want the last good set unchanged (this is the bug: a transient error must not empty it)", second)
	}
}

// TestFetchDebridHostsSurvivesAFreshCallTheWayARestartWould pins the half a
// bare in-memory cache would miss: a BRAND NEW *resolver.HostCache is built
// on every rewireBackends call (mirroring how debrid.NewAllDebrid/NewRealDebrid
// are freshly constructed every time too), so "keep the last good list" has
// to survive that reconstruction - which is what the on-disk seed
// (hostCacheFor's Load hook) is for. This drives fetchDebridHosts twice with
// two DIFFERENT flakyDebridService values sharing the same service id, the
// same way two different rewireBackends calls each build a fresh
// debrid.Service from the same stored credential.
func TestFetchDebridHostsSurvivesAFreshCallTheWayARestartWould(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	first := &flakyDebridService{id: "restart-rd", script: []func() (map[string]bool, error){succeed("persisted.example")}}
	if got := a.fetchDebridHosts(first); !got["persisted.example"] {
		t.Fatalf("seeding fetch = %v", got)
	}

	// A second, independent Service value for the same id - a fresh instance,
	// zero in-memory history of its own, exactly like a real rewireBackends
	// call after an account change or a process restart.
	second := &flakyDebridService{id: "restart-rd", script: []func() (map[string]bool, error){fail()}}
	got := a.fetchDebridHosts(second)
	if !got["persisted.example"] {
		t.Errorf("fetchDebridHosts after a fresh Service and an immediate failure = %v, want the persisted set from before", got)
	}
}

// TestRefreshHostListsIfDueRespectsTheInterval pins the "not once a minute"
// half of the timer: refreshHostListsIfDue must not re-run rewireBackends
// again immediately after one attempt, success or failure - see
// hostRefreshAttempted's own doc comment for why a sustained outage must
// back off to hostRefreshInterval rather than being retried on upkeep's own
// 1-minute tick.
func TestRefreshHostListsIfDueRespectsTheInterval(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// New() already called rewireBackends once, which stamped
	// hostRefreshAttempted[a] to just now - so the very next call must be a
	// no-op rather than an immediate second rewire.
	hostRefreshMu.Lock()
	before := hostRefreshAttempted[a]
	hostRefreshMu.Unlock()
	if before.IsZero() {
		t.Fatal("rewireBackends via New() did not stamp an attempt")
	}

	a.refreshHostListsIfDue()

	hostRefreshMu.Lock()
	after := hostRefreshAttempted[a]
	hostRefreshMu.Unlock()
	if !after.Equal(before) {
		t.Errorf("refreshHostListsIfDue re-attempted within the interval: stamp moved from %v to %v", before, after)
	}
}

// TestRefreshHostListsIfDueFiresOnceStale is the other half: once
// hostRefreshInterval has genuinely passed, the next call must actually
// re-run rewireBackends (observable as the attempt stamp moving forward).
func TestRefreshHostListsIfDueFiresOnceStale(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	hostRefreshMu.Lock()
	hostRefreshAttempted[a] = time.Now().Add(-hostRefreshInterval - time.Minute)
	hostRefreshMu.Unlock()

	a.refreshHostListsIfDue()

	hostRefreshMu.Lock()
	after := hostRefreshAttempted[a]
	hostRefreshMu.Unlock()
	if time.Since(after) > time.Minute {
		t.Errorf("refreshHostListsIfDue did not re-attempt once the interval had passed: stamp is still %v", after)
	}
}

// TestJDStatusUnconfigured pins the default: no KL_JD at all reads as
// "not configured", never as "unreachable" - those are different facts, and
// conflating them would tell a user their sidecar is down when they simply
// never set one up.
func TestJDStatusUnconfigured(t *testing.T) {
	t.Setenv("KL_JD", "")
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got := a.JDStatus()
	if got.Configured || got.Reachable || got.Version != 0 {
		t.Errorf("JDStatus() with no KL_JD = %+v, want the zero value", got)
	}
}

// TestJDStatusReachableReportsTheRevision drives JDStatus against a fake
// sidecar answering the real /help and /jd/version shapes, and pins that the
// revision it reports is the one the sidecar actually sent - the "surface
// the JD container's own version" row.
func TestJDStatusReachableReportsTheRevision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jd/version" {
			_, _ = w.Write([]byte(`{"data":24471}`))
			return
		}
		w.WriteHeader(http.StatusOK) // /help
	}))
	defer srv.Close()
	t.Setenv("KL_JD", srv.URL)

	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got := a.JDStatus()
	if !got.Configured || !got.Reachable || got.Version != 24471 {
		t.Errorf("JDStatus() = %+v, want configured+reachable and revision 24471", got)
	}
}

// TestJDStatusConfiguredButUnreachable pins the third state: a KL_JD that
// does not answer is "configured" (the user did set one up) but not
// "reachable" - the distinction that tells a user whether to check their own
// setup or wait for the sidecar to come back.
func TestJDStatusConfiguredButUnreachable(t *testing.T) {
	// A closed listener: connection refused, not a slow/hanging one, so the
	// test does not have to wait out a real timeout to see the failure.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	t.Setenv("KL_JD", "http://"+addr)

	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got := a.JDStatus()
	if !got.Configured || got.Reachable || got.Detail == "" {
		t.Errorf("JDStatus() against an unreachable KL_JD = %+v, want configured, not reachable, and a detail explaining why", got)
	}
}
