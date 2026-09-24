package api

// A page of its own for widget captchas (captcha.KindWidget): reCAPTCHA or
// hCaptcha challenges that have to run the vendor's script in a browser. Image
// and click challenges carry a data: URL and need nothing from here. The page
// carries its own Content-Security-Policy, so the SPA does not need a
// permanent policy wide enough for a challenge that may never occur.
//
// WidgetPayload does not say which vendor a challenge is: JDSource maps both
// JD challenge classes onto KindWidget and drops the class name. Enterprise, a
// V3Action or an "invisible" size only ever come from reCAPTCHA, so those
// render for real; everything else gets a page without scripts rather than a
// guess from the shape of the site key.
//
// The reCAPTCHA sources follow https://developers.google.com/recaptcha/docs/display
// and are path-scoped to /recaptcha/. A per-response nonce covers the page's own
// inline script and style, so nothing needs 'unsafe-inline'; styles are in a
// <style> element because a nonce does not cover a style attribute.
//
// The widget parameters come from the query string (siteKey, type, enterprise,
// v3Action, secureToken, plus host and prompt for the caption). The page only
// renders, and the caller already holds the payload from its own poll, so
// nothing is looked up by {id}; the id is echoed for log correlation and in
// the messages. A stale id renders like a fresh one; Source.Answer decides
// whether an answer is still valid.
//
// The page posts {source:"knightloader-captcha-widget", id, kind, detail} to
// window.parent at its own origin: kind is "ready" on load, then "solved"
// (detail is the token), "expired" or "error". The receiver still has to check
// the message origin; frame-ancestors 'self' only keeps other sites from
// embedding the page.
//
// reCAPTCHA keys are usually locked to the hoster's domains
// (https://developers.google.com/recaptcha/docs/domain_validation), and the
// secure token that once worked around that is deprecated, so rendering a key
// from this origin can fail visibly on Google's side. The secure token is
// still relayed because JD's wire format carries it.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// captchaWidgetRequest is the route's entire input, read from the query
// string.
type captchaWidgetRequest struct {
	// ID is only for log correlation and the postMessage payload.
	ID string
	// SiteKey is WidgetPayload.SiteKey and the only required field.
	SiteKey string
	// Size is WidgetPayload.Type ("normal" or "invisible"), named after the
	// data-size attribute it becomes.
	Size        string
	Enterprise  bool
	V3Action    string
	SecureToken string
	// Host and Prompt only caption the page.
	Host   string
	Prompt string
}

// errCaptchaWidgetNoSiteKey marks a malformed request, unlike a vendor that
// cannot be identified.
var errCaptchaWidgetNoSiteKey = errors.New("captcha widget: siteKey is required")

func registerCaptchaWidget(reg *Registry, _ *app.App) {
	reg.Add(http.MethodGet, "/api/captcha/{id}/widget",
		"render a live captcha widget behind a Content-Security-Policy scoped to the one vendor the query parameters identify; an honest no-script page when they do not",
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

func parseCaptchaWidgetRequest(r *http.Request) captchaWidgetRequest {
	q := r.URL.Query()
	return captchaWidgetRequest{
		ID:          r.PathValue("id"),
		SiteKey:     strings.TrimSpace(q.Get("siteKey")),
		Size:        strings.TrimSpace(q.Get("type")),
		Enterprise:  parseCaptchaWidgetBool(q.Get("enterprise")),
		V3Action:    strings.TrimSpace(q.Get("v3Action")),
		SecureToken: q.Get("secureToken"),
		Host:        q.Get("host"),
		Prompt:      q.Get("prompt"),
	}
}

func parseCaptchaWidgetBool(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

// isUnambiguouslyRecaptcha reports whether req's fields prove reCAPTCHA. No
// field proves hCaptcha, so there is no counterpart.
func isUnambiguouslyRecaptcha(req captchaWidgetRequest) bool {
	return req.Enterprise || req.V3Action != "" || strings.EqualFold(req.Size, "invisible")
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

	if !isUnambiguouslyRecaptcha(req) {
		var buf bytes.Buffer
		if err := unidentifiedWidgetPageTmpl.Execute(&buf, unidentifiedWidgetPageData{
			Nonce: nonce, Host: req.Host,
		}); err != nil {
			return captchaWidgetPage{}, fmt.Errorf("rendering the unidentified-vendor page: %w", err)
		}
		return captchaWidgetPage{csp: unidentifiedVendorCSP(nonce), body: buf.Bytes()}, nil
	}

	var buf bytes.Buffer
	if err := recaptchaWidgetPageTmpl.Execute(&buf, recaptchaWidgetPageData{
		Nonce: nonce, ID: req.ID, SiteKey: req.SiteKey, Size: req.Size,
		SecureToken: req.SecureToken, Host: req.Host, Prompt: req.Prompt,
	}); err != nil {
		return captchaWidgetPage{}, fmt.Errorf("rendering the reCAPTCHA page: %w", err)
	}
	return captchaWidgetPage{csp: recaptchaCSP(nonce), body: buf.Bytes()}, nil
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

// recaptchaWidgetPageData feeds recaptchaWidgetPageTmpl. The values come from
// the hoster's page via JD and are untrusted; html/template escapes each for
// its context.
type recaptchaWidgetPageData struct {
	Nonce, ID, SiteKey, Size, SecureToken, Host, Prompt string
}

// recaptchaWidgetPageTmpl lets reCAPTCHA render itself from the data-sitekey
// div, so the only inline script is the postMessage relay.
var recaptchaWidgetPageTmpl = template.Must(template.New("captcha-widget-recaptcha").Parse(`<!doctype html>
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
</style>
</head>
<body>
<div id="kl-wrap">
{{if .Host}}<div id="kl-host">{{.Host}}</div>{{end}}
{{if .Prompt}}<div id="kl-prompt">{{.Prompt}}</div>{{end}}
<div class="g-recaptcha"
     data-sitekey="{{.SiteKey}}"
     {{if .Size}}data-size="{{.Size}}"{{end}}
     {{if .SecureToken}}data-stoken="{{.SecureToken}}"{{end}}
     data-callback="klCaptchaSolved"
     data-expired-callback="klCaptchaExpired"
     data-error-callback="klCaptchaError"></div>
</div>
<script nonce="{{.Nonce}}">
(function(){
  var target = window.location.origin;
  function post(kind, detail){
    window.parent.postMessage({source:"knightloader-captcha-widget",id:{{.ID}},kind:kind,detail:detail||null}, target);
  }
  window.klCaptchaSolved = function(token){ post("solved", token); };
  window.klCaptchaExpired = function(){ post("expired", null); };
  window.klCaptchaError = function(){ post("error", null); };
  post("ready", null);
})();
</script>
<script src="https://www.google.com/recaptcha/api.js" nonce="{{.Nonce}}" async defer></script>
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
