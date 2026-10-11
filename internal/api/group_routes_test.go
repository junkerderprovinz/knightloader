package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/settings"
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
	"DELETE /api/auth/passkeys/{id}",
	"PATCH /api/auth/passkeys/{id}",
	"PUT /api/auth/password",
	"GET /api/tokens",
	"POST /api/tokens",
	"DELETE /api/tokens/{id}",

	// The phrase, the group, the relay and the peers. Only the owner takes a
	// phone out of the group.
	"DELETE /api/connect",
	"GET /api/connect",
	"POST /api/connect/activate",
	"DELETE /api/connect/apps/{id}",
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
	"POST /api/accounts/import",
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
	"GET /api/ytdlp/cookies",
	"POST /api/ytdlp/cookies",
	"POST /api/ytdlp/cookies/remove",

	// Settings and everything configured through them.
	"GET /api/settings",
	"PATCH /api/settings",
	"PUT /api/settings",
	"GET /api/settings/defaults",
	"GET /api/settings/export",
	"POST /api/settings/import",
	"GET /api/features",
	"PUT /api/features/{id}",
	"GET /api/feeds",
	"POST /api/feeds/test",
	"GET /api/folders",
	"POST /api/folders",
	"GET /api/fileowner",
	"POST /api/fileowner/check",
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
	"GET /api/system/deployment",
	"GET /api/system/maintenance",
	"POST /api/system/maintenance",
	"POST /api/system/quit",
	"POST /api/system/restart",
	"POST /api/system/restore",
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
	"GET /api/stats/speed",
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
// reached as the group reaches it and as its owner does.
func groupServer(t *testing.T) (a *app.App, downloads, category string, member func(method, path, body string) (int, []byte), owner func(method, path, body string) (int, []byte)) {
	t.Helper()
	a = testApp(t)
	downloads, category = t.TempDir(), t.TempDir()
	s := settings.Defaults()
	s.DownloadDir = downloads
	s.Crawl = false
	s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Dir: category}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)
	h := Handler(a)
	serve := a.Federation.Guard(relayProxyHandler(h))
	member = func(method, path, body string) (int, []byte) {
		return serve(context.Background(), relay.ProxyCall{Method: method, Path: path, Body: []byte(body)})
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

func TestOnlyTheOwnerTakesAPhoneOutOfTheGroup(t *testing.T) {
	t.Parallel()
	a, _, _, member, owner := groupServer(t)
	if err := a.Federation.SetJoined(time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if status, body := member(http.MethodDelete, "/api/connect/apps/phone-other", ""); !refusedForGroup(status, body) {
		t.Errorf("a member removing a phone = %d (%s), want ownLoginNeeded", status, body)
	}
	if a.Federation.Removed("phone-other") {
		t.Fatal("a member took a phone out of the group")
	}
	if status, body := owner(http.MethodDelete, "/api/connect/apps/phone-other", ""); status != http.StatusNoContent {
		t.Fatalf("the owner removing a phone = %d (%s), want 204", status, body)
	}
	if !a.Federation.Removed("phone-other") {
		t.Fatal("the owner's removal was not recorded")
	}
}

func TestNoMemberMakesThisInstanceLeaveTheGroup(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	const secret = "abababababababababababababababab"
	if err := a.Accounts.Set(relay.SeedAccountService, secret); err != nil {
		t.Fatal(err)
	}
	self := a.Settings.Get().InstanceID
	if status, body := member(http.MethodDelete, "/api/connect/members/"+self, ""); status != http.StatusNotFound {
		t.Errorf("a member asking this instance to leave = %d (%s), want 404", status, body)
	}
	if status, body := member(http.MethodDelete, "/api/connect", ""); !refusedForGroup(status, body) {
		t.Errorf("a member leaving the group for this instance = %d (%s), want ownLoginNeeded", status, body)
	}
	if got, _ := a.Accounts.Get(relay.SeedAccountService); got != secret {
		t.Fatalf("the phrase is %q after the members' calls, want it kept", got)
	}
}

// TestAMemberSeesNoCredentialsInWhatItReads plants a password of every kind a
// task or a peer carries, checks that the owner reads each of them somewhere,
// and then reads every route the group may GET, and the answer to adding a
// link, as a member.
func TestAMemberSeesNoCredentialsInWhatItReads(t *testing.T) {
	t.Parallel()
	a, _, _, member, owner := groupServer(t)
	const torrent = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=film&tr=https%3A%2F%2Ftracker.example%2FPASSKEYLEAK%2Fannounce"
	created, err := a.AddLinksWithOptions([]string{
		"https://user:s3cr'et@host.example/file.zip?id=7&passkey=QUERYLEAK",
		torrent,
	}, "Batch", app.OriginPaste, app.LinkBatchOptions{
		Password:         "ARCHIVELEAK",
		DownloadPassword: "DOWNLOADLEAK",
		Source:           "https://reader:SOURCELEAK@page.example/thread",
	})
	if err != nil || len(created) != 2 {
		t.Fatalf("staging the links: %v, %d tasks", err, len(created))
	}
	if err := addPeer(a, federation.Instance{Name: "attic", URL: "http://admin:PEERLEAK@192.0.2.7:8080"}); err != nil {
		t.Fatal(err)
	}
	leaks := []string{"s3cr", "QUERYLEAK", "PASSKEYLEAK", "ARCHIVELEAK", "DOWNLOADLEAK", "SOURCELEAK", "PEERLEAK"}
	id := created[0].ID

	read := func(call func(method, path, body string) (int, []byte)) map[string]string {
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
		add, _ := json.Marshal(map[string]string{"links": torrent + "\nhttps://user:s3cr'et@host.example/file.zip?id=7&passkey=QUERYLEAK", "password": "ARCHIVELEAK"})
		_, body := call(http.MethodPost, "/api/links", string(add))
		out["POST /api/links"] = string(body)
		return out
	}

	seen := map[string]bool{}
	for _, body := range read(owner) {
		for _, leak := range leaks {
			seen[leak] = seen[leak] || strings.Contains(body, leak)
		}
	}
	for _, leak := range leaks {
		if !seen[leak] {
			t.Fatalf("the owner does not read %q anywhere, so this test proves nothing", leak)
		}
	}
	for route, body := range read(member) {
		for _, leak := range leaks {
			if strings.Contains(body, leak) {
				t.Errorf("%s answered a member with %q", route, leak)
			}
		}
	}

	var tasks []core.Task
	_, body := member(http.MethodGet, "/api/tasks", "")
	if err := json.Unmarshal(body, &tasks); err != nil || len(tasks) == 0 {
		t.Fatalf("GET /api/tasks for a member = %s", body)
	}
	if !bytes.Contains(body, []byte("host.example/file.zip")) || !bytes.Contains(body, []byte("https://tracker.example")) {
		t.Errorf("the redacted list lost what names the file or the tracker: %s", body)
	}
}
