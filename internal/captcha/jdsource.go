package captcha

// The JD-backed Source: KnightLoader's arm's-length JD sidecar talking over
// its local "Deprecated API" (plain HTTP JSON, no cloud, no crypto), the same
// namespace/method/{"data":...} envelope convention
// internal/resolver/jd/client.go and internal/hosterauth/jdclient.go already
// use - see newJDClient/call below, copied from jdclient.go's own pattern
// rather than shared with it, for the same reason jdclient.go itself gives
// for not sharing with client.go: this wave owns internal/captcha only.
//
// Nothing in the tree produced a captcha challenge before this file: `grep -r
// captcha internal/` (checked before writing a line here) turns up three doc
// comments and core.ReasonCaptcha's classification, no producer. Every plan
// to build a prompt modal, a solver or a skip button was resting on a type
// that did not exist.
//
// VERIFIED THREE WAYS, not guessed at, because the three disagree often
// enough that trusting any one of them alone would have shipped a wrong field
// name or a dead endpoint:
//
//  1. The Deprecated API's own worked example (2011-era, jQuery, MyJDownloader
//     push transport):
//     https://github.com/svn2github/jdownloader-jdjsapi/blob/master/deprecated/example/captcha.html
//     This is where captcha/abort(id, blockType) with BLOCKTHIS/BLOCKHOSTER/
//     BLOCKALL comes from, where captcha/get is documented as returning a
//     bare image for direct use as an <img src>, and where captcha/solve is
//     documented as returning false for a stale id rather than failing. Its
//     own CaptchaObject is {id, link, hoster, type}, and its own comment says
//     "Currently, only normal captchas are supported [...] (no clickable
//     captchas etc.)" - this document predates click and widget support
//     entirely, which is the single biggest reason to treat it as history,
//     not spec.
//  2. The modern, still-maintained JD source, same namespace:
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/captcha/CaptchaAPI.java (the interface: list/get/getCaptchaJob/keepAlive/solve/skip - note there is no abort method, only a vestigial, unused `enum ABORT{SINGLE,HOSTER,ALL}` nested in the interface with nothing wired to it)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/captcha/CaptchaJob.java (the exact wire fields)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/api/captcha/CaptchaAPISolver.java (the implementation - isChallengeSupported's gate, getCaptchaJob's field mapping, get/solve/skip/keepAlive's real behaviour)
//     https://github.com/mirror/jdownloader/blob/master/src/jd/controlling/captcha/SkipRequest.java (the 7-value real replacement for blockType)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/Challenge.java (getRemainingTimeout's math - why list's own remaining field is the honest live countdown)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/stringcaptcha/ImageCaptchaChallenge.java (getAPIStorable's 4 branches - the "data:" prefix gap normalizeImageDataURL exists for)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/stringcaptcha/BasicCaptchaChallenge.java, .../clickcaptcha/ClickCaptchaChallenge.java, .../multiclickcaptcha/MultiClickCaptchaChallenge.java (parseAPIAnswer per family - why resultFormat="rawtoken" is safe to send unconditionally)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/recaptcha/v2/RecaptchaV2Challenge.java, .../hcaptcha/HCaptchaChallenge.java (RAWTOKEN's real payload shape, and that the *default*, no-format call returns an image fallback instead)
//     https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/oauth/AccountLoginOAuthChallenge.java, .../oauth/OAuthChallenge.java (why this one classifies KindUnsupported: getAPIStorable is never overridden, so it inherits Challenge's own `return null` - there is no payload this API could relay even if a Kind existed for it)
//  3. A live headless JD sidecar (KL_JD, revision 48637, checked 2026-08-10):
//     its own /help reflects CaptchaJob's fields identically to (2)'s source,
//     and a live probe of every method against a nonexistent id (999999999)
//     pinned the exact error envelope solve/get/skip answer with -
//     {"src":"DEVICE","data":null,"type":"NOT_AVAILABLE"} on HTTP 404 - and
//     that keepAlive on a gone id answers 200 {"data":-1}, matching (2)
//     exactly. No real captcha challenge occurred on that instance during
//     this build (nothing on it was mid-download against a captcha-gated
//     hoster), so the *success* shape of get/getCaptchaJob/solve is verified
//     against (2) and cross-checked against (3)'s reflection metadata, not
//     observed on the wire end to end; the error shape and every namespace/
//     method/parameter-count pairing is (3), live.
//
// What changed between (1) and (2)/(3), each confirmed rather than assumed:
//   - get(id) answers {"data": "<mime>;base64,<...>"} or a full "data:..."
//     URL depending on which of ImageCaptchaChallenge's four branches ran
//     (see normalizeImageDataURL) - JSON, not the bare image (1) describes.
//     Solver.get's own implementation wraps it: `data.put("data",
//     challenge.getAPIStorable(format))`, the identical {"data":...} envelope
//     every other call in this file already assumes.
//   - There is no abort route. skip(id, SkipRequest) is what is actually
//     wired to /captcha/skip - see jdSkipRequestFor for the
//     BLOCKTHIS/BLOCKHOSTER/BLOCKALL -> SINGLE/BLOCK_HOSTER/BLOCK_ALL_CAPTCHAS
//     mapping this package uses instead.
//   - solve() no longer returns false for a stale id; it throws
//     InvalidCaptchaIDException, which CaptchaAPI.Error maps to HTTP 404
//     NOT_AVAILABLE - confirmed live against a real, currently-running
//     sidecar (see (3) above), not only read in source. Answer translates
//     that back into the same boolean (1) describes, since that is still the
//     right shape for this package's own caller.
//   - A widget challenge's *default* get(id) call - no format - returns an
//     IMAGE, not sitekey data: RecaptchaV2Challenge/HCaptchaChallenge's own
//     getAPIStorable falls back to `createBasicCaptchaChallenge(true).getAPIStorable(format)`
//     unless format is literally "rawtoken". Assuming the reCAPTCHA/hCaptcha
//     public JS API's sitekey shape came back from the ordinary call - the
//     obvious guess from general knowledge of those vendors - would have
//     silently handed a screenshot to a JS-widget renderer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// SourceJD is Challenge.Source's value for every challenge JDSource produces.
const SourceJD = "jd"

// jdCaptchaJob mirrors org.jdownloader.api.captcha.CaptchaJob's JSON shape
// field for field - see this file's package comment, source (2) and (3).
// remaining is JD's own Challenge.getRemainingTimeout(): validUntil (creation
// time plus the configured timeout, extended whenever something calls
// keepAlive) minus now, recomputed on every single read - never a cached or
// derived value. That is what this package trusts for Challenge.ExpiresAt
// instead of created+timeout: a challenge nobody ever calls keepAlive on
// counts down for real in this field, and one that does gets a longer number
// back, with no extra call of this package's own required to see either -
// see JDSource.build.
type jdCaptchaJob struct {
	ID            int64  `json:"id"`
	Hoster        string `json:"hoster"`
	Link          int64  `json:"link"`
	Type          string `json:"type"`
	ChallengeType string `json:"challengeType"`
	// CaptchaCategory is decoded and then deliberately never read - see
	// jdKindByClass's doc comment for why it is not classify's input: it is a
	// hoster name for one verified family, the class name again for another,
	// and only a stable tag for a third, so trusting it would be trusting
	// whichever family was tested last.
	CaptchaCategory string `json:"captchaCategory"`
	Explain         string `json:"explain"`
	Timeout         int64  `json:"timeout"`
	Created         int64  `json:"created"`
	Remaining       int    `json:"remaining"`
}

// jdKindByClass maps a JD challenge class's simple name (Type/ChallengeType -
// CaptchaAPISolver.getCaptchaJob sets both to challenge.getClass().getSimpleName(),
// unconditionally, for every challenge family) to this package's Kind.
//
// A closed, exact-match table, not a substring or prefix guess, because the
// field that looks like it should already be a stable short tag -
// captchaCategory (Challenge.typeID) - is not one: verified per subclass in
// JD's own source, RecaptchaV2Challenge passes the constant "recaptchav2",
// but ClickCaptchaChallenge and MultiClickCaptchaChallenge both pass
// plugin.getHost() - the HOSTER'S OWN NAME - as that identical constructor
// argument, and HCaptchaChallenge passes its own class name again. None of
// that inconsistency touches ChallengeType, which is why classify keys on it
// instead. A class this table has not seen classifies KindUnsupported with
// its real name preserved (see classify) rather than guessed at from a name
// pattern this package cannot back up.
//
// Only four challenge classes ever reach this table at all.
// CaptchaAPISolver.isChallengeSupported gates list()/getCaptchaJob() to
// HCaptchaChallenge, RecaptchaV2Challenge, AccountLoginOAuthChallenge and
// ImageCaptchaChallenge (and its subclasses) before a CaptchaJob is ever
// built for one; every other JD challenge type - KeyCaptcha's puzzle and
// category challenges among them, which JD's own source marks "unsupported?"
// in a comment on that exact gate - never becomes visible over this API at
// all. So KindUnsupported below only ever fires for a class that passed JD's
// own filter but has no renderer in this app; the one live example is
// AccountLoginOAuthChallenge, whose getAPIStorable is never overridden and so
// inherits the base Challenge's `return null` - there is no payload this
// package could relay for it even if KindUnsupported were built as
// something more than a name.
var jdKindByClass = map[string]Kind{
	"ImageCaptchaChallenge":       KindImage,
	"BasicCaptchaChallenge":       KindImage,
	"SolveMediaCaptchaChallenge":  KindImage,
	"RecaptchaV1CaptchaChallenge": KindImage,
	"ClickCaptchaChallenge":       KindClick,
	"MultiClickCaptchaChallenge":  KindClick,
	"RecaptchaV2Challenge":        KindWidget,
	"HCaptchaChallenge":           KindWidget,
}

// classify turns one JD challenge class name into a Kind, defaulting to
// KindUnsupported for anything jdKindByClass has not verified - see that
// map's own doc comment for why this is a closed table and not a heuristic.
func classify(challengeType string) Kind {
	if k, ok := jdKindByClass[challengeType]; ok {
		return k
	}
	return KindUnsupported
}

// jdRawTokenFormat is JD's own RecaptchaV2Challenge.RAWTOKEN /
// HCaptchaChallenge.RAWTOKEN constant ("rawtoken"), the getAPIStorable format
// that returns sitekey data instead of an image fallback, and the
// parseAPIAnswer resultFormat that makes a widget challenge read its answer
// as a token instead of falling back to an image sub-challenge parse - see
// this file's package comment for both call sites.
const jdRawTokenFormat = "rawtoken"

// jdWidgetToken is captcha/get?id&format=rawtoken's payload for KindWidget,
// field for field from RecaptchaV2Challenge.RecaptchaV2APIStorable and
// HCaptchaChallenge.HCaptchaAPIStorable (see this file's package comment,
// source (2)). hCaptcha's own Storable has no enterprise/v3Action fields at
// all and its getStoken() is hardcoded to return null, so those three decode
// to the zero value for that vendor - not a decoding failure, and not
// evidence this struct is wrong for it.
type jdWidgetToken struct {
	SiteKey     string `json:"siteKey"`
	SiteURL     string `json:"siteUrl"`
	ContextURL  string `json:"contextUrl"`
	Type        string `json:"type"`
	Enterprise  bool   `json:"enterprise"`
	SecureToken string `json:"stoken"`
	V3Action    string `json:"v3Action"`
}

// jdSkipRequest mirrors jd.controlling.captcha.SkipRequest's real values -
// this file's package comment, source (2) and (3) (the live sidecar's own
// /help reflects the identical seven names for object
// jd.controlling.captcha.SkipRequest). Deliberately not all seven have an
// AbortScope that reaches them - see jdSkipRequestFor.
type jdSkipRequest string

const (
	jdSkipSingle           jdSkipRequest = "SINGLE"
	jdSkipBlockHoster      jdSkipRequest = "BLOCK_HOSTER"
	jdSkipBlockAllCaptchas jdSkipRequest = "BLOCK_ALL_CAPTCHAS"
	// JD also has BLOCK_PACKAGE, REFRESH, STOP_CURRENT_ACTION and TIMEOUT.
	// No AbortScope value asks for any of them: BLOCK_PACKAGE has no clean
	// KnightLoader-user-facing equivalent yet (there is no package-scoped
	// skip concept anywhere else in this app), and the other three are JD's
	// own internal signals, not a choice a person makes from a skip button.
	// A deliberate omission for this wave, not an oversight.
)

// jdSkipRequestFor maps a KnightLoader AbortScope onto the JD SkipRequest
// value that has the matching real effect - see AbortScope's own doc comment
// for why the KnightLoader-facing names differ from JD's constant spelling.
// Any AbortScope this package does not recognise (there is no such value
// today; this is defence against a future one being added here without a
// matching case) resolves to jdSkipSingle, the narrowest, least surprising
// effect - never silently blacklisting more than asked.
func jdSkipRequestFor(scope AbortScope) jdSkipRequest {
	switch scope {
	case AbortBlacklistHoster:
		return jdSkipBlockHoster
	case AbortBlacklistEverywhere:
		return jdSkipBlockAllCaptchas
	default:
		return jdSkipSingle
	}
}

// jdCaptchaAPI is the narrow slice of JD's captcha namespace JDSource needs -
// exactly what jdClient implements against a real JD sidecar, and what a
// test fakes instead of one. Named for what it is asked to do, the way
// internal/hosterauth's jdAccounts is.
//
// getCaptchaJob is deliberately absent: list already returns the identical
// CaptchaJob shape per entry (CaptchaAPISolver.list calls its own
// getCaptchaJob once per pending challenge and collects the results), so a
// second method that fetches the exact same fields one at a time would only
// exist to be unused. It is also the one captcha method whose live
// not-found behaviour does not match its own /help page - a bare HTTP 200
// {} rather than the 404 every other method answers with - which this
// package never has to reconcile precisely because it never calls it.
// keepAlive is absent for the reason in jdCaptchaJob's own doc comment: list
// already carries a live countdown, so nothing here has a use for it.
type jdCaptchaAPI interface {
	list(ctx context.Context) ([]jdCaptchaJob, error)
	image(ctx context.Context, id int64) (string, error)
	widgetToken(ctx context.Context, id int64) (jdWidgetToken, error)
	solve(ctx context.Context, id int64, text string) (stillValid bool, err error)
	skip(ctx context.Context, id int64, scope jdSkipRequest) error
}

// jdClient is the real jdCaptchaAPI, talking to headless JD's Deprecated API
// exactly the way internal/hosterauth/jdclient.go's own call() does: GET, one
// URL-encoded JSON blob per positional parameter, a {"data": ...} envelope on
// success. Kept as a private, minimal copy of that convention rather than a
// shared helper, for the reason this file's own package comment gives.
type jdClient struct {
	base string
	hc   *http.Client
}

func newJDClient(base string) *jdClient {
	return &jdClient{base: strings.TrimRight(base, "/"), hc: httpx.New(httpx.Options{Timeout: 15 * time.Second})}
}

// jdAPIError is JD's own error envelope - verified live against a real
// sidecar (see this file's package comment, source (3)):
// {"src":"DEVICE","data":null,"type":"NOT_AVAILABLE"} on HTTP 404, and
// matching CaptchaAPI.Error's two values (NOT_AVAILABLE,
// UNKNOWN_CHALLENGETYPE, both ERROR_NOT_FOUND) in source (2). typ is what
// isNotAvailable keys on to tell "this id is gone" apart from every other
// failure.
type jdAPIError struct {
	path   string
	status int
	typ    string // JD's own "type" field; empty when the body did not parse as this envelope at all
	body   string // kept for the error string in that case
}

func (e *jdAPIError) Error() string {
	if e.typ != "" {
		return fmt.Sprintf("jd %s: HTTP %d (%s)", e.path, e.status, e.typ)
	}
	return fmt.Sprintf("jd %s: HTTP %d: %s", e.path, e.status, e.body)
}

func newJDAPIError(path string, status int, body []byte) *jdAPIError {
	var env struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &env) == nil && env.Type != "" {
		return &jdAPIError{path: path, status: status, typ: env.Type}
	}
	return &jdAPIError{path: path, status: status, body: trunc(body)}
}

