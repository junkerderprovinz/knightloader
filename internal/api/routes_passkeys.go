package api

// Passkeys: signing in with the key on a phone, a laptop or a security stick
// instead of typing the password.
//
// WebAuthn binds a credential to a relying party id, which has to be a domain
// name, and browsers refuse the ceremony on a bare IP address or an untrusted
// certificate. On the usual http://<LAN IP>:8749 passkeys cannot work, so
// rpIDFor refuses with a reason, and each key records the address it belongs
// to. A passkey is an additional way in and never replaces the password.

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

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// errPasskeyOrigin is the refusal an address that cannot carry a passkey gets.
// It is an English diagnostic for API callers and the log; the interface reads
// only the boolean beside it and shows its own translation, which
// web/check-passkey-reason.mjs enforces.
var errPasskeyOrigin = errors.New(
	"passkeys need a host name and this page was opened on an IP address. " +
		"The standard binds a passkey to a domain, browsers refuse the exchange on a bare address, " +
		"and they refuse it again on a certificate the browser does not trust. " +
		"Reach KnightLoader through a reverse proxy under a real name with a valid certificate and register the key there")

// rpIDFor derives the relying-party id from the request's host, without the
// port, and refuses when the address cannot carry one. It comes from the
// request rather than a setting because an instance is often reachable under
// several names and a browser only offers a key matching the address bar.
func rpIDFor(r *http.Request) (string, error) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return "", errPasskeyOrigin
	}
	// Browsers treat localhost as a secure context and accept it as a
	// relying-party id, so an SSH tunnel or a port forward works.
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
	// A TLS-terminating proxy forwards plain HTTP, so the scheme the browser
	// saw is only in its header. Trusting it is safe here: the browser signs
	// over the origin it actually used, so a wrong value can only make a
	// ceremony fail, never make a forged one pass.
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

// passkeyUser adapts this instance to the library's user-account model: there
// is one password per instance, so the account is the instance.
type passkeyUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// passkeyUserID is the account handle. It has to be stable, or every
// registered credential becomes unusable, and unguessable, so it is derived
// from the session-signing key.
func passkeyUserID(a *app.App) []byte {
	return a.Auth.DerivedID("passkey-user")
}

// passkeyUserFor builds the account with only the credentials registered for
// this address; the browser would refuse any other.
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

// passkeyCeremony is one registration or sign-in that has been started and not
// yet answered. It lives in memory only; a restart cancels it, which costs one
// more click.
type passkeyCeremony struct {
	session webauthn.SessionData
	rpID    string
	expires time.Time
}

const (
	passkeyCeremonyTTL = 5 * time.Minute
	// passkeyCeremonyMax bounds the map, since sign-in can be started without
	// a session.
	passkeyCeremonyMax = 64
)

type passkeyCeremonies struct {
	mu sync.Mutex
	m  map[string]passkeyCeremony
}

func newPasskeyCeremonies() *passkeyCeremonies {
	return &passkeyCeremonies{m: map[string]passkeyCeremony{}}
}

// begin stores session data and returns the random, single-use handle the
// finishing call has to present.
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
	// bound.
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

// take consumes a handle, so a challenge cannot be answered twice.
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

type passkeyView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// RPID is the address this key belongs to.
	RPID string `json:"rpId"`
	// UsableHere is whether this key can answer on the address the browser has
	// open. A key that cannot is marked rather than hidden, so it does not look
	// lost.
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

func registerPasskeys(reg *Registry, a *app.App) {
	ceremonies := newPasskeyCeremonies()
	// A throttle of its own: the sign-in routes answer without a session and
	// every started ceremony is stored. Sharing the password's counter would
	// let somebody hammering the password lock the owner out of this way in too.
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
				// The interface reads only this verdict; see errPasskeyOrigin.
				"supported": rpErr == nil,
				"rpId":      rpID,
				"total":     len(all),
				"here":      here,
			}
			if rpErr != nil {
				out["reason"] = rpErr.Error()
			}
			if permits(a, r, apitoken.ScopeAdmin) {
				out["passkeys"] = passkeyViews(all, rpID)
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/auth/passkey/register/begin",
		"start registering a passkey for the address this page is open on",
		func(w http.ResponseWriter, r *http.Request) {
			// A passkey stands beside the password, so there has to be one.
			if !a.Auth.Enabled() {
				writeRefusal(w, http.StatusBadRequest, "passwordFirst", "set a password before registering a passkey", nil)
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
				// An authenticator already enrolled here then says so in the
				// browser's prompt instead of producing a duplicate.
				webauthn.WithExclusions(credentialDescriptors(user.creds)),
				// Discoverable keys let a user sign in without naming an
				// account; only preferred, so an older security stick still
				// works.
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
				writeRefusal(w, http.StatusBadRequest, "expired", "that registration has expired, start it again", nil)
				return
			}
			wa, rpID, err := webAuthnFor(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if rpID != cer.rpID {
				writeRefusal(w, http.StatusBadRequest, "otherAddress",
					"this registration was started on a different address; open the one you want the key to work on and start again", nil)
				return
			}
			user, _, err := passkeyUserFor(a, rpID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
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
			// A counter that did not advance is the signal of a cloned key. The
			// library only raises it when both sides are non-zero, since many
			// authenticators always report 0.
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
			// No second factor on top: a passkey is already a device key plus
			// the person unlocking it, and the code backs up a password this
			// sign-in did not use.
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
