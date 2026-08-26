package api

// Instance pairing: a short-lived code that lets two KnightLoader instances
// register each other in one action, instead of typing name+address twice
// (once on each side, once for each direction). No account, no relay - see
// routes_remote.go's own doc comment on why this project runs neither. A
// code just packages the exact address-detection the Access tab's QR card
// already does (remoteAddresses/firstNonLoopback, routes_remote.go) together
// with a single-use credential, the same shape containerRelay already uses
// for "another process reaches this route with no session and no way to be
// given one" (routes_containers.go's own doc comment on that).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// pairingTTL bounds how long a generated code stays redeemable - long enough
// to paste into the other instance's Instances tab, short enough that a code
// left visible on screen is not a standing credential.
const pairingTTL = 5 * time.Minute

// pairingOffer is what a generated code decodes to: enough for the other side
// to reach this instance and register it under a name, with no second lookup
// anywhere.
//
// TWO ways to be reached, and a code may carry either or both. URL is the
// direct one and is preferred wherever it works. RelayID is this instance's
// own identifier on the relay it is connected to, which is what makes pairing
// possible between two instances that have no address either can dial - the
// case the relay exists for, and the one where pairing used to be impossible
// (so a password-protected instance there stayed unreachable forever, see
// internal/api/peertokens.go).
//
// Both fields are omitempty, so a code from an instance with only one way in
// stays as short as it was. A reader must therefore check which it got: an
// older redeemer sees a relay-only code as having no URL and refuses it, which
// is the correct answer for a build that cannot use one.
type pairingOffer struct {
	Name    string `json:"n"`
	URL     string `json:"u,omitempty"`
	RelayID string `json:"r,omitempty"`
	Token   string `json:"t"`
}

// pairingCodes tracks tokens this instance itself issued, so /complete can
// tell a still-valid code from an expired or made-up one. Mirrors
// containerRelay's own mutex+map+sweep shape (routes_containers.go).
type pairingCodes struct {
	mu    sync.Mutex
	items map[string]time.Time // token -> expires
}

func newPairingCodes() *pairingCodes {
	return &pairingCodes{items: map[string]time.Time{}}
}

func (p *pairingCodes) issue() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	p.items[token] = time.Now().Add(pairingTTL)
	return token, nil
}

// redeemLocal reports whether token is a still-live code this instance
// issued, and forgets it either way: one redemption is all a pairing code is
// for, the same reasoning containerRelay.take() already documents.
func (p *pairingCodes) redeemLocal(token string) (expires time.Time, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	exp, found := p.items[token]
	delete(p.items, token)
	return exp, found && time.Now().Before(exp)
}

// restore puts a taken token back, for the case the operation it gated failed
// for a reason that has nothing to do with the code - a peer name the
// federation store rejects, say.
//
// Taking it first and restoring on failure, rather than checking without
// taking, is what keeps a code single-use under concurrent redemptions: only
// one caller can ever hold it at a time. Without this the code was burned by
// the FIRST attempt whatever went wrong, and every retry - including with a
// freshly generated code - then reported "invalid or expired", which sent
// people looking at the clock instead of at the real error.
//
// An already-expired token is not restored: it has no life left to give back.
func (p *pairingCodes) restore(token string, expires time.Time) {
	if !time.Now().Before(expires) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items[token] = expires
}

func (p *pairingCodes) sweepLocked() {
	now := time.Now()
	for k, exp := range p.items {
		if now.After(exp) {
			delete(p.items, k)
		}
	}
}

