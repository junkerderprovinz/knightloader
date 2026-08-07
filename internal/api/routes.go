package api

// The registration table. Every endpoint in the app is declared here rather
// than attached to a mux by hand, for three reasons that all cost more later
// than they do now:
//
//   - a package can add its routes from its own file, so a wave that builds a
//     subsystem does not queue behind every other wave for the right to edit one
//     router function;
//   - the self-describing index the API serves is generated from this table, so
//     it cannot drift from what is actually reachable — a hand-registered route
//     is one the index will never mention, and there is no way to notice;
//   - which routes answer without a session is data rather than a switch
//     statement listing paths a second time, next to the first list, drifting.
//
// Nothing outside this file may call mux.HandleFunc; there is a test that fails
// when something does.

import (
	"net/http"
	"sort"
	"strings"
	"sync"
)

// AnyMethod is the method of a route that answers whatever it is sent, because
// it forwards the request somewhere else and the method is part of what it
// forwards. It is not a way to avoid deciding: a route that acts on this
// instance names its method, so that a GET can never be made to do a POST's job.
const AnyMethod = ""

// Route is one endpoint as registered: enough to attach it, and enough to
// describe it to somebody who has never seen the source.
type Route struct {
	// Method is the HTTP method, exactly as net/http's pattern syntax wants it,
	// or AnyMethod.
	Method string `json:"method"`
	// Path is the pattern, wildcards included ("/api/tasks/{id}").
	Path string `json:"path"`
	// Summary is one line saying what the route does, in the language the rest of
	// the app is written in. It is what the index shows.
	Summary string `json:"summary"`
	// Open is a route reachable without a session on a password-protected
	// instance. It is false unless there is a reason, and the reason belongs in
	// the summary: these are the only doors in the building that are not locked.
	Open bool `json:"open"`

	handler http.HandlerFunc
}

// Registry collects the routes as each subsystem registers them.
type Registry struct {
	routes []Route
	// seen catches the same method and path being registered twice. ServeMux
	// panics on a duplicate pattern, which is a clear enough failure, but it
	// happens at startup in production and names nothing but the pattern; this
	// fails in the test that builds a handler, which every route file has.
	seen map[string]bool

	// The session guard's view of the table, derived once.
	openOnce   sync.Once
	openExact  map[string]bool
	openPrefix []string
}

func newRegistry() *Registry {
	return &Registry{seen: map[string]bool{}}
}

// Add registers a route that needs a session once a password is set, which is
// everything except the handful the login flow itself depends on.
func (reg *Registry) Add(method, path, summary string, h http.HandlerFunc) {
	reg.add(Route{Method: method, Path: path, Summary: summary, handler: h})
}

// AddOpen registers a route reachable without a session. Use it only for a route
// that is either how somebody gets a session in the first place, or one whose
// own credential is in the request — and say which in the summary.
func (reg *Registry) AddOpen(method, path, summary string, h http.HandlerFunc) {
	reg.add(Route{Method: method, Path: path, Summary: summary, Open: true, handler: h})
}

func (reg *Registry) add(r Route) {
	key := r.Method + " " + r.Path
	if reg.seen[key] {
		panic("api: " + key + " is registered twice")
	}
	reg.seen[key] = true
	reg.routes = append(reg.routes, r)
}

// Routes is the table, sorted by path then method, without the handlers. It is
// what the index is generated from and what a test can assert against.
func (reg *Registry) Routes() []Route {
	out := make([]Route, len(reg.routes))
	copy(out, reg.routes)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	for i := range out {
		out[i].handler = nil
	}
	return out
}

// attach wires the table onto a mux, with fallback serving everything the table
// does not claim (the single-page app). This is the only place in the package
// that touches a ServeMux.
func (reg *Registry) attach(mux *http.ServeMux, fallback http.Handler) {
	for _, r := range reg.routes {
		pattern := r.Path
		if r.Method != AnyMethod {
			pattern = r.Method + " " + r.Path
		}
		mux.HandleFunc(pattern, r.handler)
	}
	mux.Handle("/", fallback)
}

// open reports whether a request path may be served without a session.
//
// Everything outside /api/ is open because it is the interface itself, which has
// to render the login screen. Inside /api/ only what the table marked open is,
// matched on the literal prefix before the first wildcard: a wildcard route can
// only be open when the wildcard is itself the credential, which is the one case
// this is used for.
//
// The two sets are worked out on first use and kept, because this runs on every
// request the server answers — including every asset the interface loads.
func (reg *Registry) open(path string) bool {
	if !strings.HasPrefix(path, "/api/") {
		return true
	}
	reg.openOnce.Do(reg.buildOpen)
	if reg.openExact[path] {
		return true
	}
	for _, prefix := range reg.openPrefix {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (reg *Registry) buildOpen() {
	reg.openExact = map[string]bool{}
	for _, r := range reg.routes {
		if !r.Open {
			continue
		}
		if i := strings.Index(r.Path, "{"); i >= 0 {
			reg.openPrefix = append(reg.openPrefix, r.Path[:i])
			continue
		}
		reg.openExact[r.Path] = true
	}
}
