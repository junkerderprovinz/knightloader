package api

// A page of its own for widget captchas (captcha.KindWidget): reCAPTCHA or
// hCaptcha challenges that have to run the vendor's script in a browser. Image
// and click challenges carry a data: URL and need nothing from here. The page
// carries its own Content-Security-Policy, scoped to the one vendor it loads,
// so the SPA does not need a permanent policy wide enough for a challenge that
// may never occur.
//
// The phone app loads the same page through GET /api/captcha/{id}/widget/phone,
// which reaches it over the relay as well, since it answers JSON rather than a
// page: the markup plus the hoster's page address, which the app's WebView
// loads it under. The vendor then sees the origin a browser on the hoster's
// page would show, which is what a key locked to the hoster's domains asks
// for. The policy travels in a meta element there, and leaves out 'self',
// which would be the hoster, and frame-ancestors, which a meta element cannot
// carry.
//
// The vendor comes from the payload's vendor field, which JDSource fills from
// JD's challenge class. Without it, Enterprise or a V3Action still prove
// reCAPTCHA; anything else gets the unsolvable page rather than a guess.
// Cloudflare Turnstile runs only on the phone's page: every Turnstile key runs
// on the hostnames its owner lists alone, so under this instance's address it
// gets the unsolvable page and only a paid solver can answer it.
//
// JD reports reCAPTCHA v2, v3 and Enterprise under one class and tells them
// apart in the rawtoken payload: enterprise is set when the hoster loads
// enterprise.js, and v3Action holds the object the hoster passes to execute,
// {"action":"login"}. An Enterprise key goes through enterprise.js and
// grecaptcha.enterprise, as on the hoster's page, since a key created in
// Enterprise does not answer to api.js. A score-based key has no widget to
// click: the page asks for a token under the hoster's action once the script
// is ready, as the hoster's own page would. Without a usable action that token
// would be refused, so such a check gets the unsolvable page, which loads no
// vendor script and tells the parent why.
//
// The reCAPTCHA sources follow https://developers.google.com/recaptcha/docs/display
// and are path-scoped to /recaptcha/, which covers enterprise.js as well. The
// hCaptcha sources are the two hosts
// https://docs.hcaptcha.com/#content-security-policy-settings lists, since its
// asset subdomains change, and the Turnstile source is the one host
// https://developers.cloudflare.com/turnstile/reference/content-security-policy/
// names. A per-response nonce covers the page's own inline script and style,
// so nothing needs 'unsafe-inline'; styles are in a <style> element because a
// nonce does not cover a style attribute.
//
// The widget parameters come from the query string (vendor, siteKey, type,
// enterprise, v3Action, secureToken, lang, plus host and prompt for the
// caption). The page only renders, and the caller already holds the payload
// from its own poll, so nothing is looked up by {id}; the id is echoed for log
// correlation and in the messages. A stale id renders like a fresh one;
// Source.Answer decides whether an answer is still valid. The phone's route
// does look the challenge up, since the hoster's address is in its payload
// and the app need not send it back.
//
// The page posts {source:"knightloader-captcha-widget", id, kind, detail} to
// window.parent at its own origin: kind is "ready" on load, "loaded" once the
// vendor's widget is on the page, then "solved" (detail is the token),
// "expired" or "error" (detail is the vendor's error code, "network", "script"
// when its script did not load, "timeout", or the message the render or
// execute call threw). A score-based key has no widget and says "solved"
// instead of "loaded". The unsolvable page posts only "unsolvable", with
// "vendor", "turnstile" or "action" as the detail. The receiver still has to
// check the message origin; frame-ancestors 'self' only keeps other sites from
// embedding the page.
//
// Both vendors let a site owner lock a key to the hoster's domains
// (https://developers.google.com/recaptcha/docs/domain_validation), and the
// reCAPTCHA secure token that once worked around that is deprecated, so a key
// can refuse to work from this origin, though not from the phone's. The
// vendor then shows its own error in the widget; hCaptcha also calls the error
// callback once the checkbox is clicked, which the page reports as "error".

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
	// AtHoster is set for the phone's page, which runs under the hoster's
	// page address rather than this instance's.
	AtHoster bool
}

