package vpnconn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/azire"
)

// azireClient is AzireVPN's API; tests point it elsewhere.
var azireClient = func() *azire.Client { return &azire.Client{} }

// defaultAzireLocation is used when the form leaves the location empty.
const defaultAzireLocation = "se-sto"

// azireDevice is how KnightLoader's token is listed in the account.
const azireDevice = "KnightLoader"

// azireToken calls f with the kept token, and with a new one where there is
// none or AzireVPN no longer takes it.
func azireToken(ctx context.Context, c *azire.Client, login Login, f func(token string) error) error {
	fresh := false
	newToken := func() error {
		t, err := c.Token(ctx, strings.TrimSpace(login["username"]), login["password"], azireDevice)
		login[tokenField], fresh = t, true
		return err
	}
	if login[tokenField] == "" {
		if err := newToken(); err != nil {
			return err
		}
	}
	err := f(login[tokenField])
	if !fresh && refusedByAzire(err) {
		if err := newToken(); err != nil {
			return err
		}
		err = f(login[tokenField])
	}
	return err
}

func refusedByAzire(err error) bool {
	var ae *azire.APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

func openAzire(ctx context.Context, login Login) (*session, error) {
	next := owned(login, strings.TrimSpace(login["username"]))
	c := azireClient()
	var (
		key vpn.Key
		ip  azire.IP
		loc azire.Location
	)
	err := azireToken(ctx, c, next, func(token string) error {
		all, err := c.Locations(ctx, token)
		if err != nil {
			return err
		}
		if loc, err = azireLocation(all, login["location"]); err != nil {
			return err
		}
		key, ip, err = azireIP(ctx, c, token, next)
		return err
	})
	if err != nil {
		if refusedByAzire(err) {
			err = &failure{CodeLoginRefused, err}
		}
		return &session{login: next}, err
	}
	next[keyField], next[deviceField] = key.String(), ip.ID
	cfg, err := azire.Config(key, ip, loc)
	if err != nil {
		return &session{login: next}, err
	}
	internal, err := netip.ParseAddr(ip.IPv4Address)
	if err != nil {
		return &session{login: next}, err
	}
	token := next[tokenField]
	return &session{
		cfg:    cfg,
		login:  next,
		renews: true,
		askPort: func(ctx context.Context, dial dialFunc) (uint16, error) {
			return c.ForwardPort(ctx, token, internal, dial)
		},
	}, nil
}

func azireLocation(all []azire.Location, want string) (azire.Location, error) {
	if strings.TrimSpace(want) != "" {
		l, err := azire.PickLocation(all, want)
		if err != nil {
			return l, &failure{CodeLocationUnknown, fmt.Errorf("%w: %q", err, want)}
		}
		return l, nil
	}
	if l, err := azire.PickLocation(all, defaultAzireLocation); err == nil {
		return l, nil
	}
	if len(all) == 0 {
		return azire.Location{}, errors.New("azire: the API lists no locations")
	}
	return all[0], nil
}

// azireIP is the address kept with the login while AzireVPN still has it with
// the kept key, and a new one otherwise.
func azireIP(ctx context.Context, c *azire.Client, token string, login Login) (vpn.Key, azire.IP, error) {
	if key, ok := storedKey(login); ok && login[deviceField] != "" {
		ip, err := c.IP(ctx, token, login[deviceField])
		if err == nil && ip.Has(key.PublicKey()) {
			return key, ip, nil
		}
		var ae *azire.APIError
		if err != nil && !(errors.As(err, &ae) && ae.Status == http.StatusNotFound) {
			return vpn.Key{}, azire.IP{}, err
		}
	}
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Key{}, azire.IP{}, err
	}
	ip, err := c.AddIP(ctx, token, key.PublicKey())
	var ae *azire.APIError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && ae.Status != http.StatusUnauthorized {
		// The API words a refusal for people; the account says why.
		if u, uerr := c.User(ctx, token); uerr == nil {
			switch {
			case !u.Active:
				return key, ip, &failure{CodeAccountExpired, err}
			case u.IPs.Allocated >= u.IPs.Available:
				return key, ip, &failure{CodeDeviceLimit, err}
			}
		}
	}
	return key, ip, err
}

func forgetAzire(ctx context.Context, login Login) {
	token := login[tokenField]
	if token == "" {
		return
	}
	c := azireClient()
	if login[deviceField] != "" {
		c.RemoveIP(ctx, token, login[deviceField])
	}
	c.DeleteToken(ctx, token)
}
