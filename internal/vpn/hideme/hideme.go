// Package hideme gets WireGuard sessions from hide.me the way its own Linux
// client does: an access token for the account's username and password, then
// a connect request with a fresh key for each session.
package hideme

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// rootPEM is hide.me's own root CA, which signs its servers' certificates.
//
//go:embed ca.pem
var rootPEM []byte

// caPins are the SHA-256 hashes of the public keys of hide.me's CAs; every CA
// in a server's chain must be one of them.
var caPins = map[string]string{
	"Hide.Me Root CA":      "AdKh8rXi68jeqv5kEzF4wJ9M2R89gFuMILRQ1uwADQI=",
	"Hide.Me Server CA #1": "CsEyDelMHMPh9qLGgeQn8sJwdUwvc+fCMhOU9Ne5PbU=",
}

const (
	restPort   = 432
	apiVersion = "v1.0.0"
	domain     = "hide.me"
	// controlAddr answers inside the tunnel, for port forwarding.
	controlAddr = "10.255.255.250:4321"
	userAgent   = "KnightLoader"
)

// ErrUpdateRequired is hide.me's 403, its answer to a client version it does
// not accept.
var ErrUpdateRequired = errors.New("hideme: the server wants a newer client")

// Client talks to one hide.me server. Host is a server name as hide.me lists
// it, such as "nl" or "nl.hideservers.net".
type Client struct {
	Host     string
	Username string
	Password string
	// AccessToken is what FetchAccessToken got. It stands in for the
	// password from then on, so the caller keeps it rather than the
	// password.
	AccessToken []byte
	// LookupNetIP resolves the server, the system's resolver when nil.
	LookupNetIP func(ctx context.Context, network, host string) ([]netip.Addr, error)

	roots  *x509.CertPool
	pins   map[string]string
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
	remote netip.Addr
}

// Session is one connection the server set up.
type Session struct {
	Token   []byte
	Gateway []netip.Addr
	// StaleAccessToken means the server wants FetchAccessToken called again.
	StaleAccessToken bool
}

func (c *Client) server() string {
	h := strings.TrimSuffix(c.Host, ".hide.me")
	return strings.TrimSuffix(h, ".hideservers.net")
}

func (c *Client) fqdn() string {
	if _, err := netip.ParseAddr(c.Host); err == nil {
		return c.Host
	}
	return c.server() + ".hideservers.net"
}

// resolve looks the server up once per client: hide.me balances its names
// fast, and a session has to stay with the address it started on.
func (c *Client) resolve(ctx context.Context) (netip.Addr, error) {
	if c.remote.IsValid() {
		return c.remote, nil
	}
	if a, err := netip.ParseAddr(c.Host); err == nil {
		c.remote = a
		return a, nil
	}
	lookup := c.LookupNetIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	ips, err := lookup(ctx, "ip4", c.fqdn())
	if err != nil {
		return netip.Addr{}, fmt.Errorf("hideme: resolve %s: %w", c.fqdn(), err)
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("hideme: %s has no address", c.fqdn())
	}
	c.remote = ips[0].Unmap()
	return c.remote, nil
}

func (c *Client) httpClient(dial func(ctx context.Context, network, address string) (net.Conn, error)) *http.Client {
	roots, pins := c.roots, c.pins
	if roots == nil {
		roots = x509.NewCertPool()
		roots.AppendCertsFromPEM(rootPEM)
	}
	if pins == nil {
		pins = caPins
	}
	if dial == nil {
		dial = c.dial
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 15 * time.Second}
		dial = d.DialContext
	}
	return &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{
			DialContext:       dial,
			DisableKeepAlives: true,
			ForceAttemptHTTP2: true,
			TLSClientConfig: &tls.Config{
				ServerName: "hideservers.net",
				RootCAs:    roots,
				MinVersion: tls.VersionTLS13,
				VerifyPeerCertificate: func(_ [][]byte, chains [][]*x509.Certificate) error {
					return checkPins(chains, pins)
				},
			},
		},
	}
}

func checkPins(chains [][]*x509.Certificate, pins map[string]string) error {
	for _, chain := range chains {
		for _, cert := range chain {
			if !cert.IsCA {
				continue
			}
			sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			if pins[cert.Subject.CommonName] != base64.StdEncoding.EncodeToString(sum[:]) {
				return fmt.Errorf("hideme: %q is not a CA hide.me pins", cert.Subject.CommonName)
			}
		}
	}
	return nil
}

