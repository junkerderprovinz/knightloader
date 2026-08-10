package api

// Note on running these: as this file was written, the internal/api package
// does not compile at all - routes_captcha_skip.go (7D, this same wave)
// already calls a.AbortCaptcha, which lands in 7A's app_captcha.go and had
// not yet, since 7A/7B/7C/7D all run in parallel (see
// routes_captcha_widget.go's own "THE REQUEST CONTRACT" for where that is
// confirmed in the working tree). That is a package-wide compile error, so no
// test in this package - not only these - can run until 7A lands, regardless
// of which route it exercises. These tests are written to the same
// conventions as their siblings (folders_test.go, reconnect_test.go) and
// their logic - the templates, the CSP strings and the vendor-identification
// matrix - was verified in an isolated scratch program outside this module
// before this file was written, precisely because this package could not
// build to prove it directly.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// captchaWidgetServer is the widget route alone on a throwaway app. The route
// itself never reads the app (see routes_captcha_widget.go's own "THE
// REQUEST CONTRACT" for why), so a is unused here - kept only to match every
// sibling route test's setup shape in this package.
func captchaWidgetServer(t *testing.T) *httptest.Server {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerCaptchaWidget(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func captchaWidgetURL(srv *httptest.Server, id string, params url.Values) string {
	u := srv.URL + "/api/captcha/" + url.PathEscape(id) + "/widget"
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

// getCaptchaWidget is getRaw (reconnect_test.go) plus the response itself,
// because these tests assert on headers - the Content-Security-Policy above
// all - not only on the body.
func getCaptchaWidget(t *testing.T, u string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// TestCaptchaWidgetRequiresSiteKey pins the one real client error this route
// has: everything else about an unrecognised request degrades honestly
// (see TestCaptchaWidgetAmbiguousSignalsDegradeHonestly), but a request with
// no siteKey at all has nothing to render and is a 400, not a 200 with an
// empty widget.
func TestCaptchaWidgetRequiresSiteKey(t *testing.T) {
	srv := captchaWidgetServer(t)
	resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "1", url.Values{}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET with no siteKey answered %d, want %d: %s", resp.StatusCode, http.StatusBadRequest, body)
	}
}

// TestCaptchaWidgetRecaptchaSignalsRenderTheVendorScript is the decision
// matrix from the file's own "THE SIGNAL THAT DOES DISAMBIGUATE", exercised
// over real HTTP: each of the three independent, vendor-exclusive signals
// must render the same live reCAPTCHA page, with a CSP scoped to Google's own
// origins and no other vendor mentioned.
func TestCaptchaWidgetRecaptchaSignalsRenderTheVendorScript(t *testing.T) {
	cases := []struct {
		name   string
		params url.Values
	}{
		{"enterprise", url.Values{"siteKey": {"6Lc-key"}, "enterprise": {"1"}}},
		{"enterprise-true-spelling", url.Values{"siteKey": {"6Lc-key"}, "enterprise": {"true"}}},
		{"v3Action", url.Values{"siteKey": {"6Lc-key"}, "v3Action": {"download"}}},
		{"invisible", url.Values{"siteKey": {"6Lc-key"}, "type": {"invisible"}}},
		{"invisible-mixed-case", url.Values{"siteKey": {"6Lc-key"}, "type": {"Invisible"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := captchaWidgetServer(t)
			resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "42", c.params))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("answered %d: %s", resp.StatusCode, body)
			}
			html := string(body)

			if !strings.Contains(html, `class="g-recaptcha"`) {
				t.Error("body does not embed a g-recaptcha widget div")
			}
			if !strings.Contains(html, `data-sitekey="6Lc-key"`) {
				t.Error("body does not carry the sitekey through to the widget")
			}
			if !strings.Contains(html, "https://www.google.com/recaptcha/api.js") {
				t.Error("body does not load the reCAPTCHA vendor script")
			}
			if strings.Contains(html, "hcaptcha") {
				t.Error("a reCAPTCHA render must never mention hCaptcha")
			}

			csp := resp.Header.Get("Content-Security-Policy")
			if csp == "" {
				t.Fatal("no Content-Security-Policy header on a rendered widget page")
			}
			mustContainAll(t, csp, []string{
				"default-src 'none'",
				"https://www.google.com/recaptcha/",
				"https://www.gstatic.com/recaptcha/",
				"frame-ancestors 'self'",
			})
			if strings.Contains(csp, "unsafe-inline") {
				t.Errorf("CSP must not fall back to unsafe-inline: %s", csp)
			}
			assertNoBareWildcard(t, csp)
		})
	}
}

// TestCaptchaWidgetAmbiguousSignalsDegradeHonestly is the other half of the
// matrix: a request that does NOT prove reCAPTCHA - because it could equally
// be hCaptcha, or an ordinary reCAPTCHA v2 "normal" checkbox, and
// isUnambiguouslyRecaptcha does not guess between them - must answer with the
// honest no-script page, never a guess dressed up as a render.
func TestCaptchaWidgetAmbiguousSignalsDegradeHonestly(t *testing.T) {
	cases := []struct {
		name   string
		params url.Values
	}{
		{"no-signal-at-all", url.Values{"siteKey": {"anykey"}}},
		{"explicit-normal", url.Values{"siteKey": {"anykey"}, "type": {"normal"}}},
		{"explicit-false-enterprise", url.Values{"siteKey": {"anykey"}, "enterprise": {"false"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := captchaWidgetServer(t)
			resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "7", c.params))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("answered %d: %s", resp.StatusCode, body)
			}
			html := string(body)

			if strings.Contains(html, "<script") {
				t.Errorf("an unidentified-vendor page must load no script at all, got: %s", html)
			}
			if strings.Contains(html, "g-recaptcha") || strings.Contains(html, "h-captcha") {
				t.Error("an unidentified-vendor page must not embed either vendor's widget div")
			}
			if !strings.Contains(html, "cannot be shown") {
				t.Error("an unidentified-vendor page must say plainly that it cannot render this challenge")
			}

			csp := resp.Header.Get("Content-Security-Policy")
			if csp == "" {
				t.Fatal("no Content-Security-Policy header on the fallback page")
			}
			if !strings.Contains(csp, "default-src 'none'") {
				t.Errorf("the fallback page's CSP must default-deny, got: %s", csp)
			}
			for _, forbidden := range []string{"google", "gstatic", "hcaptcha", "recaptcha"} {
				if strings.Contains(strings.ToLower(csp), forbidden) {
					t.Errorf("the fallback CSP must name no vendor origin at all, got %q in: %s", forbidden, csp)
				}
			}
			assertNoBareWildcard(t, csp)
		})
	}
}

