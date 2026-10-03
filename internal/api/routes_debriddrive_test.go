package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
)

// propfind lists the drive's root with the given credential and returns the
// status and the body.
func propfind(t *testing.T, srv *httptest.Server, path string, auth func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest("PROPFIND", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Depth", "1")
	if auth != nil {
		auth(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestTheDebridDriveOpensOnlyToATokenThatCanRead(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	_, reader, err := a.APITokens.CreateScoped("rclone", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, adder, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}
	bearer := func(s string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+s) }
	}

	if code, _ := propfind(t, srv, "/dav/", bearer(reader)); code != http.StatusNotFound {
		t.Errorf("the drive answered %d while switched off, want 404", code)
	}
	if err := setFeature(a, "debriddrive", true); err != nil {
		t.Fatal(err)
	}
	if !a.Settings.Get().DebridDrive.Enabled {
		t.Fatal("the switch did not reach the flag the route reads")
	}

	if code, _ := propfind(t, srv, "/dav/", nil); code != http.StatusUnauthorized {
		t.Errorf("the drive answered %d to no credential, want 401", code)
	}
	if code, _ := propfind(t, srv, "/dav/", bearer("not-a-token")); code != http.StatusUnauthorized {
		t.Errorf("the drive answered %d to a wrong token, want 401", code)
	}
	if code, _ := propfind(t, srv, "/dav/", bearer(adder)); code != http.StatusForbidden {
		t.Errorf("the drive answered %d to a token that cannot read, want 403", code)
	}
	if code, _ := propfind(t, srv, "/dav/", bearer(reader)); code != http.StatusMultiStatus {
		t.Errorf("the drive answered %d to a Bearer token that can read, want 207", code)
	}
	basic := func(r *http.Request) { r.SetBasicAuth("anything", reader) }
	if code, _ := propfind(t, srv, "/dav/", basic); code != http.StatusMultiStatus {
		t.Errorf("the drive answered %d to the token as a Basic password, want 207", code)
	}
}

func TestTheDebridDriveRefusesWritesThroughTheRouteTable(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	_, reader, err := a.APITokens.CreateScoped("rclone", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	methods := []string{http.MethodPut, http.MethodDelete, http.MethodPost, http.MethodPatch, "MKCOL", "MOVE", "COPY",
		"LOCK", "UNLOCK", "PROPPATCH", "TRACE"}
	send := func(method string, auth bool) int {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+"/dav/somefile.txt", nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth {
			req.Header.Set("Authorization", "Bearer "+reader)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	for _, m := range methods {
		if code := send(m, true); code != http.StatusNotFound {
			t.Errorf("%s answered %d while the drive is off, want 404", m, code)
		}
	}
	if err := setFeature(a, "debriddrive", true); err != nil {
		t.Fatal(err)
	}
	for _, m := range methods {
		if code := send(m, false); code != http.StatusUnauthorized {
			t.Errorf("%s answered %d to no credential, want 401", m, code)
		}
		if code := send(m, true); code != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d, want 405", m, code)
		}
	}
}

func TestTheDebridDriveNamesItsEntriesUnderTheBasePath(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	_, reader, err := a.APITokens.CreateScoped("rclone", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	if err := setFeature(a, "debriddrive", true); err != nil {
		t.Fatal(err)
	}

	code, body := propfind(t, srv, "/kl/dav/", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+reader)
		r.Header.Set("X-Forwarded-Prefix", "/kl")
	})
	if code != http.StatusMultiStatus {
		t.Fatalf("the drive under a base path answered %d", code)
	}
	if !strings.Contains(body, "<D:href>/kl/dav/</D:href>") {
		t.Errorf("the listing does not name the root as the client asked for it:\n%s", body)
	}
}

func TestTheDebridDriveRowSaysWhatIsMissing(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	detail := func() string {
		t.Helper()
		for _, m := range featureList(a, "") {
			if m.ID == "debriddrive" {
				return m.DetailCode
			}
		}
		t.Fatal("the module registry has no debriddrive row")
		return ""
	}
	if got := detail(); got != "debriddriveOff" {
		t.Errorf("a fresh instance's row says %q, want debriddriveOff", got)
	}
	if err := setFeature(a, "debriddrive", true); err != nil {
		t.Fatal(err)
	}
	if got := detail(); got != "debriddriveNoToken" {
		t.Errorf("with no token the row says %q, want debriddriveNoToken", got)
	}
	if _, _, err := a.APITokens.CreateScoped("rclone", []apitoken.Scope{apitoken.ScopeRead}); err != nil {
		t.Fatal(err)
	}
	if got := detail(); got != "debriddriveNoAccount" {
		t.Errorf("with no debrid account the row says %q, want debriddriveNoAccount", got)
	}
	if err := setFeature(a, "debriddrive", false); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Get().DebridDrive.Enabled {
		t.Error("switching the module off left the drive open")
	}
}
