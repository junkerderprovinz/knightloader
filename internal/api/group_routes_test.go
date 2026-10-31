package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// groupRefused are the routes a member of the group is refused on purpose. A
// route on neither this list nor groupRoutes fails
// TestEveryRouteIsDecidedForTheGroup, so whoever adds one decides.
var groupRefused = []string{
	// The login, its second factor, passkeys and tokens.
	"POST /api/auth/2fa/begin",
	"POST /api/auth/2fa/confirm",
	"POST /api/auth/2fa/disable",
	"POST /api/auth/login",
	"POST /api/auth/logout",
	"POST /api/auth/passkey/login/begin",
	"POST /api/auth/passkey/login/finish",
	"POST /api/auth/passkey/register/begin",
	"POST /api/auth/passkey/register/finish",
	"GET /api/auth/passkeys",
	"POST /api/auth/reset",
	"POST /api/auth/reset/finish",
	"DELETE /api/auth/passkeys/{id}",
	"PATCH /api/auth/passkeys/{id}",
	"PUT /api/auth/password",
	"GET /api/tokens",
	"POST /api/tokens",
	"DELETE /api/tokens/{id}",
	"GET /api/mcp",
	"POST /api/mcp/clients",
	"DELETE /api/mcp/clients/{id}",

	// The phrase, the group, the relay and the peers.
	"DELETE /api/connect",
	"GET /api/connect",
	"POST /api/connect/activate",
	"POST /api/connect/join",
	"POST /api/connect/reveal",
	"GET /api/discovery",
	"POST /api/instances",
	"DELETE /api/instances/{name}",
	"/api/instances/{name}/{rest...}",
	"GET /api/relay/config",
	"PUT /api/relay/config",

	// Hoster accounts, logins, servers and connections.
	"GET /api/accounts",
	"POST /api/accounts",
	"GET /api/accounts/catalogue",
	"POST /api/accounts/enabled",
	"POST /api/accounts/label",
	"POST /api/accounts/test",
	"POST /api/accounts/verify",
	"POST /api/connections/import",
	"POST /api/connections/test",
	"GET /api/hosterauth/hosts",
	"GET /api/hosterauth/logins",
	"POST /api/hosterauth/logins",
	"POST /api/hosterauth/logins/enabled",
	"POST /api/hosterauth/logins/remove",
	"GET /api/hostheaders",
	"POST /api/hostheaders",
	"DELETE /api/hostheaders/{id}",
	"GET /api/usenet/servers",
	"POST /api/usenet/servers",
	"POST /api/usenet/servers/test",
	"DELETE /api/usenet/servers/{id}",
	"GET /api/vpn",
	"POST /api/vpn/{id}/connect",
	"POST /api/vpn/disconnect",
	"PUT /api/vpn/routes",
	"DELETE /api/vpn/{id}",
	"GET /api/ytdlp/cookies",
	"POST /api/ytdlp/cookies",
	"POST /api/ytdlp/cookies/remove",

	// Settings and everything configured through them.
	"GET /api/settings",
	"PATCH /api/settings",
	"PUT /api/settings",
	"GET /api/settings/defaults",
	"GET /api/features",
	"PUT /api/features/{id}",
	// Taking the Click'n'Load port over is a choice made on the instance that
	// takes it, never asked of it from the group.
	"POST /api/cnl/takeover",
	"GET /api/feeds",
	"POST /api/feeds/test",
	"GET /api/folders",
	"POST /api/folders",
	"GET /api/fileowner",
	"POST /api/fileowner/check",
	"GET /api/jd/update",
	"POST /api/jd/update",
	"POST /api/jdimport/apply",
	"POST /api/jdimport/read",
	"GET /api/options",
	"GET /api/reconnect",
	"POST /api/reconnect",
	"POST /api/reconnect/import",
	"GET /api/reconnect/router",
	"GET /api/resolvers/jd",
	"GET /api/resolvers/priority",
	"POST /api/resolvers/priority",
	"GET /api/rules/grammar",
	"POST /api/rules/preview",
	"GET /api/schedule",
	"PUT /api/schedule",
	"DELETE /api/schedule/suspend",
	"PUT /api/schedule/suspend",
	"GET /api/torrents/interfaces",
	"POST /api/torrents/portmap",
	"POST /api/torrents/qbittorrent/test",
	"GET /api/torrents/trackers",
	"GET /api/uistate",
	"PUT /api/uistate",
	"GET /api/ytdlp/formats",
	"GET /api/ytdlp/preset",
	"POST /api/ytdlp/preset",

	// What this instance runs: scripts, event programs and targets, media
	// hooks, the idle command and the tools.
	"GET /api/scripts",
	"POST /api/scripts",
	"GET /api/scripts/triggers",
	"DELETE /api/scripts/{id}",
	"PUT /api/scripts/{id}",
	"POST /api/scripts/{id}/run",
	"GET /api/eventprograms",
	"GET /api/eventprograms/placeholders",
	"GET /api/eventtargets",
	"GET /api/eventtargets/placeholders",
	"POST /api/eventtargets/test",
	"GET /api/mediahooks",
	"POST /api/mediahooks",
	"DELETE /api/mediahooks/{id}",
	"POST /api/mediahooks/{id}/test",
	"GET /api/playback",
	"POST /api/playback/servers",
	"DELETE /api/playback/servers/{id}",
	"POST /api/playback/servers/{id}/test",
	"GET /api/idle-action",
	"GET /api/idle-action/actions",
	"POST /api/idle-action/cancel",
	"POST /api/idle-action/check",
	"POST /api/idle-action/run",
	"GET /api/mediatools",
	"GET /api/mediatools/ytdlp/latest",
	"POST /api/mediatools/ytdlp/revert",
	"POST /api/mediatools/ytdlp/update",

	// The system: backups, restore, restart, maintenance, diagnostics and the
	// readings of this machine.
	"GET /api/system/backup",
	"GET /api/system/backups",
	"GET /api/system/backups/{name}",
	"GET /api/system/deployment",
	"GET /api/system/maintenance",
	"POST /api/system/maintenance",
	"POST /api/system/quit",
	"POST /api/system/restart",
	"POST /api/system/restore",
	"POST /api/system/restore/check",
	"GET /api/system/update-check",
	"GET /api/diagnostics",
	"GET /api/diagnostics/log",
	"GET /api/diagnostics/logfile",
	"GET /api/diagnostics/logfile/{gen}",
	"POST /api/diagnostics/startup",
	"GET /api/diagnostics/task/{id}",
	"GET /api/selftest",
	"POST /api/selftest",
	"GET /api/selftest/request",
	"GET /api/diskspace",
	"GET /api/health",
	"GET /api/health/detail",
	"GET /api/metrics",
	"GET /api/help",
	"GET /api/stats/speed/day",
	"GET /api/stats/torrents",
	"GET /api/stats/volume",
	"GET /api/stats/volume/usage",
	"GET /api/ws",

	// Work the phone and the extension do not do: uploads, the history, the
	// holding areas, cleanup by class, starting or stopping an unpacking and
	// the web interface's own captcha calls.
	"POST /api/containers",
	"GET /api/containers/relay/{token}",
	"POST /api/torrents",
	"POST /api/torrents/parse",
	"DELETE /api/history",
	"GET /api/history",
	"DELETE /api/collector/filtered",
	"GET /api/collector/filtered",
	"POST /api/collector/filtered/restore",
	"DELETE /api/collector/skipped",
	"GET /api/collector/skipped",
	"GET /api/cleanup/{class}",
	"POST /api/cleanup/{class}",
	"POST /api/extract/start",
	"POST /api/extract/{id}/abort",
	"POST /api/activity/{kind}/abort",
	"POST /api/captcha/test",
	"POST /api/captcha/{id}/unanswerable",
	"DELETE /api/captcha/{id}/unanswerable",
	"GET /api/captcha/{id}/widget",
	"GET /api/hosters/icon",
	"GET /api/hosters/icons",
	"GET /api/browser-extension/version",
	"GET /api/browser-extension.xpi",
	"GET /api/browser-extension.zip",

	// The doors other programs use, each with its own credential, and the
	// sockets members arrive through.
	"GET /api/qbittorrent/api/v2/{call...}",
	"POST /api/qbittorrent/api/v2/{call...}",
	"GET /api/sabnzbd/api",
	"POST /api/sabnzbd/api",
	"/dav/{path...}",
	"GET /dav/{path...}",
	"OPTIONS /dav/{path...}",
	"PROPFIND /dav/{path...}",
	"/mcp",
	"POST /api/group/call",
	"GET /relay/connect",
}

