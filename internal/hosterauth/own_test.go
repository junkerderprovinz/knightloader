package hosterauth

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
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

func TestALoginKnightLoadersClientGaveUpOnGoesToJDAndComesBackWhenTheClientTakesIt(t *testing.T) {
	fake := &fakeJD{}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("rg.to", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jdresolver.SetHostActive("rg.to", false) })
	own := OwnState{Checked: true, Handed: true, TrafficLeft: 0, TrafficMax: 10}
	r.Own = func(host string) (OwnState, bool) { return own, accountKey(host) == "rapidgator.net" }
	row := func() LoginState {
		t.Helper()
		for _, st := range r.States() {
			if st.Host == "rg.to" {
				return st
			}
		}
		t.Fatal("no row for the Rapidgator login")
		return LoginState{}
	}

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.added) != 1 || fake.added[0].Host != "rg.to" || fake.added[0].Password != "p" {
		t.Errorf("JD was given %+v, want the Rapidgator login", fake.added)
	}
	if got := row(); got.Status != StatusQueued || got.Code != codeAdding || !got.HandedOver || !got.Enabled {
		t.Errorf("row = %+v, want JD's word on the login with the handover marked", got)
	}

	fake.accounts[0].InfoMap = &jdAccountInfo{Username: "u", Valid: true, ValidUntil: 1830297600000, TrafficLeft: 7, TrafficMax: 10}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := row(); got.Status != StatusActive || got.Tier != "premium" || got.TrafficLeft != 7 || !got.HandedOver {
		t.Errorf("row = %+v, want the account as JD reads it", got)
	}
	if !jdresolver.HostActive("rapidgator.net") {
		t.Error("routing does not send the host's links to JD on the login")
	}
	if r.TrafficSpent("rg.to") {
		t.Error("the traffic KnightLoader's client last saw counted over JD's")
	}

	own = OwnState{Checked: true, Premium: true}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.removedIDs) != 1 || len(fake.accounts) != 0 {
		t.Errorf("JD removed %v and holds %+v, want the login taken off JD again", fake.removedIDs, fake.accounts)
	}
	if got := row(); got.HandedOver || got.Status != StatusActive || got.Tier != "premium" {
		t.Errorf("row = %+v, want KnightLoader's word on the login", got)
	}
	if jdresolver.HostActive("rapidgator.net") {
		t.Error("routing still sends the host's links to JD's login")
	}
}