// isNotAvailable reports whether err is JD saying a captcha id is gone - the
// direct, authoritative "did this arrive too late" signal Source.Answer's
// stillValid and Source.Abort's idempotent-on-gone behaviour are built on,
// rather than either being guessed at from a client-side timer.
func isNotAvailable(err error) bool {
	var e *jdAPIError
	return errors.As(err, &e) && e.typ == "NOT_AVAILABLE"
}

func trunc(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// call invokes a method: GET /namespace/method?<enc(param0)>&<enc(param1)>...
// Each parameter is URL-encoded JSON; the response envelope is
// {"data": <result>} on success, JD's own error shape (see jdAPIError)
// otherwise.
func (c *jdClient) call(ctx context.Context, path string, params ...any) (json.RawMessage, error) {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		b, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		parts = append(parts, url.QueryEscape(string(b)))
	}
	u := c.base + path
	if len(parts) > 0 {
		u += "?" + strings.Join(parts, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Same scrub internal/resolver/jd/client.go and internal/hosterauth/jdclient.go
	// apply: JD's Deprecated API can emit non-UTF-8 bytes inside string values.
	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), "�"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newJDAPIError(path, resp.StatusCode, body)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("jd %s: bad json: %w", path, err)
	}
	return env.Data, nil
}

func (c *jdClient) list(ctx context.Context) ([]jdCaptchaJob, error) {
	data, err := c.call(ctx, "/captcha/list")
	if err != nil {
		return nil, err
	}
	var out []jdCaptchaJob
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("jd /captcha/list: bad json: %w", err)
	}
	return out, nil
}

