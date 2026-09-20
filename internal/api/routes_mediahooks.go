package api

// The stored addresses called once a package has finished and its files are
// in place: listing, storing, deleting, and a test call.
//
// Each address is a settings row (address, method, header name, wait) in
// settings.json plus a header value sealed in accounts.Store under
// mediahook.Service, away from settings.json, the diagnostics bundle and the
// Advanced table. The listing type has no value field, only hasValue. Row
// writes go through PatchSettings so a concurrent edit elsewhere survives.
//
// The routes are not forwarded to peers: a sibling's media library is not this
// box's business, and the credential stays on the machine it was typed on.

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

// mediaHookRow is one stored address as the settings page sees it, plus what
// only the server can say about it. Hook is embedded, so new fields appear
// without edits here; Hook itself has no value field.
type mediaHookRow struct {
	mediahook.Hook
	// HasValue says whether a header value is sealed for this address, asked
	// of the id list (see mediahook.Store.IDs).
	HasValue bool `json:"hasValue"`
	// Host is the host[:port] the call goes to.
	Host string `json:"host"`
	// Private says the target is loopback or on a private range; false means
	// the call, header included, leaves this machine.
	Private bool `json:"private"`
	// UsedBy is the category ids pointing at this address; empty means nothing
	// calls it.
	UsedBy []string `json:"usedBy"`
	// Last is the last call this process made, test calls included, or null.
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
	// HeaderValue follows accounts.Credential: a new value replaces what is
	// stored, accounts.Redacted keeps it, and empty clears it.
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
				// GET changes least at the far end.
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
			// The whole table is validated, which catches duplicate ids.
			preview := cfg
			preview.MediaHooks = next
			if err := preview.ValidateMediaHooks(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The value first: if the row write then fails, a sealed value no
			// row names is inert, while a row without its value would be called
			// and refused.
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
			// Re-read from the store, since the sanitiser may have changed the
			// row.
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
			// A drawer pointing at a missing address would make every later
			// settings save fail, so the drawers are named and the delete
			// refused.
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
			// A 500 rather than silence: the person asked for the token to be
			// gone, and one left sealed could no longer be seen or removed.
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
			// 200 with the outcome in the body even when the far end failed, as
			// in POST /api/feeds/test. The request context cancels the call when
			// the browser goes away.
			writeJSON(w, a.TestMediaHook(r.Context(), hook))
		})
}

// mediaHookRows builds the listing in the configured order, so it lines up with
// the rows on the settings card.
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
			// Never nil, so it encodes as [].
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

// mediaHookList is the table with one row stored into it: replaced in place,
// keeping its position in the Categories picker, or appended.
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
		// The sanitiser would silently drop the extra row after a 200.
		return nil, fmt.Errorf("there are already %d stored addresses, which is the limit. "+
			"Delete one you no longer call, then store this one.", mediahook.MaxHooks)
	}
	return append(out, hook), nil
}
