package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
)

// TestTheInstancesOwnBucketsAreNotInterfaceState: a token that may read the
// layout must not read the feed keys and parked webhook headers the instance
// keeps in the same table, nor may anybody overwrite them through this route.
func TestTheInstancesOwnBucketsAreNotInterfaceState(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, read, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("owner", []apitoken.Scope{apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "indexer-apikey-8749"
	for key := range serverBuckets {
		if err := a.SetUIState(key, `{"https://indexer.example/rss?apikey=`+secret+`":1}`); err != nil {
			t.Fatal(err)
		}
	}

	call := func(method, key, token string, body io.Reader) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+"/api/uistate?key="+key, body)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for key := range serverBuckets {
		code, body := call(http.MethodGet, key, read, nil)
		if code != http.StatusForbidden || strings.Contains(body, secret) {
			t.Errorf("GET ?key=%s with a read token answered %d %q, want 403 without the stored value", key, code, body)
		}
		if code, _ := call(http.MethodPut, key, admin, strings.NewReader(`{}`)); code != http.StatusForbidden {
			t.Errorf("PUT ?key=%s answered %d, want 403", key, code)
		}
	}
	if code, _ := call(http.MethodGet, "default", read, nil); code != http.StatusOK {
		t.Errorf("GET of the layout with a read token answered %d, want 200", code)
	}
}
