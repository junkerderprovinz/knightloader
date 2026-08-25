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

// pairingOffer is what a generated code decodes to: enough for the other
// side to reach this instance and register it under a name, with no second
// lookup anywhere - there is no relay to look one up through.
type pairingOffer struct {
	Name  string `json:"n"`
	URL   string `json:"u"`
	Token string `json:"t"`
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
func pairingSelf(r *http.Request, a *app.App) (name, url string, ok bool) {
	// The desktop build has no address to offer, ever: it hands api.Handler to
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
	if buildinfo.Deployment == "desktop" {
		return "", "", false
	}
	known := a.Settings.Get().KnownDomains
	addrs := remoteAddresses(r, known)
	if u, found := preferredAddress(addrs); found {
		url = u
	} else if len(addrs) > 0 {
		url = addrs[0].URL
	} else {
		return "", "", false
	}
	return instanceDisplayName(a), url, true
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
	if err := json.Unmarshal(b, &o); err != nil || o.URL == "" || o.Token == "" {
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
			name, url, ok := pairingSelf(r, a)
			if !ok {
				http.Error(w, "no address to offer for this instance", http.StatusConflict)
				return
			}
			token, err := codes.issue()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			code, err := encodeOffer(pairingOffer{Name: name, URL: url, Token: token})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The QR encodes the code itself, not a URL: whatever scans it
			// (jdp: "später mit der app scannen") only needs to read the same
			// string a person would paste by hand and POST it to its own
			// /api/instances/pairing-code/redeem - one payload, two ways to
			// get it off this screen, never two protocols to keep in sync.
			writeJSON(w, map[string]any{"code": code, "name": name, "url": url, "expiresIn": int(pairingTTL.Seconds()), "qr": renderQR(code)})
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
			selfName, selfURL, ok := pairingSelf(r, a)
			if !ok {
				http.Error(w, "no address to offer back for this instance", http.StatusConflict)
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
			cb, _ := json.Marshal(map[string]string{"token": offer.Token, "name": selfName, "url": selfURL})
			req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, offer.URL+"/api/instances/pairing-code/complete", bytes.NewReader(cb))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := hc.Do(req)
			if err != nil {
				http.Error(w, fmt.Sprintf("could not reach %s: %v", offer.URL, err), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
				// Pass the peer's own reason through instead of replacing it
				// with a guess. The peer answers 400 for several distinct
				// things - a code that really did expire, but also a name or
				// address ITS federation store rejects - and flattening them
				// all to "expired" is why a rejected name read as a clock
				// problem and stayed unfixable through any number of fresh
				// codes.
				reason, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
				msg := strings.TrimSpace(string(reason))
				if msg == "" {
					msg = "that code is invalid or has expired"
				}
				http.Error(w, msg, http.StatusBadRequest)
				return
			}

			if err := a.Federation.Add(federation.Instance{Name: offer.Name, URL: offer.URL}); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			online := a.Federation.Ping(r.Context(), offer.Name) == nil
			writeJSON(w, map[string]any{"name": offer.Name, "url": offer.URL, "online": online})
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
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			expires, ok := codes.redeemLocal(body.Token)
			if !ok {
				http.Error(w, "that code is invalid or has expired", http.StatusBadRequest)
				return
			}
			if err := a.Federation.Add(federation.Instance{Name: body.Name, URL: body.URL}); err != nil {
				// The code itself was fine - this failed on the peer's name or
				// address. Give it back, so retrying after fixing that works
				// instead of reporting an expiry that never happened.
				codes.restore(body.Token, expires)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
