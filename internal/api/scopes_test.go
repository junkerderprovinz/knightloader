package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// TestEveryGuardedRouteNamesTheRightATokenNeeds is what keeps a new route from
// being reachable with any token before somebody has classified it.
func TestEveryGuardedRouteNamesTheRightATokenNeeds(t *testing.T) {
	t.Parallel()
	registered := map[string]bool{}
	for _, r := range buildRegistry(t).Routes() {
		key := r.pattern()
		registered[key] = true
		want, named := routeScopes[key]
		guarded := guardsScope(r)
		switch {
		case !guarded && named:
			t.Errorf("%s is open or forwards, so the table is not checked on it and its entry in routeScopes never applies", key)
		case guarded && !named:
			t.Errorf("%s has no entry in routeScopes; decide whether a token needs read, add, control or admin for it", key)
		case guarded && r.Scope != want:
			t.Errorf("%s reports scope %q in the index, the table says %q", key, r.Scope, want)
		case !guarded && r.Scope != "":
			t.Errorf("%s is open or forwards and still reports scope %q", key, r.Scope)
		}
	}
	if !registered[forwardPattern] {
		t.Errorf("no route registers %s, the one the table leaves out", forwardPattern)
	}
	for key := range folderRoutes {
		if !registered[key] {
			t.Errorf("folderRoutes names %s, which no route registers", key)
		}
	}
	for key, s := range routeScopes {
		if !registered[key] {
			t.Errorf("routeScopes names %s, which no route registers", key)
		}
		if !slices.Contains(apitoken.AllScopes(), s) {
			t.Errorf("%s needs %q, which is not a scope", key, s)
		}
	}
	for op, s := range sabnzbdScopes {
		if !slices.Contains(apitoken.AllScopes(), s) {
			t.Errorf("the SABnzbd operation %s needs %q, which is not a scope", op, s)
		}
	}
	for call, s := range qbittorrentScopes {
		if !slices.Contains(apitoken.AllScopes(), s) {
			t.Errorf("the qBittorrent call %s needs %q, which is not a scope", call, s)
		}
	}
	for call := range qbittorrentAddMay {
		if qbittorrentScopes[call] != apitoken.ScopeControl {
			t.Errorf("qbittorrentAddMay names %s, which is not a control call in qbittorrentScopes", call)
		}
	}
}

// A route the table does not name is refused to every token without admin.
func TestAnUnclassifiedRouteNeedsAdmin(t *testing.T) {
	t.Parallel()
	if got := scopeFor("POST /api/not-classified-yet"); got != apitoken.ScopeAdmin {
		t.Errorf("an unclassified route needs %q, want admin", got)
	}
}

// scopeProbe is one route per scope, each harmless to call on a test instance.
type scopeProbe struct {
	method, path, body string
	need               apitoken.Scope
}

var scopeProbes = []scopeProbe{
	{http.MethodGet, "/api/tasks", "", apitoken.ScopeRead},
	{http.MethodPost, "/api/links", `{"links":""}`, apitoken.ScopeAdd},
	{http.MethodPost, "/api/tasks/pause", `{}`, apitoken.ScopeControl},
	{http.MethodGet, "/api/settings", "", apitoken.ScopeAdmin},
}

// callAPI sends one request, with a Bearer token when secret is set, and
// decodes a JSON answer if there is one.
func callAPI(t *testing.T, c *http.Client, base, method, path, body, secret string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	return resp.StatusCode, doc
}

// lockedServer is a test server whose instance has a password, since a token
// is neither needed nor checked on one without.
func lockedServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	return srv, a
}

// wantRefused checks a 403 that names the missing right in both the sentence
// and the code a client translates.
func wantRefused(t *testing.T, what string, code int, doc map[string]any, need apitoken.Scope) {
	t.Helper()
	if code != http.StatusForbidden {
		t.Errorf("%s answered %d, want 403", what, code)
		return
	}
	if doc["code"] != "tokenScope" {
		t.Errorf("%s refused with code %v, want tokenScope", what, doc["code"])
	}
	params, _ := doc["params"].(map[string]any)
	if params["scope"] != string(need) {
		t.Errorf("%s names the missing right as %v, want %q", what, params["scope"], need)
	}
	if msg, _ := doc["error"].(string); !strings.Contains(msg, string(need)) {
		t.Errorf("%s refused with %q, which does not name the %q right", what, msg, need)
	}
}

