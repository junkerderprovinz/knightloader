package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const pairingPassword = "a-good-password"

// pairingServer is a test server with the login password pairing needs, and
// an API token its calls sign in with. The relay is off, so nothing dials the
// project relay.
func pairingServer(t *testing.T) (*httptest.Server, *app.App, string) {
	t.Helper()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	cfg := a.Settings.Get()
	cfg.RelayMode = settings.RelayModeOff
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", pairingPassword); err != nil {
		t.Fatal(err)
	}
	_, token, err := a.APITokens.Create("pairing test")
	if err != nil {
		t.Fatal(err)
	}
	return srv, a, token
}

// call sends a JSON body signed in with token and returns the status and the
// raw response.
func call(t *testing.T, token, method, url string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func connectInfoOf(t *testing.T, token, base string) ConnectInfo {
	t.Helper()
	code, body := call(t, token, http.MethodGet, base+"/api/connect", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/connect answered %d: %s", code, body)
	}
	var info ConnectInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatal(err)
	}
	return info
}

func activate(t *testing.T, token, base string) string {
	t.Helper()
	code, body := call(t, token, http.MethodPost, base+"/api/connect/activate", nil)
	if code != http.StatusOK {
		t.Fatalf("activate answered %d: %s", code, body)
	}
	var out struct {
		Phrase string `json:"phrase"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Phrase
}

func TestConnectStartsInactive(t *testing.T) {
	t.Parallel()
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
	if info.Members == nil {
		t.Error("members is null rather than an empty list")
	}
}

// TestConnectNamesTheProjectRelayInEveryMode checks that the project relay's
// address is answered while another relay, or none, is in use, since the
// relay card names it before anybody switches to it.
func TestConnectNamesTheProjectRelayInEveryMode(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	for _, mode := range []string{settings.RelayModeProject, settings.RelayModeOwn, settings.RelayModeOff} {
		cfg := a.Settings.Get()
		cfg.RelayMode = mode
		cfg.RelayURL = "wss://relay.example.com"
		if _, err := a.Settings.Set(cfg); err != nil {
			t.Fatal(err)
		}
		resp, err := http.Get(srv.URL + "/api/connect")
		if err != nil {
			t.Fatal(err)
		}
		var info ConnectInfo
		err = json.NewDecoder(resp.Body).Decode(&info)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if info.ProjectRelayURL != relay.DefaultRelayURL {
			t.Errorf("in %s mode projectRelayUrl = %q, want %q", mode, info.ProjectRelayURL, relay.DefaultRelayURL)
		}
	}
}

// TestPairingNeedsALoginPassword checks that without a login password the
// phrase can be neither generated, entered nor shown, and that the refusal
// carries a code the page can put into words.
func TestPairingNeedsALoginPassword(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	for _, c := range []struct {
		path string
		body any
	}{
		{"/api/connect/activate", nil},
		{"/api/connect/join", map[string]string{"phrase": phrase}},
		{"/api/connect/reveal", map[string]string{"password": ""}},
	} {
		code, body := postJSON(t, http.MethodPost, srv.URL+c.path, c.body)
		var out struct{ Code string }
		_ = json.Unmarshal(body, &out)
		if code != http.StatusForbidden || out.Code != "needsPassword" {
			t.Errorf("%s answered %d %s, want %d needsPassword", c.path, code, body, http.StatusForbidden)
		}
	}
	if stored, _ := a.Accounts.Get(relay.SeedAccountService); stored != "" {
		t.Fatalf("a refused call stored a secret: %q", stored)
	}
}

// TestActivateReturnsAUsablePhrase checks that activation hands back a phrase
// that decodes and stores the matching secret.
func TestActivateReturnsAUsablePhrase(t *testing.T) {
	t.Parallel()
	srv, a, token := pairingServer(t)

	code, body := call(t, token, http.MethodPost, srv.URL+"/api/connect/activate", nil)
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
	t.Parallel()
	srv, _, token := pairingServer(t)

	activate(t, token, srv.URL)
	code, body := call(t, token, http.MethodPost, srv.URL+"/api/connect/activate", nil)
	var out struct{ Code string }
	_ = json.Unmarshal(body, &out)
	if code != http.StatusConflict || out.Code != "phraseExists" {
		t.Fatalf("second activate answered %d %s, want %d phraseExists", code, body, http.StatusConflict)
	}
}

// TestJoinAcceptsAPhraseFromElsewhere checks that a phrase generated on one
// instance leaves another holding the same secret.
func TestJoinAcceptsAPhraseFromElsewhere(t *testing.T) {
	t.Parallel()
	first, firstApp, firstToken := pairingServer(t)
	second, secondApp, secondToken := pairingServer(t)

	phrase := activate(t, firstToken, first.URL)
	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/join", map[string]string{"phrase": phrase}); code != http.StatusOK {
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
	t.Parallel()
	srv, _, token := pairingServer(t)

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
			code, body := call(t, token, http.MethodPost, srv.URL+"/api/connect/join", map[string]string{"phrase": tc.phrase})
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

// TestRevealNeedsThePasswordEvenWithASession checks that being signed in is
// not enough to show the phrase again, since the phrase reaches every
// instance in the group.
func TestRevealNeedsThePasswordEvenWithASession(t *testing.T) {
	t.Parallel()
	srv, _, token := pairingServer(t)
	minted := activate(t, token, srv.URL)

	for _, c := range []struct {
		name string
		body any
		want int
	}{
		{"no password", nil, http.StatusForbidden},
		{"wrong password", map[string]string{"password": "not-it"}, http.StatusForbidden},
		{"right password", map[string]string{"password": pairingPassword}, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, out := call(t, token, http.MethodPost, srv.URL+"/api/connect/reveal", c.body)
			if code != c.want {
				t.Fatalf("reveal answered %d, want %d: %s", code, c.want, out)
			}
			var answer struct {
				Code   string
				Phrase string
			}
			_ = json.Unmarshal(out, &answer)
			if c.want == http.StatusForbidden && answer.Code != "passwordWrong" {
				t.Errorf("the refusal carries the code %q, want passwordWrong", answer.Code)
			}
			if c.want == http.StatusOK && answer.Phrase != minted {
				t.Errorf("reveal returned a different phrase:\nminted: %q\nshown:  %q", minted, answer.Phrase)
			}
		})
	}
}

func TestRevealWithNoPhraseIs404(t *testing.T) {
	t.Parallel()
	srv, _, token := pairingServer(t)

	code, _ := call(t, token, http.MethodPost, srv.URL+"/api/connect/reveal", map[string]string{"password": pairingPassword})
	if code != http.StatusNotFound {
		t.Fatalf("reveal on an instance with no phrase answered %d, want 404", code)
	}
}

// TestDeleteForgetsTheSecretAndIsIdempotent also covers a stale page's second
// click, which is not an error.
func TestDeleteForgetsTheSecretAndIsIdempotent(t *testing.T) {
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)

	for i := 0; i < 2; i++ {
		if code, body := call(t, token, http.MethodDelete, srv.URL+"/api/connect", nil); code != http.StatusNoContent {
			t.Fatalf("delete #%d answered %d, want 204: %s", i+1, code, body)
		}
	}
	if stored, _ := a.Accounts.Get(relay.SeedAccountService); stored != "" {
		t.Fatalf("the secret survived the delete: %q", stored)
	}
}

// TestConnectCountsFromWhenThisInstanceJoined checks that a new group reports
// how long it has waited with nobody seen, and that leaving starts over.
func TestConnectCountsFromWhenThisInstanceJoined(t *testing.T) {
	t.Parallel()
	srv, _, token := pairingServer(t)

	if info := connectInfoOf(t, token, srv.URL); info.JoinedAgo != 0 || info.MemberSeen {
		t.Fatalf("outside a group: joinedAgo %d, memberSeen %v", info.JoinedAgo, info.MemberSeen)
	}
	activate(t, token, srv.URL)
	info := connectInfoOf(t, token, srv.URL)
	if !info.Active || info.JoinedAgo < 0 || info.JoinedAgo > 5 {
		t.Fatalf("right after generating: active %v, joinedAgo %d", info.Active, info.JoinedAgo)
	}
	if info.MemberSeen || len(info.Members) != 0 {
		t.Fatalf("a new group reports a member: %+v", info)
	}
	if info.Name == "" {
		t.Error("no name for this instance")
	}

	call(t, token, http.MethodDelete, srv.URL+"/api/connect", nil)
	if info := connectInfoOf(t, token, srv.URL); info.Active || info.JoinedAgo != 0 {
		t.Fatalf("after leaving: active %v, joinedAgo %d", info.Active, info.JoinedAgo)
	}
}

// TestConnectRemembersThatAMemberCame checks that an instance seen on the
// relay is listed while it is there and counted as having come after it
// leaves, which tells "gone for now" from "never came".
func TestConnectRemembersThatAMemberCame(t *testing.T) {
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)

	// After activate, whose applyRelay would replace the transport.
	a.Federation.SetRelay(&relayOnlyPeer{sibs: []relay.Announce{
		{InstanceID: "id-office", Name: "office"},
		{InstanceID: "id-phone", Name: "KnightLoader app", Client: true},
	}})
	info := connectInfoOf(t, token, srv.URL)
	if len(info.Members) != 1 || info.Members[0].ID != "id-office" || info.Members[0].Name != "office" {
		t.Fatalf("members = %+v, want the office instance and not the phone", info.Members)
	}
	if !info.MemberSeen {
		t.Error("memberSeen is false with a member there")
	}

	a.Federation.SetRelay(nil)
	info = connectInfoOf(t, token, srv.URL)
	if len(info.Members) != 0 || !info.MemberSeen {
		t.Fatalf("after the member left: members %+v, memberSeen %v", info.Members, info.MemberSeen)
	}
}

// TestAGroupWithoutAJoiningTimeCountsAsJoinedAtStart checks that a stored
// secret with no joining time counts as joined when the instance starts, so
// its page waits a minute before it says nobody came.
func TestAGroupWithoutAJoiningTimeCountsAsJoinedAtStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	secret, _, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.Set(relay.SeedAccountService, hex.EncodeToString(secret)); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(Handler(a))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/connect")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info ConnectInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if !info.Active || info.JoinedAgo > 5 || info.MemberSeen {
		t.Fatalf("at start: active %v, joinedAgo %d, memberSeen %v", info.Active, info.JoinedAgo, info.MemberSeen)
	}
	if st, _ := a.Federation.Group(nil, time.Now()); st.JoinedAt.IsZero() {
		t.Fatal("the joining time was not stored")
	}
}

// TestRelayTargetDerivesRatherThanSendingTheSecret checks that the relay gets
// a derived key and never the secret, so its operator cannot rebuild a phrase.
func TestRelayTargetDerivesRatherThanSendingTheSecret(t *testing.T) {
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)
	cfg := a.Settings.Get()
	cfg.RelayMode = settings.RelayModeProject
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
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
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)
	cfg := a.Settings.Get()
	cfg.RelayMode = settings.RelayModeOwn
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
	t.Parallel()
	srv, a, token := pairingServer(t)
	activate(t, token, srv.URL)
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