func TestEveryRouteIsDecidedForTheGroup(t *testing.T) {
	t.Parallel()
	var routes []string
	for _, r := range buildRegistry(t).Routes() {
		routes = append(routes, r.pattern())
	}
	for _, r := range routes {
		allowed, refused := groupRoutes[r], slices.Contains(groupRefused, r)
		switch {
		case allowed && refused:
			t.Errorf("%s is both allowed and refused to the group", r)
		case !allowed && !refused:
			t.Errorf("%s is neither allowed nor refused to the group: add it to groupRoutes or to groupRefused", r)
		}
	}
	for r := range groupRoutes {
		if !slices.Contains(routes, r) {
			t.Errorf("groupRoutes lists %s, which is no route", r)
		}
	}
	for _, r := range groupRefused {
		if !slices.Contains(routes, r) {
			t.Errorf("groupRefused lists %s, which is no route", r)
		}
	}
	for r := range groupLimits {
		if !groupRoutes[r] {
			t.Errorf("groupLimits holds %s, which the group does not reach anyway", r)
		}
	}
}

var groupLookup struct {
	once sync.Once
	reg  *Registry
}

// groupReaches reports whether a member of the group reaches method and path
// past the route list, before any limit on what the request asks.
func groupReaches(t *testing.T, method, path string) bool {
	t.Helper()
	groupLookup.once.Do(func() {
		groupLookup.reg = buildRegistry(t)
		groupLookup.reg.lookupOnce.Do(groupLookup.reg.buildLookup)
	})
	u, err := url.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return groupRoutes[groupLookup.reg.patternOf(method, u)]
}

// groupCall is a request as relayProxyHandler hands it on.
func groupCall(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), relayGroupKey, true))
}

// refusedForGroup reports whether an answer is the refusal a member is shown.
func refusedForGroup(status int, body []byte) bool {
	var out struct {
		Code string `json:"code"`
	}
	return status == http.StatusForbidden && json.Unmarshal(body, &out) == nil && out.Code == "ownLoginNeeded"
}

