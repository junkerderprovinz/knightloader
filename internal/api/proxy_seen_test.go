package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTheSecurityPageLearnsAProxyIsInFront: /api/auth says behindProxy once a
// request arrived with a forwarding header, and not before.
func TestTheSecurityPageLearnsAProxyIsInFront(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	behind := func() bool {
		t.Helper()
		var auth struct {
			BehindProxy *bool `json:"behindProxy"`
		}
		if code := getJSON(t, srv.URL+"/api/auth", &auth); code != http.StatusOK || auth.BehindProxy == nil {
			t.Fatalf("/api/auth answered %d without behindProxy", code)
		}
		return *auth.BehindProxy
	}
	if behind() {
		t.Fatal("behindProxy before any request came through a proxy")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !behind() {
		t.Fatal("behindProxy still false after a forwarded request")
	}
}

func TestAProxyLastSeenADayAgoNoLongerCounts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seen := &proxySeen{now: func() time.Time { return now }}
	h := seen.note(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, header := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"} {
		seen.last.Store(0)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(header, "for=203.0.113.7")
		h.ServeHTTP(httptest.NewRecorder(), r)
		if !seen.recently() {
			t.Errorf("a request with %s did not count", header)
		}
	}

	now = now.Add(proxyRecent)
	if seen.recently() {
		t.Error("a proxy last seen a day ago still counts")
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen.recently() {
		t.Error("a request without a forwarding header counted")
	}
}