// image fetches KindImage/KindClick's payload: JD's own base64 image data,
// exactly as CaptchaAPI.get(id) with no format returns it - see this file's
// package comment for why that is JSON, not raw bytes, on a modern JD.
func (c *jdClient) image(ctx context.Context, id int64) (string, error) {
	data, err := c.call(ctx, "/captcha/get", id)
	if err != nil {
		return "", err
	}
	var out string
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("jd /captcha/get: bad json: %w", err)
	}
	return out, nil
}

// widgetToken fetches KindWidget's real payload - format=rawtoken is load-
// bearing, not decoration: see this file's package comment for what the same
// call answers without it.
func (c *jdClient) widgetToken(ctx context.Context, id int64) (jdWidgetToken, error) {
	data, err := c.call(ctx, "/captcha/get", id, jdRawTokenFormat)
	if err != nil {
		return jdWidgetToken{}, err
	}
	var out jdWidgetToken
	if err := json.Unmarshal(data, &out); err != nil {
		return jdWidgetToken{}, fmt.Errorf("jd /captcha/get?format=rawtoken: bad json: %w", err)
	}
	return out, nil
}

// solve submits text as id's answer, always with resultFormat="rawtoken" -
// verified safe to send unconditionally for every challenge family this
// package classifies (see this file's package comment: the image/click
// families' own parseAPIAnswer ignore resultFormat entirely, and the widget
// families require exactly this value to read the answer as a token instead
// of falling back to an image sub-challenge parse). The bool JD returns is
// propagated rather than assumed true on a nil error: source (2) shows no
// path that returns false today, but decoding it costs nothing and a future
// JD build adding one would otherwise be silently ignored.
func (c *jdClient) solve(ctx context.Context, id int64, text string) (bool, error) {
	data, err := c.call(ctx, "/captcha/solve", id, text, jdRawTokenFormat)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := json.Unmarshal(data, &ok); err != nil {
		return false, fmt.Errorf("jd /captcha/solve: bad json: %w", err)
	}
	return ok, nil
}

