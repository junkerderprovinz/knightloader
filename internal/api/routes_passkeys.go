package api

// Passkeys: signing in with the key on a phone, a laptop or a security stick
// instead of typing the password.
//
// ---------------------------------------------------------------------------
// THE CONSTRAINT THAT SHAPES ALL OF THIS, stated once because every decision
// below follows from it.
//
// WebAuthn binds a credential to a RELYING PARTY ID, which the specification
// requires to be a DOMAIN NAME. An IP address is not one, and browsers refuse
// the ceremony outright on an origin whose host is a bare address. They refuse
// it a second way on a page whose certificate the browser does not trust.
//
// KnightLoader's ordinary installation is reached at http://[LAN IP]:8749. On
// that address passkeys cannot work at all, and no amount of code here changes
// that. They work when the instance is reached through a real host name over a
// certificate the browser accepts, which in practice means a reverse proxy -
// and that is the setup somebody who wants passkeys is already running, because
// it is also the only sane way to expose this to the internet.
//
// So the feature is built to SAY so. rpIDFor refuses with a reason rather than
// letting the browser answer "NotAllowedError" to a button nobody should have
// been offered, the card writes that reason in the reader's own language, and a
// registered key records WHICH address it belongs to, because a key registered
// through the proxy does not exist over the IP and the other way round.
//
// SECOND RULE, and it outranks the first: a passkey never replaces the
// password. It is an additional way IN, never the only one. Somebody whose
// phone is lost, whose proxy is down, or who is standing in front of the box
// with a browser and its LAN address must still be able to reach their own
// downloads.
// ---------------------------------------------------------------------------

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// errPasskeyOrigin is the refusal an address that cannot carry a passkey gets.
//
// It is a DIAGNOSTIC, in English, for an API caller and for the log - never the
// paragraph the interface shows. The card writes its own translated copy and
// reads only the boolean beside this, because a server answers in one language
// and this sentence is the explanation of a whole feature to somebody whose
// interface is running in theirs. web/check-passkey-reason.mjs is what keeps
// that true.
var errPasskeyOrigin = errors.New(
	"passkeys need a host name and this page was opened on an IP address. " +
		"The standard binds a passkey to a domain, browsers refuse the exchange on a bare address, " +
		"and they refuse it again on a certificate the browser does not trust. " +
		"Reach KnightLoader through a reverse proxy under a real name with a valid certificate and register the key there")

// rpIDFor derives the relying-party id from the request, and refuses when the
// address cannot carry one.
//
// The id is the HOST of the page the operator has open, without the port. Taken
// from the request rather than from a setting, on purpose: the same instance is
// commonly reachable several ways, a browser only ever offers a key whose id
// matches the address bar, and a configured value would be wrong for every
// address except the one somebody remembered to type.
func rpIDFor(r *http.Request) (string, error) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return "", errPasskeyOrigin
	}
	// localhost is the documented exception: the specification treats it as a
	// secure context and browsers accept it as a relying-party id, which makes
	// an SSH tunnel or a port forward a working way to use this.
	if strings.EqualFold(host, "localhost") {
		return "localhost", nil
	}
	if net.ParseIP(host) != nil {
		return "", errPasskeyOrigin
	}
	return strings.ToLower(host), nil
}

// originFor rebuilds the origin the browser will report, so the library checks
// the ceremony against it rather than against a guess.
func originFor(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// A proxy that terminates TLS forwards plain HTTP inwards, so the scheme the
	// BROWSER saw is the one it reports and the one that has to match. Its own
	// header is the only witness to it. Trusting a header here is safe in a way
	// it is not in the login throttle: getting this wrong makes a legitimate
	// ceremony fail, it cannot make a forged one pass - the browser signs over
	// the origin it actually used, and a mismatch is a refusal.
	if fp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); fp != "" {
		if i := strings.IndexByte(fp, ','); i > 0 {
			fp = strings.TrimSpace(fp[:i])
		}
		if fp == "http" || fp == "https" {
			scheme = fp
		}
	}
	return scheme + "://" + r.Host
}

func webAuthnFor(r *http.Request) (*webauthn.WebAuthn, string, error) {
	rpID, err := rpIDFor(r)
	if err != nil {
		return nil, "", err
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "KnightLoader",
		RPOrigins:     []string{originFor(r)},
	})
	if err != nil {
		return nil, "", err
	}
	return w, rpID, nil
}

// ---------------------------------------------------------------------------
// The account the library insists on
// ---------------------------------------------------------------------------

