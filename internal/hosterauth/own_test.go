package hosterauth

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
)

// ownDdownload stands in for KnightLoader's own ddownload client, which keeps
// what its check found for a username until the stored login changes.
type ownDdownload struct {
	store *Store
	found map[string]OwnState
}

func newOwnDdownload(store *Store) *ownDdownload {
	return &ownDdownload{store: store, found: map[string]OwnState{}}
}

func (d *ownDdownload) own(host string) (OwnState, bool) {
	if hostalias.Canonical(host) != "ddownload.com" {
		return OwnState{}, false
	}
	cred, _ := d.store.Get(host)
	if st, ok := d.found[cred.Username]; ok {
		return st, true
	}
	return OwnState{Waiting: true}, true
}

func TestALoginKnightLoaderUsesItselfNeverReachesJD(t *testing.T) {
	fake := &fakeJD{accounts: []jdAccount{{UUID: 7, Hostname: "ddownload.com", InfoMap: &jdAccountInfo{Valid: true}}}}
	r, store := newTestReconciler(t, fake)
	r.Own = newOwnDdownload(store).own
	for _, host := range []string{"ddl.to", "rapidgator.net"} {
		if err := store.Set(host, accounts.Credential{Username: "u", Password: "p"}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.added) != 1 || fake.added[0].Host != "rapidgator.net" {
		t.Errorf("JD was given %+v, want only the rapidgator.net login", fake.added)
	}
	if len(fake.removedIDs) != 1 || fake.removedIDs[0] != 7 {
		t.Errorf("JD removed %v, want the ddownload account it held before", fake.removedIDs)
	}
}

func TestALoginKnightLoaderUsesItselfShowsWhatItsOwnCheckFound(t *testing.T) {
	r, store := newTestReconciler(t, &fakeJD{})
	client := newOwnDdownload(store)
	r.Own = client.own
	if err := store.Set("ddl.to", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	if got := r.States(); len(got) != 1 || got[0].Status != StatusQueued || got[0].Code != codeWaiting {
		t.Fatalf("before a check = %+v, want queued for the next check", got)
	}
	client.found["u"] = OwnState{Checked: true, Premium: true, TrafficMax: 200e9}
	got := r.States()
	if len(got) != 1 || got[0].Status != StatusActive || got[0].Host != "ddl.to" || got[0].Username != "u" || !got[0].Enabled {
		t.Fatalf("after the check = %+v, want the active row under ddl.to", got)
	}
	if !r.TrafficSpent("ddownload.com") {
		t.Error("a quota with nothing left does not read as spent")
	}

	r.Enabled = func(string) bool { return false }
	if got := r.States(); got[0].Status != StatusOff || got[0].Code != codeOwnOff {
		t.Errorf("switched off = %+v, want off with code %q", got[0], codeOwnOff)
	}

	if err := r.RemoveLogin("ddl.to"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("ddl.to", accounts.Credential{Username: "v", Password: "q"}); err != nil {
		t.Fatal(err)
	}
	r.Enabled = nil
	if got := r.States(); got[0].Status != StatusQueued {
		t.Errorf("a login saved again after a removal reads %+v, want it waiting for its check", got[0])
	}
}
