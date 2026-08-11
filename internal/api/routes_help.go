package api

// GET /api/help: a self-describing index of the whole REST and WebSocket
// surface, generated from the registration table in routes.go rather than
// hand-maintained (see that file's own comment on why nothing may attach a
// route outside it). Retrofitting this after eleven waves of hand-registered
// routes would mean auditing roughly two hundred of them by hand; built from
// the table itself, this index can never drift from what is actually
// reachable, because it is not a second description of the table, it is a
// read of the table.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// HelpIndex is what GET /api/help answers with.
type HelpIndex struct {
	Version    string `json:"version"`
	Deployment string `json:"deployment"`
	// About is one paragraph of orientation for somebody who has never seen
	// this API before: how it is guarded, and what "open" below means.
	About string `json:"about"`
	// Vocabulary is why every route name here is KnightLoader's own, never
	// JDownloader's Deprecated API or My.JDownloader's remote namespaces.
	// This is the field section 8's Wave 11 amendment asks for by name, so a
	// future contributor reads a decision here instead of an absence and
	// does not go half-build a compatibility shim that buys nothing.
	Vocabulary string `json:"vocabulary"`
	// RemoteAccess is why there is no hosted relay and no pairing route, and
	// points at what this build offers instead: GET /api/remote-access and
	// POST /api/tokens.
	RemoteAccess string `json:"remoteAccess"`
	// Routes is the full table, sorted by path then method, the same slice
	// TestOnlyTheseRoutesAreOpen and TestEveryRouteDescribesItself already
	// hold every route in this build to.
	Routes []Route `json:"routes"`
}

const helpAbout = "KnightLoader's REST and WebSocket API, generated from this build's own " +
	"route table so this page can never mention a route that is not actually reachable. " +
	"Everything under /api/ needs a session (a login cookie from POST /api/auth/login) or a " +
	"Bearer token (POST /api/tokens) once a password is set, except the handful of routes " +
	"marked \"open\" below: those exist only to get you a session in the first place, or carry " +
	"their own credential in the request itself. With no password set, every route answers " +
	"unauthenticated, which is the default a fresh install starts in."

const helpVocabulary = "KnightLoader does not implement JDownloader's Deprecated API or " +
	"My.JDownloader's remote-API namespaces, and does not reuse their method names. This is a " +
	"considered decision, not a gap. My.JDownloader's official clients, the mobile apps, the " +
	"browser extension and the my.jdownloader.org dashboard, all speak to AppWork's own vendor " +
	"relay first and only reach a JDownloader instance through it; none of them can be pointed " +
	"at a plain HTTP server instead of that relay. A local server answering to the same method " +
	"names would therefore still not be usable by any of those clients, so building a " +
	"compatible shim would reproduce JDownloader's vocabulary for no real interoperability, " +
	"and would only leave something for a future contributor to half-finish. KnightLoader " +
	"publishes its own vocabulary instead, documented in the routes below, meant to be called " +
	"directly rather than through anyone's relay."

const helpRemoteAccess = "There is no hosted relay and no pairing step. Reaching this " +
	"instance from outside your own network is your own port forward, reverse proxy or VPN, " +
	"the same as any other self-hosted server: an account service plus NAT traversal for other " +
	"people's boxes is a hosted product with ongoing cost and liability, not a feature of a " +
	"self-hosted binary, and this project does not run one. GET /api/remote-access reports the " +
	"addresses this instance actually answers requests on right now, whether a password " +
	"protects them, and warns when it is reachable from beyond this machine with none set. " +
	"POST /api/tokens issues a named, individually revocable credential, so a phone or a " +
	"script gets its own and losing it does not mean rotating the password for every other " +
	"client."

func registerHelp(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/help",
		"this index: every route in this build, and why a few things JDownloader has are not here",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, HelpIndex{
				Version:      buildinfo.Version,
				Deployment:   buildinfo.Deployment,
				About:        helpAbout,
				Vocabulary:   helpVocabulary,
				RemoteAccess: helpRemoteAccess,
				// Read fresh on every request rather than captured once at
				// registration time: registerHelp itself runs partway through
				// registerAll, so a snapshot taken then would miss whatever the
				// call list still had left to register. reg's own slice is
				// complete by the time Handler ever answers a real request.
				Routes: reg.Routes(),
			})
		})
}
