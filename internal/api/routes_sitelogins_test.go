package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

const siteSecret = "YT-SESSION-4c1e-never-leaves-the-store"

type siteRow struct {
	ID      string   `json:"id"`
	Added   bool     `json:"added"`
	State   string   `json:"state"`
	Account string   `json:"account"`
	Waiting bool     `json:"waiting"`
	Domains []string `json:"domains"`
	Source  string   `json:"source"`
}

func siteRows(t *testing.T, body []byte) []siteRow {
	t.Helper()
	var rows []siteRow
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("not a list of sites: %v (%s)", err, body)
	}
	return rows
}

func rowOf(t *testing.T, rows []siteRow, id string) siteRow {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no %s in %+v", id, rows)
	return siteRow{}
}

// addSite adds a site on the accounts page, as picking it there does.
func addSite(t *testing.T, owner func(method, path, body string) (int, []byte), id string) {
	t.Helper()
	if status, body := owner(http.MethodPost, "/api/sitelogins/"+id+"/add", ""); status != http.StatusOK {
		t.Fatalf("adding %s = %d (%s)", id, status, body)
	}
}

// ytHandover is the extension handing over a YouTube session.
const ytHandover = `{"cookies":[{"domain":".youtube.com","path":"/","secure":true,"expirationDate":1900000000,"name":"SID","value":"` + siteSecret + `"}]}`

func TestAHandedOverSessionSignsTheSiteInAndNoAnswerCarriesIt(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	addSite(t, owner, "youtube")
	status, body := owner(http.MethodPost, "/api/sitelogins/youtube/handover", ytHandover)
	if status != http.StatusOK {
		t.Fatalf("handing over = %d (%s)", status, body)
	}
	var handed siteRow
	if err := json.Unmarshal(body, &handed); err != nil || handed.State != string(sitelogin.SignedIn) || handed.Source != "extension" {
		t.Errorf("handing over answered %s, want youtube signed in through the extension", body)
	}
	for _, path := range []string{"/api/sitelogins", "/api/sitelogins/handover"} {
		_, listed := owner(http.MethodGet, path, "")
		if strings.Contains(string(body)+string(listed), siteSecret) {
			t.Fatalf("%s or the hand-over answered with the session", path)
		}
	}
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedIn) {
		t.Errorf("youtube is listed as %+v", r)
	}
}

// The page tells a session from a browser apart from the extension's, and
// names the browsers it could not read, so every row carries the three.
func TestEverySiteSaysWhereItsSessionComesFrom(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(listed, &rows); err != nil || len(rows) == 0 {
		t.Fatalf("not a list of sites: %v (%s)", err, listed)
	}
	for _, r := range rows {
		if string(r["source"]) != `""` || string(r["browser"]) != `""` || string(r["unreadable"]) != `[]` {
			t.Errorf("a signed-out site reads source %s, browser %s, unreadable %s", r["source"], r["browser"], r["unreadable"])
		}
	}
}

func TestCookiesCannotBePastedAnyWhere(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	for _, call := range [][2]string{
		{http.MethodPost, "/api/sitelogins/youtube/cookies"},
		{http.MethodGet, "/api/ytdlp/cookies"},
		{http.MethodPost, "/api/ytdlp/cookies"},
		{http.MethodPost, "/api/ytdlp/cookies/remove"},
	} {
		if status, body := owner(call[0], call[1], `{"text":"x","host":"youtube.com"}`); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d (%s), want no such route", call[0], call[1], status, body)
		}
	}
}

func TestSignInWaitsAndSignOutForgets(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	status, body := owner(http.MethodPost, "/api/sitelogins/youtube/begin", "")
	var begun siteRow
	if status != http.StatusOK || json.Unmarshal(body, &begun) != nil || !begun.Waiting {
		t.Fatalf("begin = %d (%s), want youtube waiting", status, body)
	}
	owner(http.MethodPost, "/api/sitelogins/youtube/handover", ytHandover)
	if status, body := owner(http.MethodPost, "/api/sitelogins/youtube/remove", ""); status != http.StatusOK {
		t.Fatalf("remove = %d (%s)", status, body)
	}
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedOut) || r.Waiting || r.Added {
		t.Errorf("after removing youtube is %+v, want it signed out without a row", r)
	}
}

