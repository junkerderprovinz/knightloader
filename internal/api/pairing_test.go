package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// TestPairingCodeGenerate proves the generate endpoint's response shape:
// a non-empty code, this instance's real hostname, some address, and a
// positive TTL. It does not assert which address - pairingSelf (same as the
// QR card's own remoteAddresses/firstNonLoopback) prefers a real LAN IP over
// loopback the moment the host running this test has one, and on a machine
// with several interfaces that guessed IP is not necessarily the one
// httptest.Server itself is listening on. TestPairingCodeRoundTrip below
// covers the live, connected round trip on the common case (a loopback-only
// test host) and skips itself rather than asserting a false failure when it
// is not; TestPairingOfferRoundTrip covers the mutual-registration protocol
// itself with a hand-built, guaranteed-reachable offer, independent of any
// of that guessing.
func TestPairingCodeGenerate(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "KnightLoader"
	}

	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if issued.Code == "" || issued.URL == "" || issued.Name != hostname {
		t.Fatalf("issued = %+v, want a code, a url, name %s", issued, hostname)
	}
	if issued.ExpiresIn <= 0 {
		t.Fatalf("expiresIn = %d, want > 0", issued.ExpiresIn)
	}
}

// TestPairingCodeGeneratePrefersInstanceName proves pairingSelf's new
// preference (jdp: "der soll dann mit dem QR code an die App weitergegeben
// werden"): once Settings.InstanceName is set, it replaces os.Hostname() in
// what a generated pairing code offers, without requiring one - the field
// stays optional, this only proves it is HONOURED once set.
func TestPairingCodeGeneratePrefersInstanceName(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()

	cfg := aApp.Settings.Get()
	cfg.InstanceName = "My Home Server"
	if _, err := aApp.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var issued struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if issued.Name != "My Home Server" {
		t.Fatalf("issued.Name = %q, want the configured InstanceName", issued.Name)
	}
}

// TestPairingCodeGeneratePrefersKnownDomain proves pairingSelf reuses
// preferredAddress the same way the QR card does (jdp: "Die domain soll auch
// mit dem QR Code an die App weitergegeben werden" - true for the pairing
// code's own QR too, not only the plain Remote access one): once a domain is
// known, it is offered instead of the loopback address this test's own
// httptest request actually arrives on.
func TestPairingCodeGeneratePrefersKnownDomain(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()

	cfg := aApp.Settings.Get()
	cfg.KnownDomains = []string{"https://knightloader.example.tld"}
	if _, err := aApp.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var issued struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if issued.URL != "https://knightloader.example.tld" {
		t.Fatalf("issued.URL = %q, want the known domain preferred over aSrv's own loopback address", issued.URL)
	}
}