// gateOnly puts guard in front of a mux on which every route answers 204, so
// what a test sees is the guard's decision alone. reached counts the calls
// that got past it.
func gateOnly(t *testing.T, a *app.App) (http.Handler, *int) {
	t.Helper()
	reg := newRegistry()
	registerAll(reg, a)
	mux := http.NewServeMux()
	reached := new(int)
	var mu sync.Mutex
	for _, r := range reg.Routes() {
		mux.HandleFunc(r.pattern(), func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			*reached++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	return guard(a, reg, mux), reached
}

// requestPath is a path that pattern matches, every wildcard filled.
func requestPath(pattern string) (method, path string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = http.MethodGet, pattern
	}
	return method, regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "abc")
}

func TestAMemberReachesTheListedRoutesAndIsRefusedTheRest(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	gate, _ := gateOnly(t, a)
	for _, r := range buildRegistry(t).Routes() {
		method, path := requestPath(r.pattern())
		w := httptest.NewRecorder()
		gate.ServeHTTP(w, groupCall(method, path, "{}"))
		if groupRoutes[r.pattern()] {
			if w.Code != http.StatusNoContent {
				t.Errorf("%s answered a member %d (%s), want it let through", r.pattern(), w.Code, w.Body)
			}
			continue
		}
		if !refusedForGroup(w.Code, w.Body.Bytes()) {
			t.Errorf("%s answered a member %d (%s), want 403 with ownLoginNeeded", r.pattern(), w.Code, w.Body)
		}
	}
}

// A path the gate cannot put a route to is never served to a member, and
// answers like a route that does not exist.
func TestAMemberGetsA404ForWhatNoRouteServes(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	gate, reached := gateOnly(t, a)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/no-such-route"},
		{http.MethodGet, "/"},
		{http.MethodGet, "/index.html"},
		{http.MethodGet, "/api/tasks/../settings"},
		{http.MethodGet, "/api/tasks/x/../../settings"},
		{http.MethodGet, "/api//settings"},
		{http.MethodGet, "/api/tasks/"},
		{http.MethodPost, "/api/tasks"},
		{http.MethodGet, "/api/auth/password"},
		{http.MethodPost, "/api/captcha"},
		{http.MethodGet, "/api/captcha/c1/widget/phone/again"},
	} {
		w := httptest.NewRecorder()
		gate.ServeHTTP(w, groupCall(c.method, c.path, ""))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s answered a member %d (%s), want 404", c.method, c.path, w.Code, w.Body)
		}
	}
	if *reached != 0 {
		t.Errorf("%d of those calls reached a route", *reached)
	}
}

// groupServer is an instance with a download folder and a category folder,
// reached as the group reaches it and as its owner does. The tasks in seed are
// in its list from the start, as they were saved. Its relay is off.
func groupServer(t *testing.T, seed ...*core.Task) (a *app.App, downloads, category string, member func(method, path, body string) (int, []byte), owner func(method, path, body string) (int, []byte)) {
	t.Helper()
	dir := t.TempDir()
	if len(seed) > 0 {
		st, err := store.Open(filepath.Join(dir, "knightloader.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range seed {
			if err := st.Save(task); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	downloads, category = t.TempDir(), t.TempDir()
	s := settings.Defaults()
	s.RelayMode = settings.RelayModeOff
	s.DownloadDir = downloads
	s.Crawl = false
	s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Dir: category}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)
	h := Handler(a)
	asPhone := asMember(t, a)
	member = func(method, path, body string) (int, []byte) {
		return asPhone(relay.ProxyCall{Method: method, Path: path, Body: []byte(body)})
	}
	owner = func(method, path, body string) (int, []byte) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:8080"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	return a, downloads, category, member, owner
}

func TestAMemberPicksOnlyAmongTheDownloadFolders(t *testing.T) {
	t.Parallel()
	a, downloads, category, member, owner := groupServer(t)
	elsewhere := t.TempDir()
	links := func(dir string) string {
		b, _ := json.Marshal(map[string]string{"links": "https://host.example/" + filepath.Base(dir) + ".bin", "dir": dir})
		return string(b)
	}
	for _, dir := range []string{
		filepath.Join(downloads, "films"),
		downloads,
		category,
		filepath.Join(downloads, "<jd:packagename>"),
	} {
		if status, body := member(http.MethodPost, "/api/links", links(dir)); status != http.StatusOK {
			t.Errorf("a member adding links into %s = %d (%s), want 200", dir, status, body)
		}
	}
	for _, dir := range []string{
		elsewhere,
		filepath.Dir(downloads),
		filepath.Join(downloads, "..", filepath.Base(elsewhere)),
		filepath.Join(downloads, "<jd:packagename>", "..", ".."),
		downloads + "-next-door",
		"relative/folder",
	} {
		if status, body := member(http.MethodPost, "/api/links", links(dir)); !refusedForGroup(status, body) {
			t.Errorf("a member adding links into %s = %d (%s), want ownLoginNeeded", dir, status, body)
		}
	}
	if status, body := owner(http.MethodPost, "/api/links", links(elsewhere)); status != http.StatusOK {
		t.Fatalf("the owner adding links into %s = %d (%s), want 200", elsewhere, status, body)
	}

	id := stage(t, a, "https://host.example/options.bin")[0].ID
	options := func(dir string) string {
		b, _ := json.Marshal(map[string]any{"ids": []string{id}, "dir": dir})
		return string(b)
	}
	if status, body := member(http.MethodPost, "/api/tasks/options", options(elsewhere)); !refusedForGroup(status, body) {
		t.Errorf("a member moving a task to %s = %d (%s), want ownLoginNeeded", elsewhere, status, body)
	}
	if status, body := member(http.MethodPost, "/api/tasks/options", options(category)); status != http.StatusNoContent {
		t.Errorf("a member moving a task to the category folder = %d (%s), want 204", status, body)
	}
	if status, body := member(http.MethodPost, "/api/tasks/options", `{"ids":["`+id+`"],"comment":"from the phone"}`); status != http.StatusNoContent {
		t.Errorf("a member changing a task without a folder = %d (%s), want 204", status, body)
	}
}

func TestAMemberCannotEraseWhatWasDownloaded(t *testing.T) {
	t.Parallel()
	a, downloads, _, member, _ := groupServer(t)
	id := putFile(t, a, downloads, "film.mkv", []byte("frames"))
	file := filepath.Join(downloads, "film.mkv")

	if status, body := member(http.MethodPost, "/api/tasks/delete", `{"ids":["`+id+`"],"files":true}`); !refusedForGroup(status, body) {
		t.Errorf("a member deleting with files = %d (%s), want ownLoginNeeded", status, body)
	}
	if status, body := member(http.MethodDelete, "/api/tasks/"+id+"?files=1", ""); !refusedForGroup(status, body) {
		t.Errorf("a member deleting one task with files = %d (%s), want ownLoginNeeded", status, body)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file is gone after the refusals: %v", err)
	}
	if status, body := member(http.MethodPost, "/api/tasks/delete", `{"ids":["`+id+`"],"files":false}`); status != http.StatusOK {
		t.Fatalf("a member removing the row = %d (%s), want 200", status, body)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("removing the row took the file: %v", err)
	}

	// Erasing files needs a password of its own, so the owner signs in first.
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	other := putFile(t, a, downloads, "other.mkv", []byte("frames"))
	r := httptest.NewRequest(http.MethodPost, "/api/tasks/delete", strings.NewReader(`{"ids":["`+other+`"],"files":true}`))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: a.Auth.Issue()})
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("the owner deleting with files = %d (%s), want 200", w.Code, w.Body.Bytes())
	}
}

