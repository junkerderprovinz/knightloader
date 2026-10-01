package api

import (
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// TestOneCallerCannotPushOutAnotherOnesSignIn: sign-in is started without a
// session, so a stranger looping it may hold a few ceremonies but must not
// push out one somebody else has half done.
func TestOneCallerCannotPushOutAnotherOnesSignIn(t *testing.T) {
	t.Parallel()
	c := newPasskeyCeremonies(passkeySignInsPerClient)
	owner, err := c.begin(&webauthn.SessionData{}, "kl.example.com", "192.168.1.5")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 * passkeyCeremonyMax {
		if _, err := c.begin(&webauthn.SessionData{}, "kl.example.com", "203.0.113.9"); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := c.take(owner); !ok {
		t.Error("a stranger starting sign-ins in a loop pushed out the owner's")
	}
}

// TestAFullSignInMapRefusesInsteadOfEvicting: many addresses together can fill
// the map; then a new sign-in waits, and the ones in progress can finish.
func TestAFullSignInMapRefusesInsteadOfEvicting(t *testing.T) {
	t.Parallel()
	c := newPasskeyCeremonies(passkeySignInsPerClient)
	first, err := c.begin(&webauthn.SessionData{}, "kl.example.com", "192.168.1.5")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < passkeyCeremonyMax; i++ {
		if _, err := c.begin(&webauthn.SessionData{}, "kl.example.com", "203.0.113."+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.begin(&webauthn.SessionData{}, "kl.example.com", "198.51.100.1"); !errors.Is(err, errCeremoniesFull) {
		t.Fatalf("begin on a full map = %v, want errCeremoniesFull", err)
	}
	if _, ok := c.take(first); !ok {
		t.Error("a full map dropped a sign-in in progress")
	}
}

// TestStartingSignInsCountsAgainstTheThrottle: begin stores a ceremony for
// anybody who asks, so asking is an attempt.
func TestStartingSignInsCountsAgainstTheThrottle(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if _, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "localhost",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	var last int
	for range loginFailBurst + 1 {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/passkey/login/begin", nil)
		req.Host = "localhost"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("sign-in start %d answered %d, want 429", loginFailBurst+1, last)
	}
}