// pairingSelf is this instance's own name and best address, built from the
// same remoteAddresses()/preferredAddress() routes_remote.go's QR card
// already computes - reused rather than re-implemented, so a pairing code
// and the QR card can never disagree about which address is "this
// instance", and a known domain reaches this code the exact same way it
// reaches the QR card (jdp: "Die domain soll auch mit dem QR Code an die
// App weitergegeben werden" - true for BOTH QR codes on the Access tab, not
// only the plain one). Unlike the QR card's own preferredAddress call, a
// loopback address is not thrown away when it is the only one available:
// two instances on the SAME machine (a desktop build and a local container,
// say) legitimately reach each other over 127.0.0.1, and only a request
// with no address at all - not even the one it arrived on - has nothing to
// offer.
//
// The name defaults to os.Hostname(), never typed, the original point of a
// code that replaces typing name AND address (jdp: "statt Name+Adresse von
// Hand zu tippen") - but a.Settings.Get().InstanceName, once someone sets
// one, replaces that default: naming this instance is still never REQUIRED
// for pairing to work, it is just no longer stuck with whatever the OS or
// the container runtime happened to call it (jdp, a later round: "der soll
// dann mit dem QR code an die App weitergegeben werden").
func pairingSelf(r *http.Request, a *app.App) (name, url, relayID string, ok bool) {
	// The desktop build has no ADDRESS to offer, ever: it hands api.Handler to
	// Wails as an in-window asset handler and opens no API listener at all
	// (desktop/main.go), so nothing outside that process can dial it.
	//
	// Refused here rather than left to fail later, because the failure was
	// SILENT and landed on somebody else's machine. remoteAddresses would
	// report the Wails webview host (http://wails.localhost on Windows,
	// http://wails elsewhere), which is not loopback and parses as a domain -
	// so preferredAddress ranks it FIRST, ahead of any real configured
	// domain. federation.Add validates only scheme and host, so redeeming a
	// code from a desktop build wrote a permanently dead peer into the OTHER
	// instance's stored list and reported success to both sides.
	//
	// The relay is the way a desktop build reaches other instances (see
	// internal/relay): both ends dial out, so neither needs an address. Adding
	// a peer BY address (POST /api/instances) still works from a desktop too -
	// that is one-way by nature and honest about it.
	//
	// It can still OFFER a code, as of relay pairing: the relay addresses it by
	// its instance id, both ends dial out, and neither needs an address. So the
	// gate below is no longer "is this a desktop" but "is there any way in at
	// all" - which is the question that was always really being asked.
	tsURL := tsnetFunnelURL(a)
	if buildinfo.Deployment != "desktop" {
		known := a.Settings.Get().KnownDomains
		addrs := remoteAddresses(r, known, tsURL)
		if u, found := preferredAddress(addrs); found {
			url = u
		} else if len(addrs) > 0 {
			url = addrs[0].URL
		}
	} else if tsURL != "" {
		// The one exception to the desktop address gate above: unlike
		// remoteAddresses(r, ...), tsnetFunnelURL carries none of the risk
		// this whole gate exists for - it reads only a.Tsnet.Info(), never
		// r, so there is no Wails-webview-host address for it to mistake
		// for a real one. A connected Funnel genuinely is "a way in" for a
		// desktop build (see routes_remote.go's own comment: it is the ONLY
		// one that has ever existed), so it is offered here directly rather
		// than through remoteAddresses/preferredAddress's ranking, which a
		// desktop build never safely runs at all - caught in review before
		// this fix, a Funnel-connected desktop instance's pairing code
		// silently ignored the very address its own Access page already
		// showed working.
		url = tsURL
	}
	// Offered alongside the URL rather than instead of it, so one code works
	// for a redeemer that can dial directly AND for one that cannot. The
	// redeemer picks; see deliverCompletion.
	if a.Federation.RelayConnected() {
		relayID = a.Settings.Get().InstanceID
	}
	if url == "" && relayID == "" {
		return "", "", "", false
	}
	// Sanitised, because this name is DERIVED - it is whatever the instance
	// calls itself, or the hostname, neither of which was chosen to satisfy
	// federation's naming rule. An unsanitised umlaut or an over-long hostname
	// made the far side answer "invalid instance name" about a name the user
	// never typed and could not see (see federation.SanitiseName).
	name = federation.SanitiseName(instanceDisplayName(a))
	if name == "" {
		// Nothing addressable could be made of it. Refused here, where the
		// message reaches the person who can fix it by naming the instance,
		// rather than on the far side as a rejection of a name they cannot
		// see - the same reasoning as the gate above.
		return "", "", "", false
	}
	return name, url, relayID, true
}

