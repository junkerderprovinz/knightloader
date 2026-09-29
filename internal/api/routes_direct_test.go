package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// fakeNetwork stands in for the multicast service: it hands out the announces
// a test puts on it, filtered through the membership check the instance
// installs, as discovery.Service does.
type fakeNetwork struct {
	mu       sync.Mutex
	heard    []discovery.Peer
	sign     func(discovery.Peer) string
	isMember func(discovery.Peer) bool
}

func (f *fakeNetwork) SetGroup(sign func(discovery.Peer) string, isMember func(discovery.Peer) bool) {
	f.mu.Lock()
	f.sign, f.isMember = sign, isMember
	f.mu.Unlock()
}

func (f *fakeNetwork) Members() []discovery.Peer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []discovery.Peer
	for _, p := range f.heard {
		if f.isMember != nil && f.isMember(p) {
			out = append(out, p)
		}
	}
	return out
}

// announce is what the instance behind srv puts on the network: its id and
// address, tagged the way its own discovery service would tag it.
func announce(t *testing.T, srv *httptest.Server, net *fakeNetwork, id string) discovery.Peer {
	t.Helper()
	p := discovery.Peer{ID: id, Name: id, URL: srv.URL, Sent: time.Now().Unix()}
	net.mu.Lock()
	sign := net.sign
	net.mu.Unlock()
	if sign == nil {
		t.Fatal("the instance installed no group tag")
	}
	p.Tag = sign(p)
	return p
}

// TestMembersOnOneNetworkTalkWithoutARelay pairs two instances with the relay
// switched off and puts them on one network: each finds the other by its
// tagged announce and reaches its downloads directly.
func TestMembersOnOneNetworkTalkWithoutARelay(t *testing.T) {
	t.Parallel()
	first, firstApp, firstToken := pairingServer(t)
	second, secondApp, secondToken := pairingServer(t)
	phrase := activate(t, firstToken, first.URL)
	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/join", map[string]string{"phrase": phrase}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, body)
	}

	net := &fakeNetwork{}
	firstApp.Federation.SetDiscovery(net)
	secondID := secondApp.Settings.Get().InstanceID
	// Both hold the same keys, so the second instance's announce is tagged
	// with the tag the first one computes.
	net.heard = []discovery.Peer{announce(t, second, net, secondID), {ID: "id-stranger", Name: "Stranger", URL: "http://192.168.1.99:8749", Sent: time.Now().Unix(), Tag: "forged"}}

	info := connectInfoOf(t, firstToken, first.URL)
	if len(info.Members) != 1 || info.Members[0].ID != secondID || !info.Members[0].Direct {
		t.Fatalf("members = %+v, want the second instance, reached directly", info.Members)
	}

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
	self := a.Settings.Get().InstanceID

	forged, _ := http.NewRequest(http.MethodPost, srv.URL+federation.DirectPath, strings.NewReader(`{"requestId":"r1","target":"`+self+`"}`))
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

	// A member's real call, captured on its way and sent again.
	var captured *http.Request
	var capturedBody []byte
	tap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		captured = r
		fwd, _ := http.NewRequest(r.Method, srv.URL+r.URL.Path, bytes.NewReader(capturedBody))
		fwd.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(fwd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer tap.Close()

	caller, callerApp, callerToken := pairingServer(t)
	phrase := revealed(t, token, srv.URL)
	if code, body := call(t, callerToken, http.MethodPost, caller.URL+"/api/connect/join", map[string]string{"phrase": phrase}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, body)
	}
	net := &fakeNetwork{}
	callerApp.Federation.SetDiscovery(net)
	net.heard = []discovery.Peer{announce(t, tap, net, self)}
	if code, body := call(t, callerToken, http.MethodGet, caller.URL+"/api/instances/"+self+"/tasks", nil); code != http.StatusOK {
		t.Fatalf("the member's direct call answered %d: %s", code, body)
	}
	if captured == nil {
		t.Fatal("the call did not go through the network tap, so it was not direct")
	}

	again, _ := http.NewRequest(http.MethodPost, srv.URL+federation.DirectPath, bytes.NewReader(capturedBody))
	again.Header = captured.Header.Clone()
	resp, err = http.DefaultClient.Do(again)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("the same direct call sent again answered %d, want 403", resp.StatusCode)
	}
}

func revealed(t *testing.T, token, base string) string {
	t.Helper()
	code, body := call(t, token, http.MethodPost, base+"/api/connect/reveal", map[string]string{"password": pairingPassword})
	if code != http.StatusOK {
		t.Fatalf("reveal answered %d: %s", code, body)
	}
	var out struct{ Phrase string }
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Phrase
}
