package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// Class is a kind of traffic that can be sent through a proxy of the app's
// own, such as the one into its VPN tunnel.
type Class string

const (
	// ClassDebrid is the calls to debrid services' APIs. A service ties the
	// links it hands out to the address that asked, so they go wherever the
	// downloads of those links go.
	ClassDebrid Class = "debrid"
	// ClassHosters is what the app asks of the hosts behind the links it
	// downloads from: checking a link, following its redirects, reading the
	// page it is on.
	ClassHosters Class = "hosters"
	// ClassYtdlp is what the app asks of a media site before yt-dlp is run.
	ClassYtdlp Class = "ytdlp"
	// ClassUsenet is the calls to the services that fetch an NZB for the app.
	ClassUsenet Class = "usenet"
)

// errClassMoved is a connection dialled past the proxy its class was moved to
// while it was being dialled.
var errClassMoved = errors.New("httpx: the connection's traffic was moved to a proxy while it was dialled")

var (
	classMu      sync.Mutex
	classProxies = map[Class]*url.URL{}
	classConns   = map[Class]map[*classConn]struct{}{}
)

// SetClassProxy sends every request of a client of class c through proxy, or,
// with nil, lets them go as the client's own Proxy says. A connection of the
// class that does not lead to proxy is hung up, so while a class has a proxy
// none of its traffic goes anywhere else.
func SetClassProxy(c Class, proxy *url.URL) {
	classMu.Lock()
	var stray []*classConn
	if proxy == nil {
		delete(classProxies, c)
	} else {
		classProxies[c] = proxy
		for cc := range classConns[c] {
			if cc.addr != proxyAddr(proxy) {
				stray = append(stray, cc)
			}
		}
	}
	classMu.Unlock()
	for _, cc := range stray {
		cc.Close()
	}
}

func classed(c Class, next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	if c == "" {
		return next
	}
	return func(req *http.Request) (*url.URL, error) {
		classMu.Lock()
		u := classProxies[c]
		classMu.Unlock()
		if u != nil {
			return u, nil
		}
		return next(req)
	}
}

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// classDial keeps every connection of class c on record while it is open, and
// drops one that a proxy set during its dial does not allow.
func classDial(c Class, dial dialFunc) dialFunc {
	if c == "" {
		return dial
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		cc := &classConn{Conn: conn, class: c, addr: addr}
		classMu.Lock()
		if u := classProxies[c]; u != nil && proxyAddr(u) != addr {
			classMu.Unlock()
			conn.Close()
			return nil, errClassMoved
		}
		if classConns[c] == nil {
			classConns[c] = map[*classConn]struct{}{}
		}
		classConns[c][cc] = struct{}{}
		classMu.Unlock()
		return cc, nil
	}
}

type classConn struct {
	net.Conn
	class Class
	addr  string
	once  sync.Once
}

func (c *classConn) Close() error {
	c.once.Do(func() {
		classMu.Lock()
		delete(classConns[c.class], c)
		classMu.Unlock()
	})
	return c.Conn.Close()
}
