package api

// The one route in this app that ever sets a Content-Security-Policy header -
// nothing else does; grep -r "Content-Security-Policy" internal/ found nothing
// before this file existed (checked 2026-08-10, this wave's own starting
// fact). The policy set here is scoped to this one response only, through
// reg.Add's own per-handler closure - nothing about it touches any other
// route, and there is no shared "security headers" middleware in api.go for
// it to have leaked into even by accident.
//
// WHAT THIS IS FOR. internal/captcha's Kind (challenge.go) has four values;
// three of them need nothing from this file. KindImage and KindClick both
// carry a complete data: URL in *ImagePayload - 7A's prompt modal renders
// that directly with an <img src>, no network fetch and no third party
// involved. KindUnsupported carries only a vendor name to display, nothing to
// render. KindWidget is the one exception: a live, hosted third-party JS
// challenge (reCAPTCHA v2 or hCaptcha) that has to run a real vendor script in
// a browser to produce an answer token, and cannot be reduced to a picture.
// That script cannot run inside KnightLoader's own SPA document without
// either giving the whole app a permanent CSP wide enough for a challenge
// that may never occur, or giving the challenge a page of its own whose
// policy exists only while that page is open. This file is that page.
//
// WHETHER 7F SHIPPED ENOUGH TO BUILD THIS FOR REAL - read in full before
// writing a line here, per this wave's own instruction, and the honest answer
// has two halves:
//
//   - Yes: WidgetPayload (internal/captcha/challenge.go) is real data, not a
//     placeholder. SiteKey/SiteURL/ContextURL/Type/Enterprise/V3Action/
//     SecureToken are read from a live JD sidecar's captcha/get?
//     format=rawtoken call and cross-verified against JD's own source three
//     independent ways (jdsource.go's package comment). Nothing had to be
//     invented here to have real sitekey data to render with.
//   - No: nothing in that payload says which of the two vendors a given
//     challenge is. jdKindByClass (jdsource.go) maps both
//     "RecaptchaV2Challenge" and "HCaptchaChallenge" onto the identical
//     Kind("widget"), and the one field that would tell them apart - the JD
//     challenge class name - is computed in JDSource.build, used only to
//     choose the Kind, and then dropped; it survives on the wire only for
//     KindUnsupported, as UnsupportedPayload.Vendor. Confirmed by reading the
//     package's own tests, not assumed: TestJDSourceListWidgetPayload asserts
//     on SiteKey/SiteURL/ContextURL/Type/Enterprise/V3Action/SecureToken and
//     nothing else, and its own fake only ever exercises
//     "RecaptchaV2Challenge" - nothing in that package proves an
//     HCaptchaChallenge payload comes back distinguishable from a
//     RecaptchaV2Challenge one, because nothing does.
//
// Google's script lives at a different origin than hCaptcha's, so a CSP
// naming "the challenge's own vendor script" - this assignment's own words -
// needs to know which vendor before it can be written, and for the ordinary
// case the payload alone does not say. Guessing from the sitekey's own shape
// (hCaptcha's are UUIDs; reCAPTCHA's never are) was considered and rejected:
// jdsource.go's own package comment already names this exact move - "the
// obvious guess from general knowledge of those vendors" - as the mistake
// that would have shipped a screenshot to a JS-widget renderer for the
// default captcha/get call. Inferring a vendor from a key format neither
// vendor documents as a stable contract is the same kind of guess aimed at
// the same kind of consequence here: a CSP quietly allowing the wrong origin,
// or a script tag quietly loading the wrong vendor's API and rendering
// nothing.
//
// So this file renders for real exactly the slice of "widget" the payload can
// identify without guessing (see isUnambiguouslyRecaptcha), and answers with
// an honest, scriptless "cannot show this" page for the rest. That is not the
// whole KindWidget population, and it is not written to read as though it
// were.
//
// THE SIGNAL THAT DOES DISAMBIGUATE, taken from WidgetPayload's own doc
// comment (challenge.go), not invented here: Enterprise and V3Action only
// ever come from reCAPTCHA - hCaptcha's Storable never populates them - and
// hCaptcha's own API always reports Type=="normal", never "invisible". So
// Enterprise==true, or V3Action != "", or Size=="invisible" each
// independently prove reCAPTCHA; none of the three is this file's own
// finding, all three are read straight out of a claim 7F already verified and
// wrote down. Nothing proves hCaptcha the same way, because nothing in the
// payload is exclusive to it - which is also why there is no hcaptchaCSP
// function here: one that could never be reached would not be a feature, it
// would be a second place for the same bug to hide.
//
// WHAT WOULD ACTUALLY CLOSE THE GAP: WidgetPayload growing a Vendor field -
// the exact shape UnsupportedPayload already has, filled from the same
// className JDSource.build already computes and currently discards - would
// turn this file's honest-degrade branch into a second real one, for free, no
// guessing required. That field lives in internal/captcha, which is 7F's file
// this wave and not this file's to touch (build-plan.md section 8's Wave 7
// note, and this wave's own file-ownership split). Flagged here for whoever
// picks it up next; not fixed here.
//
// THE CSP ITSELF, verified 2026-08-10 against each vendor's own current
// published guidance, not remembered:
//   - reCAPTCHA: https://developers.google.com/recaptcha/docs/display (the
//     api.js URL, and grecaptcha.render's real option names - sitekey, theme,
//     size, callback, tabindex, expired-callback, error-callback) and the
//     directives Google's own developer community gives for embedding it
//     (https://security.googlecloudcommunity.com/recaptcha-6/recaptcha-csp-5053):
//     script/connect from www.google.com/recaptcha/ and
//     www.gstatic.com/recaptcha/, the widget's own frame from
//     www.google.com/recaptcha/ and recaptcha.google.com/recaptcha/. Every one
//     of those is path-scoped to /recaptcha/, which CSP host-source matching
//     honours - narrower than the bare origins would be.
//   - hCaptcha: https://docs.hcaptcha.com/ documents its own CSP in these
//     exact terms and asks integrators not to narrow it further: "Please do
//     not hard-code specific subdomains, like newassets.hcaptcha.com, into
//     your CSP: asset subdomains used may vary over time or by region." This
//     file never reaches a branch that would use it (see above), but it is
//     recorded here so the next person does not have to re-derive it:
//     *.hcaptcha.com is the vendor's own answer to "never a wildcard", not
//     this file relaxing that rule, on the day a Vendor field makes the
//     hCaptcha branch real.
//
// A per-response CSP nonce (newCaptchaWidgetNonce), not 'unsafe-inline',
// covers this page's own small inline script (the postMessage relay below)
// and its inline style block, so nothing on this response runs merely for
// being inline; every script that executes here is either this page's own
// nonce-carrying code or the one named vendor origin. Both go in <style>/
// <script> elements, never a style="" attribute - a CSP nonce covers an
// inline element but not an inline attribute, so an attribute would have
// needed 'unsafe-inline' to work at all, which defeats the point of having a
// nonce.
//
// THE REQUEST CONTRACT. This route reads the widget's own rendering
// parameters from its query string (siteKey, type, enterprise, v3Action,
// secureToken; host and prompt for display only) instead of looking a
// challenge up by id server-side, and that is a deliberate departure from
// routes_captcha_skip.go's shape in this same wave, not an oversight of it.
// 7D's route performs a real, stateful action - Source.Abort - that can only
// go through the wired Source app_captcha.go holds, so it has no honest
// alternative to assuming *app.App grows an AbortCaptcha method (see that
// file's own "THE CONTRACT THIS FILE ASSUMES"). This route only ever renders;
// it never calls JD, never checks whether a challenge is still live, and has
// no comparable need to reach into app state at all - the one thing an
// id-based server-side lookup would add is a second, redundant
// captcha/get?format=rawtoken call for data the caller (7A's modal) already
// holds from its own List() poll. Query parameters are that data, passed
// straight through; {id} is carried in the path for server-log correlation
// and echoed into the postMessage payload below, and is not otherwise used -
// not a lookup key, because there is nothing here for it to look up. Should
// app_captcha.go/store.go land a reason this route needs live state after
// all, that is a reason to revisit this file, not a gap in it today: 7A's
// files did not exist while this one was written (7A/7B/7C/7D all run in
// parallel this wave, confirmed in the working tree - internal/app/
// app_captcha.go and internal/api/routes_captcha.go are both still absent as
// of this file, and the tree does not currently build because of it: see
// routes_captcha_skip.go's own reference to the not-yet-landed
// a.AbortCaptcha).
//
// THE ANSWER PATH BACK. This page's only side effect is a
// window.parent.postMessage once the vendor's own callback fires, targeted at
// window.location.origin - this page's own origin, always KnightLoader's,
// since nothing else serves it - rather than "*", so a solved token is never
// broadcast to whatever page happens to have this one open. It does not call
// a KnightLoader API itself: the route that submits a token to Source.Answer
// is 7A's, in routes_captcha.go, which does not exist yet either, and
// guessing its path or payload shape here would be exactly the mistake this
// file already declined to make about vendor identity. Whoever builds that
// caller listens for {source:"knightloader-captcha-widget", id, kind, detail}
// on the window "message" event - kind is "ready" once on load, then "solved"
// (detail is the vendor's token), "expired" or "error" - and is the one place
// that still has to check the message's own origin before trusting it: the
// frame-ancestors 'self' below only keeps a different site from embedding
// this page, not a same-origin page from mishandling what it receives.
//
// A stale or already-answered id renders here exactly the same as a fresh one
// - this file has no way to tell the difference and does not try to. That is
// resolved honestly, later, by Source.Answer's own stillValid (see
// Source.Answer's doc comment, challenge.go) - not re-guessed from a
// countdown on this page or any other.
//
// A LIMIT NO CSP FIXES. reCAPTCHA site keys are commonly locked to specific
// domains at creation (verified
// https://developers.google.com/recaptcha/docs/domain_validation, 2026-08-10),
// and a mismatch fails visibly in the browser with Google's own "ERROR for
// site owner: Invalid domain for site key" rather than quietly. The one
// mechanism that used to let a key render correctly from a domain other than
// its own - reCAPTCHA's "Secure Token" (stoken) - was deprecated by Google
// around 2016. SecureToken/data-stoken is still relayed here regardless, on
// the same reasoning 7F already gives for keeping the field at all
// (WidgetPayload's own doc comment: "dropping a field JD's wire format
// genuinely carries is a silent regression the day a JD build starts sending
// it, not a simplification") - but for an ordinary key registered only to the
// hoster's own site, rendering it from this instance's own origin may simply
// fail on Google's side, visibly, regardless of anything in this file. Not a
// bug here, and not something a Content-Security-Policy header can fix.

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