func (c *jdClient) skip(ctx context.Context, id int64, scope jdSkipRequest) error {
	_, err := c.call(ctx, "/captcha/skip", id, scope)
	return err
}

// ErrJDNotConfigured is returned by every JDSource method when jdBase() is
// empty: no headless JD is configured, an ordinary, common state (KL_JD
// unset) rather than a failure worth logging on every poll tick. Mirrors
// internal/hosterauth's own unexported errJDNotConfigured for the identical
// situation; exported here because this package's caller lives in a
// different package (internal/app) and needs errors.Is across that boundary.
var ErrJDNotConfigured = errors.New("captcha: no JD sidecar is configured (KL_JD is unset)")

// JDSource is the Source backed by a headless JD's Deprecated API.
type JDSource struct {
	jdBase    func() string
	newClient func(base string) jdCaptchaAPI
	// resolveTask maps a JD download-link id (jdCaptchaJob.Link) to the
	// KnightLoader task it belongs to - see NewJDSource.
	resolveTask func(jdLinkID int64) (taskID string, ok bool)
}

var _ Source = (*JDSource)(nil)

// NewJDSource builds a Source backed by a headless JD, reached at whatever
// jdBase returns. jdBase is called fresh on every List/Answer/Abort, not
// captured once - matching internal/hosterauth's Reconciler: a JD address
// that changes (a recreated container, a KL_JD edit) has to be picked up
// without reconstructing this value. An empty jdBase() is not a case a
// caller has to detect first: every method below answers ErrJDNotConfigured
// for it, the same sentinel internal/hosterauth uses for the same situation,
// so a poll loop can treat "not configured" as quiet exactly the way
// hosterauth.Reconciler.Run already does.
//
// resolveTask maps a JD download-link id to the KnightLoader task it blocks,
// when the caller can say. This package has no access to app-level task
// state and must not import internal/app to get it - see this wave's file-
// ownership split (build-plan.md section 8, section 3's Wave 7 table): 7A
// owns app_captcha.go, where the real index from a JD link to a core.Task.ID
// lives (internal/resolver/jd.Backend.linkIDs today only maps the other
// direction, taskID -> JD link ids - 7A's own seam to add the reverse one to,
// not this package's to reach into). resolveTask may be nil, and every test
// in this package passes nil: every Challenge.TaskID is then left empty,
// which is the honest answer for a caller that has not wired the mapping in,
// not a defect this package needs to work around.
func NewJDSource(jdBase func() string, resolveTask func(jdLinkID int64) (taskID string, ok bool)) *JDSource {
	return &JDSource{
		jdBase:      jdBase,
		newClient:   func(base string) jdCaptchaAPI { return newJDClient(base) },
		resolveTask: resolveTask,
	}
}

