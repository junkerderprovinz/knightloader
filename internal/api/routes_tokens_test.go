package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
)

// postToken issues a token over HTTP and decodes the one-time response.
func postToken(t *testing.T, url, name string) (int, newTokenResponse) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := http.Post(url+"/api/tokens", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out newTokenResponse
	if resp.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func getTokens(t *testing.T, url string) []apitoken.Token {
	t.Helper()
	resp, err := http.Get(url + "/api/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []apitoken.Token
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTokenLifecycleOverHTTP is the whole CRUD story: issue, see it listed
// without its secret, revoke it, see it gone.
func TestTokenLifecycleOverHTTP(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if got := getTokens(t, srv.URL); len(got) != 0 {
		t.Fatalf("a fresh instance already has %d tokens", len(got))
	}

	code, created := postToken(t, srv.URL, "my phone")
	if code != http.StatusCreated {
		t.Fatalf("POST /api/tokens answered %d, want 201", code)
	}
	if created.Secret == "" {
		t.Fatal("the create response carries no secret")
	}
	if created.Name != "my phone" || created.ID == "" {
		t.Fatalf("created token = %+v", created)
	}

	listed := getTokens(t, srv.URL)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("GET /api/tokens = %+v, want the one token just created", listed)
	}
	raw, _ := json.Marshal(listed)
	if bytes.Contains(raw, []byte(created.Secret)) {
		t.Error("GET /api/tokens shipped the secret to the client")
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/tokens/"+created.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE /api/tokens/{id} answered %d, want 204", resp.StatusCode)
	}
	if got := getTokens(t, srv.URL); len(got) != 0 {
		t.Errorf("the token is still listed after being revoked: %+v", got)
	}
}

// TestCreateTokenRefusesAnEmptyName mirrors apitoken.ErrEmptyName through the
// HTTP layer: a client typo (an empty POST body, or a name of only spaces)
// must not silently create an unlabelled credential nobody can tell apart
// from the next one.
func TestCreateTokenRefusesAnEmptyName(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, _ := postToken(t, srv.URL, "   ")
	if code != http.StatusBadRequest {
		t.Errorf("POST /api/tokens with a blank name answered %d, want 400", code)
	}
	if got := getTokens(t, srv.URL); len(got) != 0 {
		t.Errorf("a refused create still produced a token: %+v", got)
	}
}

// TestRevokeUnknownTokenIs404 stops a client from believing a typo'd id
// revoked something.
func TestRevokeUnknownTokenIs404(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/tokens/does-not-exist", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("revoking an unknown id answered %d, want 404", resp.StatusCode)
	}
}

// TestManagingTokensNeedsASession is the ordinary guard, checked here rather
// than assumed: token routes are not accidentally left open the way the
// login routes deliberately are.
func TestManagingTokensNeedsASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/tokens with no session answered %d, want 401", resp.StatusCode)
	}
}
