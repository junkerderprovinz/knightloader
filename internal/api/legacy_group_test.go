package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// olderInstance is an instance on a release before kit/group, on the relay
// this instance serves: it shares its roster on the old route and records the
// decisions passed on to it.
type olderInstance struct {
	mu    sync.Mutex
	calls []string
}

func (o *olderInstance) serve(_ context.Context, call relay.ProxyCall) (int, []byte) {
	if call.Method == http.MethodGet && call.Path == federation.LegacyRosterPath {
		key := keyOf("phone-old").Public().(ed25519.PublicKey)
		b, _ := json.Marshal(federation.LegacyRoster{
			Apps: []federation.LegacyApp{
				{ID: idAs("phone-old", member.KindMobile), Name: "Galaxy", Deployment: "mobile", Key: key, LastSeen: time.Now()},
				{ID: idAs("phone-unsure", member.KindMobile), Name: "Unsure", Deployment: "mobile"},
			},
			Removed: map[string]time.Time{idAs("phone-lost", member.KindMobile): time.Now()},
		})
		return http.StatusOK, b
	}
	o.mu.Lock()
	o.calls = append(o.calls, call.Method+" "+call.Path)
	o.mu.Unlock()
	return http.StatusNoContent, nil
}

func (o *olderInstance) heard(call string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Contains(o.calls, call)
}

