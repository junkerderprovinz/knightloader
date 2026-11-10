package federation

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

func TestAnOlderInstanceReadsThePhonesAndBrowsersOfTheRoster(t *testing.T) {
	phoneKey, phone := keyFrom(3, member.KindMobile)
	browserKey, browser := keyFrom(4, member.KindExtension)
	waitingKey, waiting := keyFrom(5, member.KindMobile)
	otherKey, other := keyFrom(2, member.KindInstance)
	seen := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	gone := "phone-" + strings.Repeat("7", 64)
	r := LegacyRosterOf(group.Roster{
		Members: []group.Member{
			{ID: phone, Kind: member.KindMobile, Key: phoneKey, State: group.StateApproved, Name: "Pixel 8", LastSeen: seen},
			{ID: browser, Kind: member.KindExtension, Key: browserKey, State: group.StateApproved, Name: "Firefox"},
			{ID: waiting, Kind: member.KindMobile, Key: waitingKey, State: group.StatePending, Name: "Galaxy"},
			{ID: other, Kind: member.KindInstance, Key: otherKey, State: group.StateApproved, Name: "Bottich"},
		},
		Removed: map[string]time.Time{gone: seen, strings.Repeat("9", 40): seen},
	})
	apps := map[string]LegacyApp{}
	for _, app := range r.Apps {
		apps[app.ID] = app
	}
	if len(apps) != 3 {
		t.Fatalf("apps %+v, want the two phones and the browser, no instance", r.Apps)
	}
	if p := apps[phone]; p.Name != "Pixel 8" || p.Deployment != "mobile" || !p.LastSeen.Equal(seen) || !bytes.Equal(p.Key, phoneKey) {
		t.Errorf("phone %+v", p)
	}
	if b := apps[browser]; b.Deployment != "extension" || !bytes.Equal(b.Key, browserKey) {
		t.Errorf("browser %+v", b)
	}
	if w := apps[waiting]; len(w.Key) != 0 {
		t.Errorf("the phone that waits carries its key %x, which an older instance takes as approved", w.Key)
	}
	if len(r.Removed) != 1 || r.Removed[gone].IsZero() {
		t.Errorf("removed %v, want the phone alone; an older instance never lists an instance there", r.Removed)
	}
}

func TestAnOlderInstancesRosterBringsOnlyApprovedDevicesWithAKey(t *testing.T) {
	phoneKey, phone := keyFrom(3, member.KindMobile)
	_, unknown := keyFrom(6, member.KindMobile)
	gone := "browser-" + strings.Repeat("7", 64)
	r := LegacyRoster{
		Apps: []LegacyApp{
			{ID: phone, Name: "Pixel 8", Deployment: "", Key: phoneKey},
			{ID: unknown, Name: "Galaxy", Deployment: "mobile"},
		},
		Removed: map[string]time.Time{gone: time.Now()},
	}.Roster()
	if len(r.Members) != 1 {
		t.Fatalf("members %+v, want the phone with a key alone", r.Members)
	}
	if mb := r.Members[0]; mb.ID != phone || mb.Kind != member.KindMobile || mb.State != group.StateApproved || mb.Name != "Pixel 8" {
		t.Errorf("the phone comes as %+v, want an approved phone under its name", mb)
	}
	if _, ok := r.Removed[gone]; !ok {
		t.Errorf("removed %v, want the browser taken out", r.Removed)
	}
}

func TestKindOfIDTellsTheShapesApart(t *testing.T) {
	_, instance := keyFrom(1, member.KindInstance)
	_, phone := keyFrom(1, member.KindMobile)
	_, browser := keyFrom(1, member.KindExtension)
	for id, want := range map[string]member.Kind{
		instance:                          member.KindInstance,
		phone:                             member.KindMobile,
		browser:                           member.KindExtension,
		strings.ToUpper(instance):         "",
		"phone-" + instance:               "",
		"viewer-" + phone[len("phone-"):]: "",
		"":                                "",
	} {
		if got := KindOfID(id); got != want {
			t.Errorf("KindOfID(%q) = %q, want %q", id, got, want)
		}
	}
}
