package api

// Every endpoint is declared in the registration table rather than attached to
// a mux by hand, so each subsystem can add its routes from its own file, the
// self-describing index cannot drift from what is reachable, and the open
// routes are data rather than a second list of paths. A test fails when
// anything outside this file calls mux.HandleFunc.

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// registerAll fills the table with every subsystem's routes. Handler and the
// tests both build their registry from it, so a subsystem cannot be reachable
// in tests and missing from the running server.
func registerAll(reg *Registry, a *app.App) {
	registerSystem(reg, a)
	registerTwoFactor(reg, a)
	registerPasskeys(reg, a)
	registerTasks(reg, a)
	registerExtract(reg, a)
	registerBulk(reg, a)
	registerQueue(reg, a)
	registerLinks(reg, a)
	registerContainers(reg, a)
	registerSettings(reg, a)
	registerSettingsTransfer(reg, a)
	registerAccounts(reg, a)
	registerHosterAuth(reg, a)
	registerHosterIcons(reg, a)
	registerResolvers(reg, a)
	registerMediaTools(reg, a)
	registerSchedule(reg, a)
	registerReconnect(reg, a)
	registerPortmap(reg, a)
	registerUIState(reg, a)
	registerHistory(reg, a)
	registerStats(reg, a)
	registerSpeedHistory(reg, a)
	registerFederation(reg, a)
	registerRelay(reg, a)
	registerConnect(reg, a)
	registerDiscovery(reg, a)
	registerFeatures(reg, a)
	registerConnections(reg, a)
	registerRules(reg, a)
	registerFolders(reg, a)
	registerDiskSpace(reg, a)
	registerCaptcha(reg, a)
	registerCaptchaSkip(reg, a)
	registerCaptchaWidget(reg, a)
	registerDiagnostics(reg, a)
	registerDBMaintenance(reg, a)
	registerDiagnosticsLog(reg, a)
	registerSelfTest(reg, a)
	registerFileOwner(reg, a)
	registerHealth(reg, a)
	registerFiles(reg, a)
	registerLifecycle(reg, a)
	registerBackup(reg, a)
	registerIdleAction(reg, a)
	registerBrowserTools(reg, a)
	registerTokens(reg, a)
	registerRemoteAccess(reg, a)
	registerScripts(reg, a)
	registerHelp(reg, a)
	registerTorrents(reg, a)
	registerDownloadClient(reg, a)
	registerActivity(reg, a)
	registerHostHeaders(reg, a)
	registerYtdlpCookies(reg, a)
	registerFeeds(reg, a)
	registerEventTargets(reg, a)
	registerMediaHooks(reg, a)
}

// AnyMethod is the method of a route that forwards the request elsewhere and
// passes the method along. A route that acts on this instance names its
// method, so a GET can never be made to do a POST's job.
const AnyMethod = ""

// Route is one endpoint as registered: enough to attach it, and enough to
// describe it to somebody who has never seen the source.
type Route struct {
	// Method is the HTTP method as net/http's pattern syntax wants it, or
	// AnyMethod.
	Method string `json:"method"`
	// Path is the pattern, wildcards included ("/api/tasks/{id}").
	Path string `json:"path"`
	// Summary is the one line the index shows.
	Summary string `json:"summary"`
	// Open is a route reachable without a session on a password-protected
	// instance. The reason for it belongs in the summary.
	Open bool `json:"open"`

	handler http.HandlerFunc
}

// Registry collects the routes as each subsystem registers them. It also holds
// what one subsystem's routes call in another, so two handlers in one process
// never call each other's.
type Registry struct {
	routes []Route
	// seen catches a duplicate in any test that builds a handler, instead of
	// ServeMux panicking at startup in production.
	seen map[string]bool

	// refreshDiscovery re-reads what this instance announces on the network,
	// for a settings save that renames it. registerDiscovery sets it; until
	// then, and on a build without discovery, it does nothing.
	refreshDiscovery func()

	openOnce   sync.Once
	openExact  map[string]bool
	openPrefix []string
}

func newRegistry() *Registry {
	return &Registry{seen: map[string]bool{}, refreshDiscovery: func() {}}
}

// Add registers a route that needs a session once a password is set.
func (reg *Registry) Add(method, path, summary string, h http.HandlerFunc) {
	reg.add(Route{Method: method, Path: path, Summary: summary, handler: h})
}

// AddOpen registers a route reachable without a session. It is for routes
// that hand out a session in the first place or carry their own credential in
// the request, and the summary says which.
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

// Routes returns the table sorted by path then method, without the handlers.
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

// attach wires the table onto a mux, with fallback serving everything the
// table does not claim (the single-page app).
func (reg *Registry) attach(mux *http.ServeMux, fallback http.Handler) {
	for _, r := range reg.routes {
		pattern := r.Path
		if r.Method != AnyMethod {
			pattern = r.Method + " " + r.Path
		}
		mux.HandleFunc(pattern, r.handler)
	}
	// An unknown /api/ path is a 404 rather than the SPA's index.html with a
	// 200, which a client would fail to parse without learning the route or the
	// status. ServeMux prefers the more specific pattern, and a path registered
	// under another method still gets its 405.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	})
	mux.Handle("/", fallback)
}

// open reports whether a request path may be served without a session.
//
// Everything outside /api/ is the interface, which has to render the login
// screen. Inside /api/ a wildcard route matches on the prefix before its first
// wildcard, since a wildcard route is only open when the wildcard is itself
// the credential. The sets are built once because this runs on every request.
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
