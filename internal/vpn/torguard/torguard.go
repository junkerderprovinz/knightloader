// Package torguard gets WireGuard sessions from TorGuard through the setup
// API its servers answer on port 1443 with the account's VPN username and
// password, the one TorGuard's own scripts call
// (https://github.com/TorGuard/openwrt-scripts#torguards-wireguard-api-v1).
package torguard

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// caPEM is the CA that signs the certificates of TorGuard's servers. Its own
// root is not published, so this CA is the anchor.
//
//go:embed ca.pem
var caPEM []byte

const apiPort = 1443

// RenewEvery is how often a session is set up again. TorGuard lets a key
// lapse 12 hours after the last setup call.
var RenewEvery = 30 * time.Minute

// ErrRefused is a username and password TorGuard did not take.
var ErrRefused = errors.New("torguard: the login was refused")

// Client talks to one TorGuard server.
type Client struct {
	Username string
	Password string
	// Host is the server: a name such as nl.secureconnect.me or an
	// address.
	Host string
	// LookupNetIP resolves Host, the system's resolver when nil.
	LookupNetIP func(ctx context.Context, network, host string) ([]netip.Addr, error)

	roots  *x509.CertPool
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
	remote netip.Addr
}

// resolve looks Host up once per client: a name can stand for several
// servers, and each has its own key.
func (c *Client) resolve(ctx context.Context) (netip.Addr, error) {
	if c.remote.IsValid() {
		return c.remote, nil
	}
	if a, err := netip.ParseAddr(c.Host); err == nil {
		c.remote = a.Unmap()
		return c.remote, nil
	}
	lookup := c.LookupNetIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	ips, err := lookup(ctx, "ip4", c.Host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("torguard: resolve %s: %w", c.Host, err)
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("torguard: %s has no address", c.Host)
	}
	c.remote = ips[0].Unmap()
	return c.remote, nil
}

type setupResponse struct {
	ServerPublicKey string   `json:"server_public_key"`
	ClientIPv4      string   `json:"client_ipv4"`
	Routes          string   `json:"routes"`
	DNS             []string `json:"dns"`
	ServerAddress   string   `json:"vpn_server_address"`
	ServerPort      int      `json:"vpn_server_port"`
}

// setup registers pub with the server, or extends its registration.
func (c *Client) setup(ctx context.Context, pub vpn.Key) (setupResponse, error) {
	if c.Username == "" || c.Password == "" {
		return setupResponse{}, errors.New("torguard: a username and password are needed")
	}
	ip, err := c.resolve(ctx)
	if err != nil {
		return setupResponse{}, err
	}
	u := "https://" + netip.AddrPortFrom(ip, apiPort).String() + "/api/v1/setup?" + url.Values{"public-key": {pub.String()}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return setupResponse{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.httpClient(ip).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return setupResponse{}, fmt.Errorf("torguard: setup: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return setupResponse{}, ErrRefused
	case resp.StatusCode != http.StatusOK:
		return setupResponse{}, fmt.Errorf("torguard: setup answered HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return setupResponse{}, err
	}
	var out setupResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("torguard: setup: %w", err)
	}
	return out, nil
}

// httpClient trusts TorGuard's CA alone; its certificates are for the
// server's address, not a name.
func (c *Client) httpClient(ip netip.Addr) *http.Client {
	roots := c.roots
	if roots == nil {
		roots = x509.NewCertPool()
		roots.AppendCertsFromPEM(caPEM)
	}
	dial := c.dial
	if dial == nil {
		d := &net.Dialer{Timeout: 15 * time.Second}
		dial = d.DialContext
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:       dial,
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{ServerName: ip.String(), RootCAs: roots, MinVersion: tls.VersionTLS12},
		},
	}
}

// Connect registers a new key with the server and returns the WireGuard
// configuration for it.
func (c *Client) Connect(ctx context.Context) (vpn.Config, error) {
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Config{}, err
	}
	r, err := c.setup(ctx, key.PublicKey())
	if err != nil {
		return vpn.Config{}, err
	}
	return r.config(key, c.remote)
}

// Renew extends the registration of the key in cfg.
func (c *Client) Renew(ctx context.Context, cfg vpn.Config) error {
	_, err := c.setup(ctx, cfg.PrivateKey.PublicKey())
	return err
}

func (r setupResponse) config(key vpn.Key, server netip.Addr) (vpn.Config, error) {
	cfg := vpn.Config{PrivateKey: key}
	var err error
	if cfg.Peer.PublicKey, err = vpn.ParseKey(r.ServerPublicKey); err != nil {
		return cfg, fmt.Errorf("torguard: server key: %w", err)
	}
	addr, err := prefix(r.ClientIPv4)
	if err != nil {
		return cfg, fmt.Errorf("torguard: client address: %w", err)
	}
	cfg.Addresses = []netip.Prefix{addr}
	for _, s := range strings.Split(r.Routes, ",") {
		if p, err := prefix(s); err == nil {
			cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, p)
		}
	}
	if len(cfg.Peer.AllowedIPs) == 0 {
		cfg.Peer.AllowedIPs = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	}
	for _, s := range r.DNS {
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			cfg.DNS = append(cfg.DNS, a.Unmap())
		}
	}
	host := strings.TrimSpace(r.ServerAddress)
	if host == "" {
		host = server.String()
	}
	cfg.Peer.Endpoint = net.JoinHostPort(host, fmt.Sprint(r.ServerPort))
	return cfg, cfg.Validate()
}

// prefix reads an address with or without its prefix length.
func prefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}
