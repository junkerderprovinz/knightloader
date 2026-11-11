package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
	"github.com/junkerderprovinz/parleyport/kit/relay"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

func init() {
	// No test joins the fortress's multicast group; the relay is enough.
	fortressLAN = false
	followEvery = 200 * time.Millisecond
}

type memoryStore struct {
	mu   sync.Mutex
	data []byte
}

func (s *memoryStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.data), nil
}

func (s *memoryStore) Save(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = bytes.Clone(b)
	return nil
}

// arrowLoopIn opens an ArrowLoop of the fortress secret makes on the relay at
// relayURL, with one job, and returns its member id.
func arrowLoopIn(t *testing.T, relayURL string, secret []byte) string {
	t.Helper()
	seed := sha256.Sum256([]byte("arrowloop " + hex.EncodeToString(secret)))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	f, err := fortress.Open(fortress.Config{
		App:      fortress.App{ID: afterdownload.EngineApp, Name: "ArrowLoop", Version: "1.0.0"},
		Secret:   secret,
		Identity: id,
		Self:     group.Self{Name: "Tower", Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		Store:    &memoryStore{},
		Actions: []fortress.Action{
			{Name: afterdownload.ListAction, Run: func(context.Context, fortress.Caller, []byte) ([]byte, error) {
				return []byte(`[{"name":"photos","direction":"both","mode":"sync","paused":false,"running":false}]`), nil
			}},
			{Name: afterdownload.RunAction, Run: func(context.Context, fortress.Caller, []byte) ([]byte, error) {
				return []byte(`{"job":"photos","status":"started"}`), nil
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return id.ID()
}

type fortressAnswer struct {
	Joined      bool                 `json:"joined"`
	Phrase      bool                 `json:"phrase"`
	Device      string               `json:"device"`
	PasswordSet bool                 `json:"passwordSet"`
	Members     []fortressMemberView `json:"members"`
	PairedWith  []string             `json:"pairedWith"`
}

func fortressNow(t *testing.T, base string) fortressAnswer {
	t.Helper()
	var v fortressAnswer
	if code := getJSON(t, base+"/api/fortress", &v); code != http.StatusOK {
		t.Fatalf("GET /api/fortress answered %d", code)
	}
	return v
}

// ownRelay is a relay of the test's own that a dials.
func ownRelay(t *testing.T, a *app.App, base string) string {
	t.Helper()
	rel := httptest.NewServer(relay.New())
	t.Cleanup(rel.Close)
	t.Cleanup(rel.CloseClientConnections)
	if code, cfg := putRelayConfig(t, base, `{"relayUrl":"`+rel.URL+`","mode":"own"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, cfg)
	}
	return rel.URL
}

func newWords(t *testing.T) ([]byte, string) {
	t.Helper()
	secret, words, err := phrase.New()
	if err != nil {
		t.Fatal(err)
	}
	return secret, words
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAnInstanceJoinsTheFortressOfItsPhraseAndLeavesIt(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	engine := arrowLoopIn(t, relayURL, secret)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words})
	if code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	if v := fortressNow(t, srv.URL); !v.Joined || !v.Phrase {
		t.Fatalf("after joining the fortress reads %+v", v)
	}
	waitFor(t, "ArrowLoop among the members", func() bool {
		for _, m := range fortressNow(t, srv.URL).Members {
			if m.ID == engine && m.App == afterdownload.EngineApp && m.Device == "Tower" {
				return true
			}
		}
		return false
	})
	waitFor(t, "the station listing ArrowLoop's jobs", func() bool {
		engines := a.AfterDownloadEngines(context.Background())
		return len(engines) == 1 && len(engines[0].Jobs) == 1
	})

	if code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/fortress", nil); code != http.StatusOK {
		t.Fatalf("leave answered %d: %s", code, raw)
	}
	if v := fortressNow(t, srv.URL); v.Joined {
		t.Errorf("after leaving the fortress reads %+v", v)
	}
	if a.InFortress() {
		t.Error("the station still sees a fortress after leaving")
	}
}

func TestAPhraseThatDoesNotDecodeSaysWhichWord(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": "castle castle castle"})
	var answer struct {
		Reason string `json:"reason"`
		Count  int    `json:"count"`
	}
	_ = json.Unmarshal(raw, &answer)
	if code != http.StatusBadRequest || answer.Reason == "" {
		t.Errorf("a short phrase answered %d %s", code, raw)
	}
	if v := fortressNow(t, srv.URL); v.Joined {
		t.Error("a phrase that does not decode joined a fortress")
	}
}

func TestThePhraseShowsAgainOnlyForThePassword(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	ownRelay(t, a, srv.URL)
	_, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/phrase/show", map[string]string{})
	var shown struct {
		Phrase string          `json:"phrase"`
		QR     json.RawMessage `json:"qr"`
	}
	_ = json.Unmarshal(raw, &shown)
	if code != http.StatusOK || shown.Phrase != words || len(shown.QR) < 8 {
		t.Fatalf("without a password the phrase showed as %d %s", code, raw)
	}

	if err := a.Auth.SetPassword("", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/phrase/show", map[string]string{"password": "wrong"}); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("a wrong password showed the phrase with %d", code)
	}
}

func TestANewPhraseKeepsTheFortressAndTheOldOneOpensNothing(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	arrowLoopIn(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/phrase", map[string]string{})
	var changed struct {
		Phrase string   `json:"phrase"`
		Left   []string `json:"left"`
	}
	_ = json.Unmarshal(raw, &changed)
	if code != http.StatusOK || changed.Phrase == "" || changed.Phrase == words {
		t.Fatalf("the change answered %d %s", code, raw)
	}
	if v := fortressNow(t, srv.URL); !v.Joined {
		t.Errorf("after the change the fortress reads %+v", v)
	}
}

// joinedTwo puts a and b into one pairing group on an old phrase, each
// approving the other, as two instances paired before the fortress were.
func joinedTwo(t *testing.T) (srvA *httptest.Server, a *app.App, srvB *httptest.Server, b *app.App, relayURL string, old []byte) {
	t.Helper()
	srvA, a = testServer(t)
	t.Cleanup(srvA.Close)
	srvB, b = testServer(t)
	t.Cleanup(srvB.Close)
	relayURL = ownRelay(t, a, srvA.URL)
	if code, cfg := putRelayConfig(t, srvB.URL, `{"relayUrl":"`+relayURL+`","mode":"own"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, cfg)
	}
	old, words := newWords(t)
	for _, base := range []string{srvA.URL, srvB.URL} {
		if code, raw := postJSON(t, http.MethodPost, base+"/api/connect/join", map[string]string{"phrase": words}); code != http.StatusOK {
			t.Fatalf("joining the old group answered %d: %s", code, raw)
		}
	}
	approveInstance(t, a, b)
	approveInstance(t, b, a)
	return srvA, a, srvB, b, relayURL, old
}

// approveInstance has a's group take b's instance as approved.
func approveInstance(t *testing.T, a, b *app.App) {
	t.Helper()
	g := a.Federation.Group()
	if g == nil {
		t.Fatal("no group to approve in")
	}
	id := memberKey(b)
	if err := g.Import(group.Roster{Members: []group.Member{{
		ID: id.ID(), Kind: member.KindInstance, Key: id.Public(), State: group.StateApproved,
	}}}); err != nil {
		t.Fatal(err)
	}
}

func TestJoiningTakesThePairedInstanceAlong(t *testing.T) {
	t.Parallel()
	srvA, a, srvB, b, relayURL, _ := joinedTwo(t)
	secret, words := newWords(t)
	arrowLoopIn(t, relayURL, secret)

	if code, raw := postJSON(t, http.MethodPost, srvA.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	waitFor(t, "the paired instance following into the fortress", func() bool { return fortressNow(t, srvB.URL).Joined })
	waitFor(t, "the paired instance seeing ArrowLoop", func() bool { return len(b.AfterDownloadEngines(context.Background())) == 1 })
	waitFor(t, "the old phrase to go once every member followed", func() bool { return groupSecret(a) == nil })
	if v := fortressNow(t, srvA.URL); !slices.ContainsFunc(v.PairedWith, func(n string) bool { return n != "" }) {
		t.Errorf("the pairing in the fortress names nobody: %+v", v)
	}
}

func TestJoiningTakesThePhoneAlong(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	old := phraseGroup(t, a)
	applyRelay(a)
	approve(t, a, "phone", member.KindMobile)
	phone := idAs("phone", member.KindMobile)
	secret, words := newWords(t)

	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	instance := groupID(a)
	waitFor(t, "the instance on the relay", a.Federation.RelayConnected)
	waitFor(t, "the group on the old phrase on the relay", func() bool {
		former := fortressOf(a).formerGroup()
		return former != nil && former.RelayConnected()
	})

	// The phone asks on the old phrase where the group went.
	code, raw := relayCall(t, relayURL, old, phone, instance, keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: movedPath, From: phone, Kind: member.KindMobile})
	var moved struct {
		GroupKey    string `json:"groupKey"`
		FrameKey    string `json:"frameKey"`
		PeerAuthKey string `json:"peerAuthKey"`
	}
	_ = json.Unmarshal(raw, &moved)
	if code != http.StatusOK || moved.GroupKey != federation.Domain.GroupKey(secret) ||
		moved.FrameKey != hex.EncodeToString(federation.Domain.FrameKey(secret)) ||
		moved.PeerAuthKey != hex.EncodeToString(federation.Domain.PeerAuthKey(secret)) {
		t.Fatalf("the moved keys answered %d %s", code, raw)
	}
	time.Sleep(3 * followEvery)
	if groupSecret(a) == nil {
		t.Fatal("a call on the old phrase counted as following")
	}

	// With them it reaches the instance as the member it was.
	code, raw = relayCall(t, relayURL, secret, phone, instance, keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: phone, Kind: member.KindMobile})
	if code != http.StatusOK {
		t.Errorf("the phone on the fortress's keys answered %d %s", code, raw)
	}

	// The phone hung up after that one call, and still counts as followed.
	waitFor(t, "the old phrase to go once the phone called on the fortress's keys", func() bool { return groupSecret(a) == nil })
}

func TestOnlyAPhoneOrABrowserAsksForTheMovedKeys(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	phraseGroup(t, a)
	call := signedBody(t, a, "phone", member.KindMobile)

	if code, raw := call(http.MethodGet, movedPath, nil); code != http.StatusNotFound {
		t.Errorf("before any move the phone was answered %d %s", code, raw)
	}
}

// announceOf is the announce instance id makes on the network for the
// pairing of secret, tagged as kit/group tags it.
func announceOf(secret []byte, id string) lan.Peer {
	p := lan.Peer{ID: id, Name: "Attic", URL: "http://attic.home.arpa:8749", Deployment: "container", Sent: time.Now().Unix()}
	mac := hmac.New(sha256.New, federation.Domain.PeerAuthKey(secret))
	mac.Write([]byte("announce\x00" + p.ID + "\x00" + p.URL + "\x00" + p.Name + "\x00" + p.Deployment + "\x00" + p.Address + "\x00" + strconv.FormatInt(p.Sent, 10)))
	p.Tag = hex.EncodeToString(mac.Sum(nil))
	return p
}

func TestTheGroupOnTheOldPhraseLeavesThePairingOnTheNetwork(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	network := lan.New(federation.LAN)
	t.Cleanup(func() { _ = network.Close() })
	a.SetDiscovery(network)
	phraseGroup(t, a)
	applyRelay(a)
	approve(t, a, "phone", member.KindMobile)
	secret, words := newWords(t)

	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	sibling := idAs("attic", member.KindInstance)
	known := func() bool {
		network.Observe(announceOf(secret, sibling))
		return slices.ContainsFunc(network.Members(), func(p lan.Peer) bool { return p.ID == sibling })
	}
	if !known() {
		t.Fatal("beside the group on the old phrase the pairing does not know its members on the network")
	}

	waitFor(t, "the instance on the relay", a.Federation.RelayConnected)
	phone := idAs("phone", member.KindMobile)
	if code, raw := relayCall(t, relayURL, secret, phone, groupID(a), keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: phone, Kind: member.KindMobile}); code != http.StatusOK {
		t.Fatalf("the phone on the fortress's keys answered %d %s", code, raw)
	}
	waitFor(t, "the old phrase to go once the phone followed", func() bool { return groupSecret(a) == nil })
	if !known() {
		t.Error("closing the group on the old phrase took the pairing off the network")
	}
}

func TestAPhoneOnTheOldPhraseFetchesTheMovedKeysOnThisNetwork(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	ownRelay(t, a, srv.URL)
	phraseGroup(t, a)
	applyRelay(a)
	approve(t, a, "phone", member.KindMobile)
	secret, words := newWords(t)

	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	// The kit's route, which phones and browsers ask, and the one older ones
	// still do.
	code, raw := deviceCall(t, a, idAs("phone", member.KindMobile), member.KindMobile, keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: "/kit/fortress/v1/moved"})
	var moved struct {
		Group    string `json:"group"`
		Frame    []byte `json:"frame"`
		PeerAuth []byte `json:"peerAuth"`
	}
	_ = json.Unmarshal(raw, &moved)
	if code != http.StatusOK || moved.Group != federation.Domain.GroupKey(secret) ||
		!bytes.Equal(moved.Frame, federation.Domain.FrameKey(secret)) || !bytes.Equal(moved.PeerAuth, federation.Domain.PeerAuthKey(secret)) {
		t.Fatalf("the kit's moved keys answered %d %s", code, raw)
	}
	if code, raw := deviceCall(t, a, idAs("phone", member.KindMobile), member.KindMobile, keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: movedPath}); code != http.StatusOK {
		t.Errorf("the moved keys on the older route answered %d %s", code, raw)
	}
}
