package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
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

var (
	renderedGlobal = regexp.MustCompile(`var api = window\[("[^"]*")\];`)
	renderedParams = regexp.MustCompile(`var params = (\{[^\n]*\});`)
	renderedScript = regexp.MustCompile(`script\.src = ("[^"]*");`)
)

// renderedWidget is what a widget page asks the browser to load and render,
// read back out of the page's script.
type renderedWidget struct {
	global string
	script *url.URL
	params map[string]string
}

func parseRenderedWidget(t *testing.T, html string) renderedWidget {
	t.Helper()
	jsValue := func(re *regexp.Regexp, into any) {
		t.Helper()
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("the page has no match for %s:\n%s", re, html)
		}
		if err := json.Unmarshal([]byte(m[1]), into); err != nil {
			t.Fatalf("%s: %v", m[1], err)
		}
	}
	var w renderedWidget
	var src string
	jsValue(renderedGlobal, &w.global)
	jsValue(renderedParams, &w.params)
	jsValue(renderedScript, &src)
	u, err := url.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	w.script = u
	return w
}

// cspDirectives splits a policy into its directives' source lists.
func cspDirectives(csp string) map[string][]string {
	out := map[string][]string{}
	for _, d := range strings.Split(csp, ";") {
		f := strings.Fields(d)
		if len(f) > 0 {
			out[f[0]] = f[1:]
		}
	}
	return out
}

// Every way a request names reCAPTCHA renders Google's script, with a CSP
// scoped to Google's origins.
func TestCaptchaWidgetRendersRecaptchaFromGoogle(t *testing.T) {
	cases := []struct {
		name   string
		params url.Values
	}{
		{"vendor", url.Values{"vendor": {"recaptcha"}, "siteKey": {"6Lc-key"}, "type": {"NORMAL"}}},
		{"enterprise", url.Values{"siteKey": {"6Lc-key"}, "enterprise": {"1"}}},
		{"enterprise-true-spelling", url.Values{"siteKey": {"6Lc-key"}, "enterprise": {"true"}}},
		{"v3Action", url.Values{"siteKey": {"6Lc-key"}, "v3Action": {"download"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := captchaWidgetServer(t)
			resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "42", c.params))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("answered %d: %s", resp.StatusCode, body)
			}
			html := string(body)

			w := parseRenderedWidget(t, html)
			if w.global != "grecaptcha" {
				t.Errorf("renders through window[%q], want grecaptcha", w.global)
			}
			if w.params["sitekey"] != "6Lc-key" {
				t.Errorf("render params = %v, want the sitekey carried through", w.params)
			}
			if got := w.script.Scheme + "://" + w.script.Host + w.script.Path; got != "https://www.google.com/recaptcha/api.js" {
				t.Errorf("loads %s, want the reCAPTCHA script", got)
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

// An invisible reCAPTCHA key only works rendered invisible and started by the
// page; JD spells the size in capitals.
func TestCaptchaWidgetRendersAnInvisibleRecaptchaInvisible(t *testing.T) {
	srv := captchaWidgetServer(t)
	_, body := getCaptchaWidget(t, captchaWidgetURL(srv, "42", url.Values{
		"vendor": {"recaptcha"}, "siteKey": {"6Lc-key"}, "type": {"INVISIBLE"},
	}))
	html := string(body)
	if w := parseRenderedWidget(t, html); w.params["size"] != "invisible" {
		t.Errorf("render params = %v, want size invisible", w.params)
	}
	if !strings.Contains(html, `if (params.size === "invisible") api.execute(widget);`) {
		t.Error("the page never starts an invisible widget, which then shows nothing to solve")
	}
}

// An hCaptcha loads hCaptcha's own script, under a policy that names
// hCaptcha's documented hosts for scripts, styles, frames and connections and
// nothing of Google's.
func TestCaptchaWidgetRendersHCaptchaFromItsOwnHosts(t *testing.T) {
	for _, size := range []string{"NORMAL", "INVISIBLE"} {
		t.Run(size, func(t *testing.T) {
			srv := captchaWidgetServer(t)
			resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, "42", url.Values{
				"vendor": {"hcaptcha"}, "siteKey": {"10000000-ffff-ffff-ffff-000000000001"}, "type": {size},
			}))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("answered %d: %s", resp.StatusCode, body)
			}
			html := string(body)

			w := parseRenderedWidget(t, html)
			if w.global != "hcaptcha" {
				t.Errorf("renders through window[%q], want hcaptcha", w.global)
			}
			// The checkbox either way, so a challenge the user closes can be
			// opened again.
			want := map[string]string{"sitekey": "10000000-ffff-ffff-ffff-000000000001"}
			if len(w.params) != len(want) || w.params["sitekey"] != want["sitekey"] {
				t.Errorf("render params = %v, want %v", w.params, want)
			}
			if got := w.script.Scheme + "://" + w.script.Host + w.script.Path; got != "https://js.hcaptcha.com/1/api.js" {
				t.Errorf("loads %s, want hCaptcha's script", got)
			}
			if q := w.script.Query(); q.Get("render") != "explicit" || q.Get("onload") != "klWidgetLoaded" {
				t.Errorf("script query = %v, want explicit rendering from klWidgetLoaded", q)
			}
			for _, other := range []string{"google", "gstatic", "grecaptcha"} {
				if strings.Contains(html, other) {
					t.Errorf("an hCaptcha page mentions %q", other)
				}
			}

			csp := resp.Header.Get("Content-Security-Policy")
			d := cspDirectives(csp)
			if got := strings.Join(d["default-src"], " "); got != "'none'" {
				t.Errorf("default-src = %q, want 'none'", got)
			}
			for _, name := range []string{"script-src", "style-src", "frame-src", "connect-src"} {
				sources := strings.Join(d[name], " ")
				if !strings.Contains(sources, "https://hcaptcha.com") || !strings.Contains(sources, "https://*.hcaptcha.com") {
					t.Errorf("%s = %q, want both of hCaptcha's hosts", name, sources)
				}
			}
			if got := strings.Join(d["frame-ancestors"], " "); got != "'self'" {
				t.Errorf("frame-ancestors = %q, want 'self'", got)
			}
			for _, forbidden := range []string{"google", "gstatic", "unsafe-inline", "unsafe-eval"} {
				if strings.Contains(csp, forbidden) {
					t.Errorf("the hCaptcha CSP contains %q: %s", forbidden, csp)
				}
			}
			if !strings.Contains(strings.Join(d["script-src"], " "), "'nonce-") {
				t.Errorf("script-src carries no nonce for the page's own script: %s", csp)
			}
			assertNoBareWildcard(t, csp)
		})
	}
}

