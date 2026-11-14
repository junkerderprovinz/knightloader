package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

const siteSecret = "YT-SESSION-4c1e-never-leaves-the-store"

type siteRow struct {
	ID      string   `json:"id"`
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

// ytHandover is the extension handing over a YouTube session.
const ytHandover = `{"cookies":[{"domain":".youtube.com","path":"/","secure":true,"expirationDate":1900000000,"name":"SID","value":"` + siteSecret + `"}]}`

func TestAHandedOverSessionSignsTheSiteInAndNoAnswerCarriesIt(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
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
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedOut) || r.Waiting {
		t.Errorf("after signing out youtube is %+v", r)
	}
}

func TestAnUnknownSiteIsNotFound(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	for _, path := range []string{"/api/sitelogins/example/begin", "/api/sitelogins/example/check", "/api/sitelogins/example/remove"} {
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
