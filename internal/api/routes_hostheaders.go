package api

// Header profiles over HTTP: which profiles exist, replacing one, deleting one.
// The profiles themselves are a user's own request headers for one origin - the
// cookie from a logged-in browser session, the Referer a forum insists on, the
// Basic auth a seedbox sits behind (internal/resolver/hostheaders).
//
// The package was finished, tested and unreachable: nothing in internal/api and
// nothing in web/src named it, so a profile could only be stored through the
// generic POST /api/accounts under a pseudo service id no interface offers and
// the account catalogue does not list. This file is the door.
//
// State, never secrets - registerAccounts' own rule for the debrid keys, and
// here it is a property of the TYPE rather than of these handlers.
// hostheaders.Listing has no value field for a later handler to forget to blank
// (see its doc comment), so nothing below builds a response shape of its own:
// every answer, the writes included, is what the store hands back.
//
// Two things this surface must not undo, both of them promises the package
// already keeps:
//
//   - A VALUE HAS NO PRINTABLE FORM. Set, Header and Profile print their names
//     and never their values through String, GoString and MarshalJSON, so no
//     route can leak one by logging what it was given. The refusals below hold
//     to the same rule and quote nothing that was pasted, because an error
//     string travels into the log ring and from there into the diagnostics
//     bundle people attach to public bug reports.
//   - A VALUE BELONGS TO ONE ORIGIN AND MAY NOT CROSS IT. redirect.go keeps
//     that across a redirect; here the same crossing is available through a
//     text field, and keepSealedValues is what closes it.
//
// The values stay sealed in accounts.Store under a pseudo service id, which is
// what keeps them out of that bundle in the first place: routes_diagnostics.go
// serialises settings.Settings, and nothing here writes a header anywhere near
// it.

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

// hostHeaderBody is one save: the profile as a person edits it.
type hostHeaderBody struct {
	// ID is what the profile is called. Empty means "whichever profile this
	// origin already has", which is what makes a save keyed by the origin: the
	// page re-sends the site it is editing and does not have to carry an id
	// around. Creating one still has to name it, because this name is also how
	// a Packagizer rule addresses the profile (rules.Action.Headers), and a
	// generated name is one nobody can type into that field.
	ID string `json:"id"`
	// Origin is the site the headers belong to, as scheme://host[:port]. It is
	// the only thing that decides where they may be sent, so it is required on
	// every save rather than kept from the stored profile: a save is the whole
	// profile, and a scope that can be left out is a scope that gets forgotten.
	Origin  string           `json:"origin"`
	Headers []hostHeaderLine `json:"headers"`
}

// hostHeaderLine is one header on the way in.
type hostHeaderLine struct {
	Name string `json:"name"`
	// Value carries three meanings, and they are the three
	// accounts.Credential.Redacted and WithSecretsFrom already established for
	// every other secret in this app rather than a second convention invented
	// here: a new value replaces what is stored, hostheaders.Redacted keeps
	// what is stored, and empty clears that header. Empty has to keep meaning
	// "clear this" or a header line could never be removed at all, which is
	// why the placeholder exists.
	Value string `json:"value"`
}

