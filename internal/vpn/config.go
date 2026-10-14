// Package vpn runs a WireGuard tunnel inside the process, on a userspace
// network stack, so it needs no tun device, no NET_ADMIN and no admin rights.
// Only what dials or listens through a Tunnel goes through it; the rest of the
// process stays on the system's network.
package vpn

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
)

// Key is a WireGuard key: a Curve25519 private or public key, or a preshared
// key.
type Key [32]byte

// ParseKey reads a key in WireGuard's base64 form.
func ParseKey(s string) (Key, error) {
	var k Key
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != len(k) {
		return k, errors.New("vpn: a key is 32 bytes of base64")
	}
	copy(k[:], b)
	return k, nil
}

// GeneratePrivateKey makes a new private key, clamped as Curve25519 wants.
func GeneratePrivateKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return k, nil
}

// PublicKey is the public key of a private key.
func (k Key) PublicKey() Key {
	var pub Key
	curve25519.ScalarBaseMult((*[32]byte)(&pub), (*[32]byte)(&k))
	return pub
}

func (k Key) String() string { return base64.StdEncoding.EncodeToString(k[:]) }

// IsZero reports whether k is unset.
func (k Key) IsZero() bool { return k == Key{} }

func (k Key) hex() string { return hex.EncodeToString(k[:]) }

// Config is one WireGuard connection: this side's interface and the one peer
// a VPN provider gives.
type Config struct {
	PrivateKey Key
	// Addresses are this side's addresses inside the tunnel.
	Addresses []netip.Prefix
	// DNS are the name servers inside the tunnel.
	DNS []netip.Addr
	// MTU is the tunnel's MTU, 0 for DefaultMTU.
	MTU  int
	Peer Peer
}

// Peer is the server end of a Config.
type Peer struct {
	PublicKey    Key
	PresharedKey Key
	// Endpoint is host:port, where host may be a name.
	Endpoint   string
	AllowedIPs []netip.Prefix
	// Keepalive is the persistent keepalive interval, 0 for DefaultKeepalive.
	Keepalive time.Duration
}

// DefaultMTU leaves room for WireGuard over IPv6 on a 1500 byte link.
const DefaultMTU = 1420

// DefaultKeepalive keeps the handshake fresh, which is how a Tunnel tells it
// is up, and keeps a NAT on the way open.
const DefaultKeepalive = 25 * time.Second

// ParseConfig reads a configuration file in wg-quick's format, the one VPN
// providers hand out. Keys wg-quick uses to change the system, such as
// PostUp or Table, are ignored, and so is a ListenPort: the tunnel sends from
// whatever port the system gives it.
func ParseConfig(text string) (Config, error) {
	var c Config
	section := ""
	peers := 0
	sc := bufio.NewScanner(strings.NewReader(text))
	for line := 1; sc.Scan(); line++ {
		s := sc.Text()
		if i := strings.IndexAny(s, "#;"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			section = strings.ToLower(strings.TrimSpace(s[1 : len(s)-1]))
			if section == "peer" {
				peers++
			}
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return c, fmt.Errorf("vpn: line %d is not key = value", line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		var err error
		switch section {
		case "interface":
			err = c.setInterface(key, value)
		case "peer":
			if peers > 1 {
				return c, errors.New("vpn: only one [Peer] is supported")
			}
			err = c.Peer.set(key, value)
		default:
			err = fmt.Errorf("%s outside a section", key)
		}
		if err != nil {
			return c, fmt.Errorf("vpn: line %d: %w", line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (c *Config) setInterface(key, value string) error {
	var err error
	switch key {
	case "privatekey":
		c.PrivateKey, err = ParseKey(value)
	case "address":
		for _, s := range list(value) {
			p, perr := parsePrefix(s)
			if perr != nil {
				return perr
			}
			c.Addresses = append(c.Addresses, p)
		}
	case "dns":
		for _, s := range list(value) {
			// wg-quick takes search domains here too; they mean nothing to the tunnel.
			if a, perr := netip.ParseAddr(s); perr == nil {
				c.DNS = append(c.DNS, a)
			}
		}
	case "mtu":
		c.MTU, err = strconv.Atoi(value)
	}
	return err
}

func (p *Peer) set(key, value string) error {
	var err error
	switch key {
	case "publickey":
		p.PublicKey, err = ParseKey(value)
	case "presharedkey":
		p.PresharedKey, err = ParseKey(value)
	case "endpoint":
		p.Endpoint = value
	case "allowedips":
		for _, s := range list(value) {
			pr, perr := parsePrefix(s)
			if perr != nil {
				return perr
			}
			p.AllowedIPs = append(p.AllowedIPs, pr)
		}
	case "persistentkeepalive":
		if value == "off" {
			return nil
		}
		var n int
		n, err = strconv.Atoi(value)
		p.Keepalive = time.Duration(n) * time.Second
	}
	return err
}

// Validate reports what a Config lacks to connect.
func (c Config) Validate() error {
	switch {
	case c.PrivateKey.IsZero():
		return errors.New("vpn: the interface has no private key")
	case len(c.Addresses) == 0:
		return errors.New("vpn: the interface has no address")
	case c.Peer.PublicKey.IsZero():
		return errors.New("vpn: the peer has no public key")
	case c.Peer.Endpoint == "":
		return errors.New("vpn: the peer has no endpoint")
	case len(c.Peer.AllowedIPs) == 0:
		return errors.New("vpn: the peer allows no addresses")
	case c.MTU != 0 && (c.MTU < 576 || c.MTU > 65535):
		return fmt.Errorf("vpn: MTU %d is out of range", c.MTU)
	}
	if _, _, err := splitEndpoint(c.Peer.Endpoint); err != nil {
		return err
	}
	return nil
}

func (c Config) mtu() int {
	if c.MTU == 0 {
		return DefaultMTU
	}
	return c.MTU
}

func (c Config) keepalive() time.Duration {
	if c.Peer.Keepalive <= 0 {
		return DefaultKeepalive
	}
	return c.Peer.Keepalive
}

func (c Config) addrs() []netip.Addr {
	out := make([]netip.Addr, len(c.Addresses))
	for i, p := range c.Addresses {
		out[i] = p.Addr()
	}
	return out
}

// uapi is the configuration in the form wireguard-go's IpcSet takes, with the
// endpoint already resolved.
func (c Config) uapi(endpoint netip.AddrPort) string {
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\nreplace_peers=true\n", c.PrivateKey.hex())
	fmt.Fprintf(&b, "public_key=%s\n", c.Peer.PublicKey.hex())
	if !c.Peer.PresharedKey.IsZero() {
		fmt.Fprintf(&b, "preshared_key=%s\n", c.Peer.PresharedKey.hex())
	}
	fmt.Fprintf(&b, "endpoint=%s\n", endpoint)
	fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", int(c.keepalive()/time.Second))
	b.WriteString("replace_allowed_ips=true\n")
	for _, p := range c.Peer.AllowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", p.Masked())
	}
	return b.String()
}

func list(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// parsePrefix takes an address with or without a prefix length; a bare
// address is a single host.
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func splitEndpoint(ep string) (string, uint16, error) {
	host, port, err := net.SplitHostPort(ep)
	if err != nil {
		return "", 0, fmt.Errorf("vpn: endpoint %q: %w", ep, err)
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", 0, fmt.Errorf("vpn: endpoint %q has no valid port", ep)
	}
	return host, uint16(n), nil
}
