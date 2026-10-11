package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// jsonRequest is an in-process request carrying body as JSON, the way every
// client of the API sends one.
func jsonRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// concretePath fills a route pattern's wildcards with a value no fixed route
// uses.
func concretePath(pattern string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(pattern, '{')
		if i < 0 {
			b.WriteString(pattern)
			return b.String()
		}
		j := strings.IndexByte(pattern[i:], '}')
		b.WriteString(pattern[:i] + "w1")
		pattern = pattern[i+j+1:]
	}
}

// stubbedMux attaches every registered route with a handler that only says it
// was reached, so a sweep over all of them changes nothing.
func stubbedMux(t *testing.T) (*http.ServeMux, []Route) {
	t.Helper()
	reg := buildRegistry(t)
	for i := range reg.routes {
		reg.routes[i].handler = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) }
	}
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	return mux, reg.Routes()
}

// TestEveryRouteThatChangesSomethingTakesOnlyJSON sweeps the whole table: a
// form on another website can only send urlencoded, multipart or text/plain,
// so a route that is not one of the listed exceptions turns those away before
// its handler runs. A route added later is covered without anybody listing it.
func TestEveryRouteThatChangesSomethingTakesOnlyJSON(t *testing.T) {
	t.Parallel()
	mux, routes := stubbedMux(t)
	checked := 0
	for _, rt := range routes {
		method := rt.Method
		if method == AnyMethod {
			method = http.MethodPost
		}
		if !changesState(method) {
			continue
		}
		path := concretePath(rt.Path)
		for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", ""} {
			r := httptest.NewRequest(method, path, strings.NewReader(`{"ids":["a"]}`))
			if ct != "" {
				r.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, r)
			_, exempt := otherBodies[rt.pattern()]
			switch {
			case exempt && rec.Code != 299:
				t.Errorf("%s %s with %q answered %d, want it handed to the route, which takes other bodies", method, path, ct, rec.Code)
			case !exempt && rec.Code != http.StatusUnsupportedMediaType:
				t.Errorf("%s %s with %q answered %d, want 415", method, path, ct, rec.Code)
			}
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonRequest(method, path, `{"ids":["a"]}`))
		if rec.Code != 299 {
			t.Errorf("%s %s with a JSON body answered %d, want it handed to the route", method, path, rec.Code)
		}
		checked++
	}
	if checked < 100 {
		t.Fatalf("checked %d routes that change something, which is too few to be the whole table", checked)
	}
}

// TestEveryExceptionNamesARoute keeps the exception list from outliving a
// route: an entry nothing matches would read as a decision that no longer
// exists.
func TestEveryExceptionNamesARoute(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, rt := range buildRegistry(t).Routes() {
		known[rt.pattern()] = true
	}
	for pattern := range otherBodies {
		if !known[pattern] {
			t.Errorf("otherBodies names %q, which is not a registered route", pattern)
		}
	}
}

func TestABodylessPostNeedsNoContentType(t *testing.T) {
	t.Parallel()
	mux, _ := stubbedMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queue/stop", nil))
	if rec.Code != 299 {
		t.Fatalf("a bare POST answered %d, want it handed to the route", rec.Code)
	}
}

func TestJSONWithACharsetIsJSON(t *testing.T) {
	t.Parallel()
	mux, _ := stubbedMux(t)
	r := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(`{"links":[]}`))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != 299 {
		t.Fatalf("application/json with a charset answered %d, want it handed to the route", rec.Code)
	}
}

// TestACrossSiteWriteIsRefusedEverywhere covers the routes the JSON rule
// leaves out as well: the browser marks a request another site started, and
// no page can change that mark.
func TestACrossSiteWriteIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.DownloadClientAPI = true
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.Create("sonarr")
	if err != nil {
		t.Fatal(err)
	}

	send := func(method, path, contentType, body, site string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	form := url.Values{"username": {"sonarr"}, "password": {secret}}.Encode()
	const formType = "application/x-www-form-urlencoded"

	for _, c := range []struct{ method, path, contentType, body string }{
		{http.MethodPost, "/api/queue/stop", "", ""},
		{http.MethodPost, "/api/links", "application/json", `{"links":["https://example.com/a.bin"]}`},
		{http.MethodPut, "/api/settings", "application/json", `{}`},
		{http.MethodPost, qbitPrefix + "auth/login", formType, form},
		{http.MethodPost, sabnzbdPath + "?mode=addfile&apikey=" + secret, "multipart/form-data; boundary=x", "--x--"},
	} {
		if code := send(c.method, c.path, c.contentType, c.body, "cross-site"); code != http.StatusForbidden {
			t.Errorf("a cross-site %s %s answered %d, want 403", c.method, c.path, code)
		}
	}

	if code := send(http.MethodPost, qbitPrefix+"auth/login", formType, form, ""); code != http.StatusOK {
		t.Errorf("Sonarr's form login answered %d, want 200: the bridge takes forms from servers", code)
	}
	for _, site := range []string{"same-origin", "same-site", "none"} {
		if code := send(http.MethodPost, qbitPrefix+"auth/login", formType, form, site); code != http.StatusOK {
			t.Errorf("a form login marked %s answered %d, want 200", site, code)
		}
	}
	if code := send(http.MethodGet, "/api/health", "", "", "cross-site"); code != http.StatusOK {
		t.Errorf("a cross-site GET answered %d, want 200: another site cannot read the answer", code)
	}
}