func registerHostHeaders(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/hostheaders",
		"every stored header profile: the origin it covers and the header names it holds, never a value",
		func(w http.ResponseWriter, r *http.Request) {
			// Store.List builds its slice with make, so an install that has
			// stored nothing answers [] rather than the null a page would have
			// to guard before mapping over it.
			writeJSON(w, hostHeaderStore(a).List())
		})

	reg.Add(http.MethodPost, "/api/hostheaders",
		"store or replace the header profile for one origin",
		func(w http.ResponseWriter, r *http.Request) {
			var body hostHeaderBody
			if !decodeJSON(w, r, &body) {
				return
			}
			store := hostHeaderStore(a)

			// Nothing pasted is quoted back, here or below. This field is where
			// a person puts the link they copied, signed query string and all,
			// and that query is itself a credential often enough that
			// hostheaders redacts it out of its own errors.
			origin := hostheaders.OriginOf(body.Origin)
			if origin == "" {
				http.Error(w, "origin: that is not an http or https address. Give the site the headers "+
					"belong to, scheme included (https://forum.example.org). A different port, and http "+
					"instead of https, are different sites here and need their own profile.",
					http.StatusBadRequest)
				return
			}

			held, _ := store.ForURL(origin)
			id := hostheaders.ProfileID(body.ID)
			switch {
			case strings.TrimSpace(body.ID) == "":
				id = held
				if id == "" {
					http.Error(w, "id: nothing covers that origin yet, so give this profile a name to "+
						"store it under (letters, digits and - _ or .). It is the same name a Packagizer "+
						"rule uses to send a link through this profile.", http.StatusBadRequest)
					return
				}
			case id == "":
				http.Error(w, "id: a profile name holds letters, digits and - _ or . only, at most 64 "+
					"characters. Rename it and save again.", http.StatusBadRequest)
				return
			case held != "" && held != id:
				// A second profile for one origin is refused rather than
				// stored, because the store then has to pick one of them for
				// every link on that host and the only rule it can apply is
				// "the id that sorts first" (Store.index). That is a coin toss
				// the user never sees, and it settles the same way on every
				// boot, so the losing profile looks stored and does nothing
				// forever.
				http.Error(w, "origin: the profile "+held+" already covers that origin. Edit that one, or "+
					"delete it before storing another under a new name.", http.StatusConflict)
				return
			}

			headers, err := keepSealedValues(store.Get(id), origin, body.Headers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Normalised here although Save normalises again, so that the two
			// failures can be told apart. A paste this store will not keep - a
			// name that is not a token, a line break in a value, more headers
			// than the limit - is the user's to fix and gets a 400 naming the
			// header; a store that cannot write is a 500. Save answers both
			// with one error, and reporting a broken keyring as a bad request
			// sends somebody off to re-paste a perfectly good cookie.
			set, err := hostheaders.Normalize(hostheaders.Set{Origin: origin, Headers: headers})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(set.Headers) == 0 {
				// Save reads a set with nothing left in it as a delete. That is
				// the right reading of a form somebody cleared on purpose and
				// the wrong one of a page that lost its rows on the way here,
				// and only one of the two can be told apart from the other:
				// deleting has its own route and says what it is doing.
				http.Error(w, "headers: there is no header with a value in this request. Add at least one "+
					"header, or delete the profile if that is what you meant.", http.StatusBadRequest)
				return
			}
			if err := store.Save(id, set); err != nil {
				http.Error(w, "id "+id+": the profile could not be sealed into the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable, then save it again.",
					http.StatusInternalServerError)
				return
			}
			// The listing re-read from the store, not an echo of the request:
			// header names come back in net/http's capitalisation and the
			// origin with its port spelled out, so a page redrawing from this
			// shows the profile it will get on the next load rather than the
			// shape that was typed at it.
			writeJSON(w, store.List())
		})

	// The id in the path, unlike POST /api/ytdlp/cookies/remove and POST
	// /api/hosterauth/logins/remove next door, which both take their key in a
	// body. Those keys are dotted host names a person typed; this one is a
	// hostheaders.ProfileID, which holds letters, digits and - _ or . and
	// nothing else, so it raises none of the encoding questions that made a
	// body the better answer there.
	reg.Add(http.MethodDelete, "/api/hostheaders/{id}",
		"delete one header profile",
		func(w http.ResponseWriter, r *http.Request) {
			store := hostHeaderStore(a)
			id := hostheaders.ProfileID(r.PathValue("id"))
			// Existence is asked of the id list and never of Get. Get answers
			// the zero Set for a row whose ciphertext no longer opens exactly
			// as it does for one that was never stored (see its doc comment),
			// and a profile that cannot be decrypted - a .keyring replaced
			// under a running install - is precisely the one somebody has come
			// here to remove. Deciding from Get would answer 404 for it
			// forever and leave the row sealed in the file.
			if id == "" || !slices.Contains(store.IDs(), id) {
				http.Error(w, "id: no header profile is stored under that name. The names are the ones "+
					"GET /api/hostheaders lists.", http.StatusNotFound)
				return
			}
			if err := store.Remove(id); err != nil {
				http.Error(w, "id "+id+": the profile could not be removed from the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable, then try again.",
					http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// hostHeaderStore is the store these routes read and write, and it is
// deliberately the LIVE resolver's own rather than a second one wrapped around
// the same accounts.Store.
//
// A second one would seal and list correctly and break routing in a way nothing
// reports. hostheaders.Store keeps an origin index because Resolver.Match is
// called under the app's lock and cannot open an encrypted file per link (see
// Store.byOrigin); that index is built on first use and dropped only by a write
// THROUGH THAT SAME STORE. The resolver's store is replaced when internal/app
// rewires its backends, which after boot happens on an account change or every
// six hours, whichever comes first. So a profile saved through a store of our
// own would be sealed, listed, and ignored by every paste until then: the exact
// shape of "complete, tested and unreachable" this file exists to end.
//
// The wrapping itself is safe for the reason NewCookieStore documents next
// door: this shares the app's own *accounts.Store pointer, and two
// accounts.Store instances over one accounts.json would each hold their own
// snapshot of the whole file, the second write erasing the first.
func hostHeaderStore(a *app.App) *hostheaders.Store {
	for _, res := range a.Registry.List() {
		hh, ok := res.(hostheaders.Resolver)
		if !ok {
			continue
		}
		if store, ok := hh.Profiles.(*hostheaders.Store); ok {
			return store
		}
	}
	// No resolver of that type registered: a registry assembled by hand in a
	// test, or one handed a stand-in for Profiles. The values live in
	// accounts.Store either way, so a store over the same pointer lists and
	// saves the same profiles; the index above is the only thing missed, and
	// there is no live resolver holding a stale one.
	return hostheaders.NewStore(a.Accounts)
}

// keepSealedValues puts a stored value back wherever the form sent the
// placeholder, and refuses when it cannot.
//
// It is needed because the listing carries no values at all: a form re-sending
// what it was given sends empty ones, and empty means "clear this" here as it
// does everywhere else in this app. Without this, editing a profile's origin -
// or adding one header to it - would silently delete the headers it was being
// edited for, and the user would find out at the next download. This is the
// same mechanism accounts.Credential.Redacted and WithSecretsFrom already use
// for every other secret in the app, reached through the same placeholder
// constant, rather than a second redaction scheme.
//
// A stored value goes back ONLY while the profile still names the origin it was
// captured on. Re-pointing a profile at another host and sending the
// placeholder asks for one origin's credential to be filed under another, which
// is the crossing internal/resolver/hostheaders/redirect.go exists to prevent,
// arriving through a text field instead of through a redirect. The refusal
// names the header so the person knows which one to paste again.
func keepSealedValues(stored hostheaders.Set, origin string, in []hostHeaderLine) ([]hostheaders.Header, error) {
	// Attach is the one door plaintext leaves that package by, and it opens
	// only for the profile's own origin - so this is also the origin check: a
	// profile stored for somewhere else hands back nothing at all, and every
	// placeholder below is then refused by name.
	sealed := stored.Attach(origin)
	out := make([]hostheaders.Header, 0, len(in))
	for _, h := range in {
		if h.Value != hostheaders.Redacted {
			out = append(out, hostheaders.Header{Name: h.Name, Value: h.Value})
			continue
		}
		value, ok := sealedValue(sealed, h.Name)
		if !ok {
			return nil, errors.New("headers: " + headerNameForError(h.Name) + " came back as the " +
				"placeholder, but nothing is stored under that name for this origin. Paste the header " +
				"again. A profile moved to another site never brings its values with it.")
		}
		out = append(out, hostheaders.Header{Name: h.Name, Value: value})
	}
	return out, nil
}

// sealedValue looks one header up in what Attach handed back, ignoring case.
// A header name means the same thing however it is capitalised: what is stored
// has been through the package's own canonicalisation and what a form sends has
// not, so "cookie" from a page must still find the stored Cookie rather than be
// told nothing is there.
func sealedValue(sealed map[string]string, name string) (string, bool) {
	for stored, value := range sealed {
		if strings.EqualFold(stored, name) {
			return value, true
		}
	}
	return "", false
}

// headerNameForError is what a refusal may call one header line.
//
// A header NAME is not a secret - "this profile sends an Authorization and a
// Cookie" is exactly what a settings page has to be able to say - but this
// field holds whatever arrived in the request, and a paste that went in
// sideways puts a whole cookie line in it. So it is echoed only while it still
// looks like a header token, the shape hostheaders' own canonicalName insists
// on, and described rather than quoted otherwise.
func headerNameForError(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > hostheaders.MaxNameLen {
		return "one header"
	}
	for _, r := range name {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return "one header"
		}
	}
	return name
}