func TestEachScopeOpensItsOwnRoutesAndNoOthers(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)

	for _, s := range apitoken.AllScopes() {
		_, secret, err := a.APITokens.CreateScoped("only "+string(s), []apitoken.Scope{s})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range scopeProbes {
			what := string(s) + " token on " + p.method + " " + p.path
			code, doc := callAPI(t, http.DefaultClient, srv.URL, p.method, p.path, p.body, secret)
			if p.need != s {
				wantRefused(t, what, code, doc, p.need)
				continue
			}
			if code == http.StatusForbidden || code == http.StatusUnauthorized {
				t.Errorf("%s answered %d, want it let through", what, code)
			}
		}
	}
}

func TestAFullTokenAndASessionReachEveryScope(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, full, err := a.APITokens.Create("full")
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Client{Jar: newJar(t)}
	if code := login(t, session, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}

	for _, p := range scopeProbes {
		if code, _ := callAPI(t, http.DefaultClient, srv.URL, p.method, p.path, p.body, full); code == http.StatusForbidden {
			t.Errorf("a full token was refused %s %s", p.method, p.path)
		}
		if code, _ := callAPI(t, session, srv.URL, p.method, p.path, p.body, ""); code == http.StatusForbidden {
			t.Errorf("a browser session was refused %s %s", p.method, p.path)
		}
	}
}

// A token stored before tokens had scopes could do everything, and after the
// upgrade it still can.
func TestATokenFromBeforeScopesStillReachesEveryRoute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const secret = "kl_issued-before-scopes"
	sum := sha256.Sum256([]byte(secret))
	old := `[{"id":"0011223344556677","name":"old script","createdAt":"2025-06-01T12:00:00Z","hash":"` +
		hex.EncodeToString(sum[:]) + `"}]`
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)

	for _, p := range scopeProbes {
		code, _ := callAPI(t, http.DefaultClient, srv.URL, p.method, p.path, p.body, secret)
		if code == http.StatusForbidden || code == http.StatusUnauthorized {
			t.Errorf("the old token answered %d on %s %s, want it let through", code, p.method, p.path)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed []apitoken.Token
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !slices.Equal(listed[0].Scopes, apitoken.AllScopes()) {
		t.Errorf("GET /api/tokens lists %+v, want the old token with every scope", listed)
	}
}

// The peer takes a forwarded call on this instance's own full token, so the
// check has to happen before the call leaves.
func TestAForwardedCallNeedsTheRightItWouldNeedHere(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, reader, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	// Without read: forwarding needs no right of its own.
	_, controller, err := a.APITokens.CreateScoped("remote", []apitoken.Scope{apitoken.ScopeControl})
	if err != nil {
		t.Fatal(err)
	}
	_, adder, err := a.APITokens.CreateScoped("script", []apitoken.Scope{apitoken.ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}

	// There is no peer called "office", so a call that gets past the check
	// ends in the forwarder's 404.
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/instances/office/tasks", "", reader); code == http.StatusForbidden {
		t.Error("a read token may not read a peer's task list")
	}
	code, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/tasks/delete", `{"ids":["x"]}`, reader)
	wantRefused(t, "a read token removing a peer's downloads", code, doc, apitoken.ScopeControl)
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/tasks/abc/pause", "", reader)
	wantRefused(t, "a read token pausing a peer's download", code, doc, apitoken.ScopeControl)
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/tasks/delete", `{"ids":["x"]}`, controller); code == http.StatusForbidden {
		t.Error("a control token may not remove a peer's downloads")
	}
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/links", `{"links":"`+testMagnet+`"}`, adder); code == http.StatusForbidden {
		t.Error("an add token may not add links to a peer, though it may add them here")
	}
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/instances/office/tasks", "", adder)
	wantRefused(t, "an add token reading a peer's task list", code, doc, apitoken.ScopeRead)
}

