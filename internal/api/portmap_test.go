package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// portmapServer is the portmap route on a throwaway app.
//
// Only validation is exercised here. Discovery, the SOAP calls and the
// three-way outcome are internal/portmap's own tests, the same split
// reconnect's MethodUPnP has. Letting a request reach portmap.AttemptPort
// would send a multicast search from whatever machine runs the suite, which a
// sandboxed runner may not be allowed to do.
func portmapServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerPortmap(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

const portmapPath = "/api/torrents/portmap"

// TestPortmapRefusesBadJSON is the same first line of defence every other
// POST route in this package has, through the shared decodeJSON helper.
func TestPortmapRefusesBadJSON(t *testing.T) {
	_, srv := portmapServer(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+portmapPath, strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST %s with a bad body answered %d, want 400", portmapPath, resp.StatusCode)
	}
}

// TestPortmapRefusesAnOutOfRangePort: the refusal names the field rather than
// passing on portmap.Attempt's generic message. The field is "port", matching
// web/src/pages/settings/Torrents.tsx's request body, not portmap.Request's
// internal and external split; this route takes one number for both protocols.
func TestPortmapRefusesAnOutOfRangePort(t *testing.T) {
	_, srv := portmapServer(t)
	for _, port := range []int{0, -1, 65536} {
		code, raw := postJSON(t, http.MethodPost, srv.URL+portmapPath, map[string]int{"port": port})
		if code != http.StatusBadRequest {
			t.Errorf("port=%d answered %d, want 400: %s", port, code, raw)
		}
		if !strings.Contains(string(raw), "port") {
			t.Errorf("port=%d: the refusal does not name the field: %s", port, raw)
		}
	}
}
