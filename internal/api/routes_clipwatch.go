package api

// The clipboard watchers of the group. Each watcher renews a lease with the
// instance it sends to; listing asks every peer for its own, so switching a
// watch on in one place can name the devices already watching and offer to
// stop them.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/clipwatch"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// peerWatchersWait bounds how long a listing or a stop waits for the peers. A
// warning that comes late is worth less than one that leaves out a peer
// that is slow to answer.
const peerWatchersWait = 5 * time.Second

func registerClipWatch(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/clipboard-watchers",
		"the clipboard watchers of the group, this instance's and every reachable peer's; ?local=1 for this instance's alone",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("local") != "" {
				writeJSON(w, a.ClipWatch.List(time.Now()))
				return
			}
			writeJSON(w, groupWatchers(r.Context(), a))
		})

	reg.Add(http.MethodPut, "/api/clipboard-watchers/{id}",
		"a clipboard watcher renewing its lease; the answer says whether it was asked to stop",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			stop, err := a.ClipWatch.Renew(clipwatch.Watcher{ID: r.PathValue("id"), Name: body.Name, Kind: body.Kind}, time.Now())
			if errors.Is(err, clipwatch.ErrInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]bool{"stop": stop})
		})

	reg.Add(http.MethodDelete, "/api/clipboard-watchers/{id}",
		"a clipboard watcher switched off where it runs",
		func(w http.ResponseWriter, r *http.Request) {
			a.ClipWatch.Leave(r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/clipboard-watchers/{id}/stop",
		"ask a clipboard watcher to stop, wherever in the group it sends to; ?local=1 asks this instance alone",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			found := a.ClipWatch.Stop(id, time.Now())
			// An extension whose default instance moved still holds a lease
			// at the old one until it runs out, so a copy found here is no
			// reason to leave the others alone. The answer waits only until
			// someone held the watcher; the peers still asking finish on their
			// own. A peer asked on behalf of another does not ask the group
			// again.
			if r.URL.Query().Get("local") == "" {
				held := stopAtPeers(context.WithoutCancel(r.Context()), a, id)
				if !found {
					found = <-held
				}
			}
			if !found {
				http.Error(w, "no clipboard watcher "+id+" in this group", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// groupWatchers is this instance's watchers and every reachable peer's, each
// named with the instance it sends to. A peer that does not answer in time,
// or runs a version without watchers, adds nothing. Peers added by address
// are asked too, since a watcher leases with the instance it sends to.
func groupWatchers(ctx context.Context, a *app.App) []clipwatch.Watcher {
	self := instanceDisplayName(a)
	out := a.ClipWatch.List(time.Now())
	for i := range out {
		out[i].Instance = self
	}

	ctx, cancel := context.WithTimeout(ctx, peerWatchersWait)
	defer cancel()
	peers := watcherPeers(a)
	answers := make([][]clipwatch.Watcher, len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := a.Federation.Proxy(ctx, p.Name, http.MethodGet, "/api/clipboard-watchers?local=1", nil)
			if err != nil || status != http.StatusOK {
				return
			}
			var list []clipwatch.Watcher
			if json.Unmarshal(body, &list) != nil {
				return
			}
			name := p.DisplayName
			if name == "" {
				name = p.Name
			}
			for j := range list {
				list[j].Instance = name
			}
			answers[i] = list
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, w := range out {
		seen[w.ID] = true
	}
	for _, list := range answers {
		for _, w := range list {
			if !seen[w.ID] {
				seen[w.ID] = true
				out = append(out, w)
			}
		}
	}
	return out
}

// stopAtPeers asks every reachable peer at once to stop watcher id. The channel
// yields true as soon as one of them held it, or false once all have answered
// or run out of time. Asked one after another, a peer that does not answer
// would use up the time of the ones after it.
func stopAtPeers(ctx context.Context, a *app.App, id string) <-chan bool {
	ctx, cancel := context.WithTimeout(ctx, peerWatchersWait)
	path := "/api/clipboard-watchers/" + url.PathEscape(id) + "/stop?local=1"
	held := make(chan bool, 1)
	var once sync.Once
	var wg sync.WaitGroup
	for _, p := range watcherPeers(a) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, status, err := a.Federation.Proxy(ctx, p.Name, http.MethodPost, path, nil); err == nil && status == http.StatusNoContent {
				once.Do(func() { held <- true })
			}
		}()
	}
	go func() {
		wg.Wait()
		cancel()
		once.Do(func() { held <- false })
	}()
	return held
}

// watcherPeers is the peers asked for their watchers, none while the Instances
// module is off, since it then contacts no peer at all.
func watcherPeers(a *app.App) []federation.Instance {
	if a.ModuleOff("federation") {
		return nil
	}
	return a.Federation.List()
}

// clipLeaseCall reports whether rest, an API path without its /api/, is a
// clipboard watcher renewing or dropping its lease.
func clipLeaseCall(method, rest string) bool {
	id, ok := strings.CutPrefix(rest, "clipboard-watchers/")
	return ok && id != "" && !strings.Contains(id, "/") && (method == http.MethodPut || method == http.MethodDelete)
}
