package api

// A page of its own for widget captchas (captcha.KindWidget): reCAPTCHA or
// hCaptcha challenges that have to run the vendor's script in a browser. Image
// and click challenges carry a data: URL and need nothing from here. The page
// carries its own Content-Security-Policy, scoped to the one vendor it loads,
// so the SPA does not need a permanent policy wide enough for a challenge that
// may never occur.
//
// The vendor comes from the payload's vendor field, which JDSource fills from
// JD's challenge class. Without it, Enterprise or a V3Action still prove
// reCAPTCHA; anything else gets a page without scripts rather than a guess.
//
// The reCAPTCHA sources follow https://developers.google.com/recaptcha/docs/display
// and are path-scoped to /recaptcha/. The hCaptcha sources are the two hosts
// https://docs.hcaptcha.com/#content-security-policy-settings lists, since its
// asset subdomains change. A per-response nonce covers the page's own inline
// script and style, so nothing needs 'unsafe-inline'; styles are in a <style>
// element because a nonce does not cover a style attribute.
//
// The widget parameters come from the query string (vendor, siteKey, type,
// enterprise, v3Action, secureToken, lang, plus host and prompt for the
// caption). The page only renders, and the caller already holds the payload
// from its own poll, so nothing is looked up by {id}; the id is echoed for log
// correlation and in the messages. A stale id renders like a fresh one;
// Source.Answer decides whether an answer is still valid.
//
// The page posts {source:"knightloader-captcha-widget", id, kind, detail} to
// window.parent at its own origin: kind is "ready" on load, then "solved"
// (detail is the token), "expired" or "error" (detail is the vendor's error
// code, "network", "script" when its script did not load, "timeout", or the
// message the render call threw). The receiver still has to check the message
// origin; frame-ancestors 'self' only keeps other sites from embedding the
// page.
//
// Both vendors let a site owner lock a key to the hoster's domains
// (https://developers.google.com/recaptcha/docs/domain_validation), and the
// reCAPTCHA secure token that once worked around that is deprecated, so a key
// can refuse to work from this origin. The vendor then shows its own error in
// the widget; hCaptcha also calls the error callback once the checkbox is
// clicked, which the page reports as "error".

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
)

// captchaWidgetLoadTimeout is how long the vendor's script gets to arrive
// before the page gives up. A blocked or hanging script reports nothing by
// itself.
const captchaWidgetLoadTimeout = 20 * time.Second

// captchaWidgetRequest is the route's entire input, read from the query
// string.
type captchaWidgetRequest struct {
	// ID is only for log correlation and the postMessage payload.
	ID string
	// Vendor is WidgetPayload.Vendor.
	Vendor string
	// SiteKey is WidgetPayload.SiteKey and the only required field.
	SiteKey string
	// Size is WidgetPayload.Type, named after the size parameter it becomes.
	Size        string
	Enterprise  bool
	V3Action    string
	SecureToken string
	// Lang is the interface language, handed to the vendor so its own texts
	// match. Anything that is not a language tag is dropped.
	Lang string
	// Host and Prompt only caption the page.
	Host   string
	Prompt string
}

// errCaptchaWidgetNoSiteKey marks a malformed request, unlike a vendor that
// cannot be identified.
var errCaptchaWidgetNoSiteKey = errors.New("captcha widget: siteKey is required")