// captchaWidgetRequest is this route's entire input, read once from the query
// string - see this file's own "THE REQUEST CONTRACT" for why nothing here is
// looked up server-side.
type captchaWidgetRequest struct {
	// ID is carried through for log correlation and the postMessage payload
	// only; never used as a lookup key.
	ID string
	// SiteKey is WidgetPayload.SiteKey (challenge.go) and the only required
	// field - without it there is nothing to render.
	SiteKey string
	// Size is WidgetPayload.Type ("normal"/"invisible"), renamed here to
	// match the DOM attribute it becomes (data-size) rather than the wire
	// field it came from.
	Size        string
	Enterprise  bool
	V3Action    string
	SecureToken string
	// Host and Prompt are Challenge's own fields (Challenge.Host,
	// Challenge.Prompt in challenge.go), not WidgetPayload's - carried through
	// only to caption the page for a human looking at it, never consumed by
	// either vendor's own render() call.
	Host   string
	Prompt string
}

// errCaptchaWidgetNoSiteKey is a real client error, unlike an unidentifiable
// vendor: a request with no siteKey at all is malformed, not merely a
// challenge this route cannot show.
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
			// nosniff plus a fixed type from our own side, matching
			// routes_containers.go's relay route: nothing about this body's
			// content may decide how a client treats it, even though every
			// byte of it is written by this file's own templates.
			w.Header().Set("Content-Security-Policy", page.csp)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			// A dynamically rendered, challenge-specific page: caching it
			// anywhere risks a browser or intermediary replaying one
			// challenge's sitekey under a different one's id.
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

