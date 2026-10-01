package api

// Where the interface keeps what it has to remember between reloads.
//
// One opaque blob per key, rather than a settings field per remembered thing.
// Column widths, which packages are folded shut and which settings page was
// open last are the interface's own business: a field each would mean a schema
// change, a migration and a translated label every time a list gains a column,
// and it would put a browser's layout into a document every client of the
// instance shares and every save validates.

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
// client that wants a layout of its own passes ?key=; two browsers share the
// default so a single-user instance's layout follows it from one machine to
// the next.
func uiStateKey(r *http.Request) string {
	if k := r.URL.Query().Get("key"); k != "" {
		return k
	}
	return store.UIStateKey
}

// serverBuckets are the keys the instance keeps its own state under in the
// same table: parked module values with event-target headers among them, feed
// addresses that carry indexer keys, and the bridges' and the timetable's
// bookkeeping. None of it is layout, and a token allowed to read the layout
// must not read those, so this route serves none of them.
var serverBuckets = map[string]bool{
	parkBucket:           true,
	downloadClientBucket: true,
	qbitBucket:           true,
	// internal/app's feedStateBucket and suspendBucket.
	"feeds":            true,
	"schedule.suspend": true,
}

// refuseServerBucket answers 403 for a key from serverBuckets and reports
// whether it did.
func refuseServerBucket(w http.ResponseWriter, key string) bool {
	if !serverBuckets[key] {
		return false
	}
	http.Error(w, "this key holds the instance's own data, not interface state", http.StatusForbidden)
	return true
}

func registerUIState(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/uistate", "what this client stored about its own layout; ?key= picks a bucket",
		func(w http.ResponseWriter, r *http.Request) {
			key := uiStateKey(r)
			if refuseServerBucket(w, key) {
				return
			}
			value, err := a.UIState(key)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// An unwritten bucket answers with an empty document rather than
			// 404: a fresh browser's first load wants the built-in layout,
			// not an error to handle.
			if value == "" {
				value = "{}"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, value)
		})
	reg.Add(http.MethodPut, "/api/uistate", "replace what this client stored about its own layout",
		func(w http.ResponseWriter, r *http.Request) {
			key := uiStateKey(r)
			if refuseServerBucket(w, key) {
				return
			}
			// A ceiling rather than trusting Content-Length: the store refuses
			// an oversized blob anyway, but not before the server has held the
			// whole thing in memory.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxUIStateBytes+1))
			if err != nil {
				http.Error(w, "this interface state is larger than the limit", http.StatusRequestEntityTooLarge)
				return
			}
			// Checked here because GET hands it straight back: a client that
			// stored something else would fail to parse its own layout with
			// nothing to say where it came from.
			if !isJSONDocument(body) {
				http.Error(w, "interface state has to be a JSON object", http.StatusBadRequest)
				return
			}
			if err := a.SetUIState(key, string(body)); err != nil {
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
