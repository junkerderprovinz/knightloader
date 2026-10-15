// Package obscura gets WireGuard connections from Obscura VPN through its
// public API (github.com/Sovereign-Engineering/obscuravpn-api): an auth token
// for the account number, then a tunnel of the plain WireGuard kind, the one
// Obscura's portal hands out as a .conf, for a key made here. Such a tunnel
// runs through an Obscura relay to an exit Mullvad runs.
package obscura

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

const (
	DefaultAPI = "https://v1.api.prod.obscura.net/api/"
	// userAgent names the client and a version, which Obscura asks of
	// every caller of its API.
	userAgent = "github.com/junkerderprovinz/knightloader/1"
)

// Error kinds of Obscura's API that a caller acts on.
const (
	InvalidAccountID          = "InvalidAccountId"
	MissingOrInvalidAuthToken = "MissingOrInvalidAuthToken"
	AccountExpired            = "AccountExpired"
	TunnelLimitExceeded       = "TunnelLimitExceeded"
	NoMatchingExit            = "NoMatchingExit"
)

// APIError is an error answer of Obscura's API.
type APIError struct {
	Path   string
	Status int
	Kind   string
	Msg    string
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("obscura: %s answered HTTP %d", e.Path, e.Status)
	if e.Kind != "" {
		s += ", " + e.Kind
	}
	if e.Msg != "" {
		s += ": " + e.Msg
	}
	return s
}

// Client talks to Obscura's API.
type Client struct {
	// API is DefaultAPI when empty.
	API  string
	HTTP *http.Client
}