// passkeyUser adapts this instance to the library's one-user-account model.
//
// KnightLoader has no user accounts at all: there is one password for the
// instance. So the account IS the instance. Its handle has to be stable - a
// changed handle makes every registered credential unusable - and must not be
// guessable from outside, so it is derived from the key that already defines
// this instance and already survives a restart: the one auth.json keeps to sign
// session cookies.
type passkeyUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// passkeyUserID is that handle. auth.Guard.DerivedID does the work and its own
// comment says why the key is never handed out directly.
func passkeyUserID(a *app.App) []byte {
	return a.Auth.DerivedID("passkey-user")
}

// passkeyUserFor builds the account with the credentials registered for THIS
// address, and only those: a browser offered a key whose relying-party id does
// not match the page refuses it, so listing the others would raise a prompt
// that cannot succeed.
func passkeyUserFor(a *app.App, rpID string) (passkeyUser, []store.Passkey, error) {
	rows, err := a.Store.PasskeysForRP(rpID)
	if err != nil {
		return passkeyUser{}, nil, err
	}
	u := passkeyUser{id: passkeyUserID(a), name: "knightloader"}
	for _, p := range rows {
		u.creds = append(u.creds, webauthn.Credential{
			ID:        p.CredentialID,
			PublicKey: p.PublicKey,
			Transport: parseTransports(p.Transports),
			Flags:     webauthn.CredentialFlags{BackupEligible: p.BackedUp, BackupState: p.BackedUp},
			Authenticator: webauthn.Authenticator{
				AAGUID:    p.AAGUID,
				SignCount: p.SignCount,
			},
		})
	}
	return u, rows, nil
}

func parseTransports(s string) []protocol.AuthenticatorTransport {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]protocol.AuthenticatorTransport, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, protocol.AuthenticatorTransport(p))
		}
	}
	return out
}

func joinTransports(ts []protocol.AuthenticatorTransport) string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		if s := strings.TrimSpace(string(t)); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ",")
}

func credentialDescriptors(creds []webauthn.Credential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(creds))
	for _, c := range creds {
		out = append(out, c.Descriptor())
	}
	return out
}

// ---------------------------------------------------------------------------
// Ceremonies in flight
// ---------------------------------------------------------------------------

// passkeyCeremony is one registration or sign-in that has been started and not
// yet answered.
//
// In memory rather than in the database: it is valid for a minute or two, it is
// worthless afterwards, and writing an authentication challenge to disk on every
// button press buys nothing. The consequence is that a restart cancels a
// half-finished ceremony, which costs one more click.
type passkeyCeremony struct {
	session webauthn.SessionData
	rpID    string
	expires time.Time
}

const (
	passkeyCeremonyTTL = 5 * time.Minute
	// passkeyCeremonyMax bounds the map. A registration is only startable with a
	// session, but the SIGN-IN half is reachable without one, so the ceiling
	// cannot depend on the caller behaving.
	passkeyCeremonyMax = 64
)

type passkeyCeremonies struct {
	mu sync.Mutex
	m  map[string]passkeyCeremony
}

func newPasskeyCeremonies() *passkeyCeremonies {
	return &passkeyCeremonies{m: map[string]passkeyCeremony{}}
}

// begin stores session data and returns the opaque handle the finishing call has
// to present. The handle is what proves the finishing request belongs to the
// ceremony this instance started: random, single-use and short-lived.
func (c *passkeyCeremonies) begin(s *webauthn.SessionData, rpID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	// Still full after the sweep: drop the oldest rather than grow without
	// bound. Losing somebody's in-flight ceremony costs one more click; an
	// unbounded map reachable without a session does not.
	for len(c.m) >= passkeyCeremonyMax {
		oldestKey, oldest := "", time.Time{}
		for k, v := range c.m {
			if oldest.IsZero() || v.expires.Before(oldest) {
				oldestKey, oldest = k, v.expires
			}
		}
		delete(c.m, oldestKey)
	}
	c.m[id] = passkeyCeremony{session: *s, rpID: rpID, expires: now.Add(passkeyCeremonyTTL)}
	return id, nil
}

