package api

// Where the interface keeps what it has to remember between reloads.
//
// One opaque blob per key, rather than a settings field per remembered thing.
// Column widths, which packages are folded shut and which settings page was open
// last are the interface's own business: giving each one a settings field means a
// schema change, a migration and a translated label every time a list gains a
// column, and it puts a browser's layout into a document that is shared by every
// client of the instance and validated on save.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// isJSONDocument reports whether the bytes are a JSON object. An object rather
// than any JSON value, because this is handed back verbatim on GET and the
// interface reads keys off it: storing a bare number here would come back as a
// layout the client cannot use, with nothing to say when it was broken.
func isJSONDocument(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '{' && json.Valid(b)
}

// uiStateKey is the bucket a request asks for, defaulting to the shared one. A
// client that wants a layout of its own passes ?key=; two browsers sharing the
// default is deliberate, because a single-user instance wants its layout to
// follow it from one machine to the next.
func uiStateKey(r *http.Request) string {
	if k := r.URL.Query().Get("key"); k != "" {
		return k
	}
	return store.UIStateKey
}

func registerUIState(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/uistate", "what this client stored about its own layout; ?key= picks a bucket",
		func(w http.ResponseWriter, r *http.Request) {
			value, err := a.UIState(uiStateKey(r))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// A bucket nothing has been written to answers with an empty document
			// rather than 404: a fresh browser's first load is the normal case, and it
			// wants the built-in layout, not an error to handle.
			if value == "" {
				value = "{}"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, value)
		})
	reg.Add(http.MethodPut, "/api/uistate", "replace what this client stored about its own layout",
		func(w http.ResponseWriter, r *http.Request) {
			// Read with a hard ceiling rather than trusting Content-Length: the store
			// refuses an oversized blob anyway, and without this the server would hold
			// the whole thing in memory first in order to be told so.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxUIStateBytes+1))
			if err != nil {
				http.Error(w, "this interface state is larger than the limit", http.StatusRequestEntityTooLarge)
				return
			}
			// Checked for being JSON at all, because it is handed straight back out
			// again on GET: a client that stored something else would get it back and
			// fail to parse its own layout with nothing saying where it came from.
			if !isJSONDocument(body) {
				http.Error(w, "interface state has to be a JSON object", http.StatusBadRequest)
				return
			}
			if err := a.SetUIState(uiStateKey(r), string(body)); err != nil {
				code := http.StatusBadRequest
				if errors.Is(err, store.ErrUIStateTooBig) {
					code = http.StatusRequestEntityTooLarge
				}
				http.Error(w, err.Error(), code)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
