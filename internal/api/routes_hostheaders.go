package api

// Header profiles over HTTP: listing, replacing and deleting a user's own
// request headers for one origin, such as a session cookie, the Referer a forum
// insists on or a seedbox's Basic auth (internal/resolver/hostheaders).
//
// Every answer is what the store hands back, and hostheaders.Listing has no
// value field, so no response can carry a header value. Refusals quote nothing
// that was pasted either, since error strings reach the log ring and from there
// the diagnostics bundle. A value belongs to one origin; keepSealedValues keeps
// a form from moving it to another, as redirect.go does for redirects. The
// values stay sealed in accounts.Store, away from settings.json.

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
	// ID is the profile's name. Empty means the profile this origin already
	// has. A new profile has to be named, because a Packagizer rule addresses
	// it by that name (rules.Action.Headers).
	ID string `json:"id"`
	// Origin is the site the headers belong to, as scheme://host[:port]. It
	// decides where they may be sent, so every save has to state it.
	Origin  string           `json:"origin"`
	Headers []hostHeaderLine `json:"headers"`
}

// hostHeaderLine is one header on the way in.
type hostHeaderLine struct {
	Name string `json:"name"`
	// Value follows the convention of accounts.Credential: a new value
	// replaces what is stored, hostheaders.Redacted keeps it, and empty clears
	// the header.
	Value string `json:"value"`
}

func registerHostHeaders(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/hostheaders",
		"every stored header profile: the origin it covers and the header names it holds, never a value",
		func(w http.ResponseWriter, r *http.Request) {
			// Store.List never returns nil, so an empty install answers [].
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

			// The origin field often holds a pasted link with a signed query,
			// so it is never quoted back.
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
				// With two profiles for one origin, Store.index would silently
				// use the one whose id sorts first, and the other would never
				// apply.
				http.Error(w, "origin: the profile "+held+" already covers that origin. Edit that one, or "+
					"delete it before storing another under a new name.", http.StatusConflict)
				return
			}

			headers, err := keepSealedValues(store.Get(id), origin, body.Headers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Normalised here as well as in Save, so a bad paste is a 400 and
			// a store that cannot write is a 500.
			set, err := hostheaders.Normalize(hostheaders.Set{Origin: origin, Headers: headers})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(set.Headers) == 0 {
				// Save would read an empty set as a delete, which could also be
				// a page that lost its rows; deleting has its own route.
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
			// Re-read from the store, so the page shows the normalised names
			// and origin it will get on the next load.
			writeJSON(w, store.List())
		})

	// The id can go in the path because a ProfileID holds only letters,
	// digits and - _ or ., unlike the host names other remove routes take in a
	// body.
	reg.Add(http.MethodDelete, "/api/hostheaders/{id}",
		"delete one header profile",
		func(w http.ResponseWriter, r *http.Request) {
			store := hostHeaderStore(a)
			id := hostheaders.ProfileID(r.PathValue("id"))
			// Checked against the id list, not Get: Get returns the zero Set
			// for a profile that no longer decrypts, which is exactly the one
			// somebody comes here to remove.
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

// hostHeaderStore is the live resolver's own store. A second store over the
// same accounts.Store would save and list correctly, but the resolver keeps an
// origin index (Store.byOrigin) that only writes through its own store reset,
// so new profiles would be ignored until the next backend rewire. Sharing the
// app's *accounts.Store pointer also avoids two snapshots of accounts.json
// overwriting each other.
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
	// No such resolver, as in a registry built by hand for a test; there is
	// then no live index to keep in step.
	return hostheaders.NewStore(a.Accounts)
}

// keepSealedValues puts a stored value back wherever the form sent the
// placeholder, and refuses when it cannot. The listing carries no values, so
// without this an edit would clear every header it did not retype.
//
// A stored value only goes back while the profile still names the origin it
// was captured on; otherwise one origin's credential would be filed under
// another. The refusal names the header to paste again.
func keepSealedValues(stored hostheaders.Set, origin string, in []hostHeaderLine) ([]hostheaders.Header, error) {
	// Attach only opens for the profile's own origin, so this is also the
	// origin check.
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

// sealedValue looks one header up in what Attach handed back, ignoring case,
// since stored names are canonicalised and a form's are not.
func sealedValue(sealed map[string]string, name string) (string, bool) {
	for stored, value := range sealed {
		if strings.EqualFold(stored, name) {
			return value, true
		}
	}
	return "", false
}

// headerNameForError is what a refusal may call one header line. A name is
// not secret, but a paste that went in sideways can put a whole cookie line in
// the name field, so it is only echoed while it looks like a header token.
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
