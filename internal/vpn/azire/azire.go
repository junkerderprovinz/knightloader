// Package azire gets WireGuard connections from AzireVPN through its API
// (azirevpn.com/docs/api): a token for the username and password, an IP that
// carries a key made here, and the server key and host of each location.
package azire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

const (
	DefaultAPI    = "https://api.azirevpn.com/v3"
	userAgent     = "KnightLoader"
	wireguardPort = 51820
	// forwardDays is how long a forwarded port is kept; each connection
	// takes the one already there.
	forwardDays = 30
)

// APIError is an answer of AzireVPN's API other than success. A 401 is a
// refused login or token.
type APIError struct {
	Path    string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("azire: %s answered HTTP %d: %s", e.Path, e.Status, e.Message)
	}
	return fmt.Sprintf("azire: %s answered HTTP %d", e.Path, e.Status)
}

// Client talks to AzireVPN's API.
type Client struct {
	// API is DefaultAPI when empty.
	API  string
	HTTP *http.Client
}

func (c *Client) do(ctx context.Context, hc *http.Client, method, path, token string, body, into any) error {
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
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if hc == nil {
		hc = c.HTTP
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal(data, &env)
	if resp.StatusCode < 200 || resp.StatusCode > 299 || env.Status == "error" {
		return &APIError{Path: req.URL.Path, Status: resp.StatusCode, Message: env.Message}
	}
	if into == nil {
		return nil
	}
	// The API wraps its answers in data, mostly; a list of one stands for
	// the one.
	raw := bytes.TrimSpace(env.Data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = data
	}
	err = json.Unmarshal(raw, into)
	var list []json.RawMessage
	if err != nil && json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return json.Unmarshal(list[0], into)
	}
	return err
}

// Token logs in with the username and password and returns a new token,
// listed in the account under device.
func (c *Client) Token(ctx context.Context, username, password, device string) (string, error) {
	var out struct {
		Key string `json:"key"`
	}
	body := map[string]string{"username": username, "password": password, "device_name": device}
	err := c.do(ctx, nil, http.MethodPost, "/tokens", "", body, &out)
	if err == nil && out.Key == "" {
		err = errors.New("azire: the API sent no token")
	}
	return out.Key, err
}

// DeleteToken ends the token the request is made with.
func (c *Client) DeleteToken(ctx context.Context, token string) error {
	return c.do(ctx, nil, http.MethodDelete, "/tokens", token, map[string]bool{"purge_resources": false}, nil)
}

// User is what the account allows.
type User struct {
	Active bool `json:"is_active"`
	IPs    struct {
		Allocated int `json:"allocated"`
		Available int `json:"available"`
	} `json:"ips"`
}

// User looks the account up.
func (c *Client) User(ctx context.Context, token string) (User, error) {
	var out User
	err := c.do(ctx, nil, http.MethodGet, "/users/me", token, nil, &out)
	return out, err
}

// IP is an address inside the tunnel and the keys that may use it.
type IP struct {
	ID          string   `json:"id"`
	IPv4Address string   `json:"ipv4_address"`
	IPv4Netmask int      `json:"ipv4_netmask"`
	IPv6Address string   `json:"ipv6_address"`
	IPv6Netmask int      `json:"ipv6_netmask"`
	DNS         []string `json:"dns"`
	Keys        []struct {
		Key string `json:"key"`
	} `json:"keys"`
}

// Has says whether pub may use the address.
func (ip IP) Has(pub vpn.Key) bool {
	for _, k := range ip.Keys {
		if k.Key == pub.String() {
			return true
		}
	}
	return false
}

// IP looks up an address of the account.
func (c *Client) IP(ctx context.Context, token, id string) (IP, error) {
	var out IP
	err := c.do(ctx, nil, http.MethodGet, "/ips/"+url.PathEscape(id), token, nil, &out)
	return out, err
}

// AddIP registers a public key on a new address.
func (c *Client) AddIP(ctx context.Context, token string, pub vpn.Key) (IP, error) {
	var out IP
	err := c.do(ctx, nil, http.MethodPost, "/ips", token, map[string]string{"key": pub.String()}, &out)
	if err == nil && out.IPv4Address == "" {
		err = errors.New("azire: the API sent no address")
	}
	return out, err
}