// errCaptchaWidgetNoSiteKey marks a malformed request, unlike a vendor that
// cannot be identified.
var errCaptchaWidgetNoSiteKey = errors.New("captcha widget: siteKey is required")

func registerCaptchaWidget(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/captcha/{id}/widget",
		"render a live captcha widget behind a Content-Security-Policy scoped to the one vendor the challenge names, or a page that says it cannot be solved here",
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

	reg.Add(http.MethodGet, "/api/captcha/{id}/widget/phone",
		"the widget page for one pending challenge as the phone app loads it, with the hoster's page address to load it under, so a key locked to the hoster's domains and a Cloudflare Turnstile run too",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			c, ok := pendingCaptcha(a, id)
			if !ok {
				writeRefusal(w, http.StatusNotFound, "gone", "captcha "+id+" is not pending", nil)
				return
			}
			req, baseURL, err := phoneWidgetRequest(c, captchaWidgetLangOf(r.URL.Query().Get("lang")))
			if err != nil {
				http.Error(w, "captcha widget: "+err.Error(), http.StatusUnprocessableEntity)
				return
			}
			page, err := buildCaptchaWidgetPage(req)
			if err != nil {
				if errors.Is(err, errCaptchaWidgetNoSiteKey) {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				http.Error(w, "captcha widget: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, phoneWidgetPage{HTML: string(page.body), BaseURL: baseURL})
		})
}

// phoneWidgetPage is what the phone's route answers: the page, whose policy
// is in a meta element, and the address the WebView loads it under.
type phoneWidgetPage struct {
	HTML    string `json:"html"`
	BaseURL string `json:"baseUrl"`
}

// pendingCaptcha finds challenge id among those waiting.
func pendingCaptcha(a *app.App, id string) (captcha.Challenge, bool) {
	for _, c := range a.CaptchaChallenges() {
		if c.ID == id {
			return c, true
		}
	}
	return captcha.Challenge{}, false
}

// phoneWidgetRequest is the page request for c as the phone loads it, from the
// payload the web UI copies into the query string, and the address to load it
// under: the hoster's page, or failing that JD's contextUrl, which is only its
// scheme and host, in the order tokenTask hands them to a paid solver.
func phoneWidgetRequest(c captcha.Challenge, lang string) (captchaWidgetRequest, string, error) {
	p, ok := c.Payload.(*captcha.WidgetPayload)
	if c.Kind != captcha.KindWidget || !ok || p == nil {
		return captchaWidgetRequest{}, "", fmt.Errorf("captcha %s is not a widget challenge", c.ID)
	}
	var baseURL string
	for _, raw := range []string{p.SiteURL, p.ContextURL} {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
			u.User, u.Fragment = nil, ""
			baseURL = u.String()
			break
		}
	}
	if baseURL == "" {
		return captchaWidgetRequest{}, "", fmt.Errorf("captcha %s names no page to load its widget under", c.ID)
	}
	return captchaWidgetRequest{
		ID:          c.ID,
		Vendor:      strings.ToLower(strings.TrimSpace(p.Vendor)),
		SiteKey:     strings.TrimSpace(p.SiteKey),
		Size:        strings.TrimSpace(p.Type),
		Enterprise:  p.Enterprise,
		V3Action:    strings.TrimSpace(p.V3Action),
		SecureToken: p.SecureToken,
		Lang:        lang,
		Host:        c.Host,
		Prompt:      c.Prompt,
		AtHoster:    true,
	}, baseURL, nil
}

var captchaWidgetLang = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)

// captchaWidgetLangOf is lang when it is a language tag, and "" otherwise.
func captchaWidgetLangOf(lang string) string {
	lang = strings.TrimSpace(lang)
	if !captchaWidgetLang.MatchString(lang) {
		return ""
	}
	return lang
}

