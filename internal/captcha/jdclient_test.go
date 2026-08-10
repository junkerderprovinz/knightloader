package captcha

// jdClient against a real httptest.Server - pinning the wire shapes this
// file's own package comment documents as verified live, so a future edit
// that "simplifies" the query encoding or stops checking JD's error envelope
// fails loudly here instead of only in production against a real sidecar.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// params decodes the bare, unnamed, JSON-per-segment query string call()
// builds (see jdClient.call's own doc comment) back into plain strings, in
// the order they were sent, so a test can assert on them positionally
// without caring about JSON's exact whitespace.
func params(t *testing.T, rawQuery string) []string {
	t.Helper()
	if rawQuery == "" {
		return nil
	}
	segs := strings.Split(rawQuery, "&")
	out := make([]string, len(segs))
	for i, s := range segs {
		v, err := url.QueryUnescape(s)
		if err != nil {
			t.Fatalf("query segment %d (%q) does not unescape: %v", i, s, err)
		}
		out[i] = v
	}
	return out
}

func TestJDClientListParsesTheEnvelope(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":1,"hoster":"rapidgator.net","link":555,"type":"BasicCaptchaChallenge","challengeType":"BasicCaptchaChallenge","captchaCategory":"rapidgator.net","explain":"type what you see","timeout":120000,"created":1000,"remaining":60000}]}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	jobs, err := c.list(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if gotPath != "/captcha/list" {
		t.Errorf("path = %q, want /captcha/list", gotPath)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	got := jobs[0]
	if got.ID != 1 || got.Hoster != "rapidgator.net" || got.Link != 555 ||
		got.ChallengeType != "BasicCaptchaChallenge" || got.Remaining != 60000 {
		t.Errorf("list()[0] = %+v, want the fields from the live-verified envelope", got)
	}
}

func TestJDClientListEmptyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	jobs, err := c.list(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("len(jobs) = %d, want 0", len(jobs))
	}
}

// TestJDClientImageSendsOneParam pins captcha/get's no-format shape: one
// bare id, and a plain JSON string back - see this package's jdsource.go
// comment on why that is JSON on a modern JD, not the raw bytes the
// deprecated example describes.
func TestJDClientImageSendsOneParam(t *testing.T) {
	var gotPath string
	var gotParams []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotParams = params(t, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"data":"image/png;base64,AAAA"}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	got, err := c.image(context.Background(), 42)
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if got != "image/png;base64,AAAA" {
		t.Errorf("image() = %q, want the bare data string unmarshalled from the envelope", got)
	}
	if gotPath != "/captcha/get" {
		t.Errorf("path = %q, want /captcha/get", gotPath)
	}
	if len(gotParams) != 1 || gotParams[0] != "42" {
		t.Errorf("params = %v, want exactly [42] - no format means the image fallback, not rawtoken", gotParams)
	}
}

// TestJDClientWidgetTokenSendsRawTokenFormat is the load-bearing one: without
// the second "rawtoken" parameter, a live JD answers a widget challenge's
// get() with an image fallback instead of sitekey data - see jdsource.go's
// package comment.
func TestJDClientWidgetTokenSendsRawTokenFormat(t *testing.T) {
	var gotParams []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParams = params(t, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"data":{"siteKey":"6Lc-key","siteUrl":"https://hoster.example/dl","contextUrl":"https://hoster.example","type":"normal","enterprise":false,"stoken":"","v3Action":""}}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	tok, err := c.widgetToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("widgetToken: %v", err)
	}
	if len(gotParams) != 2 || gotParams[0] != "7" || gotParams[1] != `"rawtoken"` {
		t.Fatalf("params = %v, want [7 \"rawtoken\"]", gotParams)
	}
	if tok.SiteKey != "6Lc-key" || tok.SiteURL != "https://hoster.example/dl" {
		t.Errorf("widgetToken() = %+v, want the sitekey/siteUrl decoded", tok)
	}
}

// TestJDClientSolveSendsRawTokenFormatAndSucceeds pins that solve always
// asks for rawtoken - safe for every challenge family per jdsource.go's
// package comment - and that JD's own returned boolean is what comes back,
// not an assumption of true.
func TestJDClientSolveSendsRawTokenFormatAndSucceeds(t *testing.T) {
	var gotPath string
	var gotParams []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotParams = params(t, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"data":true}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	ok, err := c.solve(context.Background(), 7, "hunter2")
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if !ok {
		t.Error("solve() ok = false, want true for a 200/true response")
	}
	if gotPath != "/captcha/solve" {
		t.Errorf("path = %q, want /captcha/solve", gotPath)
	}
	if len(gotParams) != 3 || gotParams[0] != "7" || gotParams[1] != `"hunter2"` || gotParams[2] != `"rawtoken"` {
		t.Fatalf(`params = %v, want [7 "hunter2" "rawtoken"]`, gotParams)
	}
}

// TestJDClientSolveDetectsNotAvailable replays the exact error envelope a
// live sidecar answered with for a gone/expired id (see jdsource.go's
// package comment) and pins that isNotAvailable recognises it.
func TestJDClientSolveDetectsNotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"src":"DEVICE","data":null,"type":"NOT_AVAILABLE"}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	_, err := c.solve(context.Background(), 999999999, "too-late")
	if err == nil {
		t.Fatal("solve against a gone id returned no error")
	}
	if !isNotAvailable(err) {
		t.Errorf("isNotAvailable(%v) = false, want true for JD's own NOT_AVAILABLE envelope", err)
	}
}

// TestJDClientSkipSendsTheScopeValue pins skip's real shape - id plus a
// SkipRequest name, not the abort/blockType route the deprecated example
// documents (see jdsource.go's package comment: that route does not exist on
// a modern JD).
func TestJDClientSkipSendsTheScopeValue(t *testing.T) {
	var gotPath string
	var gotParams []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotParams = params(t, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"data":true}`))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	if err := c.skip(context.Background(), 3, jdSkipBlockHoster); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if gotPath != "/captcha/skip" {
		t.Errorf("path = %q, want /captcha/skip", gotPath)
	}
	if len(gotParams) != 2 || gotParams[0] != "3" || gotParams[1] != `"BLOCK_HOSTER"` {
		t.Fatalf(`params = %v, want [3 "BLOCK_HOSTER"]`, gotParams)
	}
}

// TestJDClientCallSurfacesAnUnparsableErrorBody covers the transport-error
// path a bare 500 with no JSON body takes - jdAPIError still has to produce
// a usable error string instead of hiding behind a JSON decode failure.
func TestJDClientCallSurfacesAnUnparsableErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := newJDClient(srv.URL)
	_, err := c.list(context.Background())
	if err == nil {
		t.Fatal("list against a failing server returned no error")
	}
	if isNotAvailable(err) {
		t.Error("isNotAvailable(err) = true, want false - this was never JD's NOT_AVAILABLE shape")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to name the HTTP status", err)
	}
}
