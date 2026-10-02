package app

// The Usenet servers an .nzb is fetched from without a debrid service: their
// logins, the connection test on the accounts page, and the NNTP client every
// download of theirs shares.

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/nntp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// UsenetLogin is the part of a server's login the accounts page may show.
type UsenetLogin struct {
	Username    string `json:"username"`
	HasPassword bool   `json:"hasPassword"`
}

// UsenetLogin reads one server's sealed login.
func (a *App) UsenetLogin(id string) UsenetLogin {
	cred, _ := a.Accounts.GetCredential(settings.UsenetService, id)
	return UsenetLogin{Username: cred.Username, HasPassword: cred.Password != ""}
}

// SetUsenetLogin seals a server's login. A password of accounts.Redacted keeps
// the stored one, and an empty username and password remove the login.
func (a *App) SetUsenetLogin(id, username, password string) error {
	prev, _ := a.Accounts.GetCredential(settings.UsenetService, id)
	cred := accounts.Credential{Username: strings.TrimSpace(username), Password: password}.WithSecretsFrom(prev)
	return a.Accounts.SetCredential(settings.UsenetService, id, cred)
}

// RemoveUsenetLogin drops a server's sealed login.
func (a *App) RemoveUsenetLogin(id string) error {
	return a.Accounts.SetCredential(settings.UsenetService, id, accounts.Credential{})
}

// TestUsenetServer logs in to srv and asks it for an article, storing nothing.
// A password of accounts.Redacted is the one stored for srv.ID.
func (a *App) TestUsenetServer(ctx context.Context, srv settings.UsenetServer, username, password string) error {
	if password == accounts.Redacted {
		prev, _ := a.Accounts.GetCredential(settings.UsenetService, srv.ID)
		password = prev.Password
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s := nntpServer(srv, accounts.Credential{Username: strings.TrimSpace(username), Password: password})
	return nntp.Check(ctx, s)
}

func nntpServer(srv settings.UsenetServer, cred accounts.Credential) nntp.Server {
	return nntp.Server{
		ID: srv.ID, Host: srv.Host, Port: srv.Port, TLS: srv.TLS,
		Username: cred.Username, Password: cred.Password,
		Connections: srv.Connections, Level: srv.Level,
		Optional: srv.Optional, RetentionDays: srv.RetentionDays,
	}
}

// nntpServers lists the switched-on servers with their logins.
func (a *App) nntpServers() []nntp.Server {
	var out []nntp.Server
	for _, srv := range a.Settings.Get().UsenetServers {
		if !srv.Enabled {
			continue
		}
		cred, _ := a.Accounts.GetCredential(settings.UsenetService, srv.ID)
		out = append(out, nntpServer(srv, cred))
	}
	return out
}

// nntpClients keeps each App's client, so the servers' connections are shared
// by every download and survive from one to the next.
var (
	nntpMu      sync.Mutex
	nntpClients = map[*App]*nntpClient{}
)

type nntpClient struct {
	servers []nntp.Server
	c       *nntp.Client
}

// nntpClient returns the client for the servers as they are set up,
// building a new one when they have changed. Downloads that started on the
// old one finish on it.
func (a *App) nntpClient() *nntp.Client {
	servers := a.nntpServers()
	nntpMu.Lock()
	defer nntpMu.Unlock()
	cur := nntpClients[a]
	if cur != nil && slices.Equal(cur.servers, servers) {
		return cur.c
	}
	if cur != nil {
		cur.c.Close()
	}
	next := &nntpClient{servers: servers, c: nntp.NewClient(servers, a.Throttle)}
	nntpClients[a] = next
	return next.c
}

// usenetServersSet reports whether any server is switched on.
func (a *App) usenetServersSet() bool {
	for _, srv := range a.Settings.Get().UsenetServers {
		if srv.Enabled {
			return true
		}
	}
	return false
}
