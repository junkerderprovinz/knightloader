package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
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

// Activate is the whole first-run flow: it must hand back a usable phrase
// and leave the instance holding the matching secret.
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

// Minting a second phrase over the first would orphan every instance that
// joined with the old one, silently.
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

// The join half: a phrase minted on one instance has to be accepted by
// another and leave both holding the same secret - that is the entire
// feature.
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

// A mistyped phrase must come back as a REASON plus its specifics, not as a
// sentence. Whoever mistyped the word is reading a UI in their own language
// and a sentence written in Go can only be in one, so the browser writes the
// message and this only has to say precisely what was wrong.
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
			// Every word real, the phrase not: what the checksum is for.
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
			// The English sentence rides along for logs and for anything
			// reading the body as text; it must not be the only thing there.
			if out.Error == "" {
				t.Error("no error text alongside the reason")
			}
		})
	}
}

// With no password there is nothing to re-enter, so reveal simply answers -
// the consequence of jdp's call that activation warns rather than blocks.
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

// The security property that matters: once a password exists, holding a
// session is not enough to see the phrase again, because the phrase reaches
// every instance in the group and not just this one.
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

	// Authenticated as far as the session guard is concerned - a token is
	// exactly as good as a cookie there - and still refused without the
	// password itself.
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

// Leaving has to actually forget the secret, and has to stay quiet when
// there was nothing to forget - a stale page's second click is not an error.
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

// relayTarget is what turns a stored secret into a relay to dial. The key
// it produces must be the derived one, never the secret itself - that is
// the property keeping the relay operator from reconstructing a phrase.
func TestRelayTargetDerivesRatherThanSendingTheSecret(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatal("activate failed")
	}
	storedHex, _ := a.Accounts.Get(relay.SeedAccountService)

	url, key := relayTarget(a)
	if url != relay.DefaultRelayURL {
		t.Errorf("url = %q, want %q", url, relay.DefaultRelayURL)
	}
	if key == storedHex {
		t.Fatal("the key sent to the relay IS the stored secret")
	}
	if len(key) < 32 {
		t.Errorf("derived key is %d characters, below the relay's own minimum", len(key))
	}
}

// An override points the same phrase at somebody's own relay - the exit
// path that keeps this from being a service nobody can leave.
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

	url, key := relayTarget(a)
	if url != "wss://relay.example.com" {
		t.Errorf("url = %q, want the override", url)
	}
	if key == "" {
		t.Error("no key with an override set")
	}
}
