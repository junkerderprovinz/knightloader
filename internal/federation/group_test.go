package federation

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

// keyFrom is the member key a test names by one byte, and its id as kind.
func keyFrom(b byte, kind member.Kind) (ed25519.PublicKey, string) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	return key, member.ID(kind, key)
}

func loadFile(t *testing.T, body string) *Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "group.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheOldGroupComesOverAsTheKitTakesIt(t *testing.T) {
	_, other := keyFrom(2, member.KindInstance)
	_, phone := keyFrom(3, member.KindMobile)
	_, waiting := keyFrom(5, member.KindMobile)
	m := loadFile(t, `{"joinedAt":"2026-10-01T12:00:00Z",
		"apps":{"`+phone+`":{"name":"Pixel 8","deployment":"mobile","lastSeen":"2026-11-03T09:00:00Z"}},
		"members":{"`+other+`":{"name":"Bottich","deployment":"container","address":"https://kl.example.lan","lastSeen":"2026-11-01T09:00:00Z"}},
		"removed":{"phone-`+strings.Repeat("7", 64)+`":"2026-11-02T09:00:00Z"},
		"keys":{
			"`+other+`":{"key":"gTl3Dqh9F19Wo1Rmw0x+zMuNipG07jeiXfYPW4/Js5Q=","kind":"instance","used":"2026-11-04T09:00:00Z","approved":true},
			"`+phone+`":{"key":"7UkoxijRwsbq6QM4kFmVYSlZJzpcY/k2NsFGFKyHN9E=","kind":"mobile","used":"2026-11-02T09:00:00Z","approved":true},
			"`+waiting+`":{"key":"bnoc3Smwt4/ROvTFWY/v9O8qlxZuPKby5Pv8zYBQW/E=","kind":"mobile","used":"2026-11-05T09:00:00Z","approved":false}},
		"revoked":{"`+strings.Repeat("8", 64)+`":"2026-11-02T09:00:00Z"}}`)

	r, ok := m.OldGroup()
	if !ok {
		t.Fatal("no old group, want the one group.json holds")
	}
	got := map[string]group.Member{}
	for _, mb := range r.Members {
		got[mb.ID] = mb
	}
	at := func(s string) time.Time {
		v, _ := time.Parse(time.RFC3339, s)
		return v
	}
	if b := got[other]; b.Kind != member.KindInstance || b.State != group.StateApproved || b.Name != "Bottich" ||
		b.Deployment != "container" || b.Address != "https://kl.example.lan" || !b.LastSeen.Equal(at("2026-11-04T09:00:00Z")) {
		t.Errorf("the other instance comes over as %+v", b)
	}
	if p := got[phone]; p.Kind != member.KindMobile || p.State != group.StateApproved || p.Name != "Pixel 8" ||
		!p.LastSeen.Equal(at("2026-11-03T09:00:00Z")) {
		t.Errorf("the phone comes over as %+v", p)
	}
	if w := got[waiting]; w.State != group.StatePending || w.Name != "" {
		t.Errorf("the phone that waits comes over as %+v", w)
	}
	if len(r.Removed) != 1 || len(r.Revoked) != 1 {
		t.Errorf("removed %v and revoked %v, want one of each", r.Removed, r.Revoked)
	}
	if st, _ := m.Joined(false, time.Now()); !st.JoinedAt.Equal(at("2026-10-01T12:00:00Z")) {
		t.Errorf("joined at %v, want the time group.json keeps", st.JoinedAt)
	}
}

func TestANewGroupLeavesNothingOfTheOldOne(t *testing.T) {
	_, phone := keyFrom(3, member.KindMobile)
	m := loadFile(t, `{"joinedAt":"2026-10-01T12:00:00Z","keys":{"`+phone+`":{"key":"7UkoxijRwsbq6QM4kFmVYSlZJzpcY/k2NsFGFKyHN9E=","kind":"mobile","approved":true}}}`)
	if err := m.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.OldGroup(); ok {
		t.Error("the old group is still there after entering a new one")
	}
	again, err := Load(filepath.Dir(m.group.path))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.OldGroup(); ok {
		t.Error("group.json still holds the old group after a restart")
	}
}

func TestAGroupJSONWithoutMembersHasNoOldGroup(t *testing.T) {
	m := loadFile(t, `{"joinedAt":"2026-10-01T12:00:00Z","memberSeenAt":"2026-10-01T12:01:00Z"}`)
	if r, ok := m.OldGroup(); ok {
		t.Errorf("old group %+v from a file that holds none", r)
	}
}

func TestADamagedGroupFileStopsTheStart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "group.json"), []byte(`{"keys":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a damaged group.json loaded as an empty group")
	}
}

func TestGroupKeepsTheFirstTimeAMemberCame(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Joined(false, joined.Add(time.Minute)); !st.MemberSeenAt.IsZero() {
		t.Fatalf("member seen at %v with nobody there", st.MemberSeenAt)
	}
	first := joined.Add(2 * time.Minute)
	if st, _ := m.Joined(true, first); !st.MemberSeenAt.Equal(first) {
		t.Fatalf("member seen at %v, want %v", st.MemberSeenAt, first)
	}
	if st, _ := m.Joined(true, first.Add(time.Hour)); !st.MemberSeenAt.Equal(first) {
		t.Errorf("member seen at %v later, want the first time kept", st.MemberSeenAt)
	}
}

func TestADroppedRelayKeyIsRememberedUntilTheNextGroup(t *testing.T) {
	m := newManager(t)
	if err := m.DropRelayKey(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(filepath.Dir(m.group.path))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := again.Joined(false, time.Now()); !st.RelayKeyDropped || !st.JoinedAt.IsZero() {
		t.Fatalf("after a restart %+v, want the dropped key noted and no group", st)
	}
	if err := again.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, _ := again.Joined(false, time.Now()); st.RelayKeyDropped {
		t.Error("the dropped key is still noted in a group entered with the phrase")
	}
}

func TestStillMemberIsOnlyAnApprovedMember(t *testing.T) {
	m := newManager(t)
	_, phone := keyFrom(3, member.KindMobile)
	_, waiting := keyFrom(5, member.KindMobile)
	if m.StillMember(phone) {
		t.Error("a member outside any group")
	}
	m.SetGroup(&fakeGroup{members: []group.Member{
		{ID: phone, Kind: member.KindMobile, State: group.StateApproved},
		{ID: waiting, Kind: member.KindMobile, State: group.StatePending},
	}})
	if !m.StillMember(phone) || m.StillMember(waiting) {
		t.Errorf("approved phone %v, waiting phone %v, want only the approved one", m.StillMember(phone), m.StillMember(waiting))
	}
}
