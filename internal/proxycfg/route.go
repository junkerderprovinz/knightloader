package proxycfg

// A Route is what a download backend is handed when a task has been given a
// connection to travel on. It exists so that the one rule a backend must not get
// wrong - see Route's own comment on socks4 - is enforced once, here, instead of
// being re-derived by every backend that grows per-download routing.

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Route is one download's outbound connection, reduced to the four fields a
// backend can act on.
//
// It is a separate type from Entry rather than the Entry itself, and that is the
// whole point of the file. An Entry is a row in a user's list: it can be inert,
// half-typed, switched off, or of a kind no backend can drive. A Route is the
// same connection AFTER those questions have been answered, so a backend holding
// one may use it without asking anything further.
//
// An empty Scheme is the direct gateway: go out over the machine's own
// connection. It is not "no route" - the difference is recorded on the task, not
// here, and a backend treats the two the same way.
type Route struct {
	// ID is the connection this route came from, so a caller can record on the
	// task which connection actually carried it.
	ID string
	// Scheme is http, https or socks5, or empty for the direct gateway. socks4
	// and socks4a never appear: see Entry.Route.
	Scheme string
	// Host is host:port, with an IPv6 literal already bracketed.
	Host string
	// Username and Password are the proxy's own credentials.
	Username string
	Password string
}

// Proxied reports whether this route goes through a proxy at all.
func (r Route) Proxied() bool { return r.Scheme != "" }

// ErrOwnDialer is Route refusing a connection no proxy URL can express.
//
// It is a named error and not a sentence because it is the one refusal a caller
// has to handle rather than log: the download must be settled, never quietly
// re-sent. A task routed to a socks4 proxy that fell back to an ordinary
// download would go out over the very connection the user configured the proxy
// to avoid, and nothing on screen would say so.
var ErrOwnDialer = errors.New("proxycfg: this connection needs a dialer of its own and cannot be given to a proxy URL")

// Route reduces e to what a backend can act on, or says why it cannot.
//
// socks4 and socks4a are refused outright. Everything that consumes a proxy URL
// in this build - net/http's Transport.Proxy and the download engine's
// per-request proxy alike - ends in http.ProxyURL, which speaks http, https and
// socks5 and has never spoken socks4. Handing it one does not fail loudly at
// setup; it fails on every request afterwards, at which point the failure looks
// like the hoster. Entry.NeedsOwnDialer is the flag, and this is where it is
// honoured. Probe speaks socks4 itself, so those rows can still be tested - they
// simply cannot carry a download until something grows a dialer for them.
func (e Entry) Route() (Route, error) {
	k, ok := kindOf(e.Kind)
	if !ok {
		return Route{}, fmt.Errorf("proxycfg: %q is not a connection type", string(e.Kind))
	}
	e.Kind = k
	switch {
	case k == KindNone:
		// An inert row is not a connection, and a caller that got this far is
		// asking the wrong entry rather than holding a broken one.
		return Route{}, errors.New("proxycfg: this row names no connection")
	case k == KindDirect:
		return Route{ID: e.ID}, nil
	case e.NeedsOwnDialer():
		return Route{}, fmt.Errorf("%w: %s", ErrOwnDialer, k)
	}
	// Checked here as well as at save time, because this is the last point before
	// the address is used: a row that reached a backend without a host would
	// otherwise be handed over as ":0", which no proxy answers and every failure
	// afterwards blames on the download.
	if err := Validate(e); err != nil {
		return Route{}, err
	}
	return Route{
		ID:     e.ID,
		Scheme: e.scheme(),
		// JoinHostPort brackets an IPv6 literal. Without it the address's own
		// colons read as a port separator and the proxy comes out as a different
		// machine - the same reason URL joins it this way.
		Host:     net.JoinHostPort(normalizeHost(e.Host), strconv.Itoa(e.Port)),
		Username: e.Username,
		Password: e.Password,
	}, nil
}

// String describes the route for a log line. The password is never assembled
// into it, for the same reason Entry.String leaves it out: this value ends up
// behind %v in more places than anyone tracks.
func (r Route) String() string {
	if !r.Proxied() {
		return "direct"
	}
	if r.Username != "" {
		return r.Scheme + "://" + r.Username + "@" + r.Host
	}
	return r.Scheme + "://" + r.Host
}
