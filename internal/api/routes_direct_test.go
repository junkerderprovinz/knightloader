package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// TestMembersOnOneNetworkTalkWithoutARelay pairs two instances with the relay
// switched off and gives each the other's address, as somebody does where
// nothing announces them: each finds the other there, both owners approve,
// and the downloads come over directly.
func TestMembersOnOneNetworkTalkWithoutARelay(t *testing.T) {
	t.Parallel()
	first, firstApp, firstToken := pairingServer(t)
	second, secondApp, secondToken := pairingServer(t)
	words := activate(t, firstToken, first.URL)
	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, body)
	}
	if err := firstApp.Federation.Group().SetAddresses([]string{second.URL}); err != nil {
		t.Fatal(err)
	}
	if err := secondApp.Federation.Group().SetAddresses([]string{first.URL}); err != nil {
		t.Fatal(err)
	}
	firstID, secondID := groupID(firstApp), groupID(secondApp)

	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s never happened", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	lists := func(token, base, id string) bool {
		for _, p := range connectInfoOf(t, token, base).Pending {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	waitFor("the first listing the second as waiting", func() bool { return lists(firstToken, first.URL, secondID) })
	waitFor("the second listing the first as waiting", func() bool { return lists(secondToken, second.URL, firstID) })
	if code, body := call(t, firstToken, http.MethodPost, first.URL+"/api/connect/devices/"+secondID+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the second instance on the first = %d %s", code, body)
	}
	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/devices/"+firstID+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the first instance on the second = %d %s", code, body)
	}

	waitFor("the first reaching the second directly", func() bool {
		for _, m := range connectInfoOf(t, firstToken, first.URL).Members {
			if m.ID == secondID && m.Direct && !m.Relay {
				return true
			}
		}
		return false
	})
	code, body := call(t, firstToken, http.MethodGet, first.URL+"/api/instances/"+secondID+"/tasks", nil)
	if code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		t.Fatalf("the second instance's downloads answered %d: %s", code, body)
	}
}

// TestADirectCallNeedsTheGroupsSignature checks the open route: a call
// without a valid signature is refused, and a correctly signed call that
// arrives twice runs once.
func TestADirectCallNeedsTheGroupsSignature(t *testing.T) {
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)
	self := groupID(a)

	forged, _ := http.NewRequest(http.MethodPost, srv.URL+group.DirectPath, strings.NewReader(`{"requestId":"r1","target":"`+self+`"}`))
	forged.Header.Set("Content-Type", "application/json")
	forged.Header.Set("X-Knightloader-Peer", "id-stranger")
	forged.Header.Set("X-Knightloader-Target", self)
	forged.Header.Set("X-Knightloader-Time", strconv.FormatInt(time.Now().Unix(), 10))
	forged.Header.Set("X-Knightloader-Signature", "00")
	resp, err := http.DefaultClient.Do(forged)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an unsigned direct call answered %d, want 403", resp.StatusCode)
	}

	approve(t, a, "phone-mine", member.KindMobile)
	req, _ := directRequest(t, a, phoneMine, member.KindMobile, keyOf("phone-mine"), relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"})
	again := req.Clone(req.Context())
	again.Body, _ = req.GetBody()
	for i, want := range []int{http.StatusOK, http.StatusForbidden} {
		r := req
		if i == 1 {
			r = again
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("the member's call, sent %d times, answered %d, want %d", i+1, resp.StatusCode, want)
		}
	}
}