// Where files land on the host is configuration, so picking the folder needs
// admin, also on the routes add and control open.
func TestPickingTheFolderOfADownloadNeedsAdmin(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, sonarr, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}
	_, remote, err := a.APITokens.CreateScoped("remote", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeControl})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("owner", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	folder, err := json.Marshal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const other = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"

	links := `{"links":"` + testMagnet + `","dir":` + string(folder) + `}`
	code, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/links", links, sonarr)
	wantRefused(t, "an add token picking the folder of new links", code, doc, apitoken.ScopeAdmin)
	if n := len(a.Tasks()); n != 0 {
		t.Fatalf("the refused call staged %d tasks", n)
	}
	// Trailing bytes after the object are ignored by the handler's decoder,
	// so they must not hide the folder from the check either.
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/links", links+" trailing", sonarr)
	wantRefused(t, "an add token picking the folder behind trailing bytes", code, doc, apitoken.ScopeAdmin)
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/links", links, sonarr)
	wantRefused(t, "an add token picking the folder of links for a peer", code, doc, apitoken.ScopeAdmin)
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/links", `{"links":"`+testMagnet+`","dir":"  "}`, sonarr); code != http.StatusOK {
		t.Errorf("an add token adding links with a blank folder answered %d, want 200", code)
	}
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/links", `{"links":"`+other+`","dir":`+string(folder)+`}`, admin); code != http.StatusOK {
		t.Errorf("an admin token picking the folder answered %d, want 200", code)
	}

	options := `{"ids":["x"],"dir":` + string(folder) + `}`
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/tasks/options", options, remote)
	wantRefused(t, "a control token moving downloads to another folder", code, doc, apitoken.ScopeAdmin)
	code, doc = callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/instances/office/tasks/options", options, remote)
	wantRefused(t, "a control token moving a peer's downloads to another folder", code, doc, apitoken.ScopeAdmin)
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/tasks/options", `{"ids":["x"],"comment":"for later"}`, remote); code == http.StatusForbidden {
		t.Error("a control token was refused a comment on a download")
	}
}

// The phone app on a direct connection is set up with read, add and control,
// as docs/connecting.md says, and each call it makes has to get through.
func TestThePhoneAppsRightsReachEveryCallItMakes(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, phone, err := a.APITokens.CreateScoped("phone", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd, apitoken.ScopeControl})
	if err != nil {
		t.Fatal(err)
	}
	// mobile/src/api/client.ts and stats.ts, one line per function.
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/auth", ""},
		{http.MethodGet, "/api/tasks", ""},
		{http.MethodPost, "/api/links", `{"links":""}`},
		{http.MethodPost, "/api/tasks/enabled", `{"ids":["x"],"enabled":false}`},
		{http.MethodPost, "/api/tasks/delete", `{"ids":["x"],"deleteFiles":false}`},
		{http.MethodGet, "/api/queue", ""},
		{http.MethodGet, "/api/queue/counters", ""},
		{http.MethodPost, "/api/tasks/start", `{"ids":["x"]}`},
		{http.MethodPost, "/api/tasks/reorder", `{"ids":["x"]}`},
		{http.MethodPost, "/api/queue", `{"halted":false}`},
		{http.MethodPost, "/api/queue/stop", `{}`},
		{http.MethodGet, "/api/appearance", ""},
		{http.MethodPost, "/api/appearance", `{"rainbowPalette":null}`},
		{http.MethodGet, "/api/instances/office/tasks", ""},
	} {
		if code, doc := callAPI(t, http.DefaultClient, srv.URL, c.method, c.path, c.body, phone); code == http.StatusForbidden || code == http.StatusUnauthorized {
			t.Errorf("the phone app's %s %s answered %d: %+v", c.method, c.path, code, doc)
		}
	}
}

// GET /api/auth is open for the sign-in screen. What it adds for a caller
// that is signed in, the second factor and the recovery codes left, is
// security configuration.
func TestTheSecondFactorIsReportedOnlyToAnAdminToken(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, reader, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd, apitoken.ScopeControl})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("admin", []apitoken.Scope{apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/auth", "", reader)
	if doc["authenticated"] != true {
		t.Errorf("a valid token is not reported as signed in: %+v", doc)
	}
	if hasKey(doc, "twoFactor") || hasKey(doc, "recoveryLeft") {
		t.Errorf("a token without admin learned the second-factor state: %+v", doc)
	}
	if _, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/auth", "", admin); !hasKey(doc, "twoFactor") || !hasKey(doc, "recoveryLeft") {
		t.Errorf("an admin token did not get the second-factor state: %+v", doc)
	}
}

