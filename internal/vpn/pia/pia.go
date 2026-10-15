// Package pia gets WireGuard sessions from Private Internet Access the way
// PIA's own manual-connections scripts do: a token for the username and
// password, a fresh key registered with the chosen server, and port
// forwarding through the server's API inside the tunnel.
package pia

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// caPEM is PIA's CA, which signs the certificates of its servers' WireGuard
// and port forwarding APIs.
//
//go:embed ca.pem
var caPEM []byte

const (
	tokenURL = "https://www.privateinternetaccess.com/api/client/v2/token"
	listURL  = "https://serverlist.piaservers.net/vpninfo/servers/v6"
	// keyPort is where a WireGuard server takes keys, pfPort where it
	// forwards ports, inside the tunnel.
	keyPort = 1337
	pfPort  = 19999
)

// BindEvery is how often a forwarded port is bound again; PIA drops a port
// that goes 15 minutes without.
var BindEvery = 15 * time.Minute

// ErrRefused is a username and password, or a token, PIA did not take.
var ErrRefused = errors.New("pia: the login was refused")

// Client connects to PIA with one account.
type Client struct {
	Username string
	Password string
	// Token is what FetchToken got; it stands in for the password for a day.
	Token string
	// Location is a region's id or name, or a country code. Empty takes the
	// fastest region that forwards ports.
	Location string

	tokenURL string
	listURL  string
	web      *http.Client
	roots    *x509.CertPool
	// dial reaches a server's API outside the tunnel.
	dial func(ctx context.Context, network, address string) (net.Conn, error)
	// ping times how long a server takes to answer, to find the fastest.
	ping func(ctx context.Context, ip netip.Addr) (time.Duration, error)
}

// Region is one of PIA's locations.
type Region struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	PortForward bool   `json:"port_forward"`
	Geo         bool   `json:"geo"`
	Offline     bool   `json:"offline"`
	Servers     struct {
		WG   []Server `json:"wg"`
		Meta []Server `json:"meta"`
	} `json:"servers"`
}

// Server is one machine of a region; CN is the name its certificate is for.
type Server struct {
	IP string `json:"ip"`
	CN string `json:"cn"`
}

// Session is one connection a server set up.
type Session struct {
	Region Region
	Server Server
	// Gateway is the server's address inside the tunnel.
	Gateway netip.Addr
}