// TestCaptchaWidgetEscapesUntrustedFields is the concrete regression test for
// the reasoning in this route's own doc comment: SiteKey and friends are
// whatever JD relayed from a hoster's page, untrusted, and must never be able
// to break out of the HTML attribute or inline-script context they are
// placed in.
func TestCaptchaWidgetEscapesUntrustedFields(t *testing.T) {
	srv := captchaWidgetServer(t)
	xss := `"><script>alert(1)</script>`
	params := url.Values{
		"siteKey": {xss}, "enterprise": {"1"}, "host": {xss}, "prompt": {xss}, "secureToken": {xss},
	}
	resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, xss, params))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answered %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "<script>alert(1)</script>") {
		t.Fatalf("an untrusted field's raw payload appears unescaped in the response body:\n%s", body)
	}
}

// TestCaptchaWidgetSetsDefensiveHeaders pins the response headers this file's
// handler sets beside the CSP itself - a nosniff'd, uncached, explicitly
// text/html response, matching routes_containers.go's own precedent for a
// route whose entire body is untrusted-adjacent content.
func TestCaptchaWidgetSetsDefensiveHeaders(t *testing.T) {
	srv := captchaWidgetServer(t)
	resp, _ := getCaptchaWidget(t, captchaWidgetURL(srv, "1", url.Values{"siteKey": {"k"}, "enterprise": {"1"}}))

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// TestCaptchaWidgetNonceDiffersPerResponse guards against a nonce that was
// accidentally hoisted to a package-level constant or otherwise reused - a
// fixed nonce defeats the entire point of using one instead of
// 'unsafe-inline'.
func TestCaptchaWidgetNonceDiffersPerResponse(t *testing.T) {
	srv := captchaWidgetServer(t)
	u := captchaWidgetURL(srv, "1", url.Values{"siteKey": {"k"}, "enterprise": {"1"}})
	resp1, _ := getCaptchaWidget(t, u)
	resp2, _ := getCaptchaWidget(t, u)

	csp1, csp2 := resp1.Header.Get("Content-Security-Policy"), resp2.Header.Get("Content-Security-Policy")
	if csp1 == "" || csp2 == "" {
		t.Fatal("missing CSP header on one of the two responses")
	}
	if csp1 == csp2 {
		t.Errorf("two separate responses carried an identical CSP (same nonce): %s", csp1)
	}
}

// TestCaptchaWidgetCSPAppliesOnlyToThisRoute is this assignment's own central
// requirement, pinned directly against the full, real registration table -
// not just this route's own isolated test server - so a future change adding
// a shared "security headers" middleware in api.go and accidentally widening
// this policy's reach would fail here first.
func TestCaptchaWidgetCSPAppliesOnlyToThisRoute(t *testing.T) {
	reg := buildRegistry(t)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	widget, _ := getCaptchaWidget(t, srv.URL+"/api/captcha/1/widget?siteKey=k&enterprise=1")
	if widget.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("the widget route itself carries no Content-Security-Policy header")
	}

	health, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if csp := health.Header.Get("Content-Security-Policy"); csp != "" {
		t.Errorf("GET /api/health carries a Content-Security-Policy header (%q); "+
			"it must be scoped to the widget route alone", csp)
	}
}

func mustContainAll(t *testing.T, s string, subs []string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("expected %q to contain %q", s, sub)
		}
	}
}

// assertNoBareWildcard is the mechanical half of "never a wildcard": a
// source-expression token that is exactly "*" would allow any origin
// whatsoever, which is a different thing from hCaptcha's own documented
// *.hcaptcha.com (a single named registrable domain) - a pattern this file's
// recaptcha branch never even uses. See routes_captcha_widget.go's own "THE
// CSP ITSELF" for that distinction spelled out in full.
func assertNoBareWildcard(t *testing.T, csp string) {
	t.Helper()
	for _, directive := range strings.Split(csp, ";") {
		for _, tok := range strings.Fields(directive) {
			if tok == "*" {
				t.Errorf("CSP directive %q contains a bare wildcard source", strings.TrimSpace(directive))
			}
		}
	}
}
