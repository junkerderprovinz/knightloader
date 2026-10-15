package vpnconn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn/obscura"
)

const obscuraAccount = "30493434963831824517"

// fakeObscura is Obscura's API for one account.
type fakeObscura struct {
	mu      sync.Mutex
	token   string
	tokens  int
	max     int
	expired bool
	next    int
	tunnels map[string]string
	exits   map[string]string
	created int
	deleted []string
}

func newFakeObscura(t *testing.T) *fakeObscura {
	f := &fakeObscura{token: "tok-1", max: 5, tunnels: map[string]string{}, exits: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := obscuraClient
	obscuraClient = func() *obscura.Client { return &obscura.Client{API: srv.URL + "/api/"} }
	t.Cleanup(func() { obscuraClient = old })
	return f
}

func (f *fakeObscura) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(status int, kind string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":{%q:{}},"msg":"no"}`, kind)
	}
	tunnel := func(id string) map[string]any {
		return map[string]any{
			"id": id, "status": map[string]any{"type": "created", "when": 1},
			"config": map[string]any{"type": "udp_port",
				"client": map[string]any{"wg_pubkey": f.tunnels[id], "addresses": []string{"10.150.177.7/32"}},
				"server": map[string]any{"wg_pubkey": "4s9JIhxC/D02tosXYYcgrD+pHI+C7oTAFsXzVisKjRs=", "endpoints": []string{"198.51.100.3:51820"}, "dnses": []string{"10.64.0.1"}}},
			"exit": map[string]any{"id": f.exits[id], "country_code": "se", "city_code": "got"},
		}
	}
	if r.URL.Path == "/api/token2" {
		var body struct {
			AccountID string `json:"account_id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.AccountID != obscuraAccount {
			fail(http.StatusUnauthorized, obscura.InvalidAccountID)
			return
		}
		f.tokens++
		json.NewEncoder(w).Encode(map[string]any{"auth_token": f.token})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		fail(http.StatusUnauthorized, obscura.MissingOrInvalidAuthToken)
		return
	}
	switch {
	case r.URL.Path == "/api/exits2":
		json.NewEncoder(w).Encode(map[string]any{"exits": []map[string]any{
			{"id": "mullvad-se-got-wg-001", "country_code": "se", "city_code": "got", "provider_id": "se-got-wg-001"},
			{"id": "mullvad-at-vie-wg-001", "country_code": "at", "city_code": "vie", "provider_id": "at-vie-wg-001"},
		}})
	case r.URL.Path == "/api/tunnel" && r.Method == http.MethodGet:
		list := []map[string]any{}
		for id := range f.tunnels {
			list = append(list, tunnel(id))
		}
		json.NewEncoder(w).Encode(list)
	case r.URL.Path == "/api/tunnel" && r.Method == http.MethodPost:
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch {
		case f.expired:
			fail(http.StatusForbidden, obscura.AccountExpired)
			return
		case len(f.tunnels) >= f.max:
			fail(http.StatusBadRequest, obscura.TunnelLimitExceeded)
			return
		case body["type"] != "udp_port":
			fail(http.StatusBadRequest, "BadRequest")
			return
		}
		f.next++
		f.created++
		id := fmt.Sprintf("tun-%d", f.next)
		f.tunnels[id], _ = body["wg_pubkey"].(string)
		f.exits[id], _ = body["exit"].(string)
		json.NewEncoder(w).Encode(tunnel(id))
	case r.URL.Path == "/api/tunnel" && r.Method == http.MethodDelete:
		var body struct{ ID string }
		json.NewDecoder(r.Body).Decode(&body)
		delete(f.tunnels, body.ID)
		f.deleted = append(f.deleted, body.ID)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("null"))
	default:
		fail(http.StatusNotFound, "NoApiRoute")
	}
}

func TestObscuraKeepsUsingTheTunnelItRegistered(t *testing.T) {
	f := newFakeObscura(t)
	ctx := context.Background()
	first, err := openObscura(ctx, Login{"accountNumber": obscuraAccount})
	if err != nil {
		t.Fatal(err)
	}
	if first.cfg.Peer.Endpoint != "198.51.100.3:51820" || first.login[tokenField] != "tok-1" {
		t.Errorf("config %+v, token %q", first.cfg.Peer, first.login[tokenField])
	}
	again, err := openObscura(ctx, first.login)
	if err != nil {
		t.Fatal(err)
	}
	if f.created != 1 || f.tokens != 1 || again.login[deviceField] != first.login[deviceField] || again.cfg.PrivateKey != first.cfg.PrivateKey {
		t.Errorf("%d tunnels created, %d tokens fetched", f.created, f.tokens)
	}

	f.token = "tok-2"
	moved := Login{}
	for k, v := range again.login {
		moved[k] = v
	}
	moved["location"] = "at"
	third, err := openObscura(ctx, moved)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokens != 2 || third.login[tokenField] != "tok-2" {
		t.Errorf("a refused token was not replaced: %d fetched", f.tokens)
	}
	if f.created != 2 || len(f.deleted) != 1 || f.deleted[0] != first.login[deviceField] || f.exits[third.login[deviceField]] != "mullvad-at-vie-wg-001" {
		t.Errorf("moving to at: created %d, deleted %v, exit %q", f.created, f.deleted, f.exits[third.login[deviceField]])
	}
}

func TestObscuraFailuresCarryTheirCode(t *testing.T) {
	f := newFakeObscura(t)
	ctx := context.Background()
	login := Login{"accountNumber": obscuraAccount}
	if _, err := openObscura(ctx, Login{"accountNumber": "123"}); codeOf(err) != CodeLoginRefused {
		t.Errorf("malformed account = %v", err)
	}
	if _, err := openObscura(ctx, Login{"accountNumber": obscuraAccount, "location": "xx"}); codeOf(err) != CodeLocationUnknown {
		t.Errorf("unknown location = %v", err)
	}
	f.max = 0
	if _, err := openObscura(ctx, login); codeOf(err) != CodeDeviceLimit {
		t.Errorf("all tunnels taken = %v", err)
	}
	f.expired = true
	if _, err := openObscura(ctx, login); codeOf(err) != CodeAccountExpired {
		t.Errorf("expired account = %v", err)
	}
}

func TestForgetDeletesTheObscuraTunnel(t *testing.T) {
	f := newFakeObscura(t)
	sess, err := openObscura(context.Background(), Login{"accountNumber": obscuraAccount})
	if err != nil {
		t.Fatal(err)
	}
	forgetObscura(context.Background(), sess.login)
	if len(f.tunnels) != 0 {
		t.Errorf("tunnels left: %v", f.tunnels)
	}
}