// The ids of the devices the tests name: another instance of the group and
// two phones.
var (
	instanceB  = idAs("instance-b", relay.KindInstance)
	phoneMine  = idAs("phone-mine", relay.KindMobile)
	phoneOther = idAs("phone-other", relay.KindMobile)
)

// signedMember calls a's group routes as the device of kind whose key label
// makes, signing with that key once a has approved it, the way the guard sees
// a call that relay.OpenCall let through.
func signedMember(t *testing.T, a *app.App, label, kind string) func(method, path string) (int, []byte) {
	t.Helper()
	// Built first: a handler built for an instance that holds a phrase from
	// before member keys starts its group state over.
	serve := a.Federation.Guard(relayProxyHandler(Handler(a)))
	id := idAs(label, kind)
	if _, _, err := a.Federation.Approve(id, keyOf(label).Public().(ed25519.PublicKey), kind); err != nil {
		t.Fatal(err)
	}
	return signedAs(serve, id, kind, keyOf(label))
}

// keyOf is the member key a test names by label.
func keyOf(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(seed[:])
}

// idAs is the id keyOf(label) makes for a device of kind.
func idAs(label, kind string) string {
	return relay.MemberID(kind, keyOf(label).Public().(ed25519.PublicKey))
}

// signedAs calls serve as member id signing as kind with key, approved or
// not. A nil key sends the call unsigned.
func signedAs(serve relay.ProxyHandler, id, kind string, key ed25519.PrivateKey) func(method, path string) (int, []byte) {
	return func(method, path string) (int, []byte) {
		return serve(context.Background(), stamped(relay.ProxyCall{Method: method, Path: path}, id, kind, key))
	}
}

var callSeq atomic.Int64

// stamped is call as it opens from a member signing as id and kind with key:
// with an id of its own and the time it was sent. A nil key leaves it
// unsigned.
func stamped(call relay.ProxyCall, id, kind string, key ed25519.PrivateKey) relay.ProxyCall {
	call.From, call.Kind = id, kind
	if key != nil {
		call.Key, call.Sig = key.Public().(ed25519.PublicKey), []byte("checked by OpenCall")
	}
	call.ID = "test-" + strconv.FormatInt(callSeq.Add(1), 10)
	call.Sent = time.Now().Unix()
	return call
}

// asMember serves calls through a's guard as an approved phone of the group
// sends them.
func asMember(t *testing.T, a *app.App) func(relay.ProxyCall) (int, []byte) {
	t.Helper()
	serve := a.Federation.Guard(relayProxyHandler(Handler(a)))
	id := idAs("phone-member", relay.KindMobile)
	if _, _, err := a.Federation.Approve(id, keyOf("phone-member").Public().(ed25519.PublicKey), relay.KindMobile); err != nil {
		t.Fatal(err)
	}
	return func(call relay.ProxyCall) (int, []byte) {
		return serve(context.Background(), stamped(call, id, relay.KindMobile, keyOf("phone-member")))
	}
}

