// Package mullvad gets WireGuard connections from Mullvad VPN the way its own
// app does: an access token for the account number, a device that carries a
// key made here, and a relay from Mullvad's public relay list.
package mullvad

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

const (
	// DefaultAPI is Mullvad's API; the paths are those of its app's client
	// (github.com/mullvad/mullvadvpn-app, mullvad-api).
	DefaultAPI = "https://api.mullvad.net"
	// DefaultWhereURL tells where a request comes from, for the nearest relay.
	DefaultWhereURL = "https://am.i.mullvad.net/json"
	userAgent       = "KnightLoader"
	wireguardPort   = 51820
)

// Error codes of Mullvad's API that a caller acts on.
const (
	InvalidAccount     = "INVALID_ACCOUNT"
	InvalidAccessToken = "INVALID_ACCESS_TOKEN"
	DeviceNotFound     = "DEVICE_NOT_FOUND"
	MaxDevicesReached  = "MAX_DEVICES_REACHED"
)

// APIError is an answer of Mullvad's API other than the one asked for.
type APIError struct {
	Path   string
	Status int
	Code   string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("mullvad: %s answered HTTP %d, %s", e.Path, e.Status, e.Code)
	}
	return fmt.Sprintf("mullvad: %s answered HTTP %d", e.Path, e.Status)
}

// Client talks to Mullvad's API.
type Client struct {
	// API is DefaultAPI when empty, and WhereURL DefaultWhereURL.
	API      string
	WhereURL string
	HTTP     *http.Client
}

func (c *Client) do(ctx context.Context, method, url, token string, body, into any, want int) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != want {
		var e struct {
			Code string `json:"code"`
		}
		json.Unmarshal(data, &e)
		return &APIError{Path: req.URL.Path, Status: resp.StatusCode, Code: e.Code}
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(data, into)
}

func (c *Client) url(path string) string {
	base := c.API
	if base == "" {
		base = DefaultAPI
	}
	return strings.TrimSuffix(base, "/") + path
}

// Token trades an account number for an access token, which lasts about an
// hour.
func (c *Client) Token(ctx context.Context, account string) (string, error) {
	var out struct {
		AccessToken string `json:"access_token"`
	}
	err := c.do(ctx, http.MethodPost, c.url("/auth/v1/token"), "", map[string]string{"account_number": account}, &out, http.StatusOK)
	if err == nil && out.AccessToken == "" {
		err = errors.New("mullvad: the API sent no access token")
	}
	return out.AccessToken, err
}

// Expiry is when the account runs out of time.
func (c *Client) Expiry(ctx context.Context, token string) (time.Time, error) {
	var out struct {
		Expiry string `json:"expiry"`
	}
	if err := c.do(ctx, http.MethodGet, c.url("/accounts/v1/accounts/me"), token, nil, &out, http.StatusOK); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, out.Expiry)
}

// Device is one key registered with the account, and the addresses Mullvad
// gives it inside the tunnel.
type Device struct {
	ID        string
	PublicKey vpn.Key
	IPv4      netip.Prefix
	IPv6      netip.Prefix
}

type deviceJSON struct {
	ID          string `json:"id"`
	PublicKey   string `json:"pubkey"`
	IPv4Address string `json:"ipv4_address"`
	IPv6Address string `json:"ipv6_address"`
}

func (d deviceJSON) device() (Device, error) {
	out := Device{ID: d.ID}
	var err error
	if out.PublicKey, err = vpn.ParseKey(d.PublicKey); err != nil {
		return out, fmt.Errorf("mullvad: device key: %w", err)
	}
	if out.IPv4, err = netip.ParsePrefix(d.IPv4Address); err != nil {
		return out, fmt.Errorf("mullvad: device address: %w", err)
	}
	if d.IPv6Address != "" {
		if out.IPv6, err = netip.ParsePrefix(d.IPv6Address); err != nil {
			return out, fmt.Errorf("mullvad: device address: %w", err)
		}
	}
	return out, nil
}

// Device looks up a device of the account; an *APIError with DeviceNotFound
// means it was removed.
func (c *Client) Device(ctx context.Context, token, id string) (Device, error) {
	var out deviceJSON
	if err := c.do(ctx, http.MethodGet, c.url("/accounts/v1/devices/"+id), token, nil, &out, http.StatusOK); err != nil {
		return Device{}, err
	}
	return out.device()
}

// AddDevice registers a public key as a new device. An account with all its
// devices taken answers MaxDevicesReached.
func (c *Client) AddDevice(ctx context.Context, token string, pub vpn.Key) (Device, error) {
	var out deviceJSON
	body := map[string]any{"pubkey": pub.String(), "hijack_dns": false}
	if err := c.do(ctx, http.MethodPost, c.url("/accounts/v1/devices"), token, body, &out, http.StatusCreated); err != nil {
		return Device{}, err
	}
	return out.device()
}

// RemoveDevice frees a device's place on the account.
func (c *Client) RemoveDevice(ctx context.Context, token, id string) error {
	return c.do(ctx, http.MethodDelete, c.url("/accounts/v1/devices/"+id), token, nil, nil, http.StatusNoContent)
}

// Relay is one WireGuard server.
type Relay struct {
	Hostname  string
	Location  string
	PublicKey vpn.Key
	Addr      netip.Addr
	Weight    uint64
	// InCountry says the relay is picked for its country as a whole; one
	// that is not is only picked by its city or name.
	InCountry bool
}

// Relays is Mullvad's WireGuard relay list.
type Relays struct {
	Relays    []Relay
	Locations map[string]Place
	Ports     [][2]int
	Gateway   netip.Addr
}

