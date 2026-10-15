package vpnconn

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/mullvad"
)

// mullvadClient is Mullvad's API; tests point it elsewhere.
var mullvadClient = func() *mullvad.Client { return &mullvad.Client{} }

func openMullvad(ctx context.Context, login Login) (*session, error) {
	account := accountNumber(login["accountNumber"])
	next := owned(login, account)
	c := mullvadClient()
	token, err := c.Token(ctx, account)
	if err != nil {
		return nil, mullvadFailure(err)
	}
	if until, err := c.Expiry(ctx, token); err == nil && time.Now().After(until) {
		return nil, &failure{CodeAccountExpired, errors.New("the Mullvad account has no time left")}
	}
	relays, err := c.Relays(ctx)
	if err != nil {
		return nil, err
	}
	var near *mullvad.Place
	if strings.TrimSpace(login["location"]) == "" {
		if p, err := c.Where(ctx); err == nil {
			near = &p
		}
	}
	relay, err := relays.Pick(login["location"], near)
	if err != nil {
		return nil, &failure{CodeLocationUnknown, err}
	}
	key, dev, err := mullvadDevice(ctx, c, token, next)
	if err != nil {
		return nil, mullvadFailure(err)
	}
	next[keyField], next[deviceField] = key.String(), dev.ID
	cfg, err := relays.Config(key, dev, relay)
	if err != nil {
		return &session{login: next}, err
	}
	return &session{cfg: cfg, login: next, renews: true}, nil
}

// mullvadDevice is the device kept with the login while Mullvad still has it,
// and a new one otherwise.
func mullvadDevice(ctx context.Context, c *mullvad.Client, token string, login Login) (vpn.Key, mullvad.Device, error) {
	if key, ok := storedKey(login); ok && login[deviceField] != "" {
		dev, err := c.Device(ctx, token, login[deviceField])
		if err == nil && dev.PublicKey == key.PublicKey() {
			return key, dev, nil
		}
		var ae *mullvad.APIError
		if err != nil && !(errors.As(err, &ae) && ae.Status == http.StatusNotFound) {
			return vpn.Key{}, mullvad.Device{}, err
		}
	}
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Key{}, mullvad.Device{}, err
	}
	dev, err := c.AddDevice(ctx, token, key.PublicKey())
	return key, dev, err
}

func mullvadFailure(err error) error {
	var ae *mullvad.APIError
	switch {
	case !errors.As(err, &ae):
		return err
	case ae.Code == mullvad.MaxDevicesReached:
		return &failure{CodeDeviceLimit, err}
	case ae.Code == mullvad.InvalidAccount || ae.Code == mullvad.InvalidAccessToken ||
		ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden:
		return &failure{CodeLoginRefused, err}
	}
	return err
}

func forgetMullvad(ctx context.Context, login Login) {
	if login[deviceField] == "" {
		return
	}
	c := mullvadClient()
	if token, err := c.Token(ctx, accountNumber(login["accountNumber"])); err == nil {
		c.RemoveDevice(ctx, token, login[deviceField])
	}
}
