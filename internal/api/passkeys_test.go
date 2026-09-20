package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/store"
)

// TestRPIDRefusesAnAddressThatCannotCarryAPasskey checks that a host name
// yields a relying-party id and an IP address is refused, since WebAuthn binds
// a credential to a domain.
func TestRPIDRefusesAnAddressThatCannotCarryAPasskey(t *testing.T) {
	cases := []struct {
		host string
		want string // "" means the address is refused
	}{
		{"kl.example.com", "kl.example.com"},
		{"kl.example.com:8749", "kl.example.com"},
		{"KL.Example.COM", "kl.example.com"},
		{"localhost", "localhost"},
		{"localhost:8749", "localhost"},
		{"192.168.20.86", ""},
		{"192.168.20.86:8749", ""},
		{"127.0.0.1:8749", ""},
		{"[fe80::1]:8749", ""},
		{"fe80::1", ""},
		{"", ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/auth/passkeys", nil)
		r.Host = c.host
		got, err := rpIDFor(r)
		if c.want == "" {
			if err == nil {
				t.Errorf("rpIDFor(%q) = %q with no error; that address cannot carry a passkey", c.host, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("rpIDFor(%q) refused with %v", c.host, err)
			continue
		}
		if got != c.want {
			t.Errorf("rpIDFor(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

// TestTheVerdictTravelsAsABooleanAndTheReasonIsADiagnostic checks the server
// half: `supported: false` plus an English diagnostic. That the interface never
// renders the diagnostic is checked by web/check-passkey-reason.mjs.
func TestTheVerdictTravelsAsABooleanAndTheReasonIsADiagnostic(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	// httptest serves on 127.0.0.1, which is the refused case.
	var body struct {
		Supported bool   `json:"supported"`
		Reason    string `json:"reason"`
		RPID      string `json:"rpId"`
		Total     int    `json:"total"`
	}
	resp, err := http.Get(srv.URL + "/api/auth/passkeys")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if body.Supported {
		t.Error("an IP address reports itself as able to carry a passkey")
	}
	if strings.TrimSpace(body.Reason) == "" {
		t.Error("the refusal carries no diagnostic at all, so an API caller and the log learn nothing")
	}
	if body.RPID != "" {
		t.Errorf("rpId = %q on an address that has none", body.RPID)
	}
}

// TestThePasskeyListIsOnlyForSomebodySignedIn checks that the counts are
// public, for the login screen, while the registered keys are listed only to a
// session.
func TestThePasskeyListIsOnlyForSomebodySignedIn(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if _, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	var anon struct {
		Total    int `json:"total"`
		Passkeys *[]struct {
			Name string `json:"name"`
		} `json:"passkeys"`
	}
	resp, err := http.Get(srv.URL + "/api/auth/passkeys")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&anon); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if anon.Total != 1 {
		t.Errorf("total = %d, want 1 - the login screen needs to know whether to offer the button", anon.Total)
	}
	if anon.Passkeys != nil {
		t.Error("an anonymous caller is handed the list of registered keys")
	}

	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}
	var in struct {
		Passkeys []struct {
			Name       string `json:"name"`
			RPID       string `json:"rpId"`
			UsableHere bool   `json:"usableHere"`
		} `json:"passkeys"`
	}
	resp, err = client.Get(srv.URL + "/api/auth/passkeys")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&in); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(in.Passkeys) != 1 {
		t.Fatalf("a signed-in caller got %d keys, want 1", len(in.Passkeys))
	}
	// A key registered for another address is listed and marked, not hidden.
	if in.Passkeys[0].UsableHere {
		t.Error("a key bound to kl.example.com claims it works on an IP address")
	}
	if in.Passkeys[0].RPID != "kl.example.com" {
		t.Errorf("the row does not say which address it belongs to: %q", in.Passkeys[0].RPID)
	}
}

// TestRegisteringAPasskeyNeedsASessionAndAPassword checks that an anonymous
// caller cannot enrol a key, and that a signed-in one on an IP address is
// refused.
func TestRegisteringAPasskeyNeedsASessionAndAPassword(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/auth/passkey/register/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("register/begin without a session = %d, want 401", resp.StatusCode)
	}

	// With a session but on an address that cannot carry a passkey, it refuses
	// with the reason rather than starting a ceremony the browser will reject.
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}
	resp, err = client.Post(srv.URL+"/api/auth/passkey/register/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a ceremony was started on an IP address, which the browser cannot complete")
	}
}

// TestPasskeyLoginRefusesWhenNoneCanAnswerHere checks that no ceremony starts
// when no credential is registered for this address.
func TestPasskeyLoginRefusesWhenNoneCanAnswerHere(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/auth/passkey/login/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a passkey sign-in started with no passkey registered for this address")
	}
}

func TestRenamingAndRemovingAPasskey(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	saved, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}

	body := strings.NewReader(`{"name":"the work laptop"}`)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/passkeys/"+saved.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("rename answered %d", resp.StatusCode)
	}
	rows, _ := a.Store.ListPasskeys()
	if len(rows) != 1 || rows[0].Name != "the work laptop" {
		t.Fatalf("after the rename the store holds %+v", rows)
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/auth/passkeys/"+saved.ID, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete answered %d", resp.StatusCode)
	}
	rows, _ = a.Store.ListPasskeys()
	if len(rows) != 0 {
		t.Errorf("%d keys left after the delete", len(rows))
	}
}

// TestAnAnonymousCallerCannotRemoveAPasskey checks that clearing keys needs a
// session, so nobody can strip that protection without defeating it.
func TestAnAnonymousCallerCannotRemoveAPasskey(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	saved, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/auth/passkeys/"+saved.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an anonymous delete answered %d, want 401", resp.StatusCode)
	}
	if rows, _ := a.Store.ListPasskeys(); len(rows) != 1 {
		t.Error("the key was removed by a caller with no session")
	}
}
