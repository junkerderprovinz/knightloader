package api

// The clipboard watchers of the group. Each watcher renews a lease with the
// instance it sends to; listing asks every member for its own, so switching a
// watch on in one place can name the devices already watching and offer to
// stop them.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/clipwatch"
)

// memberWatchersWait bounds how long a listing waits for the other members. A
// warning that comes late is worth less than one that leaves out a member
// that is slow to answer.
const memberWatchersWait = 5 * time.Second

func registerClipWatch(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/clipboard-watchers",
		"the clipboard watchers of the group, this instance's and every reachable member's; ?local=1 for this instance's alone",
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
			// A member asked on behalf of another does not ask the group again.
			if !found && r.URL.Query().Get("local") == "" {
				found = stopAtMembers(r.Context(), a, id)
			}
			if !found {
				http.Error(w, "no clipboard watcher "+id+" in this group", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// groupWatchers is this instance's watchers and every reachable member's, each
// named with the instance it sends to. A member that does not answer in time,
// or runs a version without watchers, adds nothing.
func groupWatchers(ctx context.Context, a *app.App) []clipwatch.Watcher {
	self := instanceDisplayName(a)
	out := a.ClipWatch.List(time.Now())
	for i := range out {
		out[i].Instance = self
	}

	ctx, cancel := context.WithTimeout(ctx, memberWatchersWait)
	defer cancel()
	members := a.Federation.Members()
	answers := make([][]clipwatch.Watcher, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := a.Federation.Proxy(ctx, m.Name, http.MethodGet, "/api/clipboard-watchers?local=1", nil)
			if err != nil || status != http.StatusOK {
				return
			}
			var list []clipwatch.Watcher
			if json.Unmarshal(body, &list) != nil {
				return
			}
			name := m.DisplayName
			if name == "" {
				name = m.Name
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

// stopAtMembers asks every reachable member to stop watcher id and reports
// whether one of them held it.
func stopAtMembers(ctx context.Context, a *app.App, id string) bool {
	ctx, cancel := context.WithTimeout(ctx, memberWatchersWait)
	defer cancel()
	path := "/api/clipboard-watchers/" + url.PathEscape(id) + "/stop?local=1"
	found := false
	for _, m := range a.Federation.Members() {
		if _, status, err := a.Federation.Proxy(ctx, m.Name, http.MethodPost, path, nil); err == nil && status == http.StatusNoContent {
			found = true
		}
	}
	return found
}
