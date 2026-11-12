package api

// Opening the web interface of another member of the fortress, and being
// opened by one.
//
// Opening: a browser signed in here with the password asks for a tab on a
// member. The kit hands out a ticket and serves the tab under
// kitfortress.OpenPath, a bar naming the member above a sandboxed frame whose
// every request travels sealed to the member. The tab sits in front of the
// guard and the origin and host checks, which would turn away the frame's
// requests from an origin of their own; the ticket is what lets them in.
//
// Being opened: a request arrives from far away, with no cookie but those this
// instance set inside the tunnel, and goes through the same handler as a
// browser's. tunnelShut keeps the doors closed that are for other programs or
// that would hand out a way in.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// fortressUses is every action KnightLoader calls at the other members.
var fortressUses = append(slices.Clone(afterdownload.Uses), kitfortress.OpenAction)

// printLength is how much of a member's id names it in the log, as much as
// the bar above an opened tab shows.
const printLength = 8

// fortressUI is this instance's web interface as the other members open it.
// It opens only while the password is set, since without one every holder
// of the phrase could use it.
func fortressUI(a *app.App, serve http.Handler) *kitfortress.UI {
	return &kitfortress.UI{Handler: serve, SignIn: a.Auth.Enabled, Opened: func(from kitfortress.Caller) { openedBy(a, from) }}
}

// openedBy writes a member's new session on this instance's pages to the log
// the other members read.
func openedBy(a *app.App, from kitfortress.Caller) {
	name := from.App
	if g := fortressOf(a); g != nil {
		for _, m := range g.f.Members() {
			if m.ID == from.ID && m.App.Name != "" {
				name = m.App.Name
			}
		}
	}
	print := from.ID[max(len(from.ID)-printLength, 0):]
	log.Printf("fortress: %s on %s opened the web interface (%s)", name, from.Device, print)
	a.FortressLog.Opened(name, from.Device, print)
}

// passwordSession is the session token of r when it is a browser signed in
// with the password: no API token, no member of the group, no page opened
// through the fortress.
func passwordSession(a *app.App, r *http.Request) (string, bool) {
	if !a.Auth.Enabled() || fromGroup(r) {
		return "", false
	}
	if _, ok := kitfortress.Opened(r); ok {
		return "", false
	}
	c, err := r.Cookie(auth.CookieName)
	if err != nil || !a.Auth.Valid(c.Value) {
		return "", false
	}
	return c.Value, true
}

// sessionName is what the kit knows a session by. The token stays here.
func sessionName(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// fortressTabs serves the tabs opened on other members, through the fortress
// this instance is in now. A phrase change opens it anew, and the tabs of the
// one before end with it.
func fortressTabs(a *app.App, next http.Handler) http.Handler {
	session := func(r *http.Request) (string, bool) {
		token, ok := passwordSession(a, r)
		return sessionName(token), ok
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, kitfortress.OpenPath) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := kitfortress.Opened(r); ok {
			refuseTunnel(w)
			return
		}
		g := fortressOf(a)
		if g == nil {
			http.NotFound(w, r)
			return
		}
		// The kit names the tab's paths from the root and redirects within
		// them; putting the base path in front would lead out of the tab.
		if under, ok := w.(redirectsUnderBase); ok {
			w = under.ResponseWriter
		}
		g.f.OpenHandler(session, nil).ServeHTTP(w, r)
	})
}

// tunnelShut are the routes a page opened through the fortress never reaches,
// by pattern: resetting the password, the credentials other programs sign in
// with and the doors they use, the dashboards' reading, the archives that
// carry every stored secret, and the fortress's own ways in.
var tunnelShut = map[string]bool{
	"POST /api/auth/reset":                   true,
	"POST /api/auth/reset/finish":            true,
	"POST /api/auth/passkey/register/begin":  true,
	"POST /api/auth/passkey/register/finish": true,

	"GET /api/tokens":              true,
	"POST /api/tokens":             true,
	"DELETE /api/tokens/{id}":      true,
	"/mcp":                         true,
	"GET /api/mcp":                 true,
	"POST /api/mcp/clients":        true,
	"DELETE /api/mcp/clients/{id}": true,

	"GET /api/qbittorrent/api/v2/{call...}":  true,
	"POST /api/qbittorrent/api/v2/{call...}": true,
	"GET /api/sabnzbd/api":                   true,
	"POST /api/sabnzbd/api":                  true,
	"/dav/{path...}":                         true,
	"GET /dav/{path...}":                     true,
	"OPTIONS /dav/{path...}":                 true,
	"PROPFIND /dav/{path...}":                true,

	"GET /api/metrics":        true,
	"/api/fortress/v1/status": true,
	"/api/fortress/v1/log":    true,

	"GET /api/system/backup":         true,
	"GET /api/system/backups/{name}": true,

	"POST /api/fortress/open/{id}": true,
	"POST /api/group/call":         true,
	"GET /relay/connect":           true,
}

