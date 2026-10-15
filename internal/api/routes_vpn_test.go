package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

func vpnState(t *testing.T, doc map[string]any) vpnconn.State {
	t.Helper()
	raw, _ := json.Marshal(doc)
	var st vpnconn.State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func vpnProvider(st vpnconn.State, id string) vpnconn.ProviderState {
	for _, p := range st.Providers {
		if p.ID == id {
			return p
		}
	}
	return vpnconn.ProviderState{}
}

// The VPN routes connect and disconnect, and the torrent client follows: on
// the tunnel and its forwarded port while a provider is in use, back on the
// machine's own connection after.
func TestVPNRoutesPutTorrentsOnTheTunnel(t *testing.T) {
	vs := vpntest.Start(t)
	vs.SetLifetime(2 * time.Second)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)

	code, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/vpn", "", "")
	st := vpnState(t, doc)
	if code != http.StatusOK || st.Active != "" || len(st.Providers) != 34 {
		t.Fatalf("GET /api/vpn = %d, active %q, %d providers", code, st.Active, len(st.Providers))
	}

	body, _ := json.Marshal(map[string]any{"fields": map[string]string{"config": vs.Config()}})
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/vpn/protonvpn/connect", string(body), "")
	if code != http.StatusOK || vpnState(t, doc).Active != "protonvpn" {
		t.Fatalf("connect answered %d: %v", code, doc)
	}
	key := vs.ClientKey.String()
	deadline := time.Now().Add(20 * time.Second)
	var p vpnconn.ProviderState
	for time.Now().Before(deadline) {
		_, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/vpn", "", "")
		raw, _ := json.Marshal(doc)
		if strings.Contains(string(raw), key) {
			t.Fatal("the state carries the private key")
		}
		if p = vpnProvider(vpnState(t, doc), "protonvpn"); p.State == "connected" && p.ForwardedPort != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if p.State != "connected" || p.ForwardedPort != 51234 || !p.Configured {
		t.Fatalf("proton is %+v", p)
	}
	if ov := a.TorrentOverview(); ov.Interface != vpnconn.NetworkName || ov.InterfaceDown {
		t.Errorf("torrents are on %q (down %v), want the tunnel", ov.Interface, ov.InterfaceDown)
	}

	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/vpn/disconnect", "", "")
	if code != http.StatusOK || vpnState(t, doc).Active != "" {
		t.Fatalf("disconnect answered %d: %v", code, doc)
	}
	if ov := a.TorrentOverview(); ov.Interface != "" {
		t.Errorf("torrents stay on %q after disconnect", ov.Interface)
	}

	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodDelete, "/api/vpn/protonvpn", "", "")
	if code != http.StatusOK || vpnProvider(vpnState(t, doc), "protonvpn").Configured {
		t.Fatalf("delete answered %d: %v", code, doc)
	}
}

func TestVPNRefusalsCarryACodeAndTheField(t *testing.T) {
	srv, _ := testServer(t)
	t.Cleanup(srv.Close)
	cases := []struct {
		method, path, body string
		status             int
		code, field        string
	}{
		{http.MethodPost, "/api/vpn/protonvpn/connect", `{"fields":{}}`, http.StatusBadRequest, "vpn-field-missing", "config"},
		{http.MethodPost, "/api/vpn/nowhere/connect", `{"fields":{}}`, http.StatusNotFound, "vpn-unknown", ""},
		{http.MethodPost, "/api/vpn/mullvad/connect", `{"fields":{"token":"1234"}}`, http.StatusNotImplemented, "vpn-unsupported", ""},
		{http.MethodDelete, "/api/vpn/nowhere", "", http.StatusNotFound, "vpn-unknown", ""},
	}
	for _, c := range cases {
		code, doc := callAPI(t, http.DefaultClient, srv.URL, c.method, c.path, c.body, "")
		if code != c.status || doc["code"] != c.code || (c.field != "" && doc["field"] != c.field) {
			t.Errorf("%s %s = %d %v, want %d %s %s", c.method, c.path, code, doc, c.status, c.code, c.field)
		}
	}
}

func TestVPNRoutesNeedAdmin(t *testing.T) {
	srv, a := lockedServer(t)
	_, secret, err := a.APITokens.CreateScoped("control", []apitoken.Scope{apitoken.ScopeControl, apitoken.ScopeRead, apitoken.ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]string{
		{http.MethodGet, "/api/vpn"},
		{http.MethodPost, "/api/vpn/custom/connect"},
		{http.MethodPost, "/api/vpn/disconnect"},
		{http.MethodPut, "/api/vpn/routes"},
		{http.MethodDelete, "/api/vpn/custom"},
	} {
		code, doc := callAPI(t, http.DefaultClient, srv.URL, r[0], r[1], `{"fields":{}}`, secret)
		wantRefused(t, r[0]+" "+r[1], code, doc, apitoken.ScopeAdmin)
	}
}

// A peer on the relay must not switch this instance's VPN, nor read whether
// it has one.
func TestTheVPNIsNotReachableFromTheGroup(t *testing.T) {
	for _, r := range [][2]string{
		{http.MethodGet, "/api/vpn"},
		{http.MethodPost, "/api/vpn/custom/connect"},
		{http.MethodPost, "/api/vpn/disconnect"},
		{http.MethodPut, "/api/vpn/routes"},
		{http.MethodDelete, "/api/vpn/custom"},
	} {
		if groupReaches(t, r[0], r[1]) {
			t.Errorf("%s %s is reachable from the group", r[0], r[1])
		}
	}
}
