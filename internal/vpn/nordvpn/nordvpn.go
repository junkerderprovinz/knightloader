// Package nordvpn gets NordLynx sessions from NordVPN with an access token
// from the Nord Account: the token unlocks the account's NordLynx key, and
// NordVPN's public server list names a WireGuard server for the location.
package nordvpn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

const api = "https://api.nordvpn.com"

// NordLynx gives every client the same address and port, and NordVPN's own
// name servers; the API does not say them, NordVPN's clients set them.
var (
	address = netip.MustParsePrefix("10.5.0.2/32")
	dns     = []netip.Addr{netip.MustParseAddr("103.86.96.100"), netip.MustParseAddr("103.86.99.100")}
)

const wgPort = 51820

// ErrRefused is an access token NordVPN did not take.
var ErrRefused = errors.New("nordvpn: the access token was refused")

// Client connects to NordVPN with one access token.
type Client struct {
	Token string
	// Location is a country code or name or a city. Empty takes the server
	// NordVPN recommends for where the request comes from.
	Location string

	api string
	web *http.Client
}

// Server is the NordLynx server a session goes to.
type Server struct {
	Hostname  string
	Address   netip.Addr
	PublicKey vpn.Key
}

func (c *Client) get(ctx context.Context, path string, q url.Values, auth bool, into any) error {
	u := cmp.Or(c.api, api) + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if auth {
		req.SetBasicAuth("token", c.Token)
	}
	hc := c.web
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("nordvpn: %s: %w", path, err)
	}
	defer resp.Body.Close()
	switch {
	case auth && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden):
		return ErrRefused
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("nordvpn: %s answered HTTP %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, into); err != nil {
		return fmt.Errorf("nordvpn: %s: %w", path, err)
	}
	return nil
}

// PrivateKey is the account's NordLynx key. NordVPN keeps one per account,
// so it is the same on every call.
func (c *Client) PrivateKey(ctx context.Context) (vpn.Key, error) {
	if c.Token == "" {
		return vpn.Key{}, errors.New("nordvpn: an access token is needed")
	}
	var creds struct {
		Key string `json:"nordlynx_private_key"`
	}
	if err := c.get(ctx, "/v1/users/services/credentials", nil, true, &creds); err != nil {
		return vpn.Key{}, err
	}
	k, err := vpn.ParseKey(creds.Key)
	if err != nil {
		return k, fmt.Errorf("nordvpn: NordLynx key: %w", err)
	}
	return k, nil
}

type country struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Cities []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Identifier string `json:"identifier"`
		DNSName    string `json:"dns_name"`
	} `json:"cities"`
}

// filter is the recommendations filter for Location.
func (c *Client) filter(ctx context.Context) (url.Values, error) {
	q := url.Values{
		"filters[servers_technologies][identifier]": {"wireguard_udp"},
		"limit": {"1"},
	}
	loc := strings.TrimSpace(c.Location)
	if loc == "" {
		return q, nil
	}
	var countries []country
	if err := c.get(ctx, "/v1/servers/countries", nil, false, &countries); err != nil {
		return nil, err
	}
	for _, co := range countries {
		if strings.EqualFold(co.Code, loc) || strings.EqualFold(co.Name, loc) {
			q.Set("filters[country_id]", strconv.Itoa(co.ID))
			return q, nil
		}
	}
	for _, co := range countries {
		for _, ci := range co.Cities {
			if strings.EqualFold(ci.Name, loc) || strings.EqualFold(ci.Identifier, loc) || strings.EqualFold(ci.DNSName, loc) {
				q.Set("filters[country_city_id]", strconv.Itoa(ci.ID))
				return q, nil
			}
		}
	}
	return nil, fmt.Errorf("nordvpn: %q: %w", loc, vpn.ErrUnknownLocation)
}

type recommendation struct {
	Hostname     string `json:"hostname"`
	Station      string `json:"station"`
	Technologies []struct {
		Identifier string `json:"identifier"`
		Metadata   []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"metadata"`
	} `json:"technologies"`
}

// Server is the NordLynx server NordVPN recommends for Location.
func (c *Client) Server(ctx context.Context) (Server, error) {
	q, err := c.filter(ctx)
	if err != nil {
		return Server{}, err
	}
	var recs []recommendation
	if err := c.get(ctx, "/v1/servers/recommendations", q, false, &recs); err != nil {
		return Server{}, err
	}
	for _, r := range recs {
		ip, err := netip.ParseAddr(r.Station)
		if err != nil {
			continue
		}
		for _, t := range r.Technologies {
			if t.Identifier != "wireguard_udp" {
				continue
			}
			for _, m := range t.Metadata {
				if m.Name != "public_key" {
					continue
				}
				if k, err := vpn.ParseKey(m.Value); err == nil {
					return Server{Hostname: r.Hostname, Address: ip.Unmap(), PublicKey: k}, nil
				}
			}
		}
	}
	return Server{}, fmt.Errorf("nordvpn: no NordLynx server for %q: %w", c.Location, vpn.ErrUnknownLocation)
}

// Connect returns the WireGuard configuration for a server at Location.
func (c *Client) Connect(ctx context.Context) (vpn.Config, Server, error) {
	key, err := c.PrivateKey(ctx)
	if err != nil {
		return vpn.Config{}, Server{}, err
	}
	srv, err := c.Server(ctx)
	if err != nil {
		return vpn.Config{}, Server{}, err
	}
	cfg := vpn.Config{
		PrivateKey: key,
		Addresses:  []netip.Prefix{address},
		DNS:        slices.Clone(dns),
		Peer: vpn.Peer{
			PublicKey:  srv.PublicKey,
			Endpoint:   netip.AddrPortFrom(srv.Address, wgPort).String(),
			AllowedIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")},
		},
	}
	return cfg, srv, cfg.Validate()
}
