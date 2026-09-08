package api

// The stored addresses that get called once a package has finished and its files
// are in place: what is stored, storing one, deleting one, and calling one on
// purpose to see what the far end says.
//
// This is the one place in the app where a settings ROW and a sealed CREDENTIAL
// are two halves of one thing a person edits in one form, and the whole shape of
// this file follows from that.
//
//   - The row (address, method, header NAME, wait) is configuration. It goes in
//     settings.json through the same Store every other setting does, so a backup
//     carries it and the Advanced key table can show it.
//   - The header VALUE is a credential. It goes in accounts.Store under
//     mediahook.Service, so it reaches neither settings.json, the diagnostics
//     bundle nor the Advanced key table - see internal/mediahook's package
//     comment for all three doors that closes.
//
// So this file writes both, and the listing answers only one: hasValue, never
// the value. That is a property of the row TYPE below rather than of these
// handlers, the same rule hostheaders.Listing states for itself - a listing
// struct with a value field on it would be safe exactly as long as every future
// handler remembered to blank it.
//
// A ROW WRITE STILL GOES THROUGH SetPartial and never through a read of the
// whole document followed by a write of it. Two people editing two settings
// pages at once is ordinary, and a route that read everything and wrote
// everything back would carry the other one's edits back to whatever this caller
// last saw - the argument PATCH /api/settings and POST /api/relay both make for
// the same mechanism.
//
// NOT ON EITHER FORWARDING ALLOWLIST, and deliberately: a sibling instance's
// media library is not this box's business, and the credential half is sealed on
// the machine it was typed on. A new route is outside both lists until somebody
// argues it in, which is how routes_relay.go's own allowlist is meant to work.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// mediaHookRow is one stored address as the settings page sees it: the row
// itself, plus the four things only the server can answer about it.
//
// The Hook is EMBEDDED rather than copied field by field, so that a field added
// to mediahook.Hook appears here without anybody remembering to come back - and,
// more to the point, so that a field can never be added to the response by
// accident: the only way a value could appear in this JSON is if somebody put a
// value field on Hook, which its own doc comment forbids at length.
type mediaHookRow struct {
	mediahook.Hook
	// HasValue says whether a header value is sealed for this address. Never the
	// value, and asked of the id list rather than of the value itself - see
	// mediahook.Store.IDs for the one case where the two differ and why this is
	// the honest half.
	HasValue bool `json:"hasValue"`
	// Host is host[:port] the call resolves to, so the card can print where it
	// goes without re-parsing the address in the browser.
	Host string `json:"host"`
	// Private says the target is loopback or on a private range. False means the
	// call, and the header with it, leaves this machine - which is a sentence
	// worth drawing about an address that carries a token.
	Private bool `json:"private"`
	// UsedBy is the category ids pointing at this address. Empty means it is
	// stored and nothing calls it, which is a state worth being able to see: it
	// is what "I set this up and nothing happens" actually looks like.
	UsedBy []string `json:"usedBy"`
	// Last is the last call this PROCESS made, test calls included, or null. In
	// memory only - see mediahook.Runner.Last.
	Last *mediahook.Result `json:"last"`
}

// mediaHookBody is one save: the row as a person edits it, plus the secret.
type mediaHookBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Method      string `json:"method"`
	HeaderName  string `json:"headerName"`
	WaitSeconds int    `json:"waitSeconds"`
	// HeaderValue carries the three meanings every secret in this app carries,
	// and they are accounts.Credential.Redacted's own rather than a second
	// convention invented here: a new value replaces what is stored,
	// accounts.Redacted keeps it, and empty clears it. Empty has to keep meaning
	// "clear this" or a stored token could never be removed at all, which is
	// exactly why the placeholder exists - and why a form that re-sends what the
	// listing gave it (nothing) would destroy the token it was opened to edit.
	// See HeaderProfiles.tsx and keepSealedValues for the same trap, solved the
	// same way.
	HeaderValue string `json:"headerValue"`
}