// Place is where a location code is.
type Place struct {
	Latitude, Longitude float64
}

// Relays fetches the list of WireGuard relays that are up.
func (c *Client) Relays(ctx context.Context) (Relays, error) {
	var raw struct {
		Locations map[string]struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"locations"`
		Wireguard struct {
			PortRanges [][2]int `json:"port_ranges"`
			Gateway    string   `json:"ipv4_gateway"`
			Relays     []struct {
				Hostname  string `json:"hostname"`
				Location  string `json:"location"`
				Active    bool   `json:"active"`
				Addr      string `json:"ipv4_addr_in"`
				Weight    uint64 `json:"weight"`
				InCountry bool   `json:"include_in_country"`
				PublicKey string `json:"public_key"`
			} `json:"relays"`
		} `json:"wireguard"`
	}
	if err := c.do(ctx, http.MethodGet, c.url("/public/relays/wireguard/v2"), "", nil, &raw, http.StatusOK); err != nil {
		return Relays{}, err
	}
	out := Relays{Locations: map[string]Place{}, Ports: raw.Wireguard.PortRanges}
	out.Gateway, _ = netip.ParseAddr(raw.Wireguard.Gateway)
	for code, l := range raw.Locations {
		out.Locations[strings.ToLower(code)] = Place{l.Latitude, l.Longitude}
	}
	for _, r := range raw.Wireguard.Relays {
		key, kerr := vpn.ParseKey(r.PublicKey)
		addr, aerr := netip.ParseAddr(r.Addr)
		if !r.Active || kerr != nil || aerr != nil {
			continue
		}
		out.Relays = append(out.Relays, Relay{
			Hostname: strings.ToLower(r.Hostname), Location: strings.ToLower(r.Location),
			PublicKey: key, Addr: addr, Weight: r.Weight, InCountry: r.InCountry,
		})
	}
	return out, nil
}

// Where is the place a request from here appears to come from.
func (c *Client) Where(ctx context.Context) (Place, error) {
	url := c.WhereURL
	if url == "" {
		url = DefaultWhereURL
	}
	var out struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := c.do(ctx, http.MethodGet, url, "", nil, &out, http.StatusOK); err != nil {
		return Place{}, err
	}
	if out.Latitude == nil || out.Longitude == nil {
		return Place{}, errors.New("mullvad: no position for this address")
	}
	return Place{*out.Latitude, *out.Longitude}, nil
}

// ErrNoRelay is a location no relay that is up matches.
var ErrNoRelay = errors.New("mullvad: no relay matches the location")

// Pick chooses a relay for a location: a country code such as "se", a city
// such as "se-got" or a relay's name such as "se-got-wg-001". With no
// location it takes the city nearest to near, when that is known. Among the
// relays that fit it draws by their weight, as Mullvad's app does.
func (l Relays) Pick(location string, near *Place) (Relay, error) {
	location = strings.ToLower(strings.TrimSpace(location))
	var fit []Relay
	switch {
	case location != "":
		for _, r := range l.Relays {
			if r.Hostname == location || r.Location == location ||
				r.InCountry && strings.HasPrefix(r.Location, location+"-") {
				fit = append(fit, r)
			}
		}
	case near != nil:
		best, bestKm := "", math.Inf(1)
		for _, r := range l.Relays {
			if p, ok := l.Locations[r.Location]; ok {
				if km := distance(*near, p); km < bestKm {
					best, bestKm = r.Location, km
				}
			}
		}
		for _, r := range l.Relays {
			if r.Location == best {
				fit = append(fit, r)
			}
		}
	}
	if len(fit) == 0 && location == "" {
		fit = l.Relays
	}
	if len(fit) == 0 {
		return Relay{}, ErrNoRelay
	}
	var total uint64
	for _, r := range fit {
		total += r.Weight
	}
	if total == 0 {
		return fit[rand.IntN(len(fit))], nil
	}
	n := rand.Uint64N(total)
	for _, r := range fit {
		if n < r.Weight {
			return r, nil
		}
		n -= r.Weight
	}
	return fit[len(fit)-1], nil
}

func distance(a, b Place) float64 {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dlat, dlon := rad(b.Latitude-a.Latitude), rad(b.Longitude-a.Longitude)
	h := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(rad(a.Latitude))*math.Cos(rad(b.Latitude))*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 6371 * 2 * math.Asin(math.Sqrt(h))
}

// port is 51820 where the relays take it, and the first port they take
// otherwise.
func (l Relays) port() int {
	for _, r := range l.Ports {
		if r[0] <= wireguardPort && wireguardPort <= r[1] {
			return wireguardPort
		}
	}
	if len(l.Ports) > 0 {
		return l.Ports[0][0]
	}
	return wireguardPort
}

// Config is the WireGuard configuration of a device on a relay.
func (l Relays) Config(key vpn.Key, d Device, r Relay) (vpn.Config, error) {
	cfg := vpn.Config{PrivateKey: key}
	cfg.Addresses = append(cfg.Addresses, d.IPv4)
	cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("0.0.0.0/0"))
	if d.IPv6.IsValid() {
		cfg.Addresses = append(cfg.Addresses, d.IPv6)
		cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("::/0"))
	}
	if l.Gateway.IsValid() {
		cfg.DNS = []netip.Addr{l.Gateway}
	}
	cfg.Peer.PublicKey = r.PublicKey
	cfg.Peer.Endpoint = net.JoinHostPort(r.Addr.String(), strconv.Itoa(l.port()))
	return cfg, cfg.Validate()
}
