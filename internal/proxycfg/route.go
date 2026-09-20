package proxycfg

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Route is one download's outbound connection, reduced to the fields a backend
// can act on. Unlike an Entry, which may be inert, half-typed or of a kind no
// backend can drive, a Route has been checked, so a backend can use it as is.
//
// An empty Scheme is the direct gateway, the machine's own connection.
type Route struct {
	// ID is the connection this route came from, so the task can record which
	// connection carried it.
	ID string
	// Scheme is http, https or socks5, or empty for the direct gateway. socks4
	// and socks4a never appear (see Entry.Route).
	Scheme string
	// Host is host:port, with an IPv6 literal already bracketed.
	Host string
	// Username and Password are the proxy's own credentials.
	Username string
	Password string
}

// Proxied reports whether this route goes through a proxy at all.
func (r Route) Proxied() bool { return r.Scheme != "" }

// ErrOwnDialer is Route refusing a connection no proxy URL can express. The
// caller must settle the download rather than resend it directly, which would
// bypass the proxy the user configured.
var ErrOwnDialer = errors.New("proxycfg: this connection needs a dialer of its own and cannot be given to a proxy URL")

// Route reduces e to what a backend can act on, or says why it cannot.
//
// socks4 and socks4a are refused: every consumer of a proxy URL here ends in
// http.ProxyURL, which does not speak SOCKS4 and would fail every request
// later, looking like a hoster problem. Probe speaks SOCKS4 itself, so such
// rows can still be tested.
func (e Entry) Route() (Route, error) {
	k, ok := kindOf(e.Kind)
	if !ok {
		return Route{}, fmt.Errorf("proxycfg: %q is not a connection type", string(e.Kind))
	}
	e.Kind = k
	switch {
	case k == KindNone:
		return Route{}, errors.New("proxycfg: this row names no connection")
	case k == KindDirect:
		return Route{ID: e.ID}, nil
	case e.NeedsOwnDialer():
		return Route{}, fmt.Errorf("%w: %s", ErrOwnDialer, k)
	}
	// Checked again as the last step before the address is used, so a row
	// without a host is not handed over as ":0".
	if err := Validate(e); err != nil {
		return Route{}, err
	}
	return Route{
		ID:     e.ID,
		Scheme: e.scheme(),
		// JoinHostPort brackets an IPv6 literal, as in Entry.URL.
		Host:     net.JoinHostPort(normalizeHost(e.Host), strconv.Itoa(e.Port)),
		Username: e.Username,
		Password: e.Password,
	}, nil
}

// String describes the route for a log line, without the password.
func (r Route) String() string {
	if !r.Proxied() {
		return "direct"
	}
	if r.Username != "" {
		return r.Scheme + "://" + r.Username + "@" + r.Host
	}
	return r.Scheme + "://" + r.Host
}