func registerMediaHooks(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/mediahooks",
		"every stored address called after a package finishes: where it goes, whether a header value is sealed for it, which drawers point at it and how the last call went, never the value itself",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, mediaHookRows(a))
		})

	reg.Add(http.MethodPost, "/api/mediahooks",
		"store or replace one address, sealing its header value in the credential store; the placeholder keeps what is already stored and an empty value clears it",
		func(w http.ResponseWriter, r *http.Request) {
			var body mediaHookBody
			if !decodeJSON(w, r, &body) {
				return
			}
			id := mediahook.HookID(body.ID)
			if id == "" {
				http.Error(w, "id: a name holds letters, digits and - _ or . only, at most 64 characters. "+
					"It is the name a drawer under Categories points at, so it cannot be changed afterwards.",
					http.StatusBadRequest)
				return
			}
			hook := mediahook.Hook{
				ID:          id,
				Name:        strings.TrimSpace(body.Name),
				URL:         strings.TrimSpace(body.URL),
				Method:      strings.ToUpper(strings.TrimSpace(body.Method)),
				HeaderName:  strings.TrimSpace(body.HeaderName),
				WaitSeconds: body.WaitSeconds,
			}
			if hook.Method == "" {
				// A client that never showed the menu still gets a working row.
				// GET is the request that changes least at the far end, which is
				// the right default for something nobody chose.
				hook.Method = mediahook.MethodGet
			}
			if err := hook.Validate(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cfg := a.Settings.Get()
			next, err := mediaHookList(cfg, hook)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The whole table validated as a table, not just this row: a save
			// that stored a duplicate id would leave two addresses one drawer
			// cannot tell apart, and settings.ValidateMediaHooks is the one place
			// that decides what a usable table is.
			preview := cfg
			preview.MediaHooks = next
			if err := preview.ValidateMediaHooks(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// THE VALUE FIRST, THE ROW SECOND. Both orders leave a mess if the
			// second half fails, and this is the smaller one: a sealed value under
			// an id no row names is invisible and inert, while a row saved without
			// its value is an address that gets called and refused with a 401 the
			// person has no reason to expect.
			store := a.MediaHookStore()
			if body.HeaderValue != accounts.Redacted {
				if err := store.SetValue(id, body.HeaderValue); err != nil {
					http.Error(w, "id "+id+": the header value could not be sealed into the credential store ("+
						err.Error()+"). Check that KnightLoader's data directory is writable, then save it again.",
						http.StatusInternalServerError)
					return
				}
			}
			raw, err := json.Marshal(next)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := a.PatchSettings(map[string]json.RawMessage{"mediaHooks": raw}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The listing re-read from the store rather than an echo of the
			// request: the sanitiser folds the id, clamps the wait and drops a
			// header name this build cannot send, so a page redrawing from this
			// shows what it will get on the next load instead of what it typed.
			writeJSON(w, mediaHookRows(a))
		})

	reg.Add(http.MethodDelete, "/api/mediahooks/{id}",
		"delete one stored address and the header value sealed for it; refused while a category drawer still points at it",
		func(w http.ResponseWriter, r *http.Request) {
			cfg := a.Settings.Get()
			id := mediahook.HookID(r.PathValue("id"))
			if _, ok := cfg.MediaHookFor(id); !ok {
				http.Error(w, "id: no address is stored under that name. The names are the ones "+
					"GET /api/mediahooks lists.", http.StatusNotFound)
				return
			}
			// A drawer pointing at a deleted address is refused by
			// ValidateMediaHooks on the next settings save, so deleting one out
			// from under a drawer would leave the whole settings page unsaveable
			// with nothing on it saying why. Named drawers rather than a count,
			// because "set those to Call nothing first" is only actionable if the
			// person knows which ones.
			if users := cfg.MediaHookUsers(id); len(users) > 0 {
				http.Error(w, "id "+id+": these categories still call it: "+strings.Join(users, ", ")+
					". Set them to call nothing first, then delete the address.", http.StatusConflict)
				return
			}
			next := make([]mediahook.Hook, 0, len(cfg.MediaHooks))
			for _, h := range cfg.MediaHooks {
				if mediahook.HookID(h.ID) != id {
					next = append(next, h)
				}
			}
			raw, err := json.Marshal(next)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := a.PatchSettings(map[string]json.RawMessage{"mediaHooks": raw}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The row is gone either way; a value left sealed under an id nothing
			// names would be a credential nobody can see and nobody can remove.
			// Reported as a 500 rather than swallowed, because the person asked
			// for the token to be gone and it is not.
			if err := a.MediaHookStore().Remove(id); err != nil {
				http.Error(w, "id "+id+": the address is gone but its header value could not be removed from "+
					"the credential store ("+err.Error()+"). Check that KnightLoader's data directory is writable.",
					http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/mediahooks/{id}/test",
		"call one stored address once, with its sealed header, and report what came back; stores nothing and tells no drawer anything",
		func(w http.ResponseWriter, r *http.Request) {
			id := mediahook.HookID(r.PathValue("id"))
			hook, ok := a.Settings.Get().MediaHookFor(id)
			if !ok {
				http.Error(w, "id: no address is stored under that name. Save the address first, then test it.",
					http.StatusNotFound)
				return
			}
			// 200 with the reason in the body even when the call failed, the same
			// call POST /api/feeds/test makes: the request was perfectly good, it
			// was the far end that was not, and a 4xx would have the browser log
			// the one answer somebody is meant to read. It also keeps the panel
			// one shape, so a client renders a failure without a second code path.
			//
			// The request's own context, so a browser that navigated away does not
			// leave this waiting on somebody's server for the whole ceiling.
			writeJSON(w, a.TestMediaHook(r.Context(), hook))
		})
}

// mediaHookRows builds the listing: the stored table, in its own order, joined
// onto what only this process can say about each row.
//
// The CONFIGURED order and never a sorted one, for the reason GET /api/feeds
// gives: this table is drawn under the rows on a settings card, and a status list
// that does not line up with the rows it describes is one somebody reads the
// wrong row out of.
func mediaHookRows(a *app.App) []mediaHookRow {
	cfg := a.Settings.Get()
	store := a.MediaHookStore()
	sealed := make(map[string]bool)
	for _, id := range store.IDs() {
		sealed[id] = true
	}
	rows := make([]mediaHookRow, 0, len(cfg.MediaHooks))
	for _, h := range cfg.MediaHooks {
		row := mediaHookRow{
			Hook:     h,
			HasValue: sealed[mediahook.HookID(h.ID)],
			Host:     h.Host(),
			Private:  h.IsPrivateTarget(),
			// Never nil, so a card mapping over it does not have to guard first.
			UsedBy: append([]string{}, cfg.MediaHookUsers(h.ID)...),
		}
		if last, ok := a.LastMediaHookCall(h.ID); ok {
			c := last
			row.Last = &c
		}
		rows = append(rows, row)
	}
	return rows
}

// mediaHookList is the table with one row stored into it: replaced in place when
// the id is already there, appended when it is not.
//
// IN PLACE, and that is the point of doing it here rather than with an append and
// a sort. The order of this list is the order the picker on the Categories page
// offers, so an edited address that jumped to the bottom of that menu would read
// as a different address to whoever was looking at it.
func mediaHookList(cfg settings.Settings, hook mediahook.Hook) ([]mediahook.Hook, error) {
	out := make([]mediahook.Hook, len(cfg.MediaHooks))
	copy(out, cfg.MediaHooks)
	for i, h := range out {
		if mediahook.HookID(h.ID) == mediahook.HookID(hook.ID) {
			out[i] = hook
			return out, nil
		}
	}
	if len(out) >= mediahook.MaxHooks {
		// Refused here rather than left to the sanitiser, which keeps the first N
		// IN ORDER and drops the rest without a word - so a save past the ceiling
		// would answer 200 and store nothing.
		return nil, fmt.Errorf("there are already %d stored addresses, which is the limit. "+
			"Delete one you no longer call, then store this one.", mediahook.MaxHooks)
	}
	return append(out, hook), nil
}
