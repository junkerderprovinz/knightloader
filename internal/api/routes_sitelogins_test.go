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

const (
	siteSecret = "YT-SESSION-4c1e-never-leaves-the-store"
	ytSession  = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1900000000\tSID\t" + siteSecret + "\n"
)

type siteRow struct {
	ID      string   `json:"id"`
	State   string   `json:"state"`
	Account string   `json:"account"`
	Waiting bool     `json:"waiting"`
	Domains []string `json:"domains"`
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

func TestAPastedSessionSignsTheSiteInAndNoAnswerCarriesIt(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	text := ytSession
	raw, _ := json.Marshal(map[string]*string{"text": &text})
	status, body := owner(http.MethodPost, "/api/sitelogins/youtube/cookies", string(raw))
	if status != http.StatusOK {
		t.Fatalf("pasting = %d (%s)", status, body)
	}
	var pasted siteRow
	if err := json.Unmarshal(body, &pasted); err != nil || pasted.State != string(sitelogin.SignedIn) {
		t.Errorf("pasting answered %s, want youtube signed in", body)
	}
	for _, path := range []string{"/api/sitelogins", "/api/sitelogins/handover"} {
		_, listed := owner(http.MethodGet, path, "")
		if strings.Contains(string(body)+string(listed), siteSecret) {
			t.Fatalf("%s or the paste answered with the session", path)
		}
	}
	_, listed := owner(http.MethodGet, "/api/sitelogins", "")
	if r := rowOf(t, siteRows(t, listed), "youtube"); r.State != string(sitelogin.SignedIn) {
		t.Errorf("youtube is listed as %+v", r)
	}
}

func TestAnEmptyPasteIsRefusedRatherThanSigningOut(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	for _, body := range []string{`{}`, `{"text":"  "}`} {
		if status, answer := owner(http.MethodPost, "/api/sitelogins/youtube/cookies", body); status != http.StatusBadRequest {
			t.Errorf("pasting %s = %d (%s), want 400", body, status, answer)
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
	text := ytSession
	raw, _ := json.Marshal(map[string]*string{"text": &text})
	owner(http.MethodPost, "/api/sitelogins/youtube/cookies", string(raw))
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
