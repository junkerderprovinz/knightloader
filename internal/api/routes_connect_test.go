package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestConnectStartsInactive(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	var info ConnectInfo
	resp, err := http.Get(srv.URL + "/api/connect")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Active {
		t.Error("a fresh instance reports an active connection phrase")
	}
	if info.RelayURL != relay.DefaultRelayURL {
		t.Errorf("RelayURL = %q, want the compiled-in default %q", info.RelayURL, relay.DefaultRelayURL)
	}
	if info.SelfHosted {
		t.Error("a fresh instance reports a self-hosted relay")
	}
}

// TestActivateReturnsAUsablePhrase checks that activation hands back a phrase
// that decodes and stores the matching secret.
func TestActivateReturnsAUsablePhrase(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil)
	if code != http.StatusOK {
		t.Fatalf("activate answered %d: %s", code, body)
	}
	var out struct {
		Phrase string      `json:"phrase"`
		Info   ConnectInfo `json:"info"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Fields(out.Phrase)); got != seedphrase.WordCount {
		t.Fatalf("phrase has %d words, want %d: %q", got, seedphrase.WordCount, out.Phrase)
	}
	if _, err := seedphrase.Decode(out.Phrase); err != nil {
		t.Fatalf("the returned phrase does not decode: %v", err)
	}
	if !out.Info.Active {
		t.Error("info reports inactive right after activating")
	}
	if stored, _ := a.Accounts.Get(relay.SeedAccountService); stored == "" {
		t.Error("no secret was stored")
	}
}

// TestActivateRefusesToReplaceAnExistingPhrase guards the instances already
// joined with the old phrase.
func TestActivateRefusesToReplaceAnExistingPhrase(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if code, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatalf("first activate answered %d: %s", code, body)
	}
	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil)
	if code != http.StatusConflict {
		t.Fatalf("second activate answered %d, want %d", code, http.StatusConflict)
	}
}

// TestJoinAcceptsAPhraseFromElsewhere checks that a phrase minted on one
// instance leaves another holding the same secret.
func TestJoinAcceptsAPhraseFromElsewhere(t *testing.T) {
	first, firstApp := testServer(t)
	defer first.Close()
	second, secondApp := testServer(t)
	defer second.Close()

	_, body := postJSON(t, http.MethodPost, first.URL+"/api/connect/activate", nil)
	var out struct {
		Phrase string `json:"phrase"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}

	if code, body := postJSON(t, http.MethodPost, second.URL+"/api/connect/join", map[string]string{"phrase": out.Phrase}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, body)
	}

	a, _ := firstApp.Accounts.Get(relay.SeedAccountService)
	b, _ := secondApp.Accounts.Get(relay.SeedAccountService)
	if a == "" || a != b {
		t.Fatalf("the two instances hold different secrets:\n first: %q\nsecond: %q", a, b)
	}
}

