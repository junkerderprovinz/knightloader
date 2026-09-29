package federation

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

func TestGroupKeepsTheFirstTimeAMemberCame(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}

	st, err := m.Group(false, joined.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !st.JoinedAt.Equal(joined) || !st.MemberSeenAt.IsZero() {
		t.Fatalf("alone in the group: %+v", st)
	}

	came := joined.Add(2 * time.Minute)
	if st, err = m.Group(true, came); err != nil {
		t.Fatal(err)
	}
	if !st.MemberSeenAt.Equal(came) {
		t.Fatalf("MemberSeenAt = %v, want %v", st.MemberSeenAt, came)
	}
	if st, err = m.Group(false, came.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !st.MemberSeenAt.Equal(came) {
		t.Fatalf("a member going away moved MemberSeenAt to %v", st.MemberSeenAt)
	}

	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := again.Group(false, came.Add(time.Hour)); !st.JoinedAt.Equal(joined) || !st.MemberSeenAt.Equal(came) {
		t.Fatalf("after a restart: %+v", st)
	}
}

func TestLeavingStartsTheNextGroupWithNobodySeen(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Group(true, joined.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := m.SetJoined(time.Time{}); err != nil {
		t.Fatal(err)
	}
	st, err := m.Group(true, joined.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !st.JoinedAt.IsZero() || !st.MemberSeenAt.IsZero() {
		t.Fatalf("outside a group: %+v", st)
	}

	rejoined := joined.Add(3 * time.Minute)
	if err := m.SetJoined(rejoined); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Group(false, rejoined); !st.MemberSeenAt.IsZero() {
		t.Fatalf("a new group starts with a member seen: %+v", st)
	}
}

func TestMembersAreTheInstancesOnTheRelay(t *testing.T) {
	m := newManager(t)
	if err := m.Add(Instance{Name: "nas", URL: "http://192.168.1.5:8749"}); err != nil {
		t.Fatal(err)
	}
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: "aaaa", Name: "office"},
		{InstanceID: "bbbb", Name: "phone", Client: true},
	}})

	got := m.Members()
	if len(got) != 1 || got[0].RelayID != "aaaa" {
		t.Fatalf("Members() = %+v, want only the instance on the relay", got)
	}
}

func TestAPhoneThatWentAwayKeepsItsCard(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRelay{sibs: []relay.Announce{
		{InstanceID: "phone-1", Name: "Pixel 8", Deployment: "mobile", Client: true},
		{InstanceID: "browser-1", Name: "Chrome", Deployment: "extension", Client: true},
		{InstanceID: "id-office", Name: "office"},
	}}
	m.SetRelay(rt)

	seen := joined.Add(time.Minute)
	apps, err := m.Apps(seen)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].ID != "phone-1" || apps[0].Name != "Pixel 8" || !apps[0].Connected {
		t.Fatalf("apps = %+v, want the phone connected and neither the browser nor the instance", apps)
	}

	rt.sibs = rt.sibs[2:]
	apps, _ = m.Apps(seen.Add(time.Hour))
	if len(apps) != 1 || apps[0].Connected || !apps[0].LastSeen.Equal(seen) {
		t.Fatalf("apps after the phone left = %+v, want it not connected, last seen %v", apps, seen)
	}

	if err := m.SetJoined(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if apps, _ := m.Apps(seen); len(apps) != 0 {
		t.Fatalf("apps after leaving = %+v, want none", apps)
	}
}