func parseCaptchaWidgetRequest(r *http.Request) captchaWidgetRequest {
	q := r.URL.Query()
	lang := captchaWidgetLangOf(q.Get("lang"))
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
// this page knows and none of its fields prove reCAPTCHA. Both script vendors
// have an invisible size, so the size proves nothing.
func captchaWidgetVendor(req captchaWidgetRequest) string {
	switch req.Vendor {
	case captcha.VendorRecaptcha, captcha.VendorHCaptcha, captcha.VendorTurnstile:
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
		Container:     "kl-widget",
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
		csp = hcaptchaCSP(nonce, req.AtHoster)
	case captcha.VendorTurnstile:
		if !req.AtHoster {
			return unsolvableWidgetPage(nonce, req, "turnstile")
		}
		// Turnstile's render takes a selector rather than an id. Its language
		// is left at auto, the WebView's own, since a tag Turnstile does not
		// know is an error rather than English. Without retries a failure
		// stays one, so the phone hands the challenge on and Refresh is the
		// retry.
		data.Global = "turnstile"
		data.Container = "#kl-widget"
		data.ScriptURL = "https://challenges.cloudflare.com/turnstile/v0/api.js?" +
			url.Values{"onload": {"klWidgetLoaded"}, "render": {"explicit"}}.Encode()
		data.Params["retry"] = "never"
		csp = turnstileCSP(nonce)
	case captcha.VendorRecaptcha:
		data.Global = "grecaptcha"
		script := "https://www.google.com/recaptcha/api.js"
		if req.Enterprise {
			script = "https://www.google.com/recaptcha/enterprise.js"
			data.Namespace = "enterprise"
		}
		csp = recaptchaCSP(nonce, req.AtHoster)
		if req.V3Action != "" {
			action, ok := captcha.RecaptchaAction(req.V3Action)
			if !ok {
				return unsolvableWidgetPage(nonce, req, "action")
			}
			data.Action = action
			data.ScriptURL = captchaWidgetScoreScriptURL(script, req.SiteKey, req.Lang)
			break
		}
		data.ScriptURL = captchaWidgetScriptURL(script, req.Lang)
		if strings.EqualFold(req.Size, "invisible") {
			data.Params["size"] = "invisible"
		}
		if req.SecureToken != "" {
			data.Params["stoken"] = req.SecureToken
		}
	default:
		return unsolvableWidgetPage(nonce, req, "vendor")
	}
	if req.AtHoster {
		data.MetaCSP = csp
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

// captchaWidgetScoreScriptURL is reCAPTCHA's address for a score-based key:
// render names the key, which loads it without a widget, and klWidgetLoaded
// then asks for the token.
func captchaWidgetScoreScriptURL(base, siteKey, lang string) string {
	q := url.Values{"onload": {"klWidgetLoaded"}, "render": {siteKey}}
	if lang != "" {
		q.Set("hl", lang)
	}
	return base + "?" + q.Encode()
}

// unsolvableWidgetPage is the page for a challenge this route cannot solve:
// no vendor script and no vendor origin, and a message that tells the parent
// why, so it can say so in the reader's language.
func unsolvableWidgetPage(nonce string, req captchaWidgetRequest, why string) (captchaWidgetPage, error) {
	csp := unsolvableWidgetCSP(nonce, req.AtHoster)
	data := unsolvableWidgetPageData{Nonce: nonce, ID: req.ID, Host: req.Host, Why: why}
	if req.AtHoster {
		data.MetaCSP = csp
	}
	var buf bytes.Buffer
	if err := unsolvableWidgetPageTmpl.Execute(&buf, data); err != nil {
		return captchaWidgetPage{}, fmt.Errorf("rendering the unsolvable page: %w", err)
	}
	return captchaWidgetPage{csp: csp, body: buf.Bytes()}, nil
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

// widgetCSP joins a page's directives. Under the hoster's address 'self' is
// the hoster, so its sources go, and so does frame-ancestors, which a policy
// in a meta element cannot carry.
func widgetCSP(atHoster bool, directives ...string) string {
	if !atHoster {
		return strings.Join(directives, "; ")
	}
	kept := make([]string, 0, len(directives))
	for _, d := range directives {
		f := strings.Fields(d)
		if f[0] == "frame-ancestors" {
			continue
		}
		sources := f[:1]
		for _, s := range f[1:] {
			if s != "'self'" {
				sources = append(sources, s)
			}
		}
		kept = append(kept, strings.Join(sources, " "))
	}
	return strings.Join(kept, "; ")
}

// recaptchaCSP is the policy for the reCAPTCHA page.
func recaptchaCSP(nonce string, atHoster bool) string {
	n := "'nonce-" + nonce + "'"
	return widgetCSP(atHoster,
		"default-src 'none'",
		"script-src 'self' "+n+" https://www.google.com/recaptcha/ https://www.gstatic.com/recaptcha/",
		"style-src 'self' "+n,
		"img-src 'self' https://www.gstatic.com",
		"frame-src https://www.google.com/recaptcha/ https://recaptcha.google.com/recaptcha/",
		"connect-src 'self' https://www.google.com/recaptcha/",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	)
}

// hcaptchaCSP is the policy for the hCaptcha page. hCaptcha names the same two
// hosts for scripts, styles, frames and connections.
func hcaptchaCSP(nonce string, atHoster bool) string {
	n := "'nonce-" + nonce + "'"
	const hosts = "https://hcaptcha.com https://*.hcaptcha.com"
	return widgetCSP(atHoster,
		"default-src 'none'",
		"script-src 'self' "+n+" "+hosts,
		"style-src 'self' "+n+" "+hosts,
		"frame-src "+hosts,
		"connect-src 'self' "+hosts,
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	)
}

// turnstileCSP is the policy for the Turnstile page, which only the phone
// loads. Cloudflare names one host for scripts and frames, and its mobile
// guide adds connections.
func turnstileCSP(nonce string) string {
	n := "'nonce-" + nonce + "'"
	const host = "https://challenges.cloudflare.com"
	return widgetCSP(true,
		"default-src 'none'",
		"script-src "+n+" "+host,
		"style-src "+n,
		"frame-src "+host,
		"connect-src "+host,
		"base-uri 'none'",
		"form-action 'none'",
	)
}

// unsolvableWidgetCSP trusts no vendor origin; only the page's own script and
// style block may run.
func unsolvableWidgetCSP(nonce string, atHoster bool) string {
	n := "'nonce-" + nonce + "'"
	return widgetCSP(atHoster,
		"default-src 'none'",
		"script-src "+n,
		"style-src 'self' "+n,
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	)
}

// widgetPageData feeds widgetPageTmpl. SiteKey, Host, Prompt and the secure
// token come from the hoster's page via JD and are untrusted; html/template
// escapes each for its context.
type widgetPageData struct {
	Nonce, ID, Host, Prompt string
	// MetaCSP is the policy as a meta element, for the phone's page, which
	// carries no header of its own.
	MetaCSP string
	// Global is the vendor's script object, grecaptcha, hcaptcha or
	// turnstile, and Namespace the member of it the calls go to,
	// "enterprise" for reCAPTCHA Enterprise.
	Global, Namespace string
	ScriptURL         string
	// Container is what render is handed to find the widget's box.
	Container string
	// Params is the render call's parameters without the callbacks.
	Params map[string]string
	// Action is set for a score-based reCAPTCHA key, which is not rendered:
	// the token comes from execute under this action.
	Action        string
	LoadTimeoutMS int64
}

// widgetPageTmpl renders every vendor explicitly, so the page learns when the
// script has arrived and can tell a widget that never loads from one that is
// waiting for the user. The parent hears both, so a viewer that said it cannot
// load the challenge can take that back after a refresh. On failure the widget
// is hidden and the parent says why in the interface language. reCAPTCHA calls
// its error callback without a code, for lost connectivity, which is reported
// as "network". challenge-closed and challenge-expired are hCaptcha's codes for
// a challenge the user closed or left too long; the widget is reset for
// another try rather than given up.
//
// A score-based key keeps the watchdog running until execute hands over the
// token, since a key that refuses this origin may never answer at all.
var widgetPageTmpl = template.Must(template.New("captcha-widget").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
{{if .MetaCSP}}<meta http-equiv="Content-Security-Policy" content="{{.MetaCSP}}">
{{end}}<title>KnightLoader captcha</title>
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
    // A script that arrives after the watchdog would render into the hidden
    // box and take back the failure the parent is showing.
    if (failed) return;
    var api = window[{{.Global}}];
    {{- if .Namespace}}
    api = api[{{.Namespace}}];
    {{- end}}
    var params = {{.Params}};
    {{- if .Action}}
    api.ready(function(){
      // execute throws for a key it does not know, and rejects for one that
      // refuses; the promise turns both into one failure.
      new Promise(function(resolve){ resolve(api.execute(params.sitekey, {action: {{.Action}}})); }).then(function(token){
        if (failed) return;
        clearTimeout(watchdog);
        post("solved", token);
      }, function(e){
        fail(String((e && e.message) || e || "refused"));
      });
    });
    {{- else}}
    clearTimeout(watchdog);
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
      widget = api.render({{.Container}}, params);
      if (params.size === "invisible") api.execute(widget);
      post("loaded", null);
    } catch (e) {
      fail(String((e && e.message) || e));
    }
    {{- end}}
  };
  var script = document.createElement("script");
  script.src = {{.ScriptURL}};
  script.async = true;
  // Turnstile hands the nonce on to what it loads itself.
  script.nonce = {{.Nonce}};
  script.onerror = function(){ fail("script"); };
  document.head.appendChild(script);
  post("ready", null);
})();
</script>
</body>
</html>
`))

// unsolvableWidgetPageData feeds unsolvableWidgetPageTmpl. Why is "vendor"
// when the vendor cannot be identified, "turnstile" for Cloudflare Turnstile
// under this instance's address and "action" for a score-based key without a
// usable action.
type unsolvableWidgetPageData struct {
	Nonce, ID, Host, Why string
	MetaCSP              string
}

// unsolvableWidgetPageTmpl loads no vendor script. Its own script only tells
// the parent, which words the reason in the interface language; the English
// is for anyone who opens the page on its own.
var unsolvableWidgetPageTmpl = template.Must(template.New("captcha-widget-unsolvable").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
{{if .MetaCSP}}<meta http-equiv="Content-Security-Policy" content="{{.MetaCSP}}">
{{end}}<title>KnightLoader captcha</title>
<style nonce="{{.Nonce}}">
html,body{height:100%;margin:0}
body{display:flex;align-items:center;justify-content:center;padding:24px;box-sizing:border-box;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#fff;color:#1a1a1a;text-align:center}
#kl-wrap{max-width:420px}
</style>
</head>
<body>
<div id="kl-wrap">
{{if eq .Why "turnstile"}}<p><strong>This captcha cannot be shown here.</strong></p>
<p>It is a Cloudflare Turnstile check{{if .Host}} for {{.Host}}{{end}}, which this page cannot run. The phone app or a solver from the Captcha settings can answer it.</p>
{{else}}<p><strong>This captcha cannot be solved in KnightLoader.</strong></p>
{{if eq .Why "action"}}<p>It is a reCAPTCHA v3 check{{if .Host}} for {{.Host}}{{end}} without the action the hoster asks for, and a token without it would be refused.</p>
{{else}}<p>JD reported a widget challenge{{if .Host}} for {{.Host}}{{end}} without saying which vendor it is, and this page will not guess.</p>
{{end}}{{end}}</div>
<script nonce="{{.Nonce}}">
window.parent.postMessage({source:"knightloader-captcha-widget",id:{{.ID}},kind:"unsolvable",detail:{{.Why}}}, window.location.origin);
</script>
</body>
</html>
`))
