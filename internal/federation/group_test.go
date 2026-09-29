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

	st, err := m.Group(nil, joined.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !st.JoinedAt.Equal(joined) || !st.MemberSeenAt.IsZero() {
		t.Fatalf("alone in the group: %+v", st)
	}

	came := joined.Add(2 * time.Minute)
	member := []Instance{{Name: "b", RelayID: "b"}}
	if st, err = m.Group(member, came); err != nil {
		t.Fatal(err)
	}
	if !st.MemberSeenAt.Equal(came) {
		t.Fatalf("MemberSeenAt = %v, want %v", st.MemberSeenAt, came)
	}
	if st, err = m.Group(nil, came.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !st.MemberSeenAt.Equal(came) {
		t.Fatalf("a member going away moved MemberSeenAt to %v", st.MemberSeenAt)
	}

	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := again.Group(nil, came.Add(time.Hour)); !st.JoinedAt.Equal(joined) || !st.MemberSeenAt.Equal(came) {
		t.Fatalf("after a restart: %+v", st)
	}
}

func TestLeavingStartsTheNextGroupWithNobodySeen(t *testing.T) {
	m := newManager(t)
	joined := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	member := []Instance{{Name: "b", RelayID: "b"}}
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Group(member, joined.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := m.SetJoined(time.Time{}); err != nil {
		t.Fatal(err)
	}
	st, err := m.Group(member, joined.Add(2*time.Minute))
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
	if st, _ := m.Group(nil, rejoined); !st.MemberSeenAt.IsZero() {
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
