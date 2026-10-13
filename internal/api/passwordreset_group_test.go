package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// TestAResetLeavesThePairingGroup: the phrase opens the group's routes
// without a password, so whoever read it before the reset would keep that.
// The instance gets a phrase of its own that nobody else holds, the
// hand-entered relay key goes, and the answer says what was removed.
func TestAResetLeavesThePairingGroup(t *testing.T) {
	out := &output{}
	writeTo(t, out)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	relayOff(t, a)
	armTwoFactor(t, a, resetOldPassword)
	old, _, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := enterGroup(a, old); err != nil {
		t.Fatal(err)
	}
	before, _ := a.Accounts.Get(relay.SeedAccountService)
	if err := a.Accounts.Set(relay.AccountService, "a-hand-entered-relay-key"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"the phone", "the extension"} {
		if _, _, err := a.APITokens.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	}); err != nil {
		t.Fatal(err)
	}

	code := askForCode(t, srv.URL, out)
	status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword)
	if status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}

	after, _ := a.Accounts.Get(relay.SeedAccountService)
	if after == "" || after == before {
		t.Errorf("the connection secret after the reset is %q, want a new one", after)
	}
	if manual, _ := a.Accounts.Get(relay.AccountService); manual != "" {
		t.Error("the hand-entered relay key outlived the reset")
	}
	var answer struct {
		Removed resetRemoved `json:"removed"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatal(err)
	}
	want := resetRemoved{TwoFactor: true, Passkeys: 1, Tokens: 2, Group: true}
	if answer.Removed != want {
		t.Errorf("the answer says %+v was removed, want %+v", answer.Removed, want)
	}
}

// TestTheResetPageListsOnlyWhatWasRemoved: a token list or a group that could
// not be written is still there, and the page must not say otherwise.
func TestTheResetPageListsOnlyWhatWasRemoved(t *testing.T) {
	srv, a, out := resetServer(t)
	old, _, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := enterGroup(a, old); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"the phone", "the extension"} {
		if _, _, err := a.APITokens.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	// A folder where each file was makes every write of it fail.
	for _, name := range []string{"tokens.json", "accounts.json"} {
		path := filepath.Join(a.DataDir, name)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	code := askForCode(t, srv.URL, out)
	status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword)
	if status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}
	var answer struct {
		Removed resetRemoved `json:"removed"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatal(err)
	}
	if left := len(a.APITokens.List()); answer.Removed.Tokens != 2-left {
		t.Errorf("the answer says %d tokens were removed, and %d of 2 are left", answer.Removed.Tokens, left)
	}
	if answer.Removed.Group {
		t.Error("the answer says the group was left, though the new phrase could not be stored")
	}
}

// TestAResetOutsideAGroupStartsNone: an instance that was in no group stays
// out of one.
func TestAResetOutsideAGroupStartsNone(t *testing.T) {
	srv, a, out := resetServer(t)
	code := askForCode(t, srv.URL, out)
	status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword)
	if status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}
	if secret, _ := a.Accounts.Get(relay.SeedAccountService); secret != "" {
		t.Error("a reset put an instance that was in no group into one")
	}
	var answer struct {
		Removed resetRemoved `json:"removed"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Removed != (resetRemoved{}) {
		t.Errorf("the answer says %+v was removed from an instance with nothing to remove", answer.Removed)
	}
}

// TestNoGroupJoinedDuringAResetSurvivesIt: a session checked before the
// reset must not put the instance into a group once the reset is done.
func TestNoGroupJoinedDuringAResetSurvivesIt(t *testing.T) {
	srv, a, out := resetServer(t)
	code := askForCode(t, srv.URL, out)
	_, session := signedIn(t, srv.URL, resetOldPassword)
	_, phrase, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}

	body, slow := io.Pipe()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/connect/join", body)
	req.Header = sessionHeader(session)
	req.Header.Set("Content-Type", "application/json")
	answered := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			answered <- 0
			return
		}
		resp.Body.Close()
		answered <- resp.StatusCode
	}()
	start, _ := json.Marshal(phrase)
	if _, err := slow.Write([]byte(`{"phrase":` + string(start))); err != nil {
		t.Fatal(err)
	}

	if status, body := finishReset(t, &http.Client{Jar: newJar(t)}, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Fatalf("the reset answered %d: %s", status, body)
	}
	_, _ = slow.Write([]byte("}"))
	_ = slow.Close()

	if status := <-answered; status == http.StatusOK {
		t.Error("a join went through for a session the reset had ended")
	}
	if secret, _ := a.Accounts.Get(relay.SeedAccountService); secret != "" {
		t.Error("the instance is in a group joined with a session the reset had ended")
	}
}
