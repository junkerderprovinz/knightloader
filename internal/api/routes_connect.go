package api

// The connection phrase: activating remote access, showing the phrase
// again, and joining a group somebody else's instance already started.
//
// This is the whole user-facing surface of the feature jdp asked for
// (2026-08-27: "eine zeichenfolge oder eine Seed phrase ... die man dann in
// allen anderen Instanzen einfügen kann"). There is no account here, no
// registration and no login, because there is nothing to log into: the
// relay's address is compiled in (relay.DefaultRelayURL) and possession of
// the secret is the entire authorization. See
// docs/superpowers/specs/2026-08-27-public-relay-seed-phrase-design.md.
//
// Everything below stores the SECRET and hands out the PHRASE. The relay
// only ever sees relay.DeriveKey of that secret, so neither the operator of
// the relay nor anyone who reaches its memory can reconstruct what a person
// would have to type.

import (
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
)

// ConnectInfo is what GET /api/connect answers with. Deliberately never the
// phrase itself - that needs the password (see the reveal route).
type ConnectInfo struct {
	// Active is whether this instance has a connection secret at all.
	Active bool `json:"active"`
	// Connected is whether the relay socket is actually up right now, which
	// is a different question: a stored secret with a relay that cannot be
	// reached is configured but not working, and collapsing the two is what
	// made the old relay card unable to say which was wrong.
	Connected bool `json:"connected"`
	// PasswordSet mirrors GET /api/auth, so the page can warn - before
	// anything is generated - that the phrase it is about to hand out
	// reaches every instance in the group, and that this one is unprotected.
	PasswordSet bool `json:"passwordSet"`
	// RelayURL is which relay this instance dials: the compiled-in default,
	// or an override somebody set to point at their own.
	RelayURL string `json:"relayUrl"`
	// SelfHosted is whether that is an override rather than the default -
	// the one bit of the address a person actually needs to see.
	SelfHosted bool `json:"selfHosted"`
}

func registerConnect(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/connect",
		"whether this instance has a connection phrase, whether its relay socket is up, and which relay it uses",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/activate",
		"mint a new connection phrase for this instance and start dialling the relay - answers with the phrase, the only time it is returned without the password",
		func(w http.ResponseWriter, r *http.Request) {
			// Refusing to replace an existing secret rather than silently
			// minting a new one: that would orphan every instance already
			// joined to the old phrase, and the caller would have no way to
			// tell it had happened. Leaving is explicit (DELETE below).
			if existing, err := a.Accounts.Get(relay.SeedAccountService); err == nil && existing != "" {
				http.Error(w, "this instance already has a connection phrase - remove it first to start a new group", http.StatusConflict)
				return
			}
			secret, phrase, err := seedphrase.New()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := a.Accounts.Set(relay.SeedAccountService, hex.EncodeToString(secret)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			applyRelay(a)
			writeJSON(w, map[string]any{"phrase": phrase, "info": connectInfo(a)})
		})

	reg.Add(http.MethodPost, "/api/connect/join",
		"join the group a phrase belongs to - the other half of activate, for every instance after the first",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Phrase string `json:"phrase"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			secret, err := seedphrase.Decode(body.Phrase)
			if err != nil {
				// The reason and its specifics, not a sentence. Whoever
				// mistyped a word is looking at a UI in their own language,
				// and a sentence written in Go can only be in one - the
				// browser has the translations, so it writes the sentence and
				// this says what happened. `error` stays alongside for
				// anything reading the body as plain text.
				var de *seedphrase.DecodeError
				if errors.As(err, &de) {
					w.WriteHeader(http.StatusBadRequest)
					writeJSON(w, map[string]any{
						"error":    de.Error(),
						"reason":   de.Reason,
						"word":     de.Word,
						"position": de.Position,
						"count":    de.Count,
					})
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := a.Accounts.Set(relay.SeedAccountService, hex.EncodeToString(secret)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			applyRelay(a)
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/reveal",
		"show this instance's connection phrase again - requires the password to be re-entered when one is set, because the phrase reaches every instance in the group",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			// Body optional: an instance with no password has nothing to
			// re-enter, and requiring an empty field would be theatre.
			_ = decodeBody(r, &body)

			// A live session is not enough here. It may have been opened
			// hours ago on a screen nobody is sitting at any more, and what
			// is behind this button is not this instance's own password but
			// the key to every instance in the group. Same reasoning GitHub
			// applies before showing a token again.
			if a.Auth.Enabled() && !a.Auth.Check(body.Password) {
				http.Error(w, "the password is required to show the phrase again", http.StatusForbidden)
				return
			}
			secretHex, err := a.Accounts.Get(relay.SeedAccountService)
			if err != nil || secretHex == "" {
				http.Error(w, "this instance has no connection phrase", http.StatusNotFound)
				return
			}
			secret, err := hex.DecodeString(secretHex)
			if err != nil {
				http.Error(w, "the stored secret is unreadable", http.StatusInternalServerError)
				return
			}
			phrase, err := seedphrase.Encode(secret)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]string{"phrase": phrase})
		})

	reg.Add(http.MethodDelete, "/api/connect",
		"leave the group: forget this instance's connection secret and stop dialling the relay",
		func(w http.ResponseWriter, r *http.Request) {
			// Already idempotent one layer down: accounts.Set with an empty
			// secret deletes, and deleting a key that was never there is a
			// map delete plus a write, not an error. So a second click on a
			// page that had gone stale succeeds quietly, which is what the
			// caller wanted either way.
			if err := a.Accounts.Set(relay.SeedAccountService, ""); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			applyRelay(a)
			w.WriteHeader(http.StatusNoContent)
		})
}

func connectInfo(a *app.App) ConnectInfo {
	secretHex, _ := a.Accounts.Get(relay.SeedAccountService)
	// Which relay this instance is POINTED AT, which is not the same
	// question relayTarget answers. That one reports what to actually dial
	// right now and is empty until there is a secret to dial with; this one
	// has to be answerable before anything is activated, because the page
	// says "you will be connecting through here" while the button is still
	// unpressed.
	url := a.Settings.Get().RelayURL
	selfHosted := url != ""
	if !selfHosted {
		url = relay.DefaultRelayURL
	}
	return ConnectInfo{
		Active:      secretHex != "",
		Connected:   a.Federation.RelayConnected(),
		PasswordSet: a.Auth.Enabled(),
		RelayURL:    url,
		SelfHosted:  selfHosted,
	}
}
