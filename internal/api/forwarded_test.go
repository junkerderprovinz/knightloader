package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The trusted proxies show on the Security page once a request has come
// through a reverse proxy, and not on an instance nobody reaches that way.
func TestTheAuthStateSaysWhetherRequestsComeThroughAProxy(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	proxied := func(header string) bool {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth", nil)
		if header != "" {
			req.Header.Set(header, "203.0.113.7")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var state struct{ Proxied bool }
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			t.Fatal(err)
		}
		return state.Proxied
	}

	if proxied("") {
		t.Fatal("a direct request reads as proxied")
	}
	if !proxied("X-Forwarded-For") {
		t.Fatal("a request with X-Forwarded-For does not read as proxied")
	}
	if !proxied("") {
		t.Fatal("the proxy is forgotten by the next direct request")
	}
}
