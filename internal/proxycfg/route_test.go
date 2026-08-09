package proxycfg

import (
	"errors"
	"strings"
	"testing"
)

// TestRouteRefusesSocks4 is the one refusal in this file that is not tidiness.
// Everything that consumes a proxy URL in this build ends in http.ProxyURL,
// which has never spoken socks4; handing it one fails every request afterwards,
// and the failure looks like the hoster rather than like the configuration.
func TestRouteRefusesSocks4(t *testing.T) {
	for _, k := range []Kind{KindSOCKS4, KindSOCKS4A} {
		e := Entry{ID: "a", Kind: k, Host: "proxy.lan", Port: 1080, Enabled: true}
		got, err := e.Route()
		if !errors.Is(err, ErrOwnDialer) {
			t.Fatalf("%s: Route err = %v, want ErrOwnDialer", k, err)
		}
		if got.Proxied() {
			t.Fatalf("%s: Route returned a usable route %v alongside its refusal", k, got)
		}
		if !e.NeedsOwnDialer() {
			t.Fatalf("%s: NeedsOwnDialer disagrees with Route, so one of the two is now wrong", k)
		}
	}
}

// TestRouteForTheDirectGateway. The gateway is a real choice and has to produce
// a real route - one that names no proxy, which is how a backend is told to go
// out over the machine's own connection.
func TestRouteForTheDirectGateway(t *testing.T) {
	got, err := Direct().Route()
	if err != nil {
		t.Fatalf("the direct gateway has no route: %v", err)
	}
	if got.Proxied() || got.Host != "" || got.Scheme != "" {
		t.Fatalf("Route = %+v, want a route through nothing", got)
	}
	if got.ID != DirectID {
		t.Fatalf("Route.ID = %q, want the gateway id so the task can record what carried it", got.ID)
	}
}

// TestRouteCarriesTheCredentials, because a proxy that needs a password and is
// handed none refuses with a 407 that reads as an auth failure at the hoster.
func TestRouteCarriesTheCredentials(t *testing.T) {
	e := Entry{ID: "7", Kind: KindSOCKS5, Host: "proxy.lan", Port: 1080, Username: "alice", Password: "s3cret", Enabled: true}
	got, err := e.Route()
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := Route{ID: "7", Scheme: "socks5", Host: "proxy.lan:1080", Username: "alice", Password: "s3cret"}
	if got != want {
		t.Fatalf("Route = %+v, want %+v", got, want)
	}
}

// TestRouteBracketsAnIPv6Literal. Left unbracketed, the address's own colons
// read as a port separator and the proxy comes out as a different machine.
func TestRouteBracketsAnIPv6Literal(t *testing.T) {
	e := Entry{ID: "1", Kind: KindHTTP, Host: "2001:db8::1", Port: 8080, Enabled: true}
	got, err := e.Route()
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got.Host != "[2001:db8::1]:8080" {
		t.Fatalf("Route.Host = %q, want the literal bracketed", got.Host)
	}
}

// TestRouteRefusesWhatCannotBeUsed. An inert row is not a connection, and a row
// with no host would be handed over as ":0" - which no proxy answers and every
// failure afterwards blames on the download.
func TestRouteRefusesWhatCannotBeUsed(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
	}{
		{"an inert row", Entry{ID: "a", Kind: KindNone, Enabled: true}},
		{"no host", Entry{ID: "a", Kind: KindHTTP, Port: 8080, Enabled: true}},
		{"no port", Entry{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Enabled: true}},
		{"a type nothing knows", Entry{ID: "a", Kind: Kind("quic"), Host: "proxy.lan", Port: 8080, Enabled: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := c.in.Route(); err == nil {
				t.Fatal("Route accepted a row that cannot carry a download")
			}
		})
	}
}

// TestRouteStringKeepsThePasswordOut. This value ends up behind %v in more
// places than anyone tracks, which is the same reason Entry.String leaves it out.
func TestRouteStringKeepsThePasswordOut(t *testing.T) {
	e := Entry{ID: "1", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Username: "alice", Password: "s3cret", Enabled: true}
	r, err := e.Route()
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if s := r.String(); strings.Contains(s, "s3cret") {
		t.Fatalf("Route.String() = %q and it spells out the password", s)
	}
}
