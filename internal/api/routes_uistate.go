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
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

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
	// The bucket internal/store takes the feed memory over from, and
	// internal/app's suspendBucket.
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

// savedViewFields are the lists whose saved views /api/uistate/views reaches,
// by the field the web keeps each under in the shared bucket
// (web/src/lib/savedViews.ts).
var savedViewFields = []struct{ list, field string }{
	{"downloads", "list.views.downloads"},
	{"collector", "list.views.collector"},
}

// The web's own bounds on saved views, so a member cannot store what the web
// would refuse to save.
const (
	maxSavedViews     = 12
	maxSavedViewBytes = 4096
	maxSavedViewName  = 60
)

type savedView struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	State json.RawMessage `json:"state"`
}

// checkViews trims the names and reports what is wrong with a list of views,
// if anything.
func checkViews(views []savedView) error {
	if len(views) > maxSavedViews {
		return fmt.Errorf("a list keeps at most %d saved views", maxSavedViews)
	}
	seen := map[string]bool{}
	for i := range views {
		v := &views[i]
		v.Name = strings.TrimSpace(v.Name)
		switch {
		case v.ID == "" || seen[v.ID]:
			return errors.New("every saved view needs an id of its own")
		case v.Name == "" || utf8.RuneCountInString(v.Name) > maxSavedViewName:
			return fmt.Errorf("a saved view needs a name of 1 to %d characters", maxSavedViewName)
		case !isJSONDocument(v.State):
			return errors.New("a saved view's state has to be a JSON object")
		}
		seen[v.ID] = true
		if b, _ := json.Marshal(v); len(b) > maxSavedViewBytes {
			return errors.New("a saved view holds more than can be stored")
		}
	}
	return nil
}

// savedViewsOf is the saved views of each list in a stored document, an empty
// list for one that has none.
func savedViewsOf(doc map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, f := range savedViewFields {
		raw := bytes.TrimSpace(doc[f.field])
		if len(raw) == 0 || raw[0] != '[' {
			raw = json.RawMessage("[]")
		}
		out[f.list] = raw
	}
	return out
}

func writeSavedViews(w http.ResponseWriter, doc map[string]json.RawMessage) {
	views := savedViewsOf(doc)
	// Written out by hand, since a map would put the collector first.
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, "{\"downloads\":%s,\"collector\":%s}\n", views["downloads"], views["collector"])
}

// sharedDoc reads the bucket the browsers share as its fields.
func sharedDoc(a *app.App) (map[string]json.RawMessage, error) {
	value, err := a.UIState(store.UIStateKey)
	if err != nil {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	if value != "" {
		if err := json.Unmarshal([]byte(value), &doc); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// savedViewsMu keeps two calls of the views route from writing over each
// other's read of the bucket.
var savedViewsMu sync.Mutex

func registerSavedViews(reg *Registry, a *app.App) {
	// The saved views alone, for a client that may not reach the rest of the
	// interface state, which can hold anything a browser put there.
	reg.Add(http.MethodGet, "/api/uistate/views", "the saved views of the download list and the collector",
		func(w http.ResponseWriter, r *http.Request) {
			doc, err := sharedDoc(a)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeSavedViews(w, doc)
		})
	reg.Add(http.MethodPut, "/api/uistate/views", "replace the saved views of the download list, the collector or both; anything else is refused",
		func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Downloads *[]savedView `json:"downloads"`
				Collector *[]savedView `json:"collector"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, io.NopCloser(body(r)), maxJSONBody))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				http.Error(w, "this takes the saved views of downloads and collector and nothing else", http.StatusBadRequest)
				return
			}
			lists := map[string]*[]savedView{"downloads": in.Downloads, "collector": in.Collector}
			named := 0
			for _, views := range lists {
				if views == nil {
					continue
				}
				named++
				if err := checkViews(*views); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if named == 0 {
				http.Error(w, "this needs the saved views of downloads, collector or both", http.StatusBadRequest)
				return
			}

			savedViewsMu.Lock()
			defer savedViewsMu.Unlock()
			doc, err := sharedDoc(a)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, f := range savedViewFields {
				if views := lists[f.list]; views != nil {
					doc[f.field], _ = json.Marshal(*views)
				}
			}
			value, _ := json.Marshal(doc)
			// The web's own write, so both keep one document.
			if err := a.SetUIState(store.UIStateKey, string(value)); err != nil {
				code := http.StatusBadRequest
				if errors.Is(err, store.ErrUIStateTooBig) {
					code = http.StatusRequestEntityTooLarge
				}
				http.Error(w, err.Error(), code)
				return
			}
			writeSavedViews(w, doc)
		})
}
