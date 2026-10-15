package vpnconn

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/obscura"
)

// obscuraClient is Obscura's API; tests point it elsewhere.
var obscuraClient = func() *obscura.Client { return &obscura.Client{} }

// obscuraToken calls f with the kept auth token, and with a new one where
// there is none or Obscura no longer takes it.
func obscuraToken(ctx context.Context, c *obscura.Client, account string, login Login, f func(token string) error) error {
	fresh := false
	if login[tokenField] == "" {
		t, err := c.Token(ctx, account)
		if err != nil {
			return err
		}
		login[tokenField], fresh = t, true
	}
	err := f(login[tokenField])
	var ae *obscura.APIError
	if !fresh && errors.As(err, &ae) && ae.Kind == obscura.MissingOrInvalidAuthToken {
		t, terr := c.Token(ctx, account)
		if terr != nil {
			return terr
		}
		login[tokenField] = t
		err = f(t)
	}
	return err
}

func openObscura(ctx context.Context, login Login) (*session, error) {
	account := accountNumber(login["accountNumber"])
	location := strings.ToLower(strings.TrimSpace(login["location"]))
	next := owned(login, account)
	c := obscuraClient()
	var key vpn.Key
	var tunnel obscura.Tunnel
	err := obscuraToken(ctx, c, account, next, func(token string) error {
		var err error
		key, tunnel, err = obscuraTunnel(ctx, c, token, location, next)
		return err
	})
	if err != nil {
		return &session{login: next}, obscuraFailure(err)
	}
	next[keyField], next[deviceField], next[placeField] = key.String(), tunnel.ID, location
	cfg := tunnel.Config
	cfg.PrivateKey = key
	if err := cfg.Validate(); err != nil {
		return &session{login: next}, err
	}
	return &session{cfg: cfg, login: next, renews: true}, nil
}

// obscuraTunnel is the tunnel kept with the login while Obscura still has it
// and it leads to the location asked for, and a new one otherwise. A kept
// tunnel to another location is deleted, as it would hold a place on the
// account for nothing.
func obscuraTunnel(ctx context.Context, c *obscura.Client, token, location string, login Login) (vpn.Key, obscura.Tunnel, error) {
	if key, ok := storedKey(login); ok && login[deviceField] != "" {
		tunnels, err := c.Tunnels(ctx, token)
		if err != nil {
			return vpn.Key{}, obscura.Tunnel{}, err
		}
		for _, t := range tunnels {
			if t.ID != login[deviceField] {
				continue
			}
			if login[placeField] == location {
				return key, t, nil
			}
			if err := c.DeleteTunnel(ctx, token, t.ID); err != nil {
				return vpn.Key{}, obscura.Tunnel{}, err
			}
		}
		delete(login, deviceField)
	}
	exit := ""
	if location != "" {
		exits, err := c.Exits(ctx, token)
		if err != nil {
			return vpn.Key{}, obscura.Tunnel{}, err
		}
		e, err := obscura.PickExit(exits, location)
		if err != nil {
			return vpn.Key{}, obscura.Tunnel{}, &failure{CodeLocationUnknown, err}
		}
		exit = e.ID
	}
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Key{}, obscura.Tunnel{}, err
	}
	t, err := c.CreateTunnel(ctx, token, key.PublicKey(), exit)
	return key, t, err
}

func obscuraFailure(err error) error {
	var ae *obscura.APIError
	if !errors.As(err, &ae) {
		return err
	}
	switch {
	case ae.Kind == obscura.TunnelLimitExceeded:
		return &failure{CodeDeviceLimit, err}
	case ae.Kind == obscura.AccountExpired:
		return &failure{CodeAccountExpired, err}
	case ae.Kind == obscura.NoMatchingExit:
		return &failure{CodeLocationUnknown, err}
	case ae.Kind == obscura.InvalidAccountID || ae.Kind == obscura.MissingOrInvalidAuthToken ||
		ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden:
		return &failure{CodeLoginRefused, err}
	}
	return err
}

func forgetObscura(ctx context.Context, login Login) {
	if login[deviceField] == "" || login[tokenField] == "" {
		return
	}
	obscuraClient().DeleteTunnel(ctx, login[tokenField], login[deviceField])
}