func TestAMemberLearnsNoMoreOfTheLoginThanAStranger(t *testing.T) {
	t.Parallel()
	a, _, _, member, owner := groupServer(t)
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, body := member(http.MethodGet, "/api/auth", "")
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("GET /api/auth for a member = %s", body)
	}
	for _, key := range []string{"twoFactor", "recoveryLeft", "appKey", "appKeyCopiesUntil"} {
		if _, ok := got[key]; ok {
			t.Errorf("a member of the group read %q from GET /api/auth: %s", key, body)
		}
	}
	if _, body := owner(http.MethodGet, "/api/auth", ""); bytes.Contains(body, []byte("twoFactor")) {
		t.Errorf("a stranger read the second factor from GET /api/auth: %s", body)
	}
}

// TestNamingItselfWithoutAKeyDoesNotMakeAnApprovedInstance plays the whole
// attack: whoever holds the words names a fresh id without a key, then signs
// as an instance under it, and tries to take a phone out and make this
// instance leave its group.
func TestNamingItselfWithoutAKeyDoesNotMakeAnApprovedInstance(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	const secret = "abababababababababababababababab"
	if err := a.Accounts.Set(relay.SeedAccountService, secret); err != nil {
		t.Fatal(err)
	}
	serve := a.Federation.Guard(relayProxyHandler(Handler(a)))
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	evilID := idAs("evil", relay.KindInstance)
	if status, body := signedAs(serve, evilID, "", nil)(http.MethodGet, "/api/tasks"); status != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("the unsigned call naming a fresh id = %d (%s), want 403 memberUnsigned", status, body)
	}
	evil := signedAs(serve, evilID, relay.KindInstance, keyOf("evil"))
	if status, body := evil(http.MethodGet, "/api/tasks"); status != http.StatusForbidden || refusalCode(body) != "memberPending" {
		t.Errorf("its signed call as an instance = %d (%s), want 403 memberPending", status, body)
	}
	if status, body := evil(http.MethodDelete, "/api/connect/apps/"+phoneMine); status != http.StatusForbidden {
		t.Errorf("it removing a phone = %d (%s), want 403", status, body)
	}
	self := groupID(a)
	if status, body := evil(http.MethodDelete, "/api/connect/members/"+self); status != http.StatusForbidden {
		t.Errorf("it taking this instance out = %d (%s), want 403", status, body)
	}
	time.Sleep(100 * time.Millisecond)
	if got, _ := a.Accounts.Get(relay.SeedAccountService); got != secret {
		t.Fatal("the instance left its group on the word of a device nobody approved")
	}
	if status, _ := phone(http.MethodGet, "/api/tasks"); status != http.StatusOK {
		t.Errorf("the approved phone afterwards = %d, want 200", status)
	}
}

func TestOnlyTheOwnerOrAnApprovedInstanceTakesAPhoneOut(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	unsigned := signedAs(a.Federation.Guard(relayProxyHandler(Handler(a))), "phone-other", "", nil)
	if status, body := unsigned(http.MethodDelete, "/api/connect/apps/"+phoneOther); status != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("an unsigned member removing a phone = %d (%s), want 403 memberUnsigned", status, body)
	}
	if status, body := phone(http.MethodDelete, "/api/connect/apps/"+phoneOther); status != http.StatusForbidden || refusalCode(body) != "decisionNotFromInstance" {
		t.Errorf("an approved phone removing a phone = %d (%s), want 403 decisionNotFromInstance", status, body)
	}
	if a.Federation.Removed(phoneOther) {
		t.Fatal("a phone or an unsigned member took a phone out of the group")
	}

	instance := signedMember(t, a, "instance-b", relay.KindInstance)
	if status, body := instance(http.MethodDelete, "/api/connect/apps/"+phoneOther); status != http.StatusNoContent {
		t.Fatalf("an approved instance passing a removal on = %d (%s), want 204", status, body)
	}
	if !a.Federation.Removed(phoneOther) {
		t.Fatal("the removal an approved instance passed on was not recorded")
	}
	if status, body := owner(http.MethodDelete, "/api/connect/apps/"+phoneMine, ""); status != http.StatusNoContent {
		t.Fatalf("the owner removing a phone = %d (%s), want 204", status, body)
	}
	if status, _ := phone(http.MethodGet, "/api/tasks"); status != http.StatusGone {
		t.Errorf("the phone the owner removed = %d, want 410", status)
	}
	if status, body := owner(http.MethodDelete, "/api/connect/apps/"+instanceB, ""); status != http.StatusConflict || refusalCode(body) != "memberIsInstance" {
		t.Errorf("the owner removing an instance as a phone = %d (%s), want 409 memberIsInstance", status, body)
	}
}

func TestOnlyAnApprovedInstanceReadsTheRoster(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	if status, body := phone(http.MethodGet, "/api/group/roster"); status != http.StatusForbidden || refusalCode(body) != "rosterNotForInstance" {
		t.Errorf("a phone reading the roster = %d (%s), want 403 rosterNotForInstance", status, body)
	}
	if status, body := owner(http.MethodGet, "/api/group/roster", ""); status != http.StatusForbidden {
		t.Errorf("the owner's page reading the roster = %d (%s), want 403", status, body)
	}

	instance := signedMember(t, a, "instance-b", relay.KindInstance)
	status, body := instance(http.MethodGet, "/api/group/roster")
	var r federation.Roster
	if status != http.StatusOK || json.Unmarshal(body, &r) != nil {
		t.Fatalf("an approved instance reading the roster = %d (%s), want 200 and a roster", status, body)
	}
}

