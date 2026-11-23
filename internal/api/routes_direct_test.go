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