func registerCaptchaWidget(reg *Registry, _ *app.App) {
	reg.Add(http.MethodGet, "/api/captcha/{id}/widget",
		"render a live captcha widget behind a Content-Security-Policy scoped to the one vendor the challenge names; a page without scripts when it names none",
		func(w http.ResponseWriter, r *http.Request) {
			page, err := buildCaptchaWidgetPage(parseCaptchaWidgetRequest(r))
			if err != nil {
				if errors.Is(err, errCaptchaWidgetNoSiteKey) {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				http.Error(w, "captcha widget: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Security-Policy", page.csp)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			// Never cached, so one challenge's site key cannot be replayed
			// under another's id.
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(page.body)
		})
}

var captchaWidgetLang = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)

func parseCaptchaWidgetRequest(r *http.Request) captchaWidgetRequest {
	q := r.URL.Query()
	lang := strings.TrimSpace(q.Get("lang"))
	if !captchaWidgetLang.MatchString(lang) {
		lang = ""
	}
	return captchaWidgetRequest{
		ID:          r.PathValue("id"),
		Vendor:      strings.ToLower(strings.TrimSpace(q.Get("vendor"))),
		SiteKey:     strings.TrimSpace(q.Get("siteKey")),
		Size:        strings.TrimSpace(q.Get("type")),
		Enterprise:  parseCaptchaWidgetBool(q.Get("enterprise")),
		V3Action:    strings.TrimSpace(q.Get("v3Action")),
		SecureToken: q.Get("secureToken"),
		Lang:        lang,
		Host:        q.Get("host"),
		Prompt:      q.Get("prompt"),
	}
}

func parseCaptchaWidgetBool(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

// captchaWidgetVendor is the vendor req renders for, or "" when it names none
// this page knows and none of its fields prove reCAPTCHA. Both vendors have an
// invisible size, so the size proves nothing.
func captchaWidgetVendor(req captchaWidgetRequest) string {
	switch req.Vendor {
	case captcha.VendorRecaptcha, captcha.VendorHCaptcha:
		return req.Vendor
	case "":
		if req.Enterprise || req.V3Action != "" {
			return captcha.VendorRecaptcha
		}
	}
	return ""
}

// captchaWidgetPage is a rendered response, keeping the CSP together with the
// body it belongs to.
type captchaWidgetPage struct {
	csp  string
	body []byte
}

func buildCaptchaWidgetPage(req captchaWidgetRequest) (captchaWidgetPage, error) {
	if req.SiteKey == "" {
		return captchaWidgetPage{}, errCaptchaWidgetNoSiteKey
	}
	nonce, err := newCaptchaWidgetNonce()
	if err != nil {
		return captchaWidgetPage{}, fmt.Errorf("preparing a page nonce: %w", err)
	}

	data := widgetPageData{
		Nonce: nonce, ID: req.ID, Host: req.Host, Prompt: req.Prompt,
		LoadTimeoutMS: captchaWidgetLoadTimeout.Milliseconds(),
		Params:        map[string]string{"sitekey": req.SiteKey},
	}
	var csp string
	switch captchaWidgetVendor(req) {
	case captcha.VendorHCaptcha:
		// Always the checkbox, even for a key JD found invisible: hCaptcha
		// does not tie a key to a size, and a challenge the user closes can
		// then be opened again.
		data.Global = "hcaptcha"
		data.ScriptURL = captchaWidgetScriptURL("https://js.hcaptcha.com/1/api.js", req.Lang)
		csp = hcaptchaCSP(nonce)
	case captcha.VendorRecaptcha:
		data.Global = "grecaptcha"
		data.ScriptURL = captchaWidgetScriptURL("https://www.google.com/recaptcha/api.js", req.Lang)
		if strings.EqualFold(req.Size, "invisible") {
			data.Params["size"] = "invisible"
		}
		if req.SecureToken != "" {
			data.Params["stoken"] = req.SecureToken
		}
		csp = recaptchaCSP(nonce)
	default:
		var buf bytes.Buffer
		if err := unidentifiedWidgetPageTmpl.Execute(&buf, unidentifiedWidgetPageData{
			Nonce: nonce, Host: req.Host,
		}); err != nil {
			return captchaWidgetPage{}, fmt.Errorf("rendering the unidentified-vendor page: %w", err)
		}
		return captchaWidgetPage{csp: unidentifiedVendorCSP(nonce), body: buf.Bytes()}, nil
	}

	var buf bytes.Buffer
	if err := widgetPageTmpl.Execute(&buf, data); err != nil {
		return captchaWidgetPage{}, fmt.Errorf("rendering the %s page: %w", data.Global, err)
	}
	return captchaWidgetPage{csp: csp, body: buf.Bytes()}, nil
}

// captchaWidgetScriptURL is a vendor's script address, set up for explicit
// rendering from the page's klWidgetLoaded and in lang where one is known.
// Both vendors take the same three parameters.
func captchaWidgetScriptURL(base, lang string) string {
	q := url.Values{"onload": {"klWidgetLoaded"}, "render": {"explicit"}}
	if lang != "" {
		q.Set("hl", lang)
	}
	return base + "?" + q.Encode()
}

// newCaptchaWidgetNonce is one response's CSP nonce. It only has to be
// unpredictable for the life of the response.
func newCaptchaWidgetNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// recaptchaCSP is the policy for the reCAPTCHA page.
func recaptchaCSP(nonce string) string {
	n := "'nonce-" + nonce + "'"
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self' " + n + " https://www.google.com/recaptcha/ https://www.gstatic.com/recaptcha/",
		"style-src 'self' " + n,
		"img-src 'self' https://www.gstatic.com",
		"frame-src https://www.google.com/recaptcha/ https://recaptcha.google.com/recaptcha/",
		"connect-src 'self' https://www.google.com/recaptcha/",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	}, "; ")
}

