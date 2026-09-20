package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// captchaWidgetServer is the widget route alone on a throwaway app.
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

// getCaptchaWidget is getRaw plus the response, since these tests assert on
// headers too.
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

// TestCaptchaWidgetRequiresSiteKey checks that a request without a siteKey is
// a 400, the one client error this route has.
func TestCaptchaWidgetRequiresSiteKey(t *testing.T) {
	srv := captchaWidgetServer(t)
	resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "1", url.Values{}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET with no siteKey answered %d, want %d: %s", resp.StatusCode, http.StatusBadRequest, body)
	}
}

// TestCaptchaWidgetRecaptchaSignalsRenderTheVendorScript checks that each of
// the three signals that prove reCAPTCHA renders the live page, with a CSP
// scoped to Google's origins.
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

// TestCaptchaWidgetAmbiguousSignalsDegradeHonestly checks that a request that
// could be either vendor gets the page without scripts.
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

// TestCaptchaWidgetEscapesUntrustedFields checks that the fields JD relays from
// a hoster's page cannot break out of their HTML or script context.
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

// TestCaptchaWidgetSetsDefensiveHeaders pins the headers set beside the CSP.
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

// TestCaptchaWidgetNonceDiffersPerResponse checks that the nonce is not reused;
// a fixed one would be no better than 'unsafe-inline'.
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

// TestCaptchaWidgetCSPAppliesOnlyToThisRoute runs against the full
// registration table, so a shared header middleware that widened the policy's
// reach would fail here.
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

// assertNoBareWildcard fails on a source that is exactly "*", which allows any
// origin, unlike a named domain pattern such as *.hcaptcha.com.
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
