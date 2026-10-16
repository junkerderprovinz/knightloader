package api

// The media servers quiet mode watches: what the watcher last saw, storing and
// deleting a server, and one question asked on demand.
//
// A server is a row in settings.json (kind, address, name) plus a token sealed
// in accounts.Store under playback.Service, the arrangement routes_mediahooks.go
// uses. The listing has hasToken and never the token. The switches (on, the
// local network, the paused stream, the unpacking pause) are ordinary settings
// saved with the page.
//
// Not forwarded to peers: a sibling's media server is not this box's business.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/playback"
)

// playbackServerRow is one server as the card sees it.
type playbackServerRow struct {
	playback.Server
	HasToken bool   `json:"hasToken"`
	Host     string `json:"host"`
}

// playbackView is GET /api/playback.
type playbackView struct {
	Status  playback.Status     `json:"status"`
	Servers []playbackServerRow `json:"servers"`
	Kinds   []playback.Kind     `json:"kinds"`
}

// playbackServerBody is one save. Token follows accounts.Credential: a new
// value replaces what is stored, accounts.Redacted keeps it, empty clears it.
type playbackServerBody struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Kind  playback.Kind `json:"kind"`
	URL   string        `json:"url"`
	Token string        `json:"token"`
}

// playbackCheck is the answer to a test question.
type playbackCheck struct {
	OK      bool              `json:"ok"`
	Streams []playback.Stream `json:"streams"`
	Failure *playback.Failure `json:"failure,omitempty"`
}

func registerPlayback(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/playback",
		"the media servers quiet mode watches, whether a token is sealed for each, and what the watcher last saw: every stream playing, whether it counts, and how each server answered",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, playbackListing(a))
		})

	reg.Add(http.MethodPost, "/api/playback/servers",
		"store or replace one media server, sealing its token; a new row without an id is given one from its kind, the placeholder keeps the stored token and an empty one clears it",
		func(w http.ResponseWriter, r *http.Request) {
			var body playbackServerBody
			if !decodeJSON(w, r, &body) {
				return
			}
			cfg := a.Settings.Get()
			s := playback.Server{
				ID:   playback.ServerID(body.ID),
				Name: strings.TrimSpace(body.Name),
				Kind: playback.Kind(strings.ToLower(strings.TrimSpace(string(body.Kind)))),
				URL:  strings.TrimSpace(body.URL),
			}
			if strings.TrimSpace(body.ID) == "" {
				s.ID = playback.FreeID(s.Kind, cfg.PlaybackServers)
			}
			next := playbackList(cfg.PlaybackServers, s)
			if err := playback.ValidateServers(next); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The token first: a sealed token no row names is inert, while a row
			// without its token would be asked and refused.
			if body.Token != accounts.Redacted {
				if err := a.PlaybackStore().SetToken(s.ID, body.Token); err != nil {
					http.Error(w, s.ID+": the token could not be sealed into the credential store ("+
						err.Error()+"). Check that KnightLoader's data directory is writable, then save again.",
						http.StatusInternalServerError)
					return
				}
			}
			if !patchPlaybackServers(w, a, next) {
				return
			}
			writeJSON(w, playbackListing(a))
		})

	reg.Add(http.MethodDelete, "/api/playback/servers/{id}",
		"delete one media server and the token sealed for it",
		func(w http.ResponseWriter, r *http.Request) {
			id := playback.ServerID(r.PathValue("id"))
			cfg := a.Settings.Get()
			next := make([]playback.Server, 0, len(cfg.PlaybackServers))
			for _, s := range cfg.PlaybackServers {
				if s.ID != id {
					next = append(next, s)
				}
			}
			if id == "" || len(next) == len(cfg.PlaybackServers) {
				http.Error(w, "id: no media server is stored under that name. The names are the ones "+
					"GET /api/playback lists.", http.StatusNotFound)
				return
			}
			if !patchPlaybackServers(w, a, next) {
				return
			}
			if err := a.PlaybackStore().SetToken(id, ""); err != nil {
				http.Error(w, id+": the server is gone but its token could not be removed from the "+
					"credential store ("+err.Error()+"). Check that KnightLoader's data directory is writable.",
					http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/playback/servers/{id}/test",
		"ask one stored media server once what is playing, with its sealed token, and report the answer; changes nothing quiet mode acts on",
		func(w http.ResponseWriter, r *http.Request) {
			id := playback.ServerID(r.PathValue("id"))
			for _, s := range a.Settings.Get().PlaybackServers {
				if s.ID != id {
					continue
				}
				// 200 with the outcome in the body when the server failed, as
				// the media hooks' test does.
				streams, err := a.CheckPlayback(r.Context(), s)
				out := playbackCheck{OK: err == nil, Streams: streams}
				if out.Streams == nil {
					out.Streams = []playback.Stream{}
				}
				var f *playback.Failure
				if errors.As(err, &f) {
					out.Failure = f
				}
				writeJSON(w, out)
				return
			}
			http.Error(w, "id: no media server is stored under that name. Save it first, then test it.",
				http.StatusNotFound)
		})
}

func playbackListing(a *app.App) playbackView {
	cfg := a.Settings.Get()
	store := a.PlaybackStore()
	v := playbackView{
		Status:  a.PlaybackStatus(),
		Servers: make([]playbackServerRow, 0, len(cfg.PlaybackServers)),
		Kinds:   playback.Kinds(),
	}
	for _, s := range cfg.PlaybackServers {
		v.Servers = append(v.Servers, playbackServerRow{Server: s, HasToken: store.Has(s.ID), Host: s.Host()})
	}
	if v.Status.Streams == nil {
		v.Status.Streams = []playback.Stream{}
	}
	if v.Status.Servers == nil {
		v.Status.Servers = []playback.ServerState{}
	}
	return v
}

// playbackList is the list with one row stored into it, replaced in place or
// appended.
func playbackList(in []playback.Server, s playback.Server) []playback.Server {
	out := make([]playback.Server, len(in), len(in)+1)
	copy(out, in)
	for i, cur := range out {
		if cur.ID == s.ID {
			out[i] = s
			return out
		}
	}
	return append(out, s)
}

// patchPlaybackServers writes the list through PatchSettings, so a concurrent
// edit elsewhere survives, and answers the request itself on failure.
func patchPlaybackServers(w http.ResponseWriter, a *app.App, next []playback.Server) bool {
	raw, err := json.Marshal(next)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if _, err := a.PatchSettings(map[string]json.RawMessage{"playbackServers": raw}); err != nil {
		http.Error(w, fmt.Sprintf("the media servers could not be saved: %v", err), http.StatusInternalServerError)
		return false
	}
	return true
}
