package httpx

import (
	"net/http"
	"net/url"
	"sync"
)

// Class is a kind of traffic that can be sent through a proxy of the app's
// own, such as the one into its VPN tunnel.
type Class string

// ClassDebrid is the calls to debrid services' APIs. A service ties the links
// it hands out to the address that asked, so they go wherever the downloads
// of those links go.
const ClassDebrid Class = "debrid"

var classProxies sync.Map

// SetClassProxy sends every request of a client of class c through proxy, or,
// with nil, lets them go as the client's own Proxy says. Connections a client
// keeps open go on where they went until they close.
func SetClassProxy(c Class, proxy *url.URL) {
	if proxy == nil {
		classProxies.Delete(c)
		return
	}
	classProxies.Store(c, proxy)
}

func classed(c Class, next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	if c == "" {
		return next
	}
	return func(req *http.Request) (*url.URL, error) {
		if u, ok := classProxies.Load(c); ok {
			return u.(*url.URL), nil
		}
		return next(req)
	}
}