func TestAnUnknownSiteIsNotFound(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	for _, path := range []string{"/api/sitelogins/example/add", "/api/sitelogins/example/begin", "/api/sitelogins/example/check", "/api/sitelogins/example/remove"} {
		if status, body := owner(http.MethodPost, path, ""); status != http.StatusNotFound {
			t.Errorf("%s = %d (%s), want 404", path, status, body)
		}
	}
}

// signedCall is signedMember with a body.
func signedCall(t *testing.T, label, kind string, serve relay.ProxyHandler, approve func(id string, key ed25519.PublicKey)) func(method, path, body string) (int, []byte) {
	t.Helper()
	id, key := idAs(label, kind), keyOf(label)
	approve(id, key.Public().(ed25519.PublicKey))
	return func(method, path, body string) (int, []byte) {
		return serve(context.Background(), stamped(relay.ProxyCall{Method: method, Path: path, Body: []byte(body)}, id, kind, key))
	}
}

func TestTheExtensionHandsASessionOverAndNoOtherMemberMay(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	serve := a.Federation.Guard(relayProxyHandler(Handler(a)))
	approve := func(kind string) func(string, ed25519.PublicKey) {
		return func(id string, key ed25519.PublicKey) {
			if _, _, err := a.Federation.Approve(id, key, kind); err != nil {
				t.Fatal(err)
			}
		}
	}
	browser := signedCall(t, "browser-mine", relay.KindExtension, serve, approve(relay.KindExtension))
	phone := signedCall(t, "phone-mine", relay.KindMobile, serve, approve(relay.KindMobile))

	owner(http.MethodPost, "/api/sitelogins/youtube/begin", "")
	status, body := browser(http.MethodGet, "/api/sitelogins/handover", "")
	if status != http.StatusOK || !rowOf(t, siteRows(t, body), "youtube").Waiting {
		t.Fatalf("the extension's list = %d (%s), want youtube waiting", status, body)
	}
	if r := rowOf(t, siteRows(t, body), "youtube"); len(r.Domains) == 0 || r.State != "" || r.Account != "" {
		t.Errorf("the extension's list says %+v, want the domains to read and nothing about the session", r)
	}

	handover := `{"cookies":[{"domain":".youtube.com","path":"/","secure":true,"expirationDate":1900000000,"name":"SID","value":"` + siteSecret + `"}]}`
	if status, body := phone(http.MethodPost, "/api/sitelogins/youtube/handover", handover); status != http.StatusForbidden {
		t.Errorf("a phone handing a session over = %d (%s), want 403", status, body)
	}
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedOut) {
		t.Fatalf("the phone's session was stored: %+v", r)
	}

	status, body = browser(http.MethodPost, "/api/sitelogins/youtube/handover", handover)
	var handed siteRow
	if status != http.StatusOK || json.Unmarshal(body, &handed) != nil || handed.State != string(sitelogin.SignedIn) {
		t.Fatalf("the extension handing it over = %d (%s), want youtube signed in", status, body)
	}
	if strings.Contains(string(body), siteSecret) {
		t.Error("the hand-over answered with the session")
	}
	_, listed = owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedIn) || r.Waiting {
		t.Errorf("after the hand-over youtube is %+v", r)
	}

	empty := `{"cookies":[{"domain":".google.com","path":"/","expirationDate":1900000000,"name":"SID","value":"x"}]}`
	if status, body := browser(http.MethodPost, "/api/sitelogins/youtube/handover", empty); status != http.StatusUnprocessableEntity {
		t.Errorf("handing over no cookie of the site = %d (%s), want 422", status, body)
	}
}

