package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
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

	// The same code must still work once the name is acceptable.
	if status, msg := complete("cellar"); status != http.StatusNoContent {
		t.Fatalf("retry with a valid name answered %d (%s), want 204 - the code was burned by a failure that was not about it", status, msg)
	}

	// And it really is single-use: the successful redemption consumed it.
	if status, _ := complete("cellar"); status != http.StatusBadRequest {
		t.Errorf("reusing a successfully redeemed code answered %d, want 400", status)
	}
}