func (s *JDSource) client() (jdCaptchaAPI, error) {
	base := strings.TrimSpace(s.jdBase())
	if base == "" {
		return nil, ErrJDNotConfigured
	}
	return s.newClient(base), nil
}

// List asks JD for every pending challenge and fetches each one's payload -
// see build below. One failure fails the whole call rather than silently
// omitting the one challenge whose payload fetch failed: build-plan.md
// section 9 package 16 is explicit that "a relay that drops challenges is
// worse than none", and a captcha List quietly returned short of what JD
// actually reports is exactly that, indistinguishable from "solved" or
// "expired" to a caller with no way to tell the difference. Fetches run
// sequentially, not in parallel: real concurrent captchas are rare enough,
// and each fetch cheap enough (a local sidecar, a few KB), that the added
// goroutine-lifetime and error-aggregation complexity would cost more than
// the latency it saves - the same "simplest safe default" call build-plan.md
// section 9 package 16 makes for fetching the image into the descriptor at
// all.
func (s *JDSource) List(ctx context.Context) ([]Challenge, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	jobs, err := c.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("captcha: listing JD's challenges: %w", err)
	}
	out := make([]Challenge, 0, len(jobs))
	for _, job := range jobs {
		ch, err := s.build(ctx, c, job)
		if err != nil {
			return nil, fmt.Errorf("captcha: fetching payload for JD challenge %d: %w", job.ID, err)
		}
		out = append(out, ch)
	}
	return out, nil
}

