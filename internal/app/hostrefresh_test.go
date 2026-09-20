package app

// The routing host-list cache as app_accounts.go uses it: the last good list
// survives a transient failure and a fresh rewireBackends, and
// refreshHostListsIfDue refreshes once per interval. The cache itself is
// tested in internal/resolver (hostcache_test.go).

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// flakyDebridService answers Hosts() with the next scripted result, like a
// service that times out and recovers across several rewireBackends calls.
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

// A transient Hosts() error keeps the previous set rather than nil, which
// debrid.HostInSet would read as "this service claims nothing".
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
		t.Errorf("after a failed fetch, fetchDebridHosts = %v, want the last good set unchanged", second)
	}
}

// Every rewireBackends builds a new cache and service, so the last good list
// has to survive through the on-disk seed (hostCacheFor's Load hook). Two
// separate services with the same id stand in for two rewires.
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

	// A fresh service for the same id, with no history of its own.
	second := &flakyDebridService{id: "restart-rd", script: []func() (map[string]bool, error){fail()}}
	got := a.fetchDebridHosts(second)
	if !got["persisted.example"] {
		t.Errorf("fetchDebridHosts after a fresh Service and an immediate failure = %v, want the persisted set from before", got)
	}
}

// After an attempt, successful or not, there is no new one until
// hostRefreshInterval has passed, not on every one-minute upkeep tick.
func TestRefreshHostListsIfDueRespectsTheInterval(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// New already rewired once and stamped the attempt.
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

// Once the interval has passed, the next call rewires again.
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

// Without KL_JD the sidecar is not configured, which is not the same as
// unreachable.
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

// JDStatus reports the revision a fake sidecar sends on /jd/version.
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

// A KL_JD that does not answer is configured but not reachable.
func TestJDStatusConfiguredButUnreachable(t *testing.T) {
	// A closed listener refuses at once, so no timeout is waited out.
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