func TestOnlyAnApprovedInstanceMakesThisInstanceLeaveTheGroup(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	const secret = "abababababababababababababababab"
	if err := a.Accounts.Set(relay.SeedAccountService, secret); err != nil {
		t.Fatal(err)
	}
	self := groupID(a)
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	unsigned := signedAs(a.Federation.Guard(relayProxyHandler(Handler(a))), "phone-other", "", nil)
	if status, body := unsigned(http.MethodDelete, "/api/connect/members/"+self); status != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("an unsigned member asking this instance to leave = %d (%s), want 403 memberUnsigned", status, body)
	}
	if status, body := phone(http.MethodDelete, "/api/connect/members/"+self); status != http.StatusForbidden || refusalCode(body) != "decisionNotFromInstance" {
		t.Errorf("an approved phone asking this instance to leave = %d (%s), want 403 decisionNotFromInstance", status, body)
	}
	if status, body := phone(http.MethodDelete, "/api/connect"); !refusedForGroup(status, body) {
		t.Errorf("a member leaving the group for this instance = %d (%s), want ownLoginNeeded", status, body)
	}
	if status, body := owner(http.MethodDelete, "/api/connect/members/"+self, ""); status != http.StatusBadRequest {
		t.Errorf("the owner removing this instance as another = %d (%s), want 400", status, body)
	}
	if got, _ := a.Accounts.Get(relay.SeedAccountService); got != secret {
		t.Fatalf("the phrase is %q after those calls, want it kept", got)
	}

	instance := signedMember(t, a, "instance-b", relay.KindInstance)
	if status, body := instance(http.MethodDelete, "/api/connect/members/"+self); status != http.StatusNoContent {
		t.Fatalf("an approved instance taking this one out = %d (%s), want 204", status, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _ := a.Accounts.Get(relay.SeedAccountService); got == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the instance kept its phrase after an approved instance took it out of the group")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestARemovalThatArrivesDuringAResetDoesNotEndTheNewGroup has another
// instance take this one out while a reset holds every session change: the
// reset puts the instance in a group of its own, and the removal, which was
// meant for the old group, must not make it leave that one.
func TestARemovalThatArrivesDuringAResetDoesNotEndTheNewGroup(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	if err := a.Accounts.Set(relay.SeedAccountService, "abababababababababababababababab"); err != nil {
		t.Fatal(err)
	}
	instance := signedMember(t, a, "instance-b", relay.KindInstance)
	self := groupID(a)

	resetting, renew := make(chan struct{}), make(chan struct{})
	reset := make(chan error, 1)
	go func() {
		reset <- a.Auth.RevokeAll(func() {
			close(resetting)
			<-renew
			if _, err := renewGroup(a); err != nil {
				t.Error(err)
			}
		})
	}()
	<-resetting
	if status, body := instance(http.MethodDelete, "/api/connect/members/"+self); status != http.StatusNoContent {
		t.Fatalf("the instance taking this one out = %d (%s), want 204", status, body)
	}
	time.Sleep(100 * time.Millisecond)
	if got, _ := a.Accounts.Get(relay.SeedAccountService); got == "" {
		t.Error("the instance left its group while a reset was under way")
	}
	close(renew)
	if err := <-reset; err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got, _ := a.Accounts.Get(relay.SeedAccountService); got == "" {
		t.Error("a removal meant for the group before the reset made the instance leave the new one")
	}
}

func TestTheOwnerTakesAnotherInstanceOutButNotAPhoneThatWay(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	instance := signedMember(t, a, "instance-b", relay.KindInstance)
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	if status, body := owner(http.MethodDelete, "/api/connect/members/"+phoneMine, ""); status != http.StatusConflict || refusalCode(body) != "memberIsApp" {
		t.Errorf("the owner removing a phone as an instance = %d (%s), want 409 memberIsApp", status, body)
	}
	if status, body := phone(http.MethodGet, "/api/tasks"); status != http.StatusOK {
		t.Errorf("the phone after the refused removal = %d (%s), want 200", status, body)
	}
	if status, body := owner(http.MethodDelete, "/api/connect/members/"+instanceB, ""); status != http.StatusNoContent {
		t.Fatalf("the owner removing another instance = %d (%s), want 204", status, body)
	}
	if status, body := instance(http.MethodGet, "/api/tasks"); status != http.StatusGone {
		t.Errorf("the removed instance's key = %d (%s), want 410", status, body)
	}
}

// TestAMemberSeesNoCredentialsInWhatItReads plants a password of every kind a
// task or a peer carries, in links that name a file and in links that name
// none, whose address is then the task's name, and in a failure. It checks that
// the owner reads each of them somewhere, and then reads every route the group
// may GET, and the answer to adding links, as a member.
func TestAMemberSeesNoCredentialsInWhatItReads(t *testing.T) {
	t.Parallel()
	a, _, _, member, owner := groupServer(t, &core.Task{
		ID:          "failed",
		URL:         "https://host.example/?passkey=FAILEDLEAK",
		Name:        "https://host.example/?passkey=FAILEDLEAK",
		Status:      core.StatusError,
		Enabled:     true,
		CreatedAt:   time.Now(),
		Error:       `Get "https://u:ERRORLEAK@host.example/?apikey=ERRORKEYLEAK": dial tcp: connection refused`,
		ErrorParams: map[string]string{"url": "https://host.example/?auth=PARAMLEAK"},
	})
	torrent := func(hash string) string {
		return "magnet:?xt=urn:btih:" + strings.Repeat(hash, 40) + "&dn=film&tr=https%3A%2F%2Ftracker.example%2FPASSKEYLEAK%2Fannounce"
	}
	links := func(who string) []string {
		return []string{
			"https://user:s3cr'et@host.example/" + who + ".zip?id=7&passkey=QUERYLEAK",
			"https://host.example/?who=" + who + "&passkey=NOFILELEAK",
			"https://user:NOFILEPW@" + who + ".host.example/",
		}
	}
	created, err := a.AddLinksWithOptions(append(links("staged"), torrent("0")), "Batch", app.OriginPaste, app.LinkBatchOptions{
		Password:         "ARCHIVELEAK",
		DownloadPassword: "DOWNLOADLEAK",
		Source:           "https://reader:SOURCELEAK@page.example/thread",
	})
	if err != nil || len(created) != 4 {
		t.Fatalf("staging the links: %v, %d tasks", err, len(created))
	}
	if err := addPeer(a, federation.Instance{Name: "attic", URL: "http://admin:PEERLEAK@192.0.2.7:8080"}); err != nil {
		t.Fatal(err)
	}
	leaks := []string{
		"s3cr", "QUERYLEAK", "NOFILELEAK", "NOFILEPW", "FAILEDLEAK", "ERRORLEAK", "ERRORKEYLEAK", "PARAMLEAK",
		"PASSKEYLEAK", "ARCHIVELEAK", "DOWNLOADLEAK", "SOURCELEAK", "PEERLEAK",
	}
	id := created[0].ID

	// Each caller adds links of its own, or the second would only add
	// duplicates and get an empty answer.
	read := func(call func(method, path, body string) (int, []byte), who, hash string) map[string]string {
		out := map[string]string{}
		for pattern := range groupRoutes {
			method, path := requestPath(pattern)
			if method != http.MethodGet {
				continue
			}
			path = strings.ReplaceAll(path, "abc", id)
			_, body := call(method, path, "")
			out[pattern] = string(body)
		}
		add, _ := json.Marshal(map[string]string{"links": strings.Join(append(links(who), torrent(hash)), "\n"), "password": "ARCHIVELEAK"})
		_, body := call(http.MethodPost, "/api/links", string(add))
		out["POST /api/links"] = string(body)
		return out
	}

	seen := map[string]bool{}
	for _, body := range read(owner, "owner", "a") {
		for _, leak := range leaks {
			seen[leak] = seen[leak] || strings.Contains(body, leak)
		}
	}
	for _, leak := range leaks {
		if !seen[leak] {
			t.Fatalf("the owner does not read %q anywhere, so this test proves nothing", leak)
		}
	}
	answers := read(member, "member", "b")
	for route, body := range answers {
		for _, leak := range leaks {
			if strings.Contains(body, leak) {
				t.Errorf("%s answered a member with %q", route, leak)
			}
		}
	}
	if added := answers["POST /api/links"]; !strings.Contains(added, "member.host.example") {
		t.Errorf("adding links as a member answered %s, want the new tasks", added)
	}

	var tasks []core.Task
	_, body := member(http.MethodGet, "/api/tasks", "")
	if err := json.Unmarshal(body, &tasks); err != nil || len(tasks) == 0 {
		t.Fatalf("GET /api/tasks for a member = %s", body)
	}
	if !bytes.Contains(body, []byte("host.example/staged.zip")) || !bytes.Contains(body, []byte("https://tracker.example")) ||
		!bytes.Contains(body, []byte("https://staged.host.example/")) {
		t.Errorf("the redacted list lost what names the file, the host or the tracker: %s", body)
	}
}

func TestAMemberReadsTheSpeedHistory(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	code, body := member(http.MethodGet, "/api/stats/speed", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/stats/speed answered a member %d (%s)", code, body)
	}
	var got struct {
		Recent []int64 `json:"recent"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the speed history a member reads does not decode: %v (%s)", err, body)
	}
}

func TestAMemberListsThePhonesAndBrowsersOfTheGroup(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	browser := idAs("browser-mine", relay.KindExtension)
	if _, _, err := a.Federation.Approve(browser, keyOf("browser-mine").Public().(ed25519.PublicKey), relay.KindExtension); err != nil {
		t.Fatal(err)
	}
	// After the member: building its handler sets the relay from settings.
	rt := &relayOnlyPeer{sibs: []relay.Announce{
		{InstanceID: phoneMine, Name: "Pixel 8", Deployment: relay.KindMobile, Client: true},
		{InstanceID: browser, Name: "Firefox", Deployment: relay.KindExtension, Client: true},
		{InstanceID: phoneOther, Name: "Old phone", Deployment: relay.KindMobile, Client: true},
	}}
	a.Federation.SetRelay(rt)
	list := func() []map[string]any {
		t.Helper()
		status, body := phone(http.MethodGet, "/api/group/apps")
		if status != http.StatusOK {
			t.Fatalf("GET /api/group/apps answered a member %d (%s)", status, body)
		}
		var got struct {
			Apps []map[string]any `json:"apps"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("the list does not decode: %v (%s)", err, body)
		}
		return got.Apps
	}
	if apps := list(); len(apps) != 3 {
		t.Fatalf("apps = %v, want the three devices on the relay", apps)
	}

	// The extension's socket closes after its call, and the old phone is
	// taken out.
	if err := a.Federation.RemoveApp(phoneOther, time.Now()); err != nil {
		t.Fatal(err)
	}
	rt.sibs = rt.sibs[:1]
	apps := list()
	if len(apps) != 2 {
		t.Fatalf("apps = %v, want the phone and the extension", apps)
	}
	byID := map[string]map[string]any{}
	for _, entry := range apps {
		keys := make([]string, 0, len(entry))
		for k := range entry {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if want := []string{"approved", "connected", "deployment", "id", "lastSeen", "name"}; !slices.Equal(keys, want) {
			t.Errorf("an entry carries %v, want exactly %v", keys, want)
		}
		id, _ := entry["id"].(string)
		byID[id] = entry
	}
	if p := byID[phoneMine]; p == nil || p["connected"] != true || p["approved"] != true || p["deployment"] != relay.KindMobile {
		t.Errorf("the phone = %v, want it connected and approved", p)
	}
	if b := byID[browser]; b == nil || b["connected"] != false || b["approved"] != true || b["deployment"] != relay.KindExtension {
		t.Errorf("the extension = %v, want it approved and not connected", b)
	}
	if seen, _ := byID[browser]["lastSeen"].(float64); seen <= 0 {
		t.Errorf("the extension was last seen at %v, want a time", byID[browser]["lastSeen"])
	}
}

// rosterOf is what GET /api/group/apps tells a member about the instances of
// the group.
type rosterOf struct {
	Members []groupMember `json:"members"`
	Away    []awayMember  `json:"away"`
}

func askRoster(t *testing.T, member func(method, path string) (int, []byte)) rosterOf {
	t.Helper()
	status, body := member(http.MethodGet, "/api/group/apps")
	if status != http.StatusOK {
		t.Fatalf("GET /api/group/apps answered a member %d (%s)", status, body)
	}
	var got rosterOf
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the list does not decode: %v (%s)", err, body)
	}
	return got
}

func TestAMemberHearsOfADesktopAppOfTheGroupThatWasClosedUntilItIsTakenOut(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	desktop := idAs("zathallia", relay.KindInstance)
	if _, _, err := a.Federation.Approve(desktop, keyOf("zathallia").Public().(ed25519.PublicKey), relay.KindInstance); err != nil {
		t.Fatal(err)
	}

	// After the member: building its handler sets the relay from settings.
	a.Federation.SetRelay(&relayOnlyPeer{sibs: []relay.Announce{{InstanceID: desktop, Name: "Zathallia", Deployment: "desktop"}}})
	if got := askRoster(t, phone); len(got.Members) != 1 || got.Members[0].ID != desktop || len(got.Away) != 0 {
		t.Fatalf("while the desktop app is open: %+v, want it a member and nothing away", got)
	}
	a.Federation.SetRelay(nil)
	got := askRoster(t, phone)
	if len(got.Members) != 0 || len(got.Away) != 1 || got.Away[0].ID != desktop || got.Away[0].Name != "Zathallia" ||
		got.Away[0].Deployment != "desktop" || got.Away[0].LastSeen == 0 {
		t.Fatalf("after it was closed: %+v, want the desktop app away", got)
	}

	if status, body := owner(http.MethodDelete, "/api/connect/members/"+desktop, ""); status != http.StatusNoContent {
		t.Fatalf("taking it out answered %d (%s)", status, body)
	}
	if got := askRoster(t, phone); len(got.Members) != 0 || len(got.Away) != 0 {
		t.Fatalf("after it was taken out: %+v, want it gone", got)
	}
}

// listedNetwork is a network on which every announce it holds is a member's.
type listedNetwork []discovery.Peer

func (l listedNetwork) Members() []discovery.Peer { return l }

func (listedNetwork) SetGroup(string, func(discovery.Peer) string, func(discovery.Peer) bool) {}

func TestAMemberHearsOfAnInstanceThisOneReachesOnlyOnItsNetwork(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-mine", relay.KindMobile)
	nas := idAs("nas", relay.KindInstance)
	if _, _, err := a.Federation.Approve(nas, keyOf("nas").Public().(ed25519.PublicKey), relay.KindInstance); err != nil {
		t.Fatal(err)
	}
	// After the member, whose handler leaves the direct transport out of any
	// group, since this instance holds no phrase.
	a.Federation.SetGroup(bytes.Repeat([]byte{7}, 32), groupID(a), keyOf("self"))
	a.Federation.SetDiscovery(listedNetwork{{ID: nas, Name: "NAS", URL: "http://192.168.1.5:8749", Deployment: "container"}})

	got := askRoster(t, phone)
	if len(got.Members) != 1 || got.Members[0].ID != nas || got.Members[0].Name != "NAS" || got.Members[0].Deployment != "container" {
		t.Fatalf("members = %+v, want the instance on this network", got.Members)
	}
	if len(got.Away) != 0 {
		t.Fatalf("away = %+v, want none while it is reachable", got.Away)
	}
}