// build turns one jdCaptchaJob into a Challenge, fetching whatever payload
// its Kind needs.
func (s *JDSource) build(ctx context.Context, c jdCaptchaAPI, job jdCaptchaJob) (Challenge, error) {
	className := job.ChallengeType
	if className == "" {
		className = job.Type
	}
	kind := classify(className)

	ch := Challenge{
		ID:     strconv.FormatInt(job.ID, 10),
		Source: SourceJD,
		Host:   job.Hoster,
		Kind:   kind,
		Prompt: job.Explain,
	}
	if job.Link != 0 && s.resolveTask != nil {
		if taskID, ok := s.resolveTask(job.Link); ok {
			ch.TaskID = taskID
		}
	}
	// Remaining <= 0 is JD's own "no timeout configured" / "already due" -
	// see jdCaptchaJob's doc comment - which this package leaves as the zero
	// time rather than fabricating a deadline JD never gave it.
	if job.Remaining > 0 {
		ch.ExpiresAt = time.Now().Add(time.Duration(job.Remaining) * time.Millisecond)
	}

	switch kind {
	case KindImage, KindClick:
		raw, err := c.image(ctx, job.ID)
		if err != nil {
			return Challenge{}, err
		}
		ch.Payload = &ImagePayload{DataURL: normalizeImageDataURL(raw)}
	case KindWidget:
		tok, err := c.widgetToken(ctx, job.ID)
		if err != nil {
			return Challenge{}, err
		}
		ch.Payload = &WidgetPayload{
			SiteKey:     tok.SiteKey,
			SiteURL:     tok.SiteURL,
			ContextURL:  tok.ContextURL,
			Type:        tok.Type,
			Enterprise:  tok.Enterprise,
			V3Action:    tok.V3Action,
			SecureToken: tok.SecureToken,
		}
	default:
		ch.Payload = &UnsupportedPayload{Vendor: className}
	}
	return ch, nil
}

