package resolver

// AllInfo and PriorityFor exist to make one thing checkable that used to live
// only in the source: which configured service actually gets asked first when
// more than one of them can serve the same host.

import (
	"context"
	"net/url"
	"reflect"
	"testing"
)

// stubResolver is the smallest thing that satisfies Resolver, with a fixed
// host set so Match is honest about what it claims - the same shape every
// real resolver's Match takes, so a bug in how PriorityFor builds its
// synthetic URL would show up here exactly as it would against a real one.
type stubResolver struct {
	id    string
	prio  int
	hosts map[string]bool
}

func (s stubResolver) Info() Info { return Info{ID: s.id, Prio: s.prio} }
func (s stubResolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && s.hosts[u.Hostname()]
}
func (stubResolver) Resolve(context.Context, Request) (Result, error) { return Result{}, nil }

// TestPriorityForOrdersByPrioHighestFirst pins the ordering itself: two
// services that both claim a host come back highest-Prio first, and a service
// that does not claim the host is not in the answer at all.
func TestPriorityForOrdersByPrioHighestFirst(t *testing.T) {
	reg := NewRegistry()
	// Registered out of priority order on purpose - the table is what orders
	// them, not the order they were added in.
	reg.Register(stubResolver{id: "low", prio: 10, hosts: map[string]bool{"shared.example": true}})
	reg.Register(stubResolver{id: "high", prio: 90, hosts: map[string]bool{"shared.example": true}})
	reg.Register(stubResolver{id: "mid", prio: 50, hosts: map[string]bool{"shared.example": true}})
	reg.Register(stubResolver{id: "elsewhere", prio: 99, hosts: map[string]bool{"other.example": true}})

	got := reg.PriorityFor("shared.example")
	want := []Info{{ID: "high", Prio: 90}, {ID: "mid", Prio: 50}, {ID: "low", Prio: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PriorityFor = %+v, want %+v", got, want)
	}
	for _, info := range got {
		if info.ID == "elsewhere" {
			t.Errorf("PriorityFor(shared.example) includes %q, which never matched that host", info.ID)
		}
	}
}

// TestPriorityForIsStableAcrossEqualPriority is the case the row exists for:
// "two debrid accounts" configured for the same host, at the same priority,
// must still come back in the same order every time - not whichever the
// registry's internal storage happens to iterate first.
func TestPriorityForIsStableAcrossEqualPriority(t *testing.T) {
	reg := NewRegistry()
	reg.Register(stubResolver{id: "first-added", prio: 50, hosts: map[string]bool{"host.example": true}})
	reg.Register(stubResolver{id: "second-added", prio: 50, hosts: map[string]bool{"host.example": true}})

	// Run it several times: a bug that reads registration order off map
	// iteration would show it eventually, not necessarily on the first call.
	for i := 0; i < 20; i++ {
		got := reg.PriorityFor("host.example")
		want := []Info{{ID: "first-added", Prio: 50}, {ID: "second-added", Prio: 50}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: PriorityFor = %+v, want %+v (registration order must survive an equal-priority tie)", i, got, want)
		}
	}
}

// TestAllInfoIsHostIndependent pins the other half: AllInfo reports every
// registered resolver's identity regardless of what it matches, which is what
// makes it the whole-registry view rather than PriorityFor with an implicit
// host.
func TestAllInfoIsHostIndependent(t *testing.T) {
	reg := NewRegistry()
	reg.Register(stubResolver{id: "a", prio: 5, hosts: map[string]bool{"a.example": true}})
	reg.Register(stubResolver{id: "b", prio: 15, hosts: map[string]bool{"b.example": true}})

	got := reg.AllInfo()
	want := []Info{{ID: "b", Prio: 15}, {ID: "a", Prio: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllInfo = %+v, want %+v", got, want)
	}
}
