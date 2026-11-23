package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/status"
	"github.com/junkerderprovinz/parleyport/kit/statustest"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// fortressApp is an instance with a password, one failed download and two
// finished ones in its log.
func fortressApp(t *testing.T) (http.Handler, *app.App) {
	t.Helper()
	srv, a, _ := seedFailed(t, core.ReasonUnknown)
	srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	// Dated as they happen: the queue's idle event, which the instance logs a
	// moment after it starts, would otherwise land between them out of order.
	for _, name := range []string{"first.mkv", "second.mkv"} {
		a.Events.Publish(script.Firing{Trigger: script.TriggerTaskDone, At: time.Now(), Task: &script.TaskView{Name: name}})
	}
	return Handler(a), a
}

func tokenWith(t *testing.T, a *app.App, scopes ...apitoken.Scope) string {
	t.Helper()
	_, secret, err := a.APITokens.CreateScoped("dashboard", scopes)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestStatusContract(t *testing.T) {
	t.Parallel()
	h, a := fortressApp(t)
	secret := tokenWith(t, a, apitoken.ScopeStatus)
	statustest.Run(t, h, statustest.Options{Authenticate: func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+secret)
	}})
}

func TestFortressStatusTakesTheRightCredentials(t *testing.T) {
	t.Parallel()
	h, a := fortressApp(t)
	cases := []struct {
		name   string
		scopes []apitoken.Scope
		want   int
	}{
		{"status", []apitoken.Scope{apitoken.ScopeStatus}, http.StatusOK},
		{"read", []apitoken.Scope{apitoken.ScopeRead}, http.StatusOK},
		{"add only", []apitoken.Scope{apitoken.ScopeAdd}, http.StatusForbidden},
		{"admin only", []apitoken.Scope{apitoken.ScopeAdmin}, http.StatusForbidden},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, status.StatusPath, nil)
		r.Header.Set("Authorization", "Bearer "+tokenWith(t, a, c.scopes...))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("a %s token: %d, want %d", c.name, w.Code, c.want)
		}
	}
}

func TestFortressStatusIsClosedWithoutAPassword(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	h := Handler(a)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+status.StatusPath, nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no credentials on an instance without a password: %d, want 401", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+status.StatusPath, nil)
	r.Header.Set("Authorization", "Bearer "+tokenWith(t, a, apitoken.ScopeStatus))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("a status token on an instance without a password: %d, want 200", w.Code)
	}
}

func TestAStatusTokenOpensNothingElse(t *testing.T) {
	t.Parallel()
	h, a := fortressApp(t)
	secret := tokenWith(t, a, apitoken.ScopeStatus)
	for _, path := range []string{"/api/tasks", "/api/health/detail", "/api/settings"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s with a status token: %d, want 403", path, w.Code)
		}
	}
}
