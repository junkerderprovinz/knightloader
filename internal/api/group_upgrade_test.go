package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/keys"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// testdata/group-2.7 is a group as 2.7 left it on disk, written by that
// release's own federation code from published seeds: the instance's member
// key from 32 bytes of 0x01, another instance (0x02), a phone (0x03) and a
// browser (0x04) approved, a phone that waits (0x05) and one taken out
// (0x06). expect.json holds the ids and the fingerprint 2.7 made of them.
const upgradeSecret = "kl-2.7-fixture!!"

// fieldLabels are what every phone, browser and instance in the field signs
// under, written out rather than taken from federation.Signing.
var fieldLabels = keys.Signing{Call: "knightloader/relay/member-call/v2", Result: "knightloader/relay/member-result/v3"}

func upgradeSeed(b byte) []byte { return bytes.Repeat([]byte{b}, ed25519.SeedSize) }

// upgradedServer is an instance that ran 2.7 in that group and has just been
// updated: its data directory and its sealed secrets are as 2.7 left them.
func upgradedServer(t *testing.T) (*httptest.Server, *app.App, string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"group.json", "instances.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", "group-2.7", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var want map[string]string
	b, err := os.ReadFile(filepath.Join("testdata", "group-2.7", "expect.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}

	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	relayOff(t, a)
	if err := a.Auth.SetPassword("", pairingPassword); err != nil {
		t.Fatal(err)
	}
	_, token, err := a.APITokens.Create("upgrade test")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.Set(federation.SeedAccount, hex.EncodeToString([]byte(upgradeSecret))); err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.Set(federation.MemberAccount, hex.EncodeToString(upgradeSeed(1))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	return srv, a, token, want
}

func TestAnUpdatedInstanceKeepsItsPlaceInTheGroup(t *testing.T) {
	t.Parallel()
	srv, a, token, want := upgradedServer(t)
	if got := groupID(a); got != want["ownID"] {
		t.Errorf("the instance goes by %s, want %s, the id the group knows it by", got, want["ownID"])
	}
	info := connectInfoOf(t, token, srv.URL)
	if !info.Active || info.Fingerprint != want["fingerprint"] {
		t.Errorf("the Pairing card shows active %v with fingerprint %q, want the group and %q", info.Active, info.Fingerprint, want["fingerprint"])
	}
	if info.JoinedAgo == 0 {
		t.Error("the instance counts as joined just now, so the group was started over")
	}
	if len(info.Pending) != 1 || info.Pending[0].ID != want["waiting"] {
		t.Errorf("waiting for approval: %+v, want the one phone that waited", info.Pending)
	}
	apps := map[string]groupApp{}
	for _, p := range info.Apps {
		apps[p.ID] = p
	}
	for _, id := range []string{want["pixel"], want["firefox"]} {
		if p, ok := apps[id]; !ok || !p.Approved || p.Name == "" {
			t.Errorf("device %s is listed as %+v, want its card, approved", id, p)
		}
	}
	if _, ok := apps[want["lost"]]; ok {
		t.Error("the phone taken out is listed again")
	}
	if len(info.Away) != 1 || info.Away[0].ID != want["bottich"] || info.Away[0].Name != "Bottich" {
		t.Errorf("away: %+v, want the other instance under its name", info.Away)
	}
	stored := false
	for _, in := range a.Federation.List() {
		stored = stored || (in.Name == "nas" && in.URL == "http://192.168.1.5:8749")
	}
	if !stored {
		t.Error("the instance added by address is gone")
	}
}

// Each device makes a real call, sealed under the frame key the phrase makes
// and signed under the group's labels, and is answered as 2.7 answered it.
func TestEveryDeviceOfAnUpdatedGroupKeepsItsStanding(t *testing.T) {
	t.Parallel()
	_, a, _, _ := upgradedServer(t)
	for _, d := range []struct {
		name   string
		seed   byte
		kind   member.Kind
		status int
		code   string
	}{
		{"the other instance", 2, member.KindInstance, http.StatusOK, ""},
		{"the phone", 3, member.KindMobile, http.StatusOK, ""},
		{"the browser", 4, member.KindExtension, http.StatusOK, ""},
		{"the phone that waits", 5, member.KindMobile, http.StatusForbidden, "memberPending"},
		{"the phone taken out", 6, member.KindMobile, http.StatusGone, ""},
	} {
		t.Run(d.name, func(t *testing.T) {
			key := ed25519.NewKeyFromSeed(upgradeSeed(d.seed))
			status, body := deviceCall(t, a, member.ID(d.kind, key.Public().(ed25519.PublicKey)), d.kind, key,
				relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"})
			if status != d.status || refusalCode(body) != d.code {
				t.Errorf("%s = %d %s, want %d %s", d.name, status, body, d.status, d.code)
			}
		})
	}
}

func TestTheUpdateKeepsTheOldGroupInTheKitAndNotInGroupJSON(t *testing.T) {
	t.Parallel()
	_, a, _, want := upgradedServer(t)
	r := groupOf(t, a).Roster()
	states := map[string]group.State{}
	for _, m := range r.Members {
		states[m.ID] = m.State
	}
	if states[want["bottich"]] != group.StateApproved || states[want["pixel"]] != group.StateApproved ||
		states[want["firefox"]] != group.StateApproved || states[want["waiting"]] != group.StatePending {
		t.Errorf("the kit holds %v, want the instance, the phone and the browser approved and one phone waiting", states)
	}
	if _, ok := r.Removed[want["lost"]]; !ok || len(r.Revoked) == 0 {
		t.Errorf("removed %v and revoked %v, want the phone taken out and its key", r.Removed, r.Revoked)
	}
	if stored, err := a.Accounts.Get(federation.GroupAccount); err != nil || stored == "" {
		t.Fatalf("the group is not sealed in the credential store: %q, %v", stored, err)
	}

	// The next start opens the group from the kit's store, so a group.json
	// without the members leaves them where they are.
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	applyRelay(a)
	if got := len(groupOf(t, a).Roster().Members); got != len(r.Members) {
		t.Errorf("after reopening the kit holds %d members, want %d", got, len(r.Members))
	}
	b, err := os.ReadFile(filepath.Join(a.DataDir, "group.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"keys"`)) {
		t.Errorf("group.json still holds the members: %s", b)
	}
}
