package api

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// addedByAdmin reports whether the task staged for link records an admin as
// the one who added it, which is what lets it use the owner's site sessions.
func addedByAdmin(t *testing.T, a *app.App, link string) bool {
	t.Helper()
	for _, task := range a.Tasks() {
		if task.URL == link {
			return task.AddedByAdmin
		}
	}
	t.Fatalf("%s was not staged", link)
	return false
}

func linkBody(link string) string {
	b, _ := json.Marshal(map[string]string{"links": link})
	return string(b)
}

// A token that may add and read, such as a friend's or an automation's, must
// not get KnightLoader to fetch what the owner's browser sessions open.
func TestOnlyAnAdminsLinksGoOutWithTheOwnersSessions(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, adder, err := a.APITokens.CreateScoped("friend", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("script", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	session := a.Auth.Issue()
	h := Handler(a)
	add := func(link string, sign func(*http.Request)) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(linkBody(link)))
		r.Host = "127.0.0.1:8080"
		r.Header.Set("Content-Type", "application/json")
		sign(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("adding %s = %d (%s)", link, w.Code, w.Body)
		}
	}
	bearer := func(secret string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
	}
	add("https://www.patreon.com/posts/by-a-friend-1", bearer(adder))
	add("https://www.patreon.com/posts/by-a-script-2", bearer(admin))
	add("https://www.patreon.com/posts/by-the-owner-3", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	})

	if addedByAdmin(t, a, "https://www.patreon.com/posts/by-a-friend-1") {
		t.Error("a link from a token that may only add and read may use the owner's sessions")
	}
	if !addedByAdmin(t, a, "https://www.patreon.com/posts/by-a-script-2") {
		t.Error("a link from an admin token may not use the owner's sessions")
	}
	if !addedByAdmin(t, a, "https://www.patreon.com/posts/by-the-owner-3") {
		t.Error("a link the signed-in owner added may not use the owner's sessions")
	}
}

// The owner's extension reads the sessions from the owner's own browser; a
// phone of the group holds no more than an add token's share.
func TestTheExtensionsLinksGoOutWithTheOwnersSessionsAndAPhonesDoNot(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
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

	if status, body := browser(http.MethodPost, "/api/links", linkBody("https://www.patreon.com/posts/from-the-browser-1")); status != http.StatusOK {
		t.Fatalf("the extension adding a link = %d (%s)", status, body)
	}
	if status, body := phone(http.MethodPost, "/api/links", linkBody("https://www.patreon.com/posts/from-the-phone-2")); status != http.StatusOK {
		t.Fatalf("the phone adding a link = %d (%s)", status, body)
	}
	if !addedByAdmin(t, a, "https://www.patreon.com/posts/from-the-browser-1") {
		t.Error("a link the owner's extension sent may not use the owner's sessions")
	}
	if addedByAdmin(t, a, "https://www.patreon.com/posts/from-the-phone-2") {
		t.Error("a link a phone of the group sent may use the owner's sessions")
	}
}
