package app

// The Usenet servers an .nzb is fetched from without a debrid service: their
// logins, the connection test on the accounts page, and the NNTP client every
// download of theirs shares.

import (
	"context"
	"errors"
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

// ErrUsenetAddressChanged is a form that keeps the stored password while it
// names another address than the one the password was saved for.
var ErrUsenetAddressChanged = errors.New("password: the server's address changed, so enter the password again")

// StoredUsenetPassword is what a password of accounts.Redacted stands for in a
// form for srv: the password stored for srv.ID. It goes only to the address it
// was saved for, so a form that changed the host, the port or TLS gets
// ErrUsenetAddressChanged instead.
func (a *App) StoredUsenetPassword(srv settings.UsenetServer) (string, error) {
	prev, _ := a.Accounts.GetCredential(settings.UsenetService, srv.ID)
	if prev.Password == "" {
		return "", nil
	}
	for _, stored := range a.Settings.Get().UsenetServers {
		if stored.ID == srv.ID && stored.TLS == srv.TLS &&
			nntpServer(stored, prev).Addr() == nntpServer(srv, prev).Addr() {
			return prev.Password, nil
		}
	}
	return "", ErrUsenetAddressChanged
}

// TestUsenetServer logs in to srv and asks it for an article, storing nothing.
// A password of accounts.Redacted is the one stored for srv.ID, as
// StoredUsenetPassword hands it out.
func (a *App) TestUsenetServer(ctx context.Context, srv settings.UsenetServer, username, password string) error {
	if password == accounts.Redacted {
		var err error
		if password, err = a.StoredUsenetPassword(srv); err != nil {
			return err
		}
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

// nntpClient returns the client, with the servers as they are set up. A change
// to them goes into the one client every download shares, so the downloads
// under way and the next ones count against the same connection limits.
func (a *App) nntpClient() *nntp.Client {
	servers := a.nntpServers()
	nntpMu.Lock()
	defer nntpMu.Unlock()
	cur := nntpClients[a]
	if cur == nil {
		cur = &nntpClient{servers: servers, c: nntp.NewClient(servers, a.Throttle)}
		nntpClients[a] = cur
	} else if !slices.Equal(cur.servers, servers) {
		cur.c.SetServers(servers)
		cur.servers = servers
	}
	return cur.c
}

// refreshNNTPClient hands a saved change to the servers to the client, if
// there is one yet. A download under way asks for the client only as it
// starts, so this is how the change reaches its next article.
func (a *App) refreshNNTPClient() {
	servers := a.nntpServers()
	nntpMu.Lock()
	defer nntpMu.Unlock()
	if cur := nntpClients[a]; cur != nil && !slices.Equal(cur.servers, servers) {
		cur.c.SetServers(servers)
		cur.servers = servers
	}
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
