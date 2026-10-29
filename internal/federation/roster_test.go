package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

func TestAnInstanceTakesTheDevicesAnotherOneSaw(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	browserKey := memberKeyOf(204).Public().(ed25519.PublicKey)
	phoneKey := memberKeyOf(202).Public().(ed25519.PublicKey)
	// The phone called here once and waits for approval, which the other
	// instance gave while this one was away.
	m.group.state.Keys = map[string]PinnedKey{phone1: {Key: phoneKey, Kind: relay.KindMobile, Used: joined}}

	seen := joined.Add(time.Hour)
	now := seen.Add(time.Hour)
	changed, err := m.Merge(Roster{Apps: []RosterApp{
		{ID: browser1, Name: "Firefox", Deployment: relay.KindExtension, LastSeen: seen, Key: browserKey},
		{ID: phone1, Name: "Pixel 8", Deployment: relay.KindMobile, LastSeen: seen, Key: phoneKey},
	}}, now)
	if err != nil || !changed {
		t.Fatalf("merge = %v, %v; want a change", changed, err)
	}
	apps, err := m.Apps(now)
	if err != nil || len(apps) != 2 {
		t.Fatalf("apps = %+v, %v; want the extension and the phone", apps, err)
	}
	for _, app := range apps {
		if !app.Approved || !app.LastSeen.Equal(seen) {
			t.Errorf("%s = %+v, want it approved and last seen %v", app.ID, app, seen)
		}
	}

	if changed, _ := m.Merge(m.Roster(), now); changed {
		t.Error("merging what is already here changed something")
	}
}

func TestAnInstanceTakesARemovalItMissed(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	phoneKey := memberKeyOf(202).Public().(ed25519.PublicKey)
	m.group.state.Apps = map[string]KnownApp{phone1: {Name: "Pixel 8", Deployment: relay.KindMobile, LastSeen: joined}}
	m.group.state.Keys = map[string]PinnedKey{phone1: {Key: phoneKey, Kind: relay.KindMobile, Approved: true, Used: joined}}

	removed := joined.Add(time.Hour)
	if _, err := m.Merge(Roster{Removed: map[string]time.Time{phone1: removed}}, removed.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !m.Removed(phone1) || m.StillMember(phone1) {
		t.Fatal("the phone taken out elsewhere is still served here")
	}

	// Another instance that still lists it does not bring it back.
	m.Merge(Roster{Apps: []RosterApp{{ID: phone1, Name: "Pixel 8", Deployment: relay.KindMobile, LastSeen: removed, Key: phoneKey}}}, removed.Add(time.Hour))
	if apps, _ := m.Apps(removed.Add(time.Hour)); len(apps) != 0 || m.StillMember(phone1) {
		t.Fatalf("apps = %+v, want the removed phone kept out", apps)
	}
}

func TestARosterCannotPutAKeyUnderADevicesID(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	other := memberKeyOf(99).Public().(ed25519.PublicKey)
	m.Merge(Roster{Apps: []RosterApp{
		{ID: browser1, Name: "Firefox", Deployment: relay.KindExtension, LastSeen: joined, Key: other},
		{ID: officeID, Name: "office", Deployment: relay.KindExtension, LastSeen: joined},
	}}, joined.Add(time.Hour))
	if m.StillMember(browser1) {
		t.Error("a key that does not make the extension's id was approved under it")
	}
	if _, ok := m.group.state.Apps[officeID]; ok {
		t.Error("an instance's id was listed as a browser")
	}
}

func TestSyncAppsAsksApprovedInstancesOnly(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	roster, _ := json.Marshal(Roster{Apps: []RosterApp{{ID: browser1, Name: "Firefox", Deployment: relay.KindExtension, LastSeen: joined}}})
	rt := &fakeRelay{
		sibs:   []relay.Announce{{InstanceID: officeID, Name: "Bottich", Deployment: "container"}},
		resp:   roster,
		status: http.StatusOK,
	}
	m.SetRelay(rt)
	now := joined.Add(time.Hour)

	if m.SyncApps(context.Background(), now) || rt.path != "" {
		t.Fatalf("an instance nobody approved here was asked (%q)", rt.path)
	}

	m.group.state.Keys = map[string]PinnedKey{
		officeID: {Key: memberKeyOf(210).Public().(ed25519.PublicKey), Kind: relay.KindInstance, Approved: true, Used: joined},
	}
	if !m.SyncApps(context.Background(), now) || rt.path != "/api/group/roster" {
		t.Fatalf("the approved instance was not asked, or its roster changed nothing (%q)", rt.path)
	}
	if apps, _ := m.Apps(now); len(apps) != 1 || apps[0].ID != browser1 {
		t.Fatalf("apps = %+v, want the extension the other instance saw", apps)
	}

	rt.path = ""
	m.SyncApps(context.Background(), now.Add(time.Second))
	if rt.path != "" {
		t.Error("the same instance was asked again within shareEvery")
	}
}
