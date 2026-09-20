package resolver

import (
	"context"
	"net/url"
	"reflect"
	"testing"
)

// stubResolver matches by hostname like the real resolvers do, so a bug in
// the synthetic URL PriorityFor builds shows up here.
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

func TestPriorityForOrdersByPrioHighestFirst(t *testing.T) {
	reg := NewRegistry()
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

// Two accounts at the same priority must come back in registration order
// every time.
func TestPriorityForIsStableAcrossEqualPriority(t *testing.T) {
	reg := NewRegistry()
	reg.Register(stubResolver{id: "first-added", prio: 50, hosts: map[string]bool{"host.example": true}})
	reg.Register(stubResolver{id: "second-added", prio: 50, hosts: map[string]bool{"host.example": true}})

	// Repeated so that an order taken from map iteration would show.
	for i := 0; i < 20; i++ {
		got := reg.PriorityFor("host.example")
		want := []Info{{ID: "first-added", Prio: 50}, {ID: "second-added", Prio: 50}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: PriorityFor = %+v, want %+v (registration order must survive an equal-priority tie)", i, got, want)
		}
	}
}

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