// TestJoinRejectsABadPhraseAndSaysWhy checks that a mistyped phrase comes back
// as a reason plus details the browser can put into the user's language.
func TestJoinRejectsABadPhraseAndSaysWhy(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	const valid = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	for _, tc := range []struct {
		name     string
		phrase   string
		reason   string
		word     string
		position int
		count    int
	}{
		{
			name:     "a word that is not on the list",
			phrase:   "abandon abandon abandon abandon abandon abandon recieve abandon abandon abandon abandon about",
			reason:   "unknown_word",
			word:     "recieve",
			position: 7,
		},
		{
			// Every word is real but the checksum fails.
			name:   "two words swapped",
			phrase: "about abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon",
			reason: "checksum",
		},
		{
			name:   "one word short",
			phrase: strings.Join(strings.Fields(valid)[:11], " "),
			reason: "word_count",
			count:  11,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/join", map[string]string{"phrase": tc.phrase})
			if code != http.StatusBadRequest {
				t.Fatalf("join answered %d, want %d: %s", code, http.StatusBadRequest, body)
			}
			var out struct {
				Error    string `json:"error"`
				Reason   string `json:"reason"`
				Word     string `json:"word"`
				Position int    `json:"position"`
				Count    int    `json:"count"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("the rejection is not JSON, so the browser cannot translate it: %v (%s)", err, body)
			}
			if out.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", out.Reason, tc.reason)
			}
			if out.Word != tc.word {
				t.Errorf("word = %q, want %q", out.Word, tc.word)
			}
			if out.Position != tc.position {
				t.Errorf("position = %d, want %d", out.Position, tc.position)
			}
			if out.Count != tc.count {
				t.Errorf("count = %d, want %d", out.Count, tc.count)
			}
			// The English sentence comes along for logs and plain-text readers.
			if out.Error == "" {
				t.Error("no error text alongside the reason")
			}
		})
	}
}

// TestRevealWithoutAPasswordJustAnswers covers an instance without a password,
// where there is nothing to re-enter.
func TestRevealWithoutAPasswordJustAnswers(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	_, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil)
	var minted struct {
		Phrase string `json:"phrase"`
	}
	if err := json.Unmarshal(body, &minted); err != nil {
		t.Fatal(err)
	}

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/reveal", nil)
	if code != http.StatusOK {
		t.Fatalf("reveal answered %d: %s", code, body)
	}
	var shown struct {
		Phrase string `json:"phrase"`
	}
	if err := json.Unmarshal(body, &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Phrase != minted.Phrase {
		t.Fatalf("reveal returned a different phrase:\nminted: %q\nshown:  %q", minted.Phrase, shown.Phrase)
	}
}

// TestRevealNeedsThePasswordEvenWithASession checks that a session alone does
// not show the phrase once a password is set, since the phrase reaches every
// instance in the group.
func TestRevealNeedsThePasswordEvenWithASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if _, body := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); len(body) == 0 {
		t.Fatal("activate returned nothing")
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.Create("test script")
	if err != nil {
		t.Fatal(err)
	}

	// The token passes the session guard, yet the password is still required.
	for _, c := range []struct {
		name string
		body map[string]string
		want int
	}{
		{"no password", nil, http.StatusForbidden},
		{"wrong password", map[string]string{"password": "not-it"}, http.StatusForbidden},
		{"right password", map[string]string{"password": "a-good-password"}, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if c.body != nil {
				_ = json.NewEncoder(&buf).Encode(c.body)
			}
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/connect/reveal", &buf)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+secret)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("reveal answered %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

func TestRevealWithNoPhraseIs404(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/reveal", nil); code != http.StatusNotFound {
		t.Fatalf("reveal on an instance with no phrase answered %d, want 404", code)
	}
}

// TestDeleteForgetsTheSecretAndIsIdempotent also covers a stale page's second
// click, which is not an error.
func TestDeleteForgetsTheSecretAndIsIdempotent(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatal("activate failed")
	}

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/connect", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete #%d answered %d, want 204", i+1, resp.StatusCode)
		}
	}
	if stored, _ := a.Accounts.Get(relay.SeedAccountService); stored != "" {
		t.Fatalf("the secret survived the delete: %q", stored)
	}
}

// TestRelayTargetDerivesRatherThanSendingTheSecret checks that the relay gets
// a derived key and never the secret, so its operator cannot rebuild a phrase.
func TestRelayTargetDerivesRatherThanSendingTheSecret(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatal("activate failed")
	}
	storedHex, _ := a.Accounts.Get(relay.SeedAccountService)

	url, key, frameKey := relayTarget(a)
	if url != relay.DefaultRelayURL {
		t.Errorf("url = %q, want %q", url, relay.DefaultRelayURL)
	}
	if key == storedHex {
		t.Fatal("the key sent to the relay is the stored secret")
	}
	if len(key) < 32 {
		t.Errorf("derived key is %d characters, below the relay's own minimum", len(key))
	}

	// The relay gets key in every hello frame, so the frame key must be
	// neither equal to it nor derivable from it.
	if len(frameKey) != 32 {
		t.Fatalf("frame key is %d bytes, want 32", len(frameKey))
	}
	if hex.EncodeToString(frameKey) == key {
		t.Error("the frame key is the key handed to the relay")
	}
	if hex.EncodeToString(frameKey) == storedHex {
		t.Error("the frame key is the stored secret")
	}
	if bytes.Equal(frameKey, relay.FrameKeyFromRelayKey(key)) {
		t.Error("the frame key is derivable from the key the relay is already given")
	}
}

// TestRelayTargetHonoursASelfHostedOverride checks that the same phrase can be
// pointed at somebody's own relay.
func TestRelayTargetHonoursASelfHostedOverride(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatal("activate failed")
	}
	cfg := a.Settings.Get()
	cfg.RelayURL = "wss://relay.example.com"
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	url, key, frameKey := relayTarget(a)
	if url != "wss://relay.example.com" {
		t.Errorf("url = %q, want the override", url)
	}
	if key == "" {
		t.Error("no key with an override set")
	}
	// Frames are still sealed with an override.
	if len(frameKey) != 32 {
		t.Errorf("frame key is %d bytes with an override set, want 32", len(frameKey))
	}
}

// TestOwnRelayWithNoAddressDialsNothing checks that choosing an own relay
// without an address dials nothing rather than falling back to the project
// relay.
func TestOwnRelayWithNoAddressDialsNothing(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatal("activate failed")
	}
	cfg := a.Settings.Get()
	cfg.RelayMode = settings.RelayModeOwn
	cfg.RelayURL = ""
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	url, key, _ := relayTarget(a)
	if url != "" {
		t.Errorf("url = %q, want nothing dialled; own relay is selected with no address", url)
	}
	if key != "" {
		t.Errorf("key = %q, want none: there is nowhere to send it", key)
	}

	// Once an address is given it is dialled.
	cfg.RelayURL = "wss://relay.example.com"
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if url, _, _ := relayTarget(a); url != "wss://relay.example.com" {
		t.Errorf("url = %q, want the address that was just set", url)
	}
}
