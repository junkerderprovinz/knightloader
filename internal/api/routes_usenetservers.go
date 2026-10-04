package api

// The Usenet servers on the accounts page: listing, storing, deleting and the
// connection test. Each server is a settings row plus a login sealed in
// accounts.Store, as for the media hooks; the listing never carries the
// password, only whether one is stored.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// usenetServerRow is one server as the accounts page shows it.
type usenetServerRow struct {
	settings.UsenetServer
	app.UsenetLogin
}

// usenetServerBody is one save or test: the row as edited, plus the login. A
// password of accounts.Redacted keeps the stored one.
type usenetServerBody struct {
	settings.UsenetServer
	Username string `json:"username"`
	Password string `json:"password"`
}

func registerUsenetServers(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/usenet/servers",
		"the Usenet servers an .nzb is fetched from without a debrid service, with the username of each and whether a password is stored, never the password",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, usenetServerRows(a))
		})

	reg.Add(http.MethodPost, "/api/usenet/servers",
		"store or replace one Usenet server and seal its login; an empty id adds a new server, and the password placeholder keeps the stored password",
		func(w http.ResponseWriter, r *http.Request) {
			var body usenetServerBody
			if !decodeJSON(w, r, &body) {
				return
			}
			cfg := a.Settings.Get()
			srv := cleanUsenetServer(body.UsenetServer)
			if strings.TrimSpace(body.ID) == "" {
				srv.ID = newUsenetServerID(cfg.UsenetServers, srv.Host)
			}
			if err := srv.Validate(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			next := make([]settings.UsenetServer, 0, len(cfg.UsenetServers)+1)
			replaced := false
			for _, s := range cfg.UsenetServers {
				if s.ID == srv.ID {
					s, replaced = srv, true
				}
				next = append(next, s)
			}
			if !replaced {
				next = append(next, srv)
			}
			preview := cfg
			preview.UsenetServers = next
			if err := preview.ValidateUsenetServers(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.Password == accounts.Redacted {
				if _, err := a.StoredUsenetPassword(srv); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			// The login first: a sealed login no row names is inert, while a
			// row without its login would be refused by the server.
			if err := a.SetUsenetLogin(srv.ID, body.Username, body.Password); err != nil {
				http.Error(w, "the login could not be sealed into the credential store ("+err.Error()+
					"). Check that KnightLoader's data directory is writable, then save again.", http.StatusInternalServerError)
				return
			}
			if err := saveUsenetServers(a, next); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, usenetServerRows(a))
		})

	reg.Add(http.MethodDelete, "/api/usenet/servers/{id}",
		"delete one Usenet server and the login sealed for it",
		func(w http.ResponseWriter, r *http.Request) {
			id := settings.UsenetServerID(r.PathValue("id"))
			cfg := a.Settings.Get()
			next := make([]settings.UsenetServer, 0, len(cfg.UsenetServers))
			for _, s := range cfg.UsenetServers {
				if s.ID != id {
					next = append(next, s)
				}
			}
			if len(next) == len(cfg.UsenetServers) {
				http.Error(w, "id: no Usenet server is stored under that name", http.StatusNotFound)
				return
			}
			if err := saveUsenetServers(a, next); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := a.RemoveUsenetLogin(id); err != nil {
				http.Error(w, "the server is gone but its login could not be removed from the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable.", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/usenet/servers/test",
		"log in to a Usenet server as the form has it, stored or not, and ask it for an article; stores nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body usenetServerBody
			if !decodeJSON(w, r, &body) {
				return
			}
			srv := cleanUsenetServer(body.UsenetServer)
			if srv.ID == "" {
				srv.ID = "test"
			}
			if err := srv.Validate(); err != nil {
				writeJSON(w, map[string]any{"ok": false, "detail": err.Error()})
				return
			}
			// 200 with the outcome in the body, as for the accounts' check: a
			// server that refuses the login is an answer, not a failed call.
			if err := a.TestUsenetServer(r.Context(), srv, body.Username, body.Password); err != nil {
				writeJSON(w, map[string]any{"ok": false, "detail": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"ok": true, "detail": "login accepted"})
		})
}

func usenetServerRows(a *app.App) []usenetServerRow {
	servers := a.Settings.Get().UsenetServers
	rows := make([]usenetServerRow, 0, len(servers))
	for _, s := range servers {
		rows = append(rows, usenetServerRow{UsenetServer: s, UsenetLogin: a.UsenetLogin(s.ID)})
	}
	return rows
}

// cleanUsenetServer trims what a form sends and fills in the connection count
// it left empty.
func cleanUsenetServer(s settings.UsenetServer) settings.UsenetServer {
	s.ID = settings.UsenetServerID(s.ID)
	if host := settings.UsenetHost(s.Host); host != "" {
		s.Host = host
	}
	if s.Connections == 0 {
		s.Connections = settings.DefaultUsenetConnections
	}
	return s
}

// newUsenetServerID names a new row after its host, with a number added when
// another row has that name.
func newUsenetServerID(existing []settings.UsenetServer, host string) string {
	base := settings.UsenetServerID(host)
	if base == "" {
		base = "server"
	}
	base = base[:min(len(base), 60)]
	taken := map[string]bool{}
	for _, s := range existing {
		taken[s.ID] = true
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func saveUsenetServers(a *app.App, servers []settings.UsenetServer) error {
	raw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	_, err = a.PatchSettings(map[string]json.RawMessage{"usenetServers": raw})
	return err
}
