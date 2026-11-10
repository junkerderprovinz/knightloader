package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// An instance that had others added by their address starts without them,
// says once which ones they were, and keeps none of their credentials.
func TestInstancesAddedByAddressMakeWayForThePhrase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stored := `[{"name":"nas","url":"http://192.168.1.5:8749"},{"name":"cellar","url":"http://192.168.1.9:8749"}]`
	if err := os.WriteFile(filepath.Join(dir, "instances.json"), []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.SetCredential("federation-peer", "nas", accounts.Credential{APIKey: "token-the-nas-issued"}); err != nil {
		t.Fatal(err)
	}

	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, _, err := a.APITokens.Create("peer: nas"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.APITokens.Create("dashboard"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)

	var listed []map[string]any
	if code, body := call(t, "", http.MethodGet, srv.URL+"/api/instances", nil); code != http.StatusOK || json.Unmarshal(body, &listed) != nil || len(listed) != 0 {
		t.Fatalf("GET /api/instances = %d %s, want no instance", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "instances.json")); !os.IsNotExist(err) {
		t.Errorf("instances.json is still there: %v", err)
	}
	if ids := a.Accounts.AccountIDs("federation-peer"); len(ids) != 0 {
		t.Errorf("the credentials of %v are still kept", ids)
	}
	if tokens := a.APITokens.List(); len(tokens) != 1 || tokens[0].Name != "dashboard" {
		t.Errorf("tokens %+v, want the one minted for nas revoked and the dashboard's kept", tokens)
	}
	if got := connectInfoOf(t, "", srv.URL).DroppedInstances; !slices.Equal(got, []string{"cellar", "nas"}) {
		t.Fatalf("the card names %v, want the two instances added by address", got)
	}

	if code, body := call(t, "", http.MethodDelete, srv.URL+"/api/connect/dropped-instances", nil); code != http.StatusNoContent {
		t.Fatalf("putting the note away = %d %s", code, body)
	}
	if got := connectInfoOf(t, "", srv.URL).DroppedInstances; len(got) != 0 {
		t.Errorf("the card still names %v after the note was put away", got)
	}
}

func TestNoInstanceIsAddedByItsAddress(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	if code, _ := call(t, "", http.MethodPost, srv.URL+"/api/instances", map[string]string{"name": "nas", "url": "http://192.168.1.5:8749"}); code != http.StatusNotFound {
		t.Errorf("adding an instance by address = %d, want 404 as for any route there is not", code)
	}
}