// olderGroup is an instance serving the relay of its group, which an older
// instance has joined and which approved it.
func olderGroup(t *testing.T) (*httptest.Server, *app.App, *olderInstance) {
	t.Helper()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	secret := phraseGroup(t, a)
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"`+srv.URL+`","mode":"own","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}
	old := &olderInstance{}
	c, err := relay.NewClient(relay.ClientOptions{
		URL:      srv.URL,
		Key:      federation.Domain.GroupKey(secret),
		FrameKey: federation.Domain.FrameKey(secret),
		Self:     relay.Announce{InstanceID: siblingID, Name: "Older", Deployment: "container"},
		Signing:  federation.Signing,
		Identity: identityAs("sibling", member.KindInstance),
		Serve:    old.serve,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	t.Cleanup(func() { _ = c.Close() })
	approve(t, a, "sibling", member.KindInstance)
	servers.Store(a, srv)
	online := func() bool {
		for _, m := range a.Federation.GroupMembers() {
			if m.ID == siblingID && m.Online() {
				return true
			}
		}
		return false
	}
	waitUntil(t, "the older instance on the relay", online)
	return srv, a, old
}

func TestAnOlderInstanceHearsOfTheOwnersDecisionsOnItsOwnRoutes(t *testing.T) {
	t.Parallel()
	srv, a, old := olderGroup(t)
	phone := idAs("phone-new", member.KindMobile)
	if status, body := deviceAs(t, a, phone, member.KindMobile, keyOf("phone-new"))(http.MethodGet, "/api/tasks"); status != http.StatusForbidden || refusalCode(body) != "memberPending" {
		t.Fatalf("a new phone = %d %s, want it waiting", status, body)
	}
	if code, body := call(t, "", http.MethodPost, srv.URL+"/api/connect/devices/"+phone+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the phone = %d %s", code, body)
	}
	waitUntil(t, "the approval on the older instance's route", func() bool {
		return old.heard(http.MethodPost + " /api/connect/devices/" + phone + "/approve")
	})
	if code, body := call(t, "", http.MethodDelete, srv.URL+"/api/connect/apps/"+phone, nil); code != http.StatusNoContent {
		t.Fatalf("taking the phone out = %d %s", code, body)
	}
	waitUntil(t, "the removal on the older instance's route", func() bool {
		return old.heard(http.MethodDelete + " /api/connect/apps/" + phone)
	})
}

func TestTheRosterOfAnOlderInstanceComesOver(t *testing.T) {
	t.Parallel()
	srv, a, _ := olderGroup(t)
	connectInfoOf(t, "", srv.URL)
	waitUntil(t, "the phone the older instance approved", func() bool {
		for _, m := range a.Federation.GroupMembers() {
			if m.ID == idAs("phone-old", member.KindMobile) {
				return m.State == group.StateApproved && m.Name == "Galaxy"
			}
		}
		return false
	})
	if removedHere(t, a, idAs("phone-unsure", member.KindMobile)) || !removedHere(t, a, idAs("phone-lost", member.KindMobile)) {
		t.Error("want the phone the older instance took out taken out here, and nothing else")
	}
	if status, _ := signedMember(t, a, "phone-old", member.KindMobile)(http.MethodGet, "/api/tasks"); status != http.StatusOK {
		t.Errorf("the phone the older instance approved = %d here, want 200", status)
	}
}

func TestAnOlderInstancePassesItsDecisionsOnHere(t *testing.T) {
	t.Parallel()
	_, a := testServer(t)
	older := signedBody(t, a, "instance-old", member.KindInstance)
	phone := keyOf("phone-passed").Public().(ed25519.PublicKey)
	phoneID := member.ID(member.KindMobile, phone)
	body, _ := json.Marshal(deviceDecision{Key: phone, Kind: member.KindMobile})
	if status, answer := older(http.MethodPost, "/api/connect/devices/"+phoneID+"/approve", body); status != http.StatusNoContent {
		t.Fatalf("the approval passed on = %d %s", status, answer)
	}
	if status, _ := signedMember(t, a, "phone-passed", member.KindMobile)(http.MethodGet, "/api/tasks"); status != http.StatusOK {
		t.Errorf("the phone approved elsewhere = %d, want 200", status)
	}
	if status, answer := older(http.MethodPost, "/api/connect/devices/"+phoneID+"/refuse", []byte("{}")); status != http.StatusNoContent {
		t.Fatalf("the refusal passed on = %d %s", status, answer)
	}
	if !removedHere(t, a, phoneID) {
		t.Error("the refusal passed on was not taken")
	}

	phoneOnly := signedBody(t, a, "phone-pushy", member.KindMobile)
	other := keyOf("phone-other").Public().(ed25519.PublicKey)
	body, _ = json.Marshal(deviceDecision{Key: other, Kind: member.KindMobile})
	if status, answer := phoneOnly(http.MethodPost, "/api/connect/devices/"+member.ID(member.KindMobile, other)+"/approve", body); status != http.StatusForbidden || refusalCode(answer) != "decisionNotFromInstance" {
		t.Errorf("a phone passing an approval on = %d %s, want 403 decisionNotFromInstance", status, answer)
	}
}

func TestAnOlderInstanceReadsThePhonesOfTheGroup(t *testing.T) {
	t.Parallel()
	_, a := testServer(t)
	approve(t, a, "phone-mine", member.KindMobile)
	if status, body := deviceAs(t, a, idAs("phone-waits", member.KindMobile), member.KindMobile, keyOf("phone-waits"))(http.MethodGet, "/api/tasks"); status != http.StatusForbidden {
		t.Fatalf("a new phone = %d %s, want it waiting", status, body)
	}
	if err := groupOf(t, a).Remove(phoneOther); err != nil {
		t.Fatal(err)
	}
	status, body := signedMember(t, a, "instance-old", member.KindInstance)(http.MethodGet, federation.LegacyRosterPath)
	var r federation.LegacyRoster
	if status != http.StatusOK || json.Unmarshal(body, &r) != nil {
		t.Fatalf("the roster = %d %s", status, body)
	}
	keys := map[string]bool{}
	for _, app := range r.Apps {
		keys[app.ID] = len(app.Key) > 0
	}
	if len(keys) != 2 || !keys[phoneMine] || keys[idAs("phone-waits", member.KindMobile)] {
		t.Errorf("apps %+v, want the approved phone with its key and the waiting one without", r.Apps)
	}
	if _, ok := r.Removed[phoneOther]; !ok || len(r.Removed) != 1 {
		t.Errorf("removed %v, want the phone taken out", r.Removed)
	}
}