// isUnambiguouslyRecaptcha reports whether req's own fields prove reCAPTCHA
// without guessing - see this file's own "THE SIGNAL THAT DOES DISAMBIGUATE"
// for where each branch comes from and why there is no equivalent function
// for hCaptcha.
func isUnambiguouslyRecaptcha(req captchaWidgetRequest) bool {
	return req.Enterprise || req.V3Action != "" || strings.EqualFold(req.Size, "invisible")
}

// captchaWidgetPage is a fully rendered response - the CSP that belongs with
// exactly this body, never handled separately from it, so the two can never
// drift apart (a body from one branch served under the other branch's CSP,
// wider or narrower than the content actually needs).
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

// newCaptchaWidgetNonce is one response's CSP nonce - 16 bytes from
// crypto/rand, RawURLEncoding to match the token convention
// internal/auth/auth.go's own session signature already uses in this
// codebase, sized down from routes_containers.go's 32-byte relay token
// because a CSP nonce only has to be unpredictable for the life of one
// response, never looked up or compared against a stored value the way that
// token is.
func newCaptchaWidgetNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// recaptchaCSP is the policy for the one branch this file renders for real -
// see this file's own "THE CSP ITSELF" for what each origin is and where it
// was verified.
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

// unidentifiedVendorCSP is the policy for the honest-degrade page: no vendor
// origin is trusted at all, because none was identified, so nothing outside
// this response's own nonce-carrying style block may run or load.
func unidentifiedVendorCSP(nonce string) string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src 'self' 'nonce-" + nonce + "'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	}, "; ")
}

// recaptchaWidgetPageData feeds recaptchaWidgetPageTmpl. Every field is
// contextually escaped by html/template for wherever the template places it -
// HTML attribute, HTML text or JS expression - so nothing here is pre-escaped
// by hand; SiteKey and the rest are whatever JD relayed from the hoster's own
// page and are treated as untrusted input throughout.
type recaptchaWidgetPageData struct {
	Nonce, ID, SiteKey, Size, SecureToken, Host, Prompt string
}

// recaptchaWidgetPageTmpl auto-renders a reCAPTCHA v2 widget from a
// data-sitekey div - no explicit grecaptcha.render() call, so the only inline
// script this page needs is the postMessage relay, not a second script also
// invoking the vendor API by hand.
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

// unidentifiedWidgetPageTmpl is the honest degrade - see this file's own
// package comment for why it exists instead of a guess. No script, no vendor
// origin, nothing but text.
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