// normalizeImageDataURL fixes a gap in JD's own
// ImageCaptchaChallenge.getAPIStorable (see this file's package comment):
// three of its four branches (gif/png/jpg-or-jpeg) answer
// "image/xxx;base64,<data>" with no "data:" scheme, which no <img src>
// renders; only the fourth (an unrecognised extension, via
// IconIO.toDataUrl) answers a complete "data:image/jpg;base64,..." URL.
// Both of JD's own shapes are handled here so every Challenge this package
// hands out is always a complete, renderable URL regardless of which one JD
// happened to send.
func normalizeImageDataURL(raw string) string {
	if strings.HasPrefix(raw, "data:") {
		return raw
	}
	return "data:" + raw
}

// Answer implements Source.
func (s *JDSource) Answer(ctx context.Context, id string, text string) (bool, error) {
	c, err := s.client()
	if err != nil {
		return false, err
	}
	jdID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return false, fmt.Errorf("captcha: %q is not a JD challenge id: %w", id, err)
	}
	ok, err := c.solve(ctx, jdID, text)
	if err == nil {
		return ok, nil
	}
	if isNotAvailable(err) {
		// The direct answer to "did this arrive too late" - see Source.Answer's
		// own doc comment. Not itself an application error.
		return false, nil
	}
	return false, fmt.Errorf("captcha: answering JD challenge %s: %w", id, err)
}

// Abort implements Source.
func (s *JDSource) Abort(ctx context.Context, id string, scope AbortScope) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	jdID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("captcha: %q is not a JD challenge id: %w", id, err)
	}
	err = c.skip(ctx, jdID, jdSkipRequestFor(scope))
	if err == nil || isNotAvailable(err) {
		// Gone already is the state Abort exists to reach - see Source.Abort's
		// own doc comment.
		return nil
	}
	return fmt.Errorf("captcha: aborting JD challenge %s: %w", id, err)
}