// The page tells the parent when the vendor's script fails or never calls
// back, instead of leaving an empty box.
func TestCaptchaWidgetReportsAScriptThatNeverLoads(t *testing.T) {
	srv := captchaWidgetServer(t)
	_, body := getCaptchaWidget(t, captchaWidgetURL(srv, "42", url.Values{
		"vendor": {"hcaptcha"}, "siteKey": {"10000000-ffff-ffff-ffff-000000000001"},
	}))
	html := string(body)
	watchdog := fmt.Sprintf(`setTimeout(function(){ fail("timeout"); },  %d );`, captchaWidgetLoadTimeout.Milliseconds())
	for _, want := range []string{
		watchdog,
		`script.onerror = function(){ fail("script"); };`,
		`post("error", code);`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
}

// The interface language reaches the vendor's script as hl, so the widget's
// own texts match, and anything that is not a language tag is dropped.
func TestCaptchaWidgetHandsTheInterfaceLanguageToTheVendor(t *testing.T) {
	cases := []struct{ lang, hl string }{
		{"de", "de"},
		{"pt-BR", "pt-BR"},
		{"de&render=onload", ""},
		{"", ""},
	}
	for _, c := range cases {
		for _, vendor := range []string{"hcaptcha", "recaptcha"} {
			srv := captchaWidgetServer(t)
			_, body := getCaptchaWidget(t, captchaWidgetURL(srv, "1", url.Values{
				"vendor": {vendor}, "siteKey": {"k"}, "lang": {c.lang},
			}))
			q := parseRenderedWidget(t, string(body)).script.Query()
			if q.Get("hl") != c.hl || q.Get("render") != "explicit" {
				t.Errorf("%s with lang %q loads its script with %v, want hl %q and explicit rendering", vendor, c.lang, q, c.hl)
			}
		}
	}
}

// TestCaptchaWidgetAmbiguousSignalsDegradeHonestly checks that a request that
// names no known vendor and could be either gets the page without scripts.
func TestCaptchaWidgetAmbiguousSignalsDegradeHonestly(t *testing.T) {
	cases := []struct {
		name   string
		params url.Values
	}{
		{"no-signal-at-all", url.Values{"siteKey": {"anykey"}}},
		{"explicit-normal", url.Values{"siteKey": {"anykey"}, "type": {"normal"}}},
		{"explicit-false-enterprise", url.Values{"siteKey": {"anykey"}, "enterprise": {"false"}}},
		{"invisible, which both vendors have", url.Values{"siteKey": {"anykey"}, "type": {"INVISIBLE"}}},
		{"a vendor this page does not know", url.Values{"vendor": {"turnstile"}, "siteKey": {"anykey"}}},
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
	for _, vendor := range []string{"recaptcha", "hcaptcha"} {
		params := url.Values{
			"vendor": {vendor}, "siteKey": {xss}, "host": {xss}, "prompt": {xss}, "secureToken": {xss},
		}
		resp, body := getCaptchaWidget(t, captchaWidgetURL(srv, xss, params))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s answered %d: %s", vendor, resp.StatusCode, body)
		}
		if strings.Contains(string(body), "<script>alert(1)</script>") {
			t.Fatalf("an untrusted field's raw payload appears unescaped in the %s page:\n%s", vendor, body)
		}
		if got := parseRenderedWidget(t, string(body)).params["sitekey"]; got != xss {
			t.Errorf("%s sitekey reads back as %q, want the value unchanged once unescaped", vendor, got)
		}
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

// fakeJDWithHCaptcha is a JD sidecar holding one hCaptcha, id 5, that records
// what it was asked to solve it with.
func fakeJDWithHCaptcha(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var solvedWith []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/captcha/list":
			_, _ = w.Write([]byte(`{"data":[{"id":5,"hoster":"hoster.example","type":"HCaptchaChallenge",` +
				`"challengeType":"HCaptchaChallenge","remaining":60000}]}`))
		case "/captcha/get":
			if r.URL.RawQuery != "5&"+url.QueryEscape(`"rawtoken"`) {
				t.Errorf("captcha/get asked with %q, want the rawtoken format", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"siteKey":"10000000-ffff-ffff-ffff-000000000001",` +
				`"siteUrl":"https://hoster.example/file","contextUrl":"https://hoster.example","type":"NORMAL"}}`))
		case "/captcha/solve":
			mu.Lock()
			for _, p := range strings.Split(r.URL.RawQuery, "&") {
				v, _ := url.QueryUnescape(p)
				solvedWith = append(solvedWith, v)
			}
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), solvedWith...)
	}
}

// An hCaptcha JD holds arrives named as one, renders as one, and the token the
// widget hands back reaches JD's solve call as a raw token, the way a
// reCAPTCHA token does.
func TestAnHCaptchaTokenGoesBackToJD(t *testing.T) {
	jd, solvedWith := fakeJDWithHCaptcha(t)
	t.Setenv("KL_JD", jd.URL)
	a := testApp(t)
	reg := newRegistry()
	registerCaptcha(reg, a)
	registerCaptchaWidget(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/captcha/refresh", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var pending []struct {
		ID      string            `json:"id"`
		Kind    string            `json:"kind"`
		Payload map[string]string `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pending); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Kind != "widget" || pending[0].Payload["vendor"] != "hcaptcha" {
		t.Fatalf("pending = %+v, want one widget challenge naming hcaptcha", pending)
	}
	ch := pending[0]

	page, body := getCaptchaWidget(t, captchaWidgetURL(srv, ch.ID, url.Values{
		"vendor": {ch.Payload["vendor"]}, "siteKey": {ch.Payload["siteKey"]}, "type": {ch.Payload["type"]},
	}))
	if page.StatusCode != http.StatusOK || parseRenderedWidget(t, string(body)).global != "hcaptcha" {
		t.Fatalf("the widget page for %s did not render hCaptcha: %d\n%s", ch.ID, page.StatusCode, body)
	}

	const token = "10000000-aaaa-bbbb-cccc-000000000001"
	answer, err := http.Post(srv.URL+"/api/captcha/"+ch.ID+"/answer", "application/json",
		strings.NewReader(`{"text":"`+token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Body.Close()
	var got struct {
		StillValid bool `json:"stillValid"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&got); err != nil || !got.StillValid {
		t.Fatalf("answer = %+v (%v), want stillValid", got, err)
	}
	want := []string{"5", `"` + token + `"`, `"rawtoken"`}
	if s := solvedWith(); strings.Join(s, " ") != strings.Join(want, " ") {
		t.Errorf("JD was asked to solve with %q, want %q", s, want)
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