// RemoveIP frees an address of the account.
func (c *Client) RemoveIP(ctx context.Context, token, id string) error {
	return c.do(ctx, nil, http.MethodDelete, "/ips/"+url.PathEscape(id), token, nil, nil)
}

// Location is one place AzireVPN has servers.
type Location struct {
	Name      string `json:"name"`
	ISO       string `json:"iso"`
	Pool      string `json:"pool"`
	PublicKey string `json:"pubkey"`
}

// Locations lists the locations, those of the account among them where token
// is given.
func (c *Client) Locations(ctx context.Context, token string) ([]Location, error) {
	var out struct {
		Locations []Location `json:"locations"`
	}
	err := c.do(ctx, nil, http.MethodGet, "/locations", token, nil, &out)
	return out.Locations, err
}

// ErrNoLocation is a location AzireVPN does not have.
var ErrNoLocation = errors.New("azire: no location matches")

// PickLocation finds a location by its name, such as "se-sto", or the first
// of a country, such as "se".
func PickLocation(all []Location, want string) (Location, error) {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, l := range all {
		if strings.ToLower(l.Name) == want {
			return l, nil
		}
	}
	for _, l := range all {
		if strings.ToLower(l.ISO) == want {
			return l, nil
		}
	}
	return Location{}, ErrNoLocation
}

// Config is the WireGuard configuration of an address at a location.
func Config(key vpn.Key, ip IP, l Location) (vpn.Config, error) {
	cfg := vpn.Config{PrivateKey: key}
	a, err := netip.ParseAddr(ip.IPv4Address)
	if err != nil {
		return cfg, fmt.Errorf("azire: address: %w", err)
	}
	cfg.Addresses = append(cfg.Addresses, netip.PrefixFrom(a, bits(ip.IPv4Netmask, 32)))
	cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("0.0.0.0/0"))
	if a6, err := netip.ParseAddr(ip.IPv6Address); err == nil {
		cfg.Addresses = append(cfg.Addresses, netip.PrefixFrom(a6, bits(ip.IPv6Netmask, 128)))
		cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("::/0"))
	}
	for _, s := range ip.DNS {
		if d, err := netip.ParseAddr(s); err == nil {
			cfg.DNS = append(cfg.DNS, d)
		}
	}
	if cfg.Peer.PublicKey, err = vpn.ParseKey(l.PublicKey); err != nil {
		return cfg, fmt.Errorf("azire: server key: %w", err)
	}
	cfg.Peer.Endpoint = net.JoinHostPort(l.Pool, strconv.Itoa(wireguardPort))
	return cfg, cfg.Validate()
}

func bits(n, max int) int {
	if n <= 0 || n > max {
		return max
	}
	return n
}

// ForwardPort returns the port the connected server forwards to the address,
// asking for one where there is none. The server answers for itself only, so
// dial has to be the tunnel's.
func (c *Client) ForwardPort(ctx context.Context, token string, internal netip.Addr, dial func(ctx context.Context, network, address string) (net.Conn, error)) (uint16, error) {
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: dial, ForceAttemptHTTP2: true}}
	defer hc.CloseIdleConnections()
	var have struct {
		Ports []struct {
			Port int `json:"port"`
		} `json:"ports"`
	}
	q := "?internal_ipv4=" + url.QueryEscape(internal.String())
	if err := c.do(ctx, hc, http.MethodGet, "/portforwardings"+q, token, nil, &have); err != nil {
		return 0, err
	}
	for _, p := range have.Ports {
		if p.Port > 0 && p.Port < 65536 {
			return uint16(p.Port), nil
		}
	}
	var made struct {
		Port int `json:"port"`
	}
	body := map[string]any{"internal_ipv4": internal.String(), "hidden": false, "expires_in": forwardDays}
	if err := c.do(ctx, hc, http.MethodPost, "/portforwardings", token, body, &made); err != nil {
		return 0, err
	}
	if made.Port <= 0 || made.Port > 65535 {
		return 0, errors.New("azire: the server forwarded no port")
	}
	return uint16(made.Port), nil
}