func (c *Client) post(ctx context.Context, hc *http.Client, url string, body, into any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return ErrUpdateRequired
	default:
		return &StatusError{Method: path.Base(url), Code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || into == nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func (c *Client) restURL(ctx context.Context, method string) (string, error) {
	ip, err := c.resolve(ctx)
	if err != nil {
		return "", err
	}
	return "https://" + netip.AddrPortFrom(ip, restPort).String() + "/" + apiVersion + "/" + method, nil
}

// FetchAccessToken trades the username and password, or an access token
// about to go stale, for a new access token.
func (c *Client) FetchAccessToken(ctx context.Context) error {
	req := struct {
		Host        string `json:"host,omitempty"`
		Domain      string `json:"domain"`
		AccessToken []byte `json:"accessToken,omitempty"`
		Username    string `json:"username,omitempty"`
		Password    string `json:"password,omitempty"`
	}{Host: c.server(), Domain: domain, AccessToken: c.AccessToken}
	if len(c.AccessToken) == 0 {
		if c.Username == "" || c.Password == "" {
			return errors.New("hideme: a username and password are needed")
		}
		req.Username, req.Password = c.Username, c.Password
	}
	url, err := c.restURL(ctx, "accessToken")
	if err != nil {
		return err
	}
	var token string
	if err := c.post(ctx, c.httpClient(nil), url, req, &token); err != nil {
		return err
	}
	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("hideme: access token: %w", err)
	}
	c.AccessToken = b
	return nil
}

type connectResponse struct {
	PublicKey []byte `json:"publicKey"`
	Endpoint  struct {
		IP   string `json:"IP"`
		Port int    `json:"Port"`
	} `json:"endpoint"`
	PresharedKey        []byte        `json:"presharedKey"`
	PersistentKeepalive time.Duration `json:"persistentKeepalive"`
	AllowedIPs          []string      `json:"allowedIps"`
	DNS                 []string      `json:"DNS"`
	Gateway             []string      `json:"gateway"`
	StaleAccessToken    bool          `json:"staleAccessToken"`
	SessionToken        []byte        `json:"sessionToken"`
}

// Connect opens a session with a new key and returns the WireGuard
// configuration for it. It fetches an access token first when there is none.
func (c *Client) Connect(ctx context.Context) (vpn.Config, Session, error) {
	if len(c.AccessToken) == 0 {
		if err := c.FetchAccessToken(ctx); err != nil {
			return vpn.Config{}, Session{}, err
		}
	}
	key, err := vpn.GeneratePrivateKey()
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	pub := key.PublicKey()
	url, err := c.restURL(ctx, "connect")
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	var resp connectResponse
	err = c.post(ctx, c.httpClient(nil), url, struct {
		Host        string `json:"host"`
		Domain      string `json:"domain"`
		AccessToken []byte `json:"accessToken"`
		PublicKey   []byte `json:"publicKey"`
	}{c.server(), domain, c.AccessToken, pub[:]}, &resp)
	if err != nil {
		return vpn.Config{}, Session{}, err
	}
	return resp.config(key)
}

func (r connectResponse) config(key vpn.Key) (vpn.Config, Session, error) {
	cfg := vpn.Config{PrivateKey: key}
	if len(r.PublicKey) != 32 {
		return cfg, Session{}, errors.New("hideme: the server sent no public key")
	}
	copy(cfg.Peer.PublicKey[:], r.PublicKey)
	if len(r.PresharedKey) == 32 {
		copy(cfg.Peer.PresharedKey[:], r.PresharedKey)
	}
	ep, err := netip.ParseAddr(r.Endpoint.IP)
	if err != nil {
		return cfg, Session{}, fmt.Errorf("hideme: endpoint: %w", err)
	}
	ep = ep.Unmap()
	cfg.Peer.Endpoint = net.JoinHostPort(ep.String(), strconv.Itoa(r.Endpoint.Port))
	cfg.Peer.Keepalive = r.PersistentKeepalive
	// The same arithmetic as hide.me's client: a DS-Lite line can be as small
	// as 1452 bytes, less IP, UDP and WireGuard headers.
	cfg.MTU = 1452 - 60
	if ep.Is6() {
		cfg.MTU = 1360 - 80
	}
	for _, s := range r.AllowedIPs {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return cfg, Session{}, fmt.Errorf("hideme: address %q: %w", s, err)
		}
		a = a.Unmap()
		cfg.Addresses = append(cfg.Addresses, netip.PrefixFrom(a, a.BitLen()))
		if a.Is4() {
			cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("0.0.0.0/0"))
		} else {
			cfg.Peer.AllowedIPs = append(cfg.Peer.AllowedIPs, netip.MustParsePrefix("::/0"))
		}
	}
	for _, s := range r.DNS {
		if a, err := netip.ParseAddr(s); err == nil {
			cfg.DNS = append(cfg.DNS, a.Unmap())
		}
	}
	s := Session{Token: r.SessionToken, StaleAccessToken: r.StaleAccessToken}
	for _, g := range r.Gateway {
		if a, err := netip.ParseAddr(g); err == nil {
			s.Gateway = append(s.Gateway, a.Unmap())
		}
	}
	return cfg, s, cfg.Validate()
}

// Disconnect ends a session on the server.
func (c *Client) Disconnect(ctx context.Context, s Session) error {
	url, err := c.restURL(ctx, "disconnect")
	if err != nil {
		return err
	}
	return c.post(ctx, c.httpClient(nil), url, struct {
		Host         string `json:"host"`
		Domain       string `json:"domain"`
		SessionToken []byte `json:"sessionToken"`
	}{c.server(), domain, s.Token}, nil)
}

// EnablePortForwarding has the server open UPnP and NAT-PMP for this session
// on its gateway. It talks to the server inside the tunnel, so dial is the
// tunnel's.
func (c *Client) EnablePortForwarding(ctx context.Context, dial func(ctx context.Context, network, address string) (net.Conn, error)) error {
	var ok bool
	err := c.post(ctx, c.httpClient(dial), "https://"+controlAddr+"/upnp", struct {
		Enabled     bool   `json:"enabled"`
		AccessToken []byte `json:"accessToken"`
	}{true, c.AccessToken}, &ok)
	if err == nil && !ok {
		err = errors.New("hideme: the server did not enable port forwarding")
	}
	return err
}

// StatusError is an answer of hide.me's API other than 200. A 4xx to
// accessToken or connect is a refused login.
type StatusError struct {
	Method string
	Code   int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("hideme: %s answered HTTP %d", e.Method, e.Code)
}