// shutInTunnel reports whether r came through the fortress for a route that
// stays closed to it. Setting the first password counts while there is none,
// though the fortress opens nothing then.
func (reg *Registry) shutInTunnel(a *app.App, r *http.Request) bool {
	if _, ok := kitfortress.Opened(r); !ok {
		return false
	}
	pattern := reg.patternOf(r.Method, r.URL)
	return tunnelShut[pattern] || pattern == "PUT /api/auth/password" && !a.Auth.Enabled()
}

func refuseTunnel(w http.ResponseWriter) {
	writeRefusal(w, http.StatusForbidden, "notThroughFortress",
		"this is not open to a page opened through the fortress; use KnightLoader's own address", nil)
}

// refuseOpen answers an Open the fortress refused, with the code GlimStone's
// card has words for.
func refuseOpen(w http.ResponseWriter, err error) {
	var no *kitfortress.Refusal
	switch {
	case errors.Is(err, group.ErrNoSuchMember) || errors.Is(err, group.ErrRemoved):
		writeRefusal(w, http.StatusNotFound, "unreachable", "that member is not in the fortress", nil)
	case errors.Is(err, group.ErrUnreachable):
		writeRefusal(w, http.StatusBadGateway, "unreachable", "that member cannot be reached now", nil)
	case errors.Is(err, kitfortress.ErrNoSuchAction):
		writeRefusal(w, http.StatusNotFound, "no_ui", "that member has no pages to open", nil)
	case errors.Is(err, kitfortress.ErrPermissionOff):
		writeRefusal(w, http.StatusForbidden, "permission_off", "the permission to open its pages is off", nil)
	case errors.As(err, &no) && no.Code == "ui_no_login":
		writeRefusal(w, http.StatusForbidden, "ui_no_login", "that app opens from elsewhere only once it has a password", nil)
	case errors.As(err, &no) && (no.Code == "busy" || no.Code == "jars_full"):
		writeRefusal(w, http.StatusServiceUnavailable, "busy", "that member is busy, try again in a moment", nil)
	default:
		log.Printf("fortress: open a member: %v", err)
		writeRefusal(w, http.StatusBadGateway, "failed", "that member did not open", nil)
	}
}

// statusLogPages bounds how far back the status window reads a member's log,
// a page at a time.
const statusLogPages = 3

// statusLogLines is how many of the newest entries the window shows.
const statusLogLines = 10

func registerFortressOpen(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/fortress/open/{id}",
		"open the web interface of a member of the fortress in a new tab, behind that member's own sign-in; "+
			"only for a browser signed in here with the password; answers the tab's path or a refusal code",
		func(w http.ResponseWriter, r *http.Request) {
			token, ok := passwordSession(a, r)
			if !ok {
				writeRefusal(w, http.StatusForbidden, "passwordSession", "only a browser signed in with the password opens another app", nil)
				return
			}
			g := fortressOf(a)
			if g == nil {
				refuseFortress(w, errNoFortress)
				return
			}
			path, err := g.f.Open(r.Context(), kitfortress.Visit{
				Member:  r.PathValue("id"),
				Session: sessionName(token),
				Valid:   func() bool { return a.Auth.Valid(token) },
				Back:    requestBasePath(r) + "/settings/pairing",
			})
			if err != nil {
				refuseOpen(w, err)
				return
			}
			writeJSON(w, map[string]string{"path": requestBasePath(r) + path})
		})

	reg.Add(http.MethodGet, "/api/fortress/members/{id}/status",
		"how a member of the fortress is doing and its newest log entries, newest first, in ?lang=; "+
			"what Open shows for a member without pages of its own",
		func(w http.ResponseWriter, r *http.Request) {
			g := fortressOf(a)
			if g == nil {
				refuseFortress(w, errNoFortress)
				return
			}
			id, lang := r.PathValue("id"), r.URL.Query().Get("lang")
			if !slices.ContainsFunc(g.f.Members(), func(m kitfortress.Member) bool { return m.ID == id && m.Kind == member.KindInstance }) {
				writeRefusal(w, http.StatusNotFound, "unreachable", "that member is not in the fortress", nil)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			report, err := g.f.Status(ctx, id, lang)
			if err != nil {
				refuseOpen(w, err)
				return
			}
			writeJSON(w, map[string]any{"report": report, "log": newestLog(ctx, g.f, id, lang)})
		})
}

// newestLog reads a member's log up to statusLogPages pages far and returns
// its newest statusLogLines entries, newest first. What it cannot read is
// left out: the report alone still says how the member is doing.
func newestLog(ctx context.Context, f *kitfortress.Fortress, id, lang string) []status.LogEntry {
	entries := []status.LogEntry{}
	after := ""
	for range statusLogPages {
		page, err := f.Log(ctx, id, status.LogQuery{After: after, Limit: status.MaxLogLimit, Lang: lang})
		if err != nil {
			break
		}
		entries = append(entries, page.Entries...)
		if len(page.Entries) < status.MaxLogLimit || page.Next == after {
			break
		}
		after = page.Next
	}
	entries = entries[max(len(entries)-statusLogLines, 0):]
	slices.Reverse(entries)
	return entries
}