func (c *Client) do(ctx context.Context, method, path, token string, body, into any) error {
	base := c.API
	if base == "" {
		base = DefaultAPI
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+"/"+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		e := &APIError{Path: req.URL.Path, Status: resp.StatusCode}
		var raw struct {
			Error json.RawMessage `json:"error"`
			Msg   string          `json:"msg"`
		}
		if json.Unmarshal(data, &raw) == nil {
			e.Msg = raw.Msg
			var kinds map[string]json.RawMessage
			if json.Unmarshal(raw.Error, &kinds) == nil {
				for k := range kinds {
					e.Kind = k
				}
			}
		}
		return e
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(data, into)
}

// Token trades an account number for an auth token. Obscura sets no end to
// it, so it is kept until the API refuses it.
func (c *Client) Token(ctx context.Context, account string) (string, error) {
	var out struct {
		AuthToken string `json:"auth_token"`
	}
	err := c.do(ctx, http.MethodPost, "token2", "", map[string]any{"account_id": account, "pow": nil}, &out)
	if err == nil && out.AuthToken == "" {
		err = errors.New("obscura: the API sent no auth token")
	}
	return out.AuthToken, err
}

// Exit is a server traffic leaves Obscura by.
type Exit struct {
	ID          string `json:"id"`
	CountryCode string `json:"country_code"`
	CityCode    string `json:"city_code"`
	ProviderID  string `json:"provider_id"`
}

// Exits lists the exits.
func (c *Client) Exits(ctx context.Context, token string) ([]Exit, error) {
	var out struct {
		Exits []Exit `json:"exits"`
	}
	err := c.do(ctx, http.MethodGet, "exits2", token, nil, &out)
	return out.Exits, err
}

// ErrNoExit is a location no exit matches.
var ErrNoExit = errors.New("obscura: no exit matches the location")

// PickExit chooses an exit for a location: a country code such as "se", a
// city such as "se-got" or an exit's own name. It draws among those that fit.
func PickExit(exits []Exit, location string) (Exit, error) {
	location = strings.ToLower(strings.TrimSpace(location))
	var fit []Exit
	for _, e := range exits {
		cc := strings.ToLower(e.CountryCode)
		if location == cc || location == cc+"-"+strings.ToLower(e.CityCode) ||
			location == strings.ToLower(e.ID) || location == strings.ToLower(e.ProviderID) {
			fit = append(fit, e)
		}
	}
	if len(fit) == 0 {
		return Exit{}, ErrNoExit
	}
	return fit[rand.IntN(len(fit))], nil
}

// Tunnel is one WireGuard key registered with the account, with what the
// server gave it.
type Tunnel struct {
	ID     string
	Exit   Exit
	Config vpn.Config
}

type tunnelJSON struct {
	ID     string `json:"id"`
	Config struct {
		Type   string `json:"type"`
		Client struct {
			PublicKey string   `json:"wg_pubkey"`
			Addresses []string `json:"addresses"`
		} `json:"client"`
		Server struct {
			PublicKey string   `json:"wg_pubkey"`
			Endpoints []string `json:"endpoints"`
			DNS       []string `json:"dnses"`
		} `json:"server"`
	} `json:"config"`
	Exit Exit `json:"exit"`
}

// tunnel turns the server's answer into a configuration without a private
// key, which stays with the caller.
func (t tunnelJSON) tunnel() (Tunnel, error) {
	out := Tunnel{ID: t.ID, Exit: t.Exit}
	if t.Config.Type != "udp_port" {
		return out, fmt.Errorf("obscura: tunnel %s is of the kind %q", t.ID, t.Config.Type)
	}
	cfg := &out.Config
	var err error
	if cfg.Peer.PublicKey, err = vpn.ParseKey(t.Config.Server.PublicKey); err != nil {
		return out, fmt.Errorf("obscura: server key: %w", err)
	}
	if len(t.Config.Server.Endpoints) == 0 {
		return out, errors.New("obscura: the tunnel has no endpoint")
	}
	cfg.Peer.Endpoint = t.Config.Server.Endpoints[0]
	for _, ep := range t.Config.Server.Endpoints {
		if ap, err := netip.ParseAddrPort(ep); err == nil && ap.Addr().Is4() {
			cfg.Peer.Endpoint = ep
			break
		}
	}
	for _, s := range t.Config.Client.Addresses {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return out, fmt.Errorf("obscura: address %q: %w", s, err)
		}
		cfg.Addresses = append(cfg.Addresses, p)
		if p.Addr().Is4() {
			cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("0.0.0.0/0"))
		} else {
			cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("::/0"))
		}
	}
	for _, s := range t.Config.Server.DNS {
		if a, err := netip.ParseAddr(s); err == nil {
			cfg.DNS = append(cfg.DNS, a)
		}
	}
	return out, nil
}

// Tunnels lists the account's tunnels of the plain WireGuard kind.
func (c *Client) Tunnels(ctx context.Context, token string) ([]Tunnel, error) {
	var raw []tunnelJSON
	if err := c.do(ctx, http.MethodGet, "tunnel", token, nil, &raw); err != nil {
		return nil, err
	}
	var out []Tunnel
	for _, t := range raw {
		if tn, err := t.tunnel(); err == nil {
			out = append(out, tn)
		}
	}
	return out, nil
}

// CreateTunnel registers a public key as a new tunnel to an exit, or to one
// Obscura picks when exit is empty. An account with all its tunnels taken
// answers TunnelLimitExceeded.
func (c *Client) CreateTunnel(ctx context.Context, token string, pub vpn.Key, exit string) (Tunnel, error) {
	body := map[string]any{"type": "udp_port", "label": "KnightLoader", "wg_pubkey": pub.String(), "relay": nil, "exit": nil}
	if exit != "" {
		body["exit"] = exit
	}
	var raw tunnelJSON
	if err := c.do(ctx, http.MethodPost, "tunnel", token, body, &raw); err != nil {
		return Tunnel{}, err
	}
	return raw.tunnel()
}

// DeleteTunnel frees a tunnel's place on the account.
func (c *Client) DeleteTunnel(ctx context.Context, token, id string) error {
	return c.do(ctx, http.MethodDelete, "tunnel", token, map[string]string{"id": id}, nil)
}