func (c *Client) webClient() *http.Client {
	if c.web != nil {
		return c.web
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// FetchToken trades the username and password for a token.
func (c *Client) FetchToken(ctx context.Context) error {
	if c.Username == "" || c.Password == "" {
		return errors.New("pia: a username and password are needed")
	}
	form := url.Values{"username": {c.Username}, "password": {c.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cmp.Or(c.tokenURL, tokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out struct {
		Token string `json:"token"`
	}
	if err := do(c.webClient(), req, "token", &out); err != nil {
		return err
	}
	if out.Token == "" {
		return ErrRefused
	}
	c.Token = out.Token
	return nil
}

// Regions is PIA's server list.
func (c *Client) Regions(ctx context.Context) ([]Region, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cmp.Or(c.listURL, listURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.webClient().Do(req)
	if err != nil {
		return nil, plain("server list", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pia: the server list answered HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// The JSON is the first line; a signature over it follows.
	line, _, _ := strings.Cut(string(b), "\n")
	var list struct {
		Regions []Region `json:"regions"`
	}
	if err := json.Unmarshal([]byte(line), &list); err != nil {
		return nil, fmt.Errorf("pia: server list: %w", err)
	}
	return list.Regions, nil
}

// pick chooses the region for Location: the one it names, or the fastest of
// those it matches. Without a location every region that forwards ports and
// stands where it says is a candidate; a geo region only looks as if it
// were in its country.
func (c *Client) pick(ctx context.Context, regions []Region) (Region, error) {
	loc := strings.ToLower(strings.TrimSpace(c.Location))
	var cands []Region
	for _, r := range regions {
		if r.Offline || len(r.Servers.WG) == 0 {
			continue
		}
		named := strings.EqualFold(r.ID, loc) || strings.EqualFold(r.Name, loc) || strings.EqualFold(r.Country, loc)
		if loc == "" && r.PortForward && !r.Geo || loc != "" && named {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return Region{}, fmt.Errorf("pia: %q: %w", c.Location, vpn.ErrUnknownLocation)
	}
	if len(cands) == 1 {
		return cands[0], nil
	}
	return c.fastest(ctx, cands), nil
}

// fastest times a connection to each region, as PIA's scripts do, and takes
// the quickest; if none answers it takes the first.
func (c *Client) fastest(ctx context.Context, regions []Region) Region {
	ping := c.ping
	if ping == nil {
		ping = dialTime
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var (
		mu   sync.Mutex
		best = -1
		took time.Duration
		wg   sync.WaitGroup
	)
	for i, r := range regions {
		s := r.Servers.WG[0]
		if len(r.Servers.Meta) > 0 {
			s = r.Servers.Meta[0]
		}
		ip, err := netip.ParseAddr(s.IP)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := ping(ctx, ip)
			if err != nil {
				return
			}
			mu.Lock()
			if best < 0 || d < took {
				best, took = i, d
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if best < 0 {
		return regions[0]
	}
	return regions[best]
}

func dialTime(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(ip, 443).String())
	if err != nil {
		return 0, err
	}
	conn.Close()
	return time.Since(start), nil
}

type keyResponse struct {
	Status     string   `json:"status"`
	ServerKey  string   `json:"server_key"`
	ServerPort int      `json:"server_port"`
	ServerIP   string   `json:"server_ip"`
	ServerVIP  string   `json:"server_vip"`
	PeerIP     string   `json:"peer_ip"`
	DNSServers []string `json:"dns_servers"`
}

// Connect registers a new key with a server of the chosen region and returns
// the WireGuard configuration for it. It fetches a token first when there is
// none.
func (c *Client) Connect(ctx context.Context) (vpn.Config, Session, error) {
	if c.Token == "" {
		if err := c.FetchToken(ctx); err != nil {
			return vpn.Config{}, Session{}, err
		}
	}
	regions, err := c.Regions(ctx)
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	region, err := c.pick(ctx, regions)
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	srv := region.Servers.WG[rand.IntN(len(region.Servers.WG))]
	ip, err := netip.ParseAddr(srv.IP)
	if err != nil {
		return vpn.Config{}, Session{}, fmt.Errorf("pia: server address: %w", err)
	}
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	q := url.Values{"pt": {c.Token}, "pubkey": {key.PublicKey().String()}}
	u := "https://" + netip.AddrPortFrom(ip, keyPort).String() + "/addKey?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	var resp keyResponse
	if err := do(c.serverClient(srv.CN, c.dial), req, "addKey", &resp); err != nil {
		return vpn.Config{}, Session{}, err
	}
	if resp.Status != "OK" {
		return vpn.Config{}, Session{}, fmt.Errorf("pia: addKey answered %q", resp.Status)
	}
	cfg, err := resp.config(key, ip)
	if err != nil {
		return cfg, Session{}, err
	}
	s := Session{Region: region, Server: srv}
	if gw, err := netip.ParseAddr(resp.ServerVIP); err == nil {
		s.Gateway = gw.Unmap()
	}
	return cfg, s, nil
}

func (r keyResponse) config(key vpn.Key, server netip.Addr) (vpn.Config, error) {
	cfg := vpn.Config{PrivateKey: key}
	var err error
	if cfg.Peer.PublicKey, err = vpn.ParseKey(r.ServerKey); err != nil {
		return cfg, fmt.Errorf("pia: server key: %w", err)
	}
	peer, err := netip.ParseAddr(r.PeerIP)
	if err != nil {
		return cfg, fmt.Errorf("pia: peer address: %w", err)
	}
	cfg.Addresses = []netip.Prefix{netip.PrefixFrom(peer.Unmap(), 32)}
	for _, s := range r.DNSServers {
		if a, err := netip.ParseAddr(s); err == nil {
			cfg.DNS = append(cfg.DNS, a.Unmap())
		}
	}
	cfg.Peer.Endpoint = netip.AddrPortFrom(server, uint16(r.ServerPort)).String()
	cfg.Peer.AllowedIPs = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	return cfg, cfg.Validate()
}

// serverClient talks to a PIA server's API, whose certificate is for its CN
// rather than its address and signed by PIA's own CA.
func (c *Client) serverClient(cn string, dial func(ctx context.Context, network, address string) (net.Conn, error)) *http.Client {
	roots := c.roots
	if roots == nil {
		roots = x509.NewCertPool()
		roots.AppendCertsFromPEM(caPEM)
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 15 * time.Second}
		dial = d.DialContext
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:       dial,
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{ServerName: cn, RootCAs: roots, MinVersion: tls.VersionTLS12},
		},
	}
}

type signature struct {
	Status    string `json:"status"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// KeepPort asks the server for a port to forward and binds it every
// BindEvery until ctx ends, reporting it to set once bound. It talks to the
// server inside the tunnel, so dial is the tunnel's. A region that does not
// forward ports returns at once.
func (c *Client) KeepPort(ctx context.Context, s Session, dial func(ctx context.Context, network, address string) (net.Conn, error), set func(uint16)) error {
	if !s.Region.PortForward {
		return nil
	}
	hc := c.serverClient(s.Server.CN, dial)
	var (
		sig  signature
		api  netip.Addr
		errs []error
	)
	// gluetun found the API on the tunnel's gateway; PIA's scripts use the
	// server's own address.
	for _, a := range apiAddresses(s) {
		err := c.pfGet(ctx, hc, a, "getSignature", url.Values{"token": {c.Token}}, &sig)
		if err == nil {
			api = a
			break
		}
		errs = append(errs, err)
	}
	if !api.IsValid() {
		return errors.Join(errs...)
	}
	if sig.Status != "OK" {
		return fmt.Errorf("pia: getSignature answered %q", sig.Status)
	}
	port, err := payloadPort(sig.Payload)
	if err != nil {
		return err
	}
	q := url.Values{"payload": {sig.Payload}, "signature": {sig.Signature}}
	for {
		var bound signature
		if err := c.pfGet(ctx, hc, api, "bindPort", q, &bound); err != nil {
			return err
		}
		if bound.Status != "OK" {
			return fmt.Errorf("pia: bindPort answered %q", bound.Status)
		}
		set(port)
		t := time.NewTimer(BindEvery)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

func apiAddresses(s Session) []netip.Addr {
	var out []netip.Addr
	if s.Gateway.IsValid() {
		out = append(out, s.Gateway)
	}
	if a, err := netip.ParseAddr(s.Server.IP); err == nil && !slices.Contains(out, a) {
		out = append(out, a)
	}
	return out
}

func (c *Client) pfGet(ctx context.Context, hc *http.Client, api netip.Addr, method string, q url.Values, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u := "https://" + netip.AddrPortFrom(api, pfPort).String() + "/" + method + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return do(hc, req, method, into)
}

func payloadPort(payload string) (uint16, error) {
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return 0, fmt.Errorf("pia: payload: %w", err)
	}
	var p struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, fmt.Errorf("pia: payload: %w", err)
	}
	if p.Port <= 0 || p.Port > 65535 {
		return 0, fmt.Errorf("pia: payload port %d", p.Port)
	}
	return uint16(p.Port), nil
}

// do sends req and decodes a 200 answer into into. A 401 or 403 is a refused
// login.
func do(hc *http.Client, req *http.Request, method string, into any) error {
	resp, err := hc.Do(req)
	if err != nil {
		return plain(method, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("pia: %s: %w", method, ErrRefused)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("pia: %s answered HTTP %d", method, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, into); err != nil {
		return fmt.Errorf("pia: %s: %w", method, err)
	}
	return nil
}

// plain drops the URL from a request's error: it carries the token.
func plain(method string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("pia: %s: %w", method, err)
}
