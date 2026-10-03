// Package clipwatch keeps the clipboard watchers that send their links to this
// instance: the desktop app's own, a web interface tab's and a browser
// extension's. A watcher holds a lease it renews while it watches, so one that
// goes away without a word drops off by itself, and switching a watch on
// anywhere in the group can name the ones already running.
package clipwatch

import (
	"errors"
	"sort"
	"sync"
	"time"
	"unicode/utf8"
)

// Lease is how long a watcher stays listed after it last renewed. Watchers
// renew every minute, so one missed renewal does not drop them.
const Lease = 3 * time.Minute

// The kinds of watcher.
const (
	KindDesktop   = "desktop"
	KindExtension = "extension"
	KindWeb       = "web"
)

// Every member of the group can register a watcher here, so the list and what
// each entry holds are bounded.
const (
	maxWatchers  = 32
	maxIDBytes   = 64
	maxNameBytes = 80
)

// ErrInvalid is a watcher without an id, with an oversized id or of a kind
// this package does not know.
var ErrInvalid = errors.New("clipwatch: a watcher needs an id of at most 64 bytes and a known kind")

// Watcher is one device watching the clipboard.
type Watcher struct {
	// ID is chosen by the watcher and stays the same across its renewals.
	ID string `json:"id"`
	// Name is what a person calls the device, such as "Firefox, Windows" or
	// the desktop instance's name.
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Instance is the instance the watcher sends its links to. The registry
	// leaves it empty; a listing across the group fills it in.
	Instance string `json:"instance,omitempty"`
}

type entry struct {
	w    Watcher
	seen time.Time
}

// Registry is the watchers of one instance. The zero value is not usable; call
// New.
type Registry struct {
	mu   sync.Mutex
	live map[string]entry
	// stopped holds the watchers asked to stop, by id, until they renew and
	// learn it or their lease would have run out anyway.
	stopped map[string]time.Time
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{live: map[string]entry{}, stopped: map[string]time.Time{}}
}

// Renew lists w until now plus Lease and reports whether it was asked to stop
// since it last renewed. A watcher told to stop is not listed again; it shows
// up once it renews after being switched on anew.
func (r *Registry) Renew(w Watcher, now time.Time) (stop bool, err error) {
	if w.ID == "" || len(w.ID) > maxIDBytes || !knownKind(w.Kind) {
		return false, ErrInvalid
	}
	w.Name = clip(w.Name)
	w.Instance = ""

	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	if _, asked := r.stopped[w.ID]; asked {
		delete(r.stopped, w.ID)
		return true, nil
	}
	if _, known := r.live[w.ID]; !known && len(r.live) >= maxWatchers {
		r.dropStalestLocked()
	}
	r.live[w.ID] = entry{w: w, seen: now}
	return false, nil
}

// Stop asks watcher id to stop. It leaves the list at once and learns it on its
// next renewal. Stop reports whether this registry held the watcher.
func (r *Registry) Stop(id string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	if _, ok := r.live[id]; !ok {
		return false
	}
	delete(r.live, id)
	r.stopped[id] = now
	return true
}

// Leave takes id off the list, for a watcher switched off where it runs.
func (r *Registry) Leave(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, id)
	delete(r.stopped, id)
}

// List returns the watchers whose lease still runs at now, by name and then id.
func (r *Registry) List(now time.Time) []Watcher {
	r.mu.Lock()
	r.pruneLocked(now)
	out := make([]Watcher, 0, len(r.live))
	for _, e := range r.live {
		out = append(out, e.w)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) pruneLocked(now time.Time) {
	for id, e := range r.live {
		if now.Sub(e.seen) >= Lease {
			delete(r.live, id)
		}
	}
	for id, at := range r.stopped {
		if now.Sub(at) >= Lease {
			delete(r.stopped, id)
		}
	}
}

func (r *Registry) dropStalestLocked() {
	stalest := ""
	for id, e := range r.live {
		if stalest == "" || e.seen.Before(r.live[stalest].seen) {
			stalest = id
		}
	}
	delete(r.live, stalest)
}

func knownKind(kind string) bool {
	return kind == KindDesktop || kind == KindExtension || kind == KindWeb
}

// clip cuts name to maxNameBytes at a whole UTF-8 character.
func clip(name string) string {
	if len(name) <= maxNameBytes {
		return name
	}
	cut := maxNameBytes
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut]
}
