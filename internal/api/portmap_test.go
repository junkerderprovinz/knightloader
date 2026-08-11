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
// Only validation is exercised at this layer, deliberately: what a
// well-formed request actually does - discovery, the SOAP calls, the
// three-way honest outcome - is internal/portmap's own job and has its own,
// thoroughly injected test suite, the same split reconnect's MethodUPnP
// already has (no test in this package exercises real SSDP either). A test
// here that let a request reach portmap.AttemptPort for real would send a
// multicast search from whatever machine runs `go test`, which upnp.go's
// own Discoverer doc comment already explains a sandboxed runner may not
// even be allowed to do.
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

// TestPortmapRefusesAnOutOfRangePort names the field rather than letting
// portmap.Attempt's own generic validation message reach the caller - see
// routes_portmap.go's own comment on why the check is duplicated here. The
// field is "port", matching web/src/pages/settings/Torrents.tsx's own
// request body ({ port }) rather than portmap.Request's internal/external
// split - this route asks for one number and maps it on both protocols.
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