// instanceDisplayName is InstanceName if the user set one, else os.Hostname,
// else the fixed fallback "KnightLoader" for the rare host where even that
// fails - the same precedence pairingSelf originally had inline, shared here
// because routes_relay.go's own Announce needs the identical name a pairing
// code already offers, and a name resolved two different ways in two places
// is a name that eventually disagrees with itself.
func instanceDisplayName(a *app.App) string {
	if name := a.Settings.Get().InstanceName; name != "" {
		return name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "KnightLoader"
}

func encodeOffer(o pairingOffer) (string, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeOffer(code string) (pairingOffer, error) {
	b, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return pairingOffer{}, errors.New("not a pairing code")
	}
	var o pairingOffer
	// A code needs its one-time token and at least one way in. Either way in
	// counts: an instance reachable only through a relay carries a relay id and
	// no URL, and requiring a URL here is what made such a code read as "not a
	// pairing code" - a rejection about the wrong thing entirely.
	if err := json.Unmarshal(b, &o); err != nil || o.Token == "" || (o.URL == "" && o.RelayID == "") {
		return pairingOffer{}, errors.New("not a pairing code")
	}
	return o, nil
}

func registerPairing(reg *Registry, a *app.App) {
	codes := newPairingCodes()

	// Generate: the Access tab's own action, run from an already-logged-in
	// session - a normal authenticated route, nothing exempt here.
	reg.Add(http.MethodPost, "/api/instances/pairing-code",
		"issue a short-lived code another instance can redeem to add this one, and be added back",
		func(w http.ResponseWriter, r *http.Request) {
			name, url, relayID, ok := pairingSelf(r, a)
			if !ok {
				http.Error(w, "no address to offer for this instance, and no relay connected", http.StatusConflict)
				return
			}
			token, err := codes.issue()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			code, err := encodeOffer(pairingOffer{Name: name, URL: url, RelayID: relayID, Token: token})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The QR encodes the code itself, not a URL: whatever scans it
			// (jdp: "später mit der app scannen") only needs to read the same
			// string a person would paste by hand and POST it to its own
			// /api/instances/pairing-code/redeem - one payload, two ways to
			// get it off this screen, never two protocols to keep in sync.
			// viaRelay rather than the id itself: the page wants to say HOW this
			// code can be redeemed, and the id is an address, not a fact worth
			// putting on screen.
			writeJSON(w, map[string]any{
				"code": code, "name": name, "url": url,
				"viaRelay":  relayID != "",
				"expiresIn": int(pairingTTL.Seconds()), "qr": renderQR(code),
			})
		})

	// Redeem: the Instances tab's own action, on the instance joining an
	// existing one (B). Also a normal authenticated route - it is B's own
	// logged-in session asking B's own backend to go pair, not a
	// cross-instance call itself.
	reg.Add(http.MethodPost, "/api/instances/pairing-code/redeem",
		"add the instance behind a pairing code, and register this instance back with it in the same action",
		func(w http.ResponseWriter, r *http.Request) {
			// Before the code is even looked at: redeeming registers this
			// instance BACK with the other one, so an instance with no address
			// to offer cannot redeem at all, however good the code is. Checked
			// first so the answer names the real reason - a desktop build that
			// got "not a pairing code" for a perfectly valid code would send
			// somebody hunting the wrong problem.
			selfName, selfURL, selfRelayID, ok := pairingSelf(r, a)
			if !ok {
				http.Error(w, "no address to offer back for this instance, and no relay connected", http.StatusConflict)
				return
			}
			var body struct {
				Code string `json:"code"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			offer, err := decodeOffer(body.Code)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Complete the other half first: if the other instance cannot be
			// told about this one, this one must not add it either - a
			// one-sided pairing is worse than none, see the doc comment atop
			// this file.
			hc := httpx.New(httpx.Options{Timeout: 15 * time.Second})
			// Each side mints a credential for the OTHER and hands it over in
			// the same exchange, so a peer with a password set is reachable
			// the moment pairing finishes rather than answering 401 forever
			// (issue #26). Minted before the call, because it has to travel
			// IN it; dropped again below if the pairing does not complete, so
			// a failed attempt does not leave a usable token behind.
			// Everything this peer can be addressed by. The relay id is
			// validated on the way in - see peerIdentity.
			peer := newPeerIdentity(offer.Name, offer.RelayID)
			mintedID, forPeer, err := mintPeerToken(a, peer.canonical())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cb, _ := json.Marshal(map[string]string{
				"token": offer.Token, "name": selfName, "url": selfURL,
				"relayId": selfRelayID, "peerToken": forPeer,
			})
			status, respBody, viaRelay, err := deliverCompletion(r.Context(), a, hc, offer, cb)
			if err != nil {
				revokeMintedToken(a, mintedID)
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if status != http.StatusOK && status != http.StatusNoContent {
				// Pass the peer's own reason through instead of replacing it
				// with a guess. The peer answers 400 for several distinct
				// things - a code that really did expire, but also a name or
				// address ITS federation store rejects - and flattening them
				// all to "expired" is why a rejected name read as a clock
				// problem and stayed unfixable through any number of fresh
				// codes.
				msg := strings.TrimSpace(string(respBody))
				if msg == "" {
					msg = "that code is invalid or has expired"
				}
				revokeMintedToken(a, mintedID)
				http.Error(w, msg, http.StatusBadRequest)
				return
			}

			// The peer answers with the credential IT minted for us. An older
			// build answers 204 with no body and no token, which stays
			// perfectly valid: the peer is then simply called unauthenticated,
			// exactly as before this existed.
			var back struct {
				PeerToken string `json:"peerToken"`
				// Whether the peer could reach US back. Absent from an older
				// peer, where it reads as false - which is honest: that build
				// genuinely never checked.
				ReachedBack bool `json:"reachedBack"`
			}
			_ = json.Unmarshal(respBody, &back)

			// An address is only written down once it has been PROVEN to carry -
			// which the delivery above just did or did not do. Storing one the
			// relay had to rescue leaves a peer nothing can reach sitting in
			// instances.json next to the live relay entry for the same machine:
			// two rows, one of them permanently dead.
			//
			// A relay peer is never stored either way. It appears and disappears
			// with the connection, and one remembered across a restart is a peer
			// this instance cannot reach and cannot explain (see
			// federation.Instance.RelayID's own doc comment).
			if offer.URL != "" && !viaRelay {
				if err := addPeer(a, federation.Instance{Name: offer.Name, URL: offer.URL}); err != nil {
					revokeMintedToken(a, mintedID)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if back.PeerToken != "" {
				if err := storePeerTokens(a, peer, back.PeerToken); err != nil {
					// The only exit here that used to leave the minted token
					// live: the peer has already committed its half, so this
					// half is what fails, and the credential this side handed
					// out has to come back with it.
					revokeMintedToken(a, mintedID)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			// Pairing is complete. Anything issued for this peer by an EARLIER
			// pairing is now dead weight - see supersedePeerTokens for why
			// this happens here and not before the mint.
			supersedePeerTokens(a, peer.canonical(), mintedID)
			online := a.Federation.Ping(r.Context(), peer.canonical()) == nil
			// Both halves reported separately, because they fail separately:
			// "online" is us reaching the peer, "reachedBack" is the peer
			// reaching us. A pairing with one of them false works in one
			// direction only, and the page can now say so instead of showing
			// an unqualified success.
			writeJSON(w, map[string]any{
				"name": offer.Name, "url": offer.URL,
				"viaRelay": viaRelay,
				"online":   online, "reachedBack": back.ReachedBack,
			})
		})

	// Complete: called by the OTHER instance's backend, with no session and
	// no way to be given one - the token is the credential, unguessable,
	// single-use and short-lived, the same shape
	// /api/containers/relay/{token} already uses for exactly this situation
	// (routes_containers.go).
	reg.AddOpen(http.MethodPost, "/api/instances/pairing-code/complete",
		"register the instance that just redeemed this instance's pairing code; the token in the body is the credential",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Token string `json:"token"`
				Name  string `json:"name"`
				URL   string `json:"url"`
				// RelayID is the redeemer's own instance id, set when it has no
				// address of its own to offer back. Absent from an older
				// redeemer, and from any redeemer that does have an address -
				// in which case the URL above is the better way to reach it and
				// this stays empty.
				RelayID string `json:"relayId"`
				// The credential the redeemer minted for THIS instance to call
				// it with. Absent from an older redeemer, which is fine: that
				// peer is then called unauthenticated, as every peer was
				// before peer tokens existed.
				PeerToken string `json:"peerToken"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			expires, ok := codes.redeemLocal(body.Token)
			if !ok {
				http.Error(w, "that code is invalid or has expired", http.StatusBadRequest)
				return
			}
			// Same as the redeem side: file under everything this peer can be
			// addressed by, rather than guessing which one will be used.
			peer := newPeerIdentity(body.Name, body.RelayID)
			// At least one WAY IN, not merely a name. A name is a label; it is
			// not somewhere this instance can call. Without this, a completion
			// carrying only a name got a full-power token minted for a peer
			// that can never be reached - and a relay id that is not shaped
			// like one silently degrades to exactly that shape, so the two
			// checks belong together.
			if body.URL == "" && peer.RelayID == "" {
				codes.restore(body.Token, expires)
				http.Error(w, "that instance offered no address and no usable relay id", http.StatusBadRequest)
				return
			}
			// This side cannot test the redeemer's address - it is the one being
			// called, not the one calling - so it uses the next best evidence:
			// a peer the relay already makes visible needs no stored row, and
			// adding one would put a possibly-dead address beside a live entry
			// for the same machine.
			_, relayVisible := findPeer(a, peer.RelayID)
			if body.URL != "" && !relayVisible {
				if err := addPeer(a, federation.Instance{Name: body.Name, URL: body.URL}); err != nil {
					// The code itself was fine - this failed on the peer's name or
					// address. Give it back, so retrying after fixing that works
					// instead of reporting an expiry that never happened.
					codes.restore(body.Token, expires)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if body.PeerToken != "" {
				if err := storePeerTokens(a, peer, body.PeerToken); err != nil {
					// Give the code back for the same reason the Add failure
					// above does: this failed on the credential store, not on
					// the code, and reporting an expiry that never happened
					// makes a retry look pointless when it is the right move.
					codes.restore(body.Token, expires)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			// Mint one back, so the exchange is symmetric and BOTH directions
			// are authenticated. Answered as JSON rather than the old 204: a
			// redeemer that predates this ignores the body and simply calls
			// this instance unauthenticated, which is exactly what it did
			// before.
			mintedID, forPeer, err := mintPeerToken(a, peer.canonical())
			if err != nil {
				// Same again: the code is fine, this instance's token store is
				// not (full, or unwritable). A retry after clearing that out
				// has to still work.
				codes.restore(body.Token, expires)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Issue #28: pairing used to prove only redeemer -> generator,
			// because that is the direction the redemption itself travels.
			// THIS direction - generator -> redeemer - was stored on the
			// redeemer's word alone and never tried, so asymmetric
			// reachability produced a pairing both sides called successful
			// with one half permanently dead.
			//
			// Reported, not enforced: a peer that is not reachable right now
			// is still worth keeping (it may be asleep, or behind a relay
			// that is not up yet), and refusing the pairing would throw away
			// the half that does work. The redeemer shows this, so the
			// failure surfaces at the moment somebody is looking.
			//
			// On its OWN short deadline, not the request's. A federation call
			// is allowed 15s (federation.peerTimeout) and the redeemer gives
			// this whole request 15s, so a peer that accepts a connection and
			// then goes quiet would burn the redeemer's entire budget on a
			// check that changes nothing - turning a pairing that used to
			// succeed into one that times out. Everything above is already
			// stored; this only decides which sentence the redeemer shows.
			pingCtx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			reachable := a.Federation.Ping(pingCtx, peer.canonical()) == nil
			cancel()

			// The previous credential for this peer is retired only once the
			// new one is DEMONSTRATED to work - the probe above just used it.
			//
			// Retiring it before that opened a window where a re-pairing could
			// kill both directions at once: this side had already dropped the
			// old token, and if the redeemer then failed (its connection
			// dropped, its own 15s budget expired during this very probe, its
			// credential store would not write) it revokes what it minted, and
			// a pairing that had been working ends up 401 in both directions
			// over an attempt that reported one unrelated error.
			//
			// An extra token that outlives its pairing is untidy; two dead
			// directions are a broken feature. The next successful pairing
			// clears it, and it is one revocable row on the Access tab
			// meanwhile.
			if reachable {
				supersedePeerTokens(a, peer.canonical(), mintedID)
			} else {
				// Unproven, so the older credential stays - but not forever:
				// see keepPerPeer for why an unbounded pile is its own bug.
				trimPeerTokens(a, peer.canonical(), mintedID)
			}
			writeJSON(w, map[string]any{"peerToken": forPeer, "reachedBack": reachable})
		})
}

// deliverCompletion carries the completion payload to the instance that issued
// the code, over whichever way in that code offered.
//
// Direct HTTP is preferred wherever it is available: it is one hop, it does not
// depend on a third machine being up, and it does not put the exchange in front
// of a relay operator. The relay is the fallback, and it is what makes pairing
// possible at all between two instances that have no address either can dial -
// the deployment the relay exists for, and the one where pairing used to be
// impossible, so a password-protected instance there stayed unreachable
// forever.
//
// Both ways end at the same handler: the receiving side's relay client serves a
// proxied request through its own normal stack (see internal/api/routes_relay.go),
// so /complete cannot tell the two apart and needs no second implementation.
func deliverCompletion(ctx context.Context, a *app.App, hc *http.Client, offer pairingOffer, payload []byte) (status int, body []byte, viaRelay bool, err error) {
	var directErr error
	if offer.URL != "" {
		status, body, err := postCompletion(ctx, hc, offer.URL, payload)
		if err == nil {
			return status, body, false, nil
		}
		directErr = err
		if offer.RelayID == "" {
			return 0, nil, false, err
		}
		// Fall through to the relay. An address in a code is what the issuing
		// instance believes it is reachable at, and it is often right for
		// somebody and wrong for somebody else - a NAS announcing its LAN
		// address is unreachable from anywhere but that LAN, and refusing to
		// pair there while both ends sit on the same relay is giving up with
		// the answer in hand.
		//
		// Only on a TRANSPORT failure, never on an answer: a peer that replied
		// at all, however badly, has seen this code, and asking again over
		// another road would be asking it to redeem a one-time token twice. The
		// narrow risk this leaves is a connection dying after the far side
		// committed but before its answer arrived - the retry then reports the
		// code as spent, which is confusing but true, and a fresh code works.
	}
	if offer.RelayID == "" {
		return 0, nil, false, errors.New("that code carries no address and no relay id")
	}
	if !a.Federation.RelayConnected() {
		// Named precisely, because the fix is specific: this code can only be
		// redeemed over a relay, and this instance is not on one. "Could not
		// reach it" would send somebody checking a network path that was never
		// going to be used.
		return 0, nil, false, errors.New("that code can only be redeemed over a relay, and this instance is not connected to one")
	}
	// Addressed by instance id, which is exactly how the relay routes and how
	// federation already addresses a sibling - no new addressing scheme, and no
	// need for the peer to be registered first: the relay makes every sibling
	// reachable the moment it sees it.
	rbody, code, err := a.Federation.Proxy(ctx, offer.RelayID, http.MethodPost, "/api/instances/pairing-code/complete", payload)
	if err != nil {
		if directErr != nil {
			// Both roads failed, so both reasons are reported. Naming only the
			// relay would hide that the address in the code did not work
			// either, which is the half somebody can actually go and fix.
			return 0, nil, false, fmt.Errorf("%w, and the relay could not reach it either: %v", directErr, err)
		}
		return 0, nil, false, fmt.Errorf("could not reach that instance over the relay: %w", err)
	}
	return code, rbody, true, nil
}

// postCompletion is the direct half of deliverCompletion: one ordinary HTTP
// POST to the address a code carries.
func postCompletion(ctx context.Context, hc *http.Client, base string, payload []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/instances/pairing-code/complete", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("could not reach %s: %w", base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
	return resp.StatusCode, body, nil
}