// The extension watches only the cookies a signed-in session carries.
func TestTheHandOverListNamesEachSitesSignInCookies(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	want := map[string]string{
		"facebook":  "c_user,xs",
		"instagram": "sessionid",
		"patreon":   "session_id",
		"reddit":    "reddit_session",
		"tiktok":    "sessionid,sessionid_ss,sid_tt",
		"twitch":    "auth-token",
		"vimeo":     "vimeo",
		"x":         "auth_token",
		"youtube":   "SID,__Secure-1PSID,__Secure-3PSID,LOGIN_INFO",
	}
	for id := range want {
		addSite(t, owner, id)
	}
	_, body := owner(http.MethodGet, "/api/sitelogins/handover", "")
	var rows []struct {
		ID     string   `json:"id"`
		SignIn []string `json:"signIn"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("not a list of sites: %v (%s)", err, body)
	}
	if len(rows) != len(want) {
		t.Errorf("the hand-over list has %d sites, want %d", len(rows), len(want))
	}
	for _, r := range rows {
		if got := strings.Join(r.SignIn, ","); got != want[r.ID] {
			t.Errorf("%s lists the sign-in cookies %q, want %q", r.ID, got, want[r.ID])
		}
	}
	if strings.Contains(string(body), siteSecret) {
		t.Fatal("the hand-over list carries a session")
	}
}

// The extension reads only what the list names, so a site that is not added
// is neither read nor handed over.
func TestTheHandOverListNamesOnlyTheSitesAdded(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	listed := func() string {
		_, body := owner(http.MethodGet, "/api/sitelogins/handover", "")
		var ids []string
		for _, r := range siteRows(t, body) {
			ids = append(ids, r.ID)
		}
		return strings.Join(ids, ",")
	}
	if got := listed(); got != "" {
		t.Fatalf("a new instance lists %q for hand-over, want none", got)
	}
	addSite(t, owner, "instagram")
	addSite(t, owner, "patreon")
	if got := listed(); got != "instagram,patreon" {
		t.Errorf("the hand-over list = %q, want instagram,patreon", got)
	}
	owner(http.MethodPost, "/api/sitelogins/instagram/remove", "")
	if got := listed(); got != "patreon" {
		t.Errorf("after removing instagram the list = %q, want patreon", got)
	}
	if status, body := owner(http.MethodPost, "/api/sitelogins/youtube/handover", ytHandover); status != http.StatusConflict {
		t.Errorf("handing over a site that is not added = %d (%s), want 409", status, body)
	}
	_, all := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, all), "youtube"); r.Added || r.State != string(sitelogin.SignedOut) {
		t.Errorf("the refused hand-over left youtube %+v", r)
	}
}

// A browser that signed out hands over nothing, and the session it handed
// over before is forgotten.
func TestAnEmptyHandOverSignsOut(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	addSite(t, owner, "youtube")
	if status, body := owner(http.MethodPost, "/api/sitelogins/youtube/handover", ytHandover); status != http.StatusOK {
		t.Fatalf("handing over = %d (%s)", status, body)
	}
	status, body := owner(http.MethodPost, "/api/sitelogins/youtube/handover", `{"cookies":[]}`)
	var row siteRow
	if status != http.StatusOK || json.Unmarshal(body, &row) != nil || row.State != string(sitelogin.SignedOut) {
		t.Fatalf("an empty hand-over = %d (%s), want 200 and signed out", status, body)
	}
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedOut) {
		t.Errorf("after an empty hand-over youtube is %+v", r)
	}
}

func TestAHandOverThatSignsNobodyInIsStillRefused(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	addSite(t, owner, "youtube")
	other := `{"cookies":[{"domain":".google.com","path":"/","expirationDate":1900000000,"name":"SID","value":"x"}]}`
	if status, body := owner(http.MethodPost, "/api/sitelogins/youtube/handover", other); status != http.StatusUnprocessableEntity {
		t.Errorf("a hand-over without the site's cookies = %d (%s), want 422", status, body)
	}
}

// An instance in a container reads no browser, and the page says how to sign
// in there instead.
func TestEverySiteSaysWhetherThisInstanceReadsBrowsers(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(listed, &rows); err != nil || len(rows) == 0 {
		t.Fatalf("not a list of sites: %v (%s)", err, listed)
	}
	for _, r := range rows {
		if string(r["readsBrowsers"]) != "false" {
			t.Errorf("an instance that reads no browser lists readsBrowsers %s", r["readsBrowsers"])
		}
	}
}

// Remove takes a site's row away whatever its session came from, and adding
// it or signing in through KnightLoader brings the row back.
func TestRemovingTakesTheRowAwayUntilTheSiteIsAddedAgain(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	row := func() siteRow {
		_, listed := owner(http.MethodGet, "/api/sitelogins", "")
		return rowOf(t, siteRows(t, listed), "patreon")
	}
	if r := row(); r.Added {
		t.Fatalf("patreon starts out %+v", r)
	}
	if status, body := owner(http.MethodPost, "/api/sitelogins/patreon/add", ""); status != http.StatusOK || !strings.Contains(string(body), `"added":true`) {
		t.Fatalf("add = %d (%s), want 200 and added", status, body)
	}
	if status, body := owner(http.MethodPost, "/api/sitelogins/patreon/remove", ""); status != http.StatusOK || !strings.Contains(string(body), `"added":false`) {
		t.Fatalf("remove = %d (%s), want 200 and not added", status, body)
	}
	if r := row(); r.Added || r.State != "signedOut" {
		t.Errorf("after removing patreon is %+v, want no row", r)
	}
	if status, body := owner(http.MethodPost, "/api/sitelogins/patreon/begin", ""); status != http.StatusOK {
		t.Fatalf("begin = %d (%s)", status, body)
	}
	if r := row(); !r.Added {
		t.Errorf("after signing in again patreon is %+v, want its row back", r)
	}
	if status, _ := owner(http.MethodPost, "/api/sitelogins/patreon/reconnect", ""); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Errorf("reconnect = %d, want no such route", status)
	}
}

func handsOver(t *testing.T, owner func(method, path, body string) (int, []byte)) map[string]bool {
	t.Helper()
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	var rows []struct {
		ID        string `json:"id"`
		HandsOver bool   `json:"handsOver"`
	}
	if err := json.Unmarshal(listed, &rows); err != nil {
		t.Fatalf("not a list of sites: %v (%s)", err, listed)
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.ID] = r.HandsOver
	}
	return out
}

// The window offers the user's own browser only where an extension of the
// group said it may read the site, since nothing else hands the sign-in over.
func TestASiteHandsOverWhileAnExtensionOfTheGroupMayReadIt(t *testing.T) {
	t.Parallel()
	a, _, _, _, owner := groupServer(t)
	serve := a.Federation.Guard(relayProxyHandler(Handler(a)))
	approve := func(kind string) func(string, ed25519.PublicKey) {
		return func(id string, key ed25519.PublicKey) {
			if _, _, err := a.Federation.Approve(id, key, kind); err != nil {
				t.Fatal(err)
			}
		}
	}
	browser := signedCall(t, "browser-reads", relay.KindExtension, serve, approve(relay.KindExtension))
	other := signedCall(t, "browser-other", relay.KindExtension, serve, approve(relay.KindExtension))
	phone := signedCall(t, "phone-reads", relay.KindMobile, serve, approve(relay.KindMobile))

	if got := handsOver(t, owner); got["patreon"] || got["youtube"] {
		t.Fatalf("before any extension said so, the sites hand over %v", got)
	}
	phone(http.MethodGet, "/api/sitelogins/handover?reads=patreon", "")
	owner(http.MethodGet, "/api/sitelogins/handover?reads=patreon", "")
	if got := handsOver(t, owner); got["patreon"] {
		t.Errorf("a phone or the owner saying it reads patreon made it hand over: %v", got)
	}

	if status, body := browser(http.MethodGet, "/api/sitelogins/handover?reads=patreon", ""); status != http.StatusOK {
		t.Fatalf("the extension's list = %d (%s)", status, body)
	}
	if got := handsOver(t, owner); !got["patreon"] || got["youtube"] {
		t.Errorf("with the extension reading patreon, the sites hand over %v", got)
	}
	browser(http.MethodGet, "/api/sitelogins/handover", "")
	other(http.MethodGet, "/api/sitelogins/handover?reads=", "")
	if got := handsOver(t, owner); !got["patreon"] {
		t.Errorf("a list that says nothing, or another browser's, took patreon away: %v", got)
	}

	browser(http.MethodGet, "/api/sitelogins/handover?reads=", "")
	if got := handsOver(t, owner); got["patreon"] {
		t.Errorf("the extension no longer reads patreon, and it still hands over: %v", got)
	}

	browser(http.MethodGet, "/api/sitelogins/handover?reads=patreon,youtube", "")
	if err := a.Federation.RemoveApp(idAs("browser-reads", relay.KindExtension), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := handsOver(t, owner); got["patreon"] || got["youtube"] {
		t.Errorf("a browser taken out of the group still hands over %v", got)
	}
}