func TestCreatingATokenTakesItsScopes(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	post := func(body string) (int, newTokenResponse) {
		t.Helper()
		resp, err := http.Post(srv.URL+"/api/tokens", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out newTokenResponse
		if resp.StatusCode == http.StatusCreated {
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, out
	}

	code, tok := post(`{"name":"sonarr","scopes":["add","read"]}`)
	if code != http.StatusCreated || !slices.Equal(tok.Scopes, []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd}) {
		t.Errorf("creating an add and read token answered %d with %v", code, tok.Scopes)
	}
	code, tok = post(`{"name":"old client"}`)
	if code != http.StatusCreated || !slices.Equal(tok.Scopes, apitoken.AllScopes()) {
		t.Errorf("a request naming no scopes answered %d with %v, want every scope", code, tok.Scopes)
	}
	// Only a request without the field is an old client; null is a client
	// that named the rights and got them wrong.
	for _, body := range []string{
		`{"name":"nothing","scopes":[]}`, `{"name":"null","scopes":null}`, `{"name":"typo","scopes":["read","delete"]}`,
	} {
		if code, _ := post(body); code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, code)
		}
	}
}

// A token that may manage tokens cannot mint one that may do more than itself.
func TestATokenCannotIssueOneWithMoreRights(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, admin, err := a.APITokens.CreateScoped("admin only", []apitoken.Scope{apitoken.ScopeAdmin, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	code, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/tokens", `{"name":"wider","scopes":["read","control"]}`, admin)
	wantRefused(t, "an admin and read token issuing a control token", code, doc, apitoken.ScopeControl)
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/tokens", `{"name":"narrower","scopes":["read"]}`, admin); code != http.StatusCreated {
		t.Errorf("issuing a token with a subset of the caller's rights answered %d, want 201", code)
	}
	if code, _ := callAPI(t, http.DefaultClient, srv.URL, http.MethodPost, "/api/tokens", `{"name":"everything"}`, admin); code != http.StatusForbidden {
		t.Errorf("issuing a full token from a narrower one answered %d, want 403", code)
	}
}

// The passkey list is open for the sign-in screen's counts; the keys
// themselves need the admin right when a token asks.
func TestPasskeysAreListedOnlyToAnAdminToken(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	_, reader, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("admin", []apitoken.Scope{apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if _, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/auth/passkeys", "", reader); hasKey(doc, "passkeys") {
		t.Errorf("a read token got the registered passkeys: %+v", doc)
	}
	if _, doc := callAPI(t, http.DefaultClient, srv.URL, http.MethodGet, "/api/auth/passkeys", "", admin); !hasKey(doc, "passkeys") {
		t.Errorf("an admin token did not get the registered passkeys: %+v", doc)
	}
}

// The bridge and metrics rows send the owner to make a token when there is
// none. A token that cannot do what the module needs is no better, and the
// row says so.
func TestModuleRowsNoticeTokensWithoutTheRightsTheModuleNeeds(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	s := a.Settings.Get()
	s.DownloadClientAPI, s.Metrics, s.SubfolderByPackage = true, true, false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.APITokens.CreateScoped("remote", []apitoken.Scope{apitoken.ScopeControl}); err != nil {
		t.Fatal(err)
	}
	rows := func(bridge, metrics string) {
		t.Helper()
		if got := featureRow(t, a, "downloadclient").DetailCode; got != bridge {
			t.Errorf("the bridge row says %q, want %q", got, bridge)
		}
		if got := featureRow(t, a, "metrics").DetailCode; got != metrics {
			t.Errorf("the metrics row says %q, want %q", got, metrics)
		}
	}
	rows("downloadclientNoAddReadTokenNoSubfolders", "metricsNoReadToken")

	s.SubfolderByPackage = true
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead}); err != nil {
		t.Fatal(err)
	}
	rows("downloadclientNoAddReadToken", "metricsReady")

	if _, _, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd}); err != nil {
		t.Fatal(err)
	}
	rows("downloadclientReadyBoth", "metricsReady")
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}
