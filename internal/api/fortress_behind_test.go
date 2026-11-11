package api

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// awayInstance has a's group take an instance called name as approved, one
// that never comes online, and returns its id.
func awayInstance(t *testing.T, a *app.App, name string) string {
	t.Helper()
	key := keyOf(name).Public().(ed25519.PublicKey)
	id := member.ID(member.KindInstance, key)
	if err := groupOf(t, a).Import(group.Roster{Members: []group.Member{{
		ID: id, Kind: member.KindInstance, Key: key, State: group.StateApproved,
		Name: name, Deployment: "container", LastSeen: time.Now(),
	}}}); err != nil {
		t.Fatal(err)
	}
	return id
}

// backdateMove opens a's groups again as if the move into the fortress had
// started ago before now.
func backdateMove(t *testing.T, a *app.App, ago time.Duration) {
	t.Helper()
	err := regroup(a, func() error {
		raw, err := a.Accounts.Get(federation.GroupAccount)
		if err != nil {
			return err
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return err
		}
		if doc["retired"], err = json.Marshal(time.Now().Add(-ago)); err != nil {
			return err
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		return a.Accounts.Set(federation.GroupAccount, string(b))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moveStarted(a *app.App) bool {
	raw, err := a.Accounts.Get(federation.GroupAccount)
	if err != nil {
		return false
	}
	var doc struct {
		Retired time.Time `json:"retired"`
	}
	return json.Unmarshal([]byte(raw), &doc) == nil && !doc.Retired.IsZero()
}

type behindView struct {
	Behind []map[string]any `json:"behind"`
	Away   []struct {
		ID string `json:"id"`
	} `json:"away"`
}

func behindOn(t *testing.T, url string) behindView {
	t.Helper()
	var v behindView
	if code := getJSON(t, url, &v); code != http.StatusOK {
		t.Fatalf("GET %s answered %d", url, code)
	}
	return v
}

func TestTheOldPairingClosesAfterThirtyDaysAndNamesWhoStayedBehind(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	ownRelay(t, a, srv.URL)
	phraseGroup(t, a)
	applyRelay(a)
	attic := awayInstance(t, a, "Attic")
	_, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	waitFor(t, "the move to start its clock", func() bool { return moveStarted(a) })
	time.Sleep(3 * followEvery)
	if groupSecret(a) == nil {
		t.Fatal("the old pairing closed before the 30 days were over")
	}
	if v := behindOn(t, srv.URL+"/api/connect"); len(v.Behind) != 0 {
		t.Fatalf("left behind %v before the 30 days were over", v.Behind)
	}

	backdateMove(t, a, 30*24*time.Hour+time.Minute)
	waitFor(t, "the old phrase to go after 30 days", func() bool { return groupSecret(a) == nil })
	for _, path := range []string{"/api/connect", "/api/group/apps", "/api/fortress"} {
		v := behindOn(t, srv.URL+path)
		if len(v.Behind) != 1 || v.Behind[0]["id"] != attic || v.Behind[0]["name"] != "Attic" || v.Behind[0]["kind"] != "instance" {
			t.Fatalf("%s left behind %v, want Attic", path, v.Behind)
		}
		for field := range v.Behind[0] {
			if field != "id" && field != "name" && field != "kind" && field != "deployment" {
				t.Errorf("%s: a device left behind carries %q", path, field)
			}
		}
		if slices.ContainsFunc(v.Away, func(m struct {
			ID string `json:"id"`
		}) bool {
			return m.ID == attic
		}) {
			t.Errorf("%s lists the instance left behind as away as well", path)
		}
	}

	if code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/connect/members/"+attic, nil); code != http.StatusNoContent {
		t.Fatalf("taking it out answered %d %s", code, raw)
	}
	if v := behindOn(t, srv.URL+"/api/connect"); len(v.Behind) != 0 {
		t.Errorf("left behind %v after it was taken out", v.Behind)
	}
}
