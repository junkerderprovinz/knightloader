package vpnconn

import (
	"cmp"
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/nordvpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/pia"
	"github.com/junkerderprovinz/knightloader/internal/vpn/torguard"
)

// apiFailure gives an error of a provider's API the code the page words it
// by.
func apiFailure(err error) error {
	switch {
	case errors.Is(err, nordvpn.ErrRefused), errors.Is(err, pia.ErrRefused), errors.Is(err, torguard.ErrRefused):
		return &failure{CodeLoginRefused, err}
	case errors.Is(err, vpn.ErrUnknownLocation):
		return &failure{CodeLocationUnknown, err}
	}
	return err
}

func openNordVPN(ctx context.Context, login Login) (*session, error) {
	c := &nordvpn.Client{Token: strings.TrimSpace(login["token"]), Location: strings.TrimSpace(login["location"])}
	cfg, _, err := c.Connect(ctx)
	if err != nil {
		return nil, apiFailure(err)
	}
	return &session{cfg: cfg}, nil
}

func openPIA(ctx context.Context, login Login) (*session, error) {
	c := &pia.Client{
		Username: strings.TrimSpace(login["username"]),
		Password: login["password"],
		Token:    login[accessTokenField],
		Location: strings.TrimSpace(login["location"]),
	}
	cfg, s, err := c.Connect(ctx)
	if errors.Is(err, pia.ErrRefused) && login[accessTokenField] != "" {
		// The token lasts a day; the password fetches a new one.
		c.Token = ""
		cfg, s, err = c.Connect(ctx)
	}
	if err != nil {
		return nil, apiFailure(err)
	}
	next := Login{}
	for k, v := range login {
		next[k] = v
	}
	next[accessTokenField] = c.Token
	return &session{
		cfg:    cfg,
		login:  next,
		renews: true,
		keep: func(ctx context.Context, k keeper) {
			c.KeepPort(ctx, s, k.dial, k.setPort, func(err error) {
				k.logf("vpn: Private Internet Access port forwarding: %v", err)
			})
		},
	}, nil
}

// TorGuard names its servers <prefix>.secureconnect.me, with prefixes of its
// own such as ger or us-ny.
const (
	torGuardDomain        = ".secureconnect.me"
	defaultTorGuardServer = "nl"
)

func torGuardHost(location string) string {
	h := cmp.Or(strings.TrimSpace(location), defaultTorGuardServer)
	if _, err := netip.ParseAddr(h); err == nil || strings.Contains(h, ".") {
		return h
	}
	return strings.ToLower(h) + torGuardDomain
}

func openTorGuard(ctx context.Context, login Login) (*session, error) {
	c := &torguard.Client{
		Username:    strings.TrimSpace(login["username"]),
		Password:    login["password"],
		Host:        torGuardHost(login["location"]),
		LookupNetIP: lookupServer,
	}
	cfg, err := c.Connect(ctx)
	if err != nil {
		return nil, unknownServer(apiFailure(err))
	}
	return &session{
		cfg:    cfg,
		renews: true,
		keep: func(ctx context.Context, k keeper) {
			t := time.NewTicker(torguard.RenewEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				if err := c.Renew(ctx, cfg); err != nil && ctx.Err() == nil {
					k.logf("vpn: TorGuard renewal: %v", err)
				}
			}
		},
	}, nil
}
