package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"syscall"
)

// Scope is how far inside the network an address sits, from the open internet
// inwards.
type Scope int

const (
	ScopePublic Scope = iota
	// ScopeLocal is this machine and the LAN: loopback, the private ranges and
	// carrier-grade NAT.
	ScopeLocal
	// ScopeInternal is what no fetched page has any business reaching: link-local
	// addresses, where cloud metadata services answer, and multicast.
	ScopeInternal
)

// ErrRefused is a connection a confined request was not allowed to make.
var ErrRefused = errors.New("httpx: the address is further inside the network than the one that was entered")

var (
	carrierNAT = netip.MustParsePrefix("100.64.0.0/10")
	// metadataAddrs are cloud metadata endpoints outside the link-local ranges.
	metadataAddrs = []netip.Addr{
		netip.MustParseAddr("fd00:ec2::254"),
		netip.MustParseAddr("100.100.100.200"),
	}
)

// ScopeOf places one address. An address it cannot read counts as internal.
func ScopeOf(a netip.Addr) Scope {
	a = a.Unmap()
	switch {
	case !a.IsValid(), a.IsLinkLocalUnicast(), a.IsMulticast(), a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast():
		return ScopeInternal
	}
	for _, m := range metadataAddrs {
		if a.WithZone("") == m {
			return ScopeInternal
		}
	}
	// 0.0.0.0 connects to this machine on Linux.
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || carrierNAT.Contains(a) {
		return ScopeLocal
	}
	return ScopePublic
}

// ScopeOfHost resolves host and returns the innermost scope among its
// addresses. A name that does not resolve here counts as public: the request
// would fail on it anyway, or it goes through a proxy that resolves it.
func ScopeOfHost(ctx context.Context, host string) Scope {
	if a, err := netip.ParseAddr(host); err == nil {
		return ScopeOf(a)
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return ScopePublic
	}
	s := ScopePublic
	for _, a := range addrs {
		s = max(s, ScopeOf(a))
	}
	return s
}

type confineKey struct{}

// Confine returns a context under which a client from this package connects to
// no address further inside the network than limit, and never widens a limit
// ctx already has. A caller following addresses a page chose passes the scope
// of the address the user entered, so a LAN page can be crawled on purpose but
// a public one cannot steer a request into the LAN.
//
// The check runs at dial time, after the name is resolved, so a DNS answer
// that changes after a check cannot get past it. A proxied request is checked
// by resolving its host when it is handed to the proxy.
func Confine(ctx context.Context, limit Scope) context.Context {
	if outer, ok := confinement(ctx); ok && outer < limit {
		limit = outer
	}
	return context.WithValue(ctx, confineKey{}, limit)
}

func confinement(ctx context.Context) (Scope, bool) {
	limit, ok := ctx.Value(confineKey{}).(Scope)
	return limit, ok
}

// checkAddr enforces a confinement on one resolved address.
func checkAddr(ctx context.Context, address string) error {
	limit, ok := confinement(ctx)
	if !ok {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if ScopeOf(ap.Addr()) > limit {
		return fmt.Errorf("%w (%s)", ErrRefused, ap.Addr())
	}
	return nil
}

// proxies is what a transport has been told to proxy through. The connection
// to the proxy is the operator's choice, so it is not held to a request's
// confinement.
type proxies struct{ addrs sync.Map }

func (p *proxies) proxy(next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		u, err := next(req)
		if err != nil || u == nil {
			return u, err
		}
		if limit, ok := confinement(req.Context()); ok {
			if ScopeOfHost(req.Context(), req.URL.Hostname()) > limit {
				return nil, fmt.Errorf("%w (%s)", ErrRefused, req.URL.Hostname())
			}
		}
		p.addrs.Store(proxyAddr(u), true)
		return u, nil
	}
}

// proxyAddr is the address the transport dials for a proxy.
func proxyAddr(u *url.URL) string {
	if port := u.Port(); port != "" {
		return net.JoinHostPort(u.Hostname(), port)
	}
	port := "80"
	switch u.Scheme {
	case "https":
		port = "443"
	case "socks5", "socks5h":
		port = "1080"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// dial checks each address a confined request connects to, and lets a
// connection to a proxy through.
func (p *proxies) dial(d *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	checked := *d
	checked.ControlContext = func(ctx context.Context, _, address string, _ syscall.RawConn) error {
		return checkAddr(ctx, address)
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if _, ok := p.addrs.Load(addr); ok {
			return d.DialContext(ctx, network, addr)
		}
		return checked.DialContext(ctx, network, addr)
	}
}