// take consumes a handle. Single-use: a challenge that has been answered once
// must not be answerable again.
func (c *passkeyCeremonies) take(id string) (passkeyCeremony, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[id]
	if !ok {
		return passkeyCeremony{}, false
	}
	delete(c.m, id)
	if time.Now().After(v.expires) {
		return passkeyCeremony{}, false
	}
	return v, true
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

type passkeyView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// RPID is the address this key belongs to, shown in the list because a key
	// registered through the proxy does not exist over the IP.
	RPID string `json:"rpId"`
	// UsableHere is whether this key can answer on the address the browser has
	// open right now. An entry that cannot is MARKED, never hidden: hiding a key
	// somebody deliberately created would make it look lost.
	UsableHere bool   `json:"usableHere"`
	BackedUp   bool   `json:"backedUp"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
	Transports string `json:"transports"`
}

func passkeyViews(rows []store.Passkey, hereRPID string) []passkeyView {
	out := make([]passkeyView, 0, len(rows))
	for _, p := range rows {
		out = append(out, passkeyView{
			ID: p.ID, Name: p.Name, RPID: p.RPID,
			UsableHere: hereRPID != "" && p.RPID == hereRPID,
			BackedUp:   p.BackedUp, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt,
			Transports: p.Transports,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

func registerPasskeys(reg *Registry, a *app.App) {
	ceremonies := newPasskeyCeremonies()
	// This door's OWN counter, deliberately not the password login's.
	//
	// It is not here to stop guessing: an assertion is a signature over this
	// instance's own challenge, and there is nothing to guess. It is here
	// because these two routes answer without a session, so how often they are
	// called is chosen by the caller - and a ceremony started is a ceremony
	// stored.
	//
	// Separate from the password's counter for a reason that only shows up
	// under attack. A shared one would mean somebody hammering the password
	// also shuts the passkey door, which takes the owner's WORKING way in away
	// exactly when they need it. Nothing is gained by sharing, because neither
	// route can be used to learn anything about the other.
	gate := newLoginGate()

	reg.AddOpen(http.MethodGet, "/api/auth/passkeys",
		"whether this address can carry passkeys and how many are registered; open because the sign-in screen has to know before anybody is signed in. The registered keys themselves are only listed to a session",
		func(w http.ResponseWriter, r *http.Request) {
			rpID, rpErr := rpIDFor(r)
			all, err := a.Store.ListPasskeys()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			here := 0
			for _, p := range all {
				if rpID != "" && p.RPID == rpID {
					here++
				}
			}
			out := map[string]any{
				// supported is the verdict, and it is the only part of this the
				// interface is allowed to read. See errPasskeyOrigin.
				"supported": rpErr == nil,
				"rpId":      rpID,
				"total":     len(all),
				"here":      here,
			}
			if rpErr != nil {
				out["reason"] = rpErr.Error()
			}
			if authenticated(a, r) {
				out["passkeys"] = passkeyViews(all, rpID)
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/auth/passkey/register/begin",
		"start registering a passkey for the address this page is open on",
		func(w http.ResponseWriter, r *http.Request) {
			// A passkey stands beside the password. Without one there is no
			// login for it to be a second way into, and the check belongs here
			// rather than only in the card: a card can be out of date, a route
			// cannot.
			if !a.Auth.Enabled() {
				http.Error(w, "set a password before registering a passkey", http.StatusBadRequest)
				return
			}
			wa, rpID, err := webAuthnFor(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			user, _, err := passkeyUserFor(a, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			creation, session, err := wa.BeginRegistration(
				user,
				// The credentials already registered for this address are
				// excluded, so an authenticator that is already enrolled says so
				// in the browser's own prompt instead of producing a duplicate
				// this instance then has to refuse.
				webauthn.WithExclusions(credentialDescriptors(user.creds)),
				// Resident (discoverable) keys, because the point of a passkey is
				// signing in without first saying who you are. Preferred rather
				// than required: an older security stick that cannot store one
				// still works as a way in rather than being turned away.
				webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
					ResidentKey:      protocol.ResidentKeyRequirementPreferred,
					UserVerification: protocol.VerificationPreferred,
				}),
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handle, err := ceremonies.begin(session, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ceremonyId": handle, "options": creation.Response})
		})

	reg.Add(http.MethodPost, "/api/auth/passkey/register/finish",
		"finish registering a passkey: the browser's own answer, a ceremony handle and a name",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				CeremonyID string          `json:"ceremonyId"`
				Name       string          `json:"name"`
				Credential json.RawMessage `json:"credential"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			cer, ok := ceremonies.take(body.CeremonyID)
			if !ok {
				http.Error(w, "that registration has expired, start it again", http.StatusBadRequest)
				return
			}
			wa, rpID, err := webAuthnFor(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if rpID != cer.rpID {
				http.Error(w, "this registration was started on a different address; open the one you want the key to work on and start again", http.StatusBadRequest)
				return
			}
			user, _, err := passkeyUserFor(a, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Handed to the library verbatim: re-encoding the browser's answer
			// here would mean re-implementing the parsing that validates it.
			parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cred, err := wa.CreateCredential(user, cer.session, parsed)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			name := strings.TrimSpace(body.Name)
			if name == "" {
				// The name is the operator's own label and means nothing to the
				// protocol, so an empty one is filled in rather than refused.
				name = "Passkey"
			}
			saved, err := a.Store.AddPasskey(store.Passkey{
				Name:         name,
				CredentialID: cred.ID,
				PublicKey:    cred.PublicKey,
				AAGUID:       cred.Authenticator.AAGUID,
				SignCount:    cred.Authenticator.SignCount,
				Transports:   joinTransports(cred.Transport),
				RPID:         rpID,
				BackedUp:     cred.Flags.BackupEligible,
			})
			if errors.Is(err, store.ErrPasskeyExists) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			log.Printf("api: passkey %q registered for %s", saved.Name, rpID)
			writeJSON(w, map[string]any{"passkey": passkeyViews([]store.Passkey{saved}, rpID)[0]})
		})

	reg.AddOpen(http.MethodPost, "/api/auth/passkey/login/begin",
		"start signing in with a passkey; open because it is one half of the way in",
		func(w http.ResponseWriter, r *http.Request) {
			if !a.Auth.Enabled() {
				http.Error(w, "no password is set on this instance", http.StatusBadRequest)
				return
			}
			if gate.blocked(r) {
				http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
				return
			}
			wa, rpID, err := webAuthnFor(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			user, rows, err := passkeyUserFor(a, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if len(rows) == 0 {
				http.Error(w, "no passkey is registered for this address", http.StatusBadRequest)
				return
			}
			assertion, session, err := wa.BeginLogin(user)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handle, err := ceremonies.begin(session, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ceremonyId": handle, "options": assertion.Response})
		})

	reg.AddOpen(http.MethodPost, "/api/auth/passkey/login/finish",
		"finish signing in with a passkey and take the session cookie; open because it is the other half of the way in",
		func(w http.ResponseWriter, r *http.Request) {
			if !a.Auth.Enabled() {
				http.Error(w, "no password is set on this instance", http.StatusBadRequest)
				return
			}
			if gate.blocked(r) {
				http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
				return
			}
			var body struct {
				CeremonyID string          `json:"ceremonyId"`
				Credential json.RawMessage `json:"credential"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			cer, ok := ceremonies.take(body.CeremonyID)
			if !ok {
				gate.fail(r)
				http.Error(w, "that sign-in has expired, try again", http.StatusBadRequest)
				return
			}
			wa, rpID, err := webAuthnFor(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if rpID != cer.rpID {
				gate.fail(r)
				http.Error(w, "that sign-in was started on a different address", http.StatusBadRequest)
				return
			}
			user, rows, err := passkeyUserFor(a, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
			if err != nil {
				gate.fail(r)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cred, err := wa.ValidateLogin(user, cer.session, parsed)
			if err != nil {
				gate.fail(r)
				log.Printf("api: passkey sign-in refused: %v", err)
				http.Error(w, "that passkey was not accepted", http.StatusUnauthorized)
				return
			}
			// A counter that did not advance when the authenticator says it keeps
			// one is the documented signal of a cloned key. Many modern
			// authenticators report 0 and never move it, which is why the library
			// only raises this when both sides are non-zero: a fixed zero is "no
			// counter", not "no progress".
			if cred.Authenticator.CloneWarning {
				gate.fail(r)
				log.Printf("api: passkey sign-in refused: the authenticator's counter went backwards, which is how a copied key looks")
				http.Error(w, "that passkey was refused: its counter went backwards, which is how a copied key looks. Remove it and register a new one", http.StatusUnauthorized)
				return
			}
			for _, p := range rows {
				if string(p.CredentialID) == string(cred.ID) {
					if tErr := a.Store.TouchPasskey(p.ID, cred.Authenticator.SignCount, time.Now().Unix()); tErr != nil {
						log.Printf("api: passkey: recording use: %v", tErr)
					}
					log.Printf("api: passkey %q signed in", p.Name)
					break
				}
			}
			gate.pass(r)
			// No second factor is asked for on top. A passkey is already two
			// things - a key the device holds and the person unlocking it - and
			// the code exists to back up a password, which this sign-in did not
			// use. Asking for both would be asking for a third factor and calling
			// it a second one.
			setSession(w, r, a.Auth.Issue())
			writeJSON(w, map[string]any{"enabled": true, "authenticated": true})
		})

	reg.Add(http.MethodPatch, "/api/auth/passkeys/{id}", "rename one registered passkey",
		func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.PathValue("id"))
			var body struct {
				Name string `json:"name"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			name := strings.TrimSpace(body.Name)
			if name == "" {
				http.Error(w, "a passkey needs a name", http.StatusBadRequest)
				return
			}
			if err := a.Store.RenamePasskey(id, name); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodDelete, "/api/auth/passkeys/{id}", "unregister one passkey; the password still works",
		func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.PathValue("id"))
			if id == "" {
				http.Error(w, "no passkey id", http.StatusBadRequest)
				return
			}
			if err := a.Store.DeletePasskey(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