// hcaptchaCSP is the policy for the hCaptcha page. hCaptcha names the same two
// hosts for scripts, styles, frames and connections.
func hcaptchaCSP(nonce string) string {
	n := "'nonce-" + nonce + "'"
	const hosts = "https://hcaptcha.com https://*.hcaptcha.com"
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self' " + n + " " + hosts,
		"style-src 'self' " + n + " " + hosts,
		"frame-src " + hosts,
		"connect-src 'self' " + hosts,
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	}, "; ")
}

// unidentifiedVendorCSP trusts no vendor origin; only the page's own style
// block may load.
func unidentifiedVendorCSP(nonce string) string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src 'self' 'nonce-" + nonce + "'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	}, "; ")
}

// widgetPageData feeds widgetPageTmpl. SiteKey, Host, Prompt and the secure
// token come from the hoster's page via JD and are untrusted; html/template
// escapes each for its context.
type widgetPageData struct {
	Nonce, ID, Host, Prompt string
	// Global is the vendor's script object, grecaptcha or hcaptcha.
	Global    string
	ScriptURL string
	// Params is the render call's parameters without the callbacks.
	Params        map[string]string
	LoadTimeoutMS int64
}

// widgetPageTmpl renders either vendor explicitly, so the page learns when the
// script has arrived and can tell a widget that never loads from one that is
// waiting for the user. On failure the widget is hidden and the parent says
// why in the interface language. reCAPTCHA calls its error callback without a
// code, for lost connectivity, which is reported as "network".
// challenge-closed and challenge-expired are hCaptcha's codes for a challenge
// the user closed or left too long; the widget is reset for another try rather
// than given up.
var widgetPageTmpl = template.Must(template.New("captcha-widget").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>KnightLoader captcha</title>
<style nonce="{{.Nonce}}">
html,body{height:100%;margin:0}
body{display:flex;align-items:center;justify-content:center;font:14px/1.4 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#fff;color:#1a1a1a}
#kl-wrap{text-align:center;max-width:420px;padding:16px}
#kl-host{font-weight:600;margin-bottom:4px}
#kl-prompt{font-size:12px;color:#666;margin-bottom:12px}
#kl-widget{display:inline-block}
</style>
</head>
<body>
<div id="kl-wrap">
{{if .Host}}<div id="kl-host">{{.Host}}</div>{{end}}
{{if .Prompt}}<div id="kl-prompt">{{.Prompt}}</div>{{end}}
<div id="kl-widget"></div>
</div>
<script nonce="{{.Nonce}}">
(function(){
  var target = window.location.origin;
  var failed = false;
  var widget;
  function post(kind, detail){
    window.parent.postMessage({source:"knightloader-captcha-widget",id:{{.ID}},kind:kind,detail:detail||null}, target);
  }
  function fail(code){
    if (failed) return;
    failed = true;
    clearTimeout(watchdog);
    document.getElementById("kl-widget").hidden = true;
    post("error", code);
  }
  var watchdog = setTimeout(function(){ fail("timeout"); }, {{.LoadTimeoutMS}});
  window.klWidgetLoaded = function(){
    clearTimeout(watchdog);
    var api = window[{{.Global}}];
    var params = {{.Params}};
    params.callback = function(token){ post("solved", token); };
    params["expired-callback"] = function(){ post("expired", null); };
    params["error-callback"] = function(code){
      if (code === "challenge-closed" || code === "challenge-expired") {
        api.reset(widget);
        return;
      }
      fail(code || "network");
    };
    try {
      widget = api.render("kl-widget", params);
      if (params.size === "invisible") api.execute(widget);
    } catch (e) {
      fail(String((e && e.message) || e));
    }
  };
  var script = document.createElement("script");
  script.src = {{.ScriptURL}};
  script.async = true;
  script.onerror = function(){ fail("script"); };
  document.head.appendChild(script);
  post("ready", null);
})();
</script>
</body>
</html>
`))

// unidentifiedWidgetPageData feeds unidentifiedWidgetPageTmpl.
type unidentifiedWidgetPageData struct {
	Nonce, Host string
}

// unidentifiedWidgetPageTmpl is the page for a vendor that cannot be
// identified: text only, no script and no vendor origin.
var unidentifiedWidgetPageTmpl = template.Must(template.New("captcha-widget-unidentified").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>KnightLoader captcha</title>
<style nonce="{{.Nonce}}">
html,body{height:100%;margin:0}
body{display:flex;align-items:center;justify-content:center;padding:24px;box-sizing:border-box;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#fff;color:#1a1a1a;text-align:center}
#kl-wrap{max-width:420px}
</style>
</head>
<body>
<div id="kl-wrap">
<p><strong>This captcha cannot be shown here.</strong></p>
<p>JD reported a widget challenge{{if .Host}} for {{.Host}}{{end}} without saying which vendor it is, and this page will not guess.</p>
</div>
</body>
</html>
`))