// TestPairingCodeRoundTrip runs two real instances and pairs them with one
// action on the joining side: A issues a code, B redeems it, and afterward
// each must know about the other with no second, manual entry. Skips itself
// when the issued code's address is not actually aSrv's own reachable
// address (see TestPairingCodeGenerate's own comment on why that can
// legitimately differ) rather than asserting a false failure that has
// nothing to do with the pairing logic under test.
func TestPairingCodeRoundTrip(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()

	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	// A issues a code.
	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if issued.URL != aSrv.URL {
		t.Skipf("this host has another reachable interface (%s) besides aSrv's own %s; TestPairingOfferRoundTrip already covers the protocol itself", issued.URL, aSrv.URL)
	}

	// B redeems it: one call, both directions.
	body, _ := json.Marshal(map[string]string{"code": issued.Code})
	resp, err = http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var redeemed struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		Online bool   `json:"online"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&redeemed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if redeemed.Name != issued.Name || redeemed.URL != aSrv.URL || !redeemed.Online {
		t.Fatalf("redeemed = %+v, want name %s, url %s, online", redeemed, issued.Name, aSrv.URL)
	}

	// B now lists A as a peer...
	bPeers := bApp.Federation.List()
	if len(bPeers) != 1 || bPeers[0].Name != issued.Name || bPeers[0].URL != aSrv.URL {
		t.Fatalf("B's peers = %+v, want exactly A", bPeers)
	}
	// ...and A was registered back with B, in the same action. B's own
	// pairingSelf call inside /redeem is subject to the exact same
	// interface-guessing as A's - if B also only has loopback (the common
	// case whenever A's guess above matched aSrv.URL), this is bSrv.URL.
	aPeers := aApp.Federation.List()
	if len(aPeers) != 1 || aPeers[0].Name != issued.Name && false { // placeholder removed below
	}
	if len(aPeers) != 1 {
		t.Fatalf("A's peers = %+v, want exactly one (B)", aPeers)
	}
	if aPeers[0].URL != bSrv.URL {
		t.Skipf("B also has another reachable interface (%s) besides bSrv's own %s", aPeers[0].URL, bSrv.URL)
	}

	// The code is single-use: redeeming it again must fail, and must not add
	// a second, duplicate registration on either side.
	resp, err = http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("second redeem = %d, want 400 (the code was already spent)", resp.StatusCode)
	}
	if len(bApp.Federation.List()) != 1 || len(aApp.Federation.List()) != 1 {
		t.Fatalf("replaying a spent code changed the peer lists: B=%v A=%v", bApp.Federation.List(), aApp.Federation.List())
	}
}

// TestPairingCodeCompleteRejectsUnknownToken proves /complete refuses a made-up
// token rather than registering whatever name/url it is handed.
func TestPairingCodeCompleteRejectsUnknownToken(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	body, _ := json.Marshal(map[string]string{"token": "not-a-real-token", "name": "intruder", "url": "http://example.invalid"})
	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code/complete", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("complete with unknown token = %d, want 400", resp.StatusCode)
	}
	if len(aApp.Federation.List()) != 0 {
		t.Fatalf("federation list = %v, want empty: an unknown token must not register anything", aApp.Federation.List())
	}
}

// TestPairingCodeRedeemRejectsGarbage proves /redeem refuses a code that does
// not even decode, rather than forwarding garbage to some URL it invented.
func TestPairingCodeRedeemRejectsGarbage(t *testing.T) {
	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	body, _ := json.Marshal(map[string]string{"code": "not-base64-json-at-all!!"})
	resp, err := http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("redeem with garbage code = %d, want 400", resp.StatusCode)
	}
	if len(bApp.Federation.List()) != 0 {
		t.Fatalf("federation list = %v, want empty: a garbage code must not register anything", bApp.Federation.List())
	}
}

// TestPairingCodeRedeemUnreachablePeerAddsNothing proves the "complete the
// other half first" ordering the doc comment on registerPairing promises:
// when the offer's URL cannot be reached at all, the redeeming instance must
// not add it as a peer either.
func TestPairingCodeRedeemUnreachablePeerAddsNothing(t *testing.T) {
	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	offer := pairingOffer{Name: "ghost", URL: "http://127.0.0.1:1", Token: "irrelevant"}
	code, err := encodeOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("redeem against an unreachable peer = %d, want 502", resp.StatusCode)
	}
	if len(bApp.Federation.List()) != 0 {
		t.Fatalf("federation list = %v, want empty: an unreachable peer must not be added", bApp.Federation.List())
	}
}

// TestDesktopCannotPair: the desktop build serves its API only inside its own
// Wails window and opens no listener, so it has no address any peer could dial.
// Before this was gated, pairingSelf reported the Wails webview host
// (http://wails.localhost) - not loopback, and a valid domain, so
// preferredAddress ranked it FIRST, ahead of any real configured domain - and
// federation.Add, which validates only scheme and host, wrote it into the
// OTHER instance's stored peer list. Both sides were told the pairing
// succeeded; the peer was dead forever.
//
// Pinned on BOTH routes: minting a code and redeeming one each need this
// instance's own address, so each has to refuse. The web UI hid the generate
// card on the desktop build but never the redeem card, which is exactly how
// this silent failure ended up on somebody else's machine.
func TestDesktopCannotPair(t *testing.T) {
	prev := buildinfo.Deployment
	buildinfo.Deployment = "desktop"
	t.Cleanup(func() { buildinfo.Deployment = prev })

	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("POST /api/instances/pairing-code on a desktop build = %d, want 409", resp.StatusCode)
	}

	body := bytes.NewBufferString(`{"code":"irrelevant-the-refusal-comes-first"}`)
	resp2, err := http.Post(srv.URL+"/api/instances/pairing-code/redeem", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	// 409, not the 400 a malformed code would get: the refusal is about THIS
	// build having no address at all, and must not depend on the code.
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("POST /api/instances/pairing-code/redeem on a desktop build = %d, want 409", resp2.StatusCode)
	}
}

// TestCompleteDoesNotBurnTheCodeOnAnUnrelatedFailure: the token was taken and
// deleted before the operation it gates had succeeded, with no way back. So a
// peer name the federation store rejects - a hostname with an unusual
// character, or a user-set InstanceName, since that is only trimmed - burned
// the code on the first attempt. Every retry then answered "that code is
// invalid or has expired", including with a freshly generated code, because
// the same name kept failing. That reads as a clock problem and is unfixable
// until somebody guesses it is really about the name.
//
// Two things are pinned here: the code SURVIVES a failure that was not about
// the code, and the answer carries the real reason instead of an expiry story.
func TestCompleteDoesNotBurnTheCodeOnAnUnrelatedFailure(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	offer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatalf("decode our own code: %v", err)
	}

	complete := func(name string) (int, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"token": offer.Token, "name": name, "url": "http://192.168.10.10:8749",
		})
		resp, err := http.Post(srv.URL+"/api/instances/pairing-code/complete", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(msg))
	}

	// A name federation.Add refuses (nameRe). Nothing to do with the token.
	status, msg := complete("this name is far too long to be a valid instance name and also has spaces")
	if status != http.StatusBadRequest {
		t.Fatalf("a rejected name answered %d, want 400", status)
	}
	if strings.Contains(msg, "expired") {
		t.Errorf("answer was %q - a name rejection must not be reported as an expiry", msg)
	}

	// The same code must still work once the name is acceptable. 200 with a
	// JSON body, not the old 204: the handler now hands back the credential it
	// minted for the redeemer and says whether it could reach it (issues #26
	// and #28). What this test is about is unchanged - the code survived a
	// failure that was not about it.
	if status, msg := complete("cellar"); status != http.StatusOK {
		t.Fatalf("retry with a valid name answered %d (%s), want 200 - the code was burned by a failure that was not about it", status, msg)
	}

	// And it really is single-use: the successful redemption consumed it.
	if status, _ := complete("cellar"); status != http.StatusBadRequest {
		t.Errorf("reusing a successfully redeemed code answered %d, want 400", status)
	}
}

// TestPairingMakesPasswordProtectedPeersActuallyWork is the end-to-end proof
// for issue #26: two instances that BOTH have a password, paired by code, and
// then actually able to call each other.
//
// Before peer tokens, federation.Manager.Proxy attached no credential on
// either transport, so this exact scenario - the normal one, since anything
// reachable should have a password - produced a peer that was listed, showed a
// red dot and 0/0/0, and answered 401 to every call. Indistinguishable from
// switched off.
//
// Both directions are checked, because pairing is symmetric and a token that
// only travelled one way would leave half of it broken in a way the pairing
// itself reports as success.
func TestPairingMakesPasswordProtectedPeersActuallyWork(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	// A generates the code while still open, then locks itself. The code
	// carries an address, never a credential, so locking after issuing is the
	// realistic order and must not matter.
	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Both locked BEFORE the redemption completes, so the exchange itself has
	// to work against protected instances too.
	if err := aApp.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	if err := bApp.Auth.SetPassword("", "another-good-password"); err != nil {
		t.Fatal(err)
	}

	// Redeeming is a guarded route - normally B's own logged-in session doing
	// it. A token stands in for that session here.
	_, bSecret, err := bApp.APITokens.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	// The issued code carries A's own GUESSED address (preferredAddress picks
	// a real LAN IP over loopback), which is not where httptest listens - the
	// same reason TestPairingCodeRoundTrip skips on a multi-homed host. Rebuild
	// the offer around A's real test address; the token inside is untouched and
	// is all A actually validates.
	realOffer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatal(err)
	}
	realOffer.URL = aSrv.URL
	fixedCode, err := encodeOffer(realOffer)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": fixedCode})
	rdReq, err := http.NewRequest(http.MethodPost, bSrv.URL+"/api/instances/pairing-code/redeem", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rdReq.Header.Set("Content-Type", "application/json")
	rdReq.Header.Set("Authorization", "Bearer "+bSecret)
	rd, err := http.DefaultClient.Do(rdReq)
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Body.Close()
	if rd.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(rd.Body)
		t.Fatalf("redeem answered %d (%s), want 200", rd.StatusCode, msg)
	}

	// B -> A. /api/tasks is a guarded route, so a 401 here is exactly the bug.
	if _, code, err := bApp.Federation.Proxy(context.Background(), issued.Name, http.MethodGet, "/api/tasks", nil); err != nil || code != http.StatusOK {
		t.Errorf("B calling A = %d %v, want 200 - a paired peer with a password must be reachable", code, err)
	}

	// A -> B, the direction the generator never tested before.
	peers := aApp.Federation.List()
	if len(peers) != 1 {
		t.Fatalf("A knows %+v, want exactly the peer it just paired with", peers)
	}
	// A stored B's SELF-REPORTED address, which on a multi-homed host is B's
	// guessed LAN IP rather than where httptest listens - the same artefact
	// TestPairingCodeRoundTrip skips over. Point it at B's real test address
	// so what is measured below is the credential, not address discovery
	// (that is issue #28's own problem, and it is what makes this direction
	// untestable here otherwise). The name is unchanged, so the peer token
	// filed against it stays in place.
	if err := aApp.Federation.Add(federation.Instance{Name: peers[0].Name, URL: bSrv.URL}); err != nil {
		t.Fatal(err)
	}
	if _, code, err := aApp.Federation.Proxy(context.Background(), peers[0].Name, http.MethodGet, "/api/tasks", nil); err != nil || code != http.StatusOK {
		t.Errorf("A calling B = %d %v, want 200", code, err)
	}
}

// TestPairingReportsBothDirections pins issue #28: pairing travels one way
// (redeemer -> generator), so that half was proven and the other half was
// stored on the redeemer's word alone. With asymmetric reachability - one
// side behind NAT, the two on different networks - that produced a pairing
// both sides reported as successful with one direction permanently dead.
//
// The fix is to SAY so, not to refuse: a peer that is unreachable right now
// may simply be asleep, and throwing the pairing away would lose the half
// that does work.
func TestPairingReportsBothDirections(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	offer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatal(err)
	}
	offer.URL = aSrv.URL // see the sibling test for why the announced address is not usable here
	code, err := encodeOffer(offer)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"code": code})
	rd, err := http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Body.Close()
	var got struct {
		Online      bool `json:"online"`
		ReachedBack bool `json:"reachedBack"`
	}
	if err := json.NewDecoder(rd.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if rd.StatusCode != http.StatusOK {
		t.Fatalf("redeem answered %d", rd.StatusCode)
	}

	// B reached A - that is the direction the redemption itself travelled, so
	// it was always proven.
	if !got.Online {
		t.Error("online = false, want true: the redeemer just successfully called the generator")
	}
	// A tried to reach B and could not: B announced its guessed LAN address,
	// which is not where httptest listens. That is exactly the asymmetric case
	// this field exists for, and the point is that it is now REPORTED rather
	// than presented as a clean success.
	if got.ReachedBack {
		t.Error("reachedBack = true, but B is not reachable at the address it announced - the field is not being measured")
	}
}

// TestPairingSurvivesASilentPeer guards the cost of the issue #28 check.
//
// The reverse-direction probe is INFORMATIONAL: the peer and its credential
// are already stored before it runs, and its only job is to decide which
// sentence the redeemer shows. A federation call is allowed 15s
// (federation.peerTimeout) and the redeemer allows the whole redemption 15s -
// so an unbounded probe against a peer that accepts a connection and then says
// nothing eats the entire budget, and a pairing that used to succeed times out
// instead. A worse pairing in exchange for a nicer message is not a trade
// worth making, so the probe gets its own short deadline.
//
// /complete is driven directly here rather than through a second instance:
// pairingSelf picks the address a redeemer announces, and it deliberately
// refuses loopback, so a real redeemer cannot be made to point at a listener
// on this machine. The token still comes from a genuine pairing code, so the
// handler runs its real path.
//
// The peer is a listener that accepts and never answers, which is the exact
// shape that hangs - a refused connection fails fast and would prove nothing.
func TestPairingSurvivesASilentPeer(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// One defer, not two: closing the listener is what ends the accept loop,
	// so draining must happen AFTER it. Two separate defers run in the wrong
	// order (LIFO) and the drain waits forever on a channel the loop cannot
	// reach the code to close.
	held := make(chan net.Conn, 8)
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				close(held)
				return
			}
			held <- c // kept open and unanswered, which is the whole point
		}
	}()
	defer func() {
		_ = silent.Close()
		for c := range held {
			_ = c.Close()
		}
	}()

	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(Handler(a))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	offer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{
		"token": offer.Token,
		"name":  "Silent",
		"url":   "http://" + silent.Addr().String(),
	})
	start := time.Now()
	// No client timeout of its own, so what is measured is the handler.
	cr, err := http.Post(srv.URL+"/api/instances/pairing-code/complete", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Body.Close()
	elapsed := time.Since(start)

	if cr.StatusCode != http.StatusOK {
		t.Fatalf("complete answered %d, want 200 - the pairing must not depend on the probe", cr.StatusCode)
	}
	var got struct {
		PeerToken   string `json:"peerToken"`
		ReachedBack bool   `json:"reachedBack"`
	}
	if err := json.NewDecoder(cr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ReachedBack {
		t.Error("reachedBack = true for a peer that never answered a byte")
	}
	if got.PeerToken == "" {
		t.Error("no peer token was minted, so the probe cost the exchange its credential")
	}
	// The probe's own budget is 4s. Well under the 15s that breaks a real
	// redemption, and far enough above 4s that a slow box does not fail this
	// for the wrong reason.
	if elapsed > 9*time.Second {
		t.Errorf("complete took %v against a silent peer - the reverse probe is not bounded", elapsed)
	}
	// The peer is stored regardless: unreachable now is not the same as wrong,
	// and dropping it would throw away the direction that does work.
	if list := a.Federation.List(); len(list) != 1 || list[0].Name != "Silent" {
		t.Errorf("stored peers = %+v, want the peer kept despite the failed probe", list)
	}
	t.Logf("elapsed=%v", elapsed)
}

// pairOnce drives a full pairing of b against a's freshly issued code and
// returns whether it succeeded, so the token-lifecycle tests below can pair,
// re-pair and fail-to-pair without three copies of the same twenty lines.
func pairOnce(t *testing.T, aSrv, bSrv *httptest.Server) bool {
	t.Helper()
	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	offer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatal(err)
	}
	offer.URL = aSrv.URL // the announced address is a guessed LAN one; see the sibling tests
	code, err := encodeOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	rd, err := http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Body.Close()
	return rd.StatusCode == http.StatusOK
}

func peerTokensNamed(a *app.App, peer string) []string {
	var ids []string
	for _, tk := range a.APITokens.List() {
		if tk.Name == "peer: "+peer {
			ids = append(ids, tk.ID)
		}
	}
	return ids
}

// TestFailedRepairKeepsTheWorkingToken: a peer token used to be revoked by
// NAME, and apitoken does not enforce unique names, so "drop the token I just
// minted" actually dropped every token ever minted for that peer.
//
// The damage lands on the pairing that WORKED: pair, then retry later against
// a code that fails for any reason, and the retry's cleanup takes the live
// credential with it. Federation then answers 401 forever, with nothing in the
// UI connecting that to a pairing attempt the user already knows failed.
func TestFailedRepairKeepsTheWorkingToken(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	if !pairOnce(t, aSrv, bSrv) {
		t.Fatal("the first pairing did not succeed")
	}
	live := peerTokensNamed(bApp, instanceDisplayName(aApp))
	if len(live) != 1 {
		t.Fatalf("after one pairing B holds %d tokens for A, want exactly 1", len(live))
	}
	working := live[0]

	// A retry that fails on the far side: a code B has already burned.
	resp, err := http.Post(aSrv.URL+"/api/instances/pairing-code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	offer, err := decodeOffer(issued.Code)
	if err != nil {
		t.Fatal(err)
	}
	offer.URL = aSrv.URL
	offer.Token = "not-a-real-token" // A will refuse this, so the pairing fails on A
	code, err := encodeOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	rd, err := http.Post(bSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rd.Body.Close()
	if rd.StatusCode == http.StatusOK {
		t.Fatal("the retry was supposed to fail")
	}

	after := peerTokensNamed(bApp, instanceDisplayName(aApp))
	if len(after) != 1 || after[0] != working {
		t.Errorf("B holds %v after a FAILED retry, want the original %q untouched - a retry that did nothing must not kill a live credential", after, working)
	}
}

// TestRepeatedPairingDoesNotAccumulateTokens: nothing revoked the previous
// credential on a SUCCESSFUL re-pair, so every retry left another live,
// full-power token behind. The Access tab fills with identically named rows
// nobody can tell apart, and at apitoken.MaxTokens a perfectly valid pairing
// code starts failing with "too many tokens" - an error about a limit the
// user never knowingly approached.
func TestRepeatedPairingDoesNotAccumulateTokens(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	for i := 0; i < 4; i++ {
		if !pairOnce(t, aSrv, bSrv) {
			t.Fatalf("pairing %d did not succeed", i+1)
		}
	}

	// B is the redeemer: it gets a 200 back, so the exchange is PROVEN from
	// where it stands and it retires everything older. Exactly one.
	if got := peerTokensNamed(bApp, instanceDisplayName(aApp)); len(got) != 1 {
		t.Errorf("B holds %d tokens for A after 4 pairings, want 1", len(got))
	}
	// A is the generator, and B announced a guessed LAN address A cannot
	// reach, so A's reverse probe never proves anything. A therefore keeps the
	// older credential on purpose - retiring it on an unproven pairing is how
	// a failed retry kills a link that was working. What it must NOT do is
	// pile them up without limit, which is what keepPerPeer bounds.
	if got := peerTokensNamed(aApp, instanceDisplayName(bApp)); len(got) > keepPerPeer {
		t.Errorf("A holds %d tokens for B after 4 pairings, want at most %d", len(got), keepPerPeer)
	} else if len(got) == 0 {
		t.Error("A holds no token for B at all - an unproven pairing must keep the credential, not drop it")
	}

	// And the surviving credential is the LIVE one, not a stale hash: the
	// whole point of superseding is that the pairing still works afterwards.
	if err := aApp.Federation.Ping(context.Background(), instanceDisplayName(bApp)); err != nil {
		// B announced a guessed LAN address, so an unreachable peer here is
		// expected and says nothing about the token. Only a 401 would.
		if strings.Contains(err.Error(), "401") {
			t.Errorf("A -> B answered 401 after re-pairing: the surviving token is not the live one")
		}
	}
}

// TestPairingWorksWithANameFederationWouldRefuse: an instance's own name is
// whatever its owner chose, or its hostname - neither picked to satisfy
// federation's naming rule. "Bürglers Keller" is a perfectly ordinary thing to
// call a box, and it made pairing fail on the FAR side with "federation:
// invalid instance name", about a name the person redeeming the code never
// typed and could not see from there.
func TestPairingWorksWithANameFederationWouldRefuse(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	// B is the redeemer, so B's own name is the one it hands to A in
	// /complete - the direction that used to be rejected.
	cfg := bApp.Settings.Get()
	cfg.InstanceName = "Bürglers Keller"
	if _, err := bApp.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	if !pairOnce(t, aSrv, bSrv) {
		t.Fatal("pairing failed for an instance named \"Bürglers Keller\"")
	}

	peers := aApp.Federation.List()
	if len(peers) != 1 {
		t.Fatalf("A stored %d peers, want 1", len(peers))
	}
	if peers[0].Name != "Burglers Keller" {
		t.Errorf("A stored the peer as %q, want the folded form \"Burglers Keller\"", peers[0].Name)
	}
}

// TestRepointingAPeerDoesNotHandOverItsCredential is the sharpest edge peer
// tokens added, so it gets the most direct test in this file.
//
// federation.Add overwrites by NAME, and a credential is filed by name too.
// So a pairing code naming a peer that already exists re-points that name at a
// new address while the OLD peer's token stays filed under it - and the very
// next call, the reachability Ping the handler makes anyway, puts that token in
// an Authorization header addressed to whoever now owns the name.
//
// The attacker's side of this is a pairing code somebody was persuaded to
// paste, or read off a screen; /complete takes one without authentication at
// all. Before peer tokens the same hostile code could re-point the entry, but
// no secret travelled. This is what turns that into credential theft.
func TestRepointingAPeerDoesNotHandOverItsCredential(t *testing.T) {
	victimApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer victimApp.Close()
	victimSrv := httptest.NewServer(Handler(victimApp))
	defer victimSrv.Close()

	// The instance being attacked, which pairs with the victim first.
	meApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer meApp.Close()
	meSrv := httptest.NewServer(Handler(meApp))
	defer meSrv.Close()

	if !pairOnce(t, victimSrv, meSrv) {
		t.Fatal("the honest pairing did not succeed")
	}
	victimName := instanceDisplayName(victimApp)
	stolen := peerTokens{a: meApp}.TokenFor(victimName)
	if stolen == "" {
		t.Fatal("no credential was stored for the honest peer, so this test proves nothing")
	}

	// Somewhere that records every Authorization header it is sent, standing in
	// for the attacker's box.
	var seen []string
	var mu sync.Mutex
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		// Answers a pairing completion with no credential of its own, which is
		// what makes the stored one survive to be misused.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer evil.Close()

	// A hostile code: the victim's NAME, the attacker's address.
	code, err := encodeOffer(pairingOffer{Name: victimName, URL: evil.URL, Token: "whatever"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(meSrv.URL+"/api/instances/pairing-code/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	for _, h := range got {
		if strings.Contains(h, stolen) {
			t.Fatalf("the honest peer's credential was sent to the attacker: %q", h)
		}
		if h != "" {
			t.Errorf("attacker saw an Authorization header %q - a re-pointed name must be called with nothing", h)
		}
	}
	// And the credential must not survive under a name that now points
	// somewhere else, where the next call would find it again.
	if left := (peerTokens{a: meApp}).TokenFor(victimName); left == stolen {
		t.Error("the honest peer's credential is still filed under a name that now points at the attacker")
	}
}

// TestRemovingAPeerEndsItsCredentials: "Remove" is the action somebody takes to
// end the relationship. It used to delete a line from instances.json and
// nothing else, leaving the peer a live, unscoped, full-power API token on this
// instance - and leaving this instance's own credential for that peer filed
// under a name that anything registered next would inherit.
func TestRemovingAPeerEndsItsCredentials(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()

	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	if !pairOnce(t, aSrv, bSrv) {
		t.Fatal("pairing did not succeed")
	}
	aName := instanceDisplayName(aApp)
	if (peerTokens{a: bApp}).TokenFor(aName) == "" {
		t.Fatal("no credential stored, so this test proves nothing")
	}
	if len(peerTokensNamed(bApp, aName)) == 0 {
		t.Fatal("no token minted, so this test proves nothing")
	}

	req, _ := http.NewRequest(http.MethodDelete, bSrv.URL+"/api/instances/"+url.PathEscape(aName), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove answered %d, want 204", resp.StatusCode)
	}

	if left := (peerTokens{a: bApp}).TokenFor(aName); left != "" {
		t.Error("the credential for the removed peer is still stored")
	}
	if live := peerTokensNamed(bApp, aName); len(live) != 0 {
		t.Errorf("%d token(s) minted for the removed peer are still live - it can still reach this instance", len(live))
	}
}
