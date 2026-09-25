package captcha

// The JD-backed Source: the headless JD sidecar's local "Deprecated API"
// (plain HTTP JSON, no cloud, no crypto), with the same
// namespace/method/{"data":...} envelope internal/resolver/jd/client.go and
// internal/hosterauth/jdclient.go use.
//
// On a headless JD with no MyJDownloader session /captcha/list stays empty
// however many challenges JD is holding. CaptchaAPISolver.enqueue marks a job
// done the moment it is created unless isMyJDownloaderActive(), and list only
// emits jobs that are not done; the dialog solvers that would otherwise hold
// the job are guarded by !Application.isHeadless(). Challenges reach this file
// only once a solver key is pushed into JD's own solver config or JD runs with
// a display. Until then core.Task.Note carries JD's package status instead, see
// internal/resolver/jd/backend.go.
//
// The shapes below follow JD's current source rather than the Deprecated API's
// own worked example, which predates click and widget challenges and disagrees
// in four places.
//
//	https://github.com/svn2github/jdownloader-jdjsapi/blob/master/deprecated/example/captcha.html
//	https://github.com/mirror/jdownloader/tree/master/src/org/jdownloader/api/captcha
//
//   - get(id) answers JSON, either "<mime>;base64,..." or a full "data:" URL
//     depending on which branch of ImageCaptchaChallenge.getAPIStorable ran.
//     normalizeImageDataURL covers both.
//   - There is no abort route. skip(id, SkipRequest) is what is wired to
//     /captcha/skip; jdSkipRequestFor holds the mapping.
//   - solve throws InvalidCaptchaIDException for a stale id, which
//     CaptchaAPI.Error maps to HTTP 404 NOT_AVAILABLE, instead of returning
//     false. Answer turns that back into the boolean its callers want.
//   - get(id) without format=rawtoken answers an image even for a widget
//     challenge, because RecaptchaV2Challenge and HCaptchaChallenge fall back
//     to a basic captcha storable.

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
// field for field. Remaining is JD's Challenge.getRemainingTimeout(), validUntil
// minus now, recomputed on every read, which is why Challenge.ExpiresAt is
// built from it rather than from created plus timeout: a challenge something
// calls keepAlive on reports a longer number here with no extra call.
type jdCaptchaJob struct {
	ID            int64  `json:"id"`
	Hoster        string `json:"hoster"`
	Link          int64  `json:"link"`
	Type          string `json:"type"`
	ChallengeType string `json:"challengeType"`
	// CaptchaCategory is decoded but never read: it is a hoster name for one
	// challenge family, the class name for another and a stable tag for a
	// third, so classify keys on ChallengeType instead.
	CaptchaCategory string `json:"captchaCategory"`
	Explain         string `json:"explain"`
	Timeout         int64  `json:"timeout"`
	Created         int64  `json:"created"`
	Remaining       int    `json:"remaining"`
}

// jdKindByClass maps a JD challenge class's simple name, which
// CaptchaAPISolver.getCaptchaJob writes into both Type and ChallengeType, onto
// this package's Kind.
//
// An exact-match table rather than a prefix or substring guess: a class this
// table has not seen classifies KindUnsupported with its real name kept, which
// is more use than a guess from a name pattern. In practice only the classes
// CaptchaAPISolver.isChallengeSupported lets through reach it, so
// KindUnsupported fires for a class JD supports and this app has no renderer
// for. AccountLoginOAuthChallenge is the live example: it never overrides
// getAPIStorable, so there is no payload to relay for it either way.
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

// jdWidgetVendorByClass names the vendor behind each KindWidget class. JD's
// rawtoken payload has the same fields for both, so the class is the only
// place the vendor shows.
var jdWidgetVendorByClass = map[string]string{
	"RecaptchaV2Challenge": VendorRecaptcha,
	"HCaptchaChallenge":    VendorHCaptcha,
}

// classify turns one JD challenge class name into a Kind, defaulting to
// KindUnsupported for anything jdKindByClass does not list.
func classify(challengeType string) Kind {
	if k, ok := jdKindByClass[challengeType]; ok {
		return k
	}
	return KindUnsupported
}

// jdRawTokenFormat is JD's RAWTOKEN constant: as a getAPIStorable format it
// returns sitekey data instead of an image fallback, and as a parseAPIAnswer
// resultFormat it makes a widget challenge read its answer as a token.
const jdRawTokenFormat = "rawtoken"

// jdWidgetToken is captcha/get?id&format=rawtoken's payload for KindWidget,
// field for field from RecaptchaV2Challenge.RecaptchaV2APIStorable and
// HCaptchaChallenge.HCaptchaAPIStorable. hCaptcha's Storable has no enterprise
// or v3Action field and hardcodes getStoken() to null, so those three decode to
// the zero value for that vendor.
type jdWidgetToken struct {
	SiteKey     string `json:"siteKey"`
	SiteURL     string `json:"siteUrl"`
	ContextURL  string `json:"contextUrl"`
	Type        string `json:"type"`
	Enterprise  bool   `json:"enterprise"`
	SecureToken string `json:"stoken"`
	V3Action    string `json:"v3Action"`
}

// jdSkipRequest mirrors jd.controlling.captcha.SkipRequest's values. Only the
// three an AbortScope can reach are named; see jdSkipRequestFor.
type jdSkipRequest string

const (
	jdSkipSingle           jdSkipRequest = "SINGLE"
	jdSkipBlockHoster      jdSkipRequest = "BLOCK_HOSTER"
	jdSkipBlockAllCaptchas jdSkipRequest = "BLOCK_ALL_CAPTCHAS"
	// JD also has BLOCK_PACKAGE, REFRESH, STOP_CURRENT_ACTION and TIMEOUT. The
	// app has no package-scoped skip to map BLOCK_PACKAGE onto, and the other
	// three are JD's internal signals rather than a choice a person makes.
)

// jdSkipRequestFor maps an AbortScope onto the JD SkipRequest with the matching
// effect. An unrecognised scope resolves to jdSkipSingle, the narrowest one, so
// a scope added without a case here cannot blacklist more than was asked.
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

// jdCaptchaAPI is the slice of JD's captcha namespace JDSource needs: what
// jdClient implements against a real sidecar and what a test fakes.
//
// getCaptchaJob is left out because list already returns the same CaptchaJob
// shape per entry, and keepAlive because list carries a live countdown.
type jdCaptchaAPI interface {
	list(ctx context.Context) ([]jdCaptchaJob, error)
	image(ctx context.Context, id int64) (string, error)
	widgetToken(ctx context.Context, id int64) (jdWidgetToken, error)
	solve(ctx context.Context, id int64, text string) (stillValid bool, err error)
	skip(ctx context.Context, id int64, scope jdSkipRequest) error
}

// jdClient is the real jdCaptchaAPI, talking to headless JD's Deprecated API
// the way internal/hosterauth/jdclient.go's call does: GET, one URL-encoded
// JSON blob per positional parameter, a {"data": ...} envelope on success.
type jdClient struct {
	base string
	hc   *http.Client
}

func newJDClient(base string) *jdClient {
	return &jdClient{base: strings.TrimRight(base, "/"), hc: httpx.New(httpx.Options{Timeout: 15 * time.Second})}
}

// jdAPIError is JD's error envelope, {"src":...,"data":null,"type":...} on an
// HTTP 404, carrying CaptchaAPI.Error's NOT_AVAILABLE or UNKNOWN_CHALLENGETYPE.
// isNotAvailable keys on typ to tell "this id is gone" from any other failure.
type jdAPIError struct {
	path   string
	status int
	typ    string // JD's "type" field; empty when the body was not this envelope
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

// isNotAvailable reports whether err is JD saying a captcha id is gone, the
// signal Source.Answer's stillValid and Source.Abort's idempotence rest on.
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
	// JD's Deprecated API can emit non-UTF-8 bytes inside string values, the
	// same scrub internal/resolver/jd/client.go applies.
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

// image fetches KindImage and KindClick's payload: JD's base64 image data, as
// CaptchaAPI.get(id) with no format returns it.
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

// widgetToken fetches KindWidget's payload. Without format=rawtoken the same
// call answers an image.
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

// solve submits text as id's answer, always with resultFormat="rawtoken". The
// image and click families ignore resultFormat, and the widget families need
// this value to read the answer as a token rather than as an image
// sub-challenge. The bool JD returns is passed on rather than assumed true on a
// nil error, so a JD build that starts returning false is not ignored.
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
// empty. No headless JD configured is an ordinary state, not a failure worth
// logging on every poll tick, so a caller can match on it and stay quiet.
var ErrJDNotConfigured = errors.New("captcha: no JDownloader backend is configured (KL_JD is unset)")

// JDSource is the Source backed by a headless JD's Deprecated API.
type JDSource struct {
	jdBase    func() string
	newClient func(base string) jdCaptchaAPI
	// resolveTask maps a JD download-link id (jdCaptchaJob.Link) to the task it
	// belongs to; see NewJDSource.
	resolveTask func(jdLinkID int64) (taskID string, ok bool)
}

var _ Source = (*JDSource)(nil)

// NewJDSource builds a Source backed by a headless JD reached at whatever
// jdBase returns. jdBase is called on every List, Answer and Abort rather than
// captured, so a JD address that changes is picked up without rebuilding this
// value. An empty jdBase() yields ErrJDNotConfigured from every method, which a
// poll loop can treat as quiet.
//
// resolveTask maps a JD download-link id to the task it blocks where the caller
// can say. This package has no access to app-level task state, so the index
// lives in internal/app. A nil resolveTask leaves every Challenge.TaskID empty.
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

// List asks JD for every pending challenge and fetches each one's payload.
//
// A failed payload fetch fails the whole call rather than dropping that one
// challenge, because a short list is indistinguishable from "solved" or
// "expired" to the caller. The fetches run one after another: concurrent
// captchas are rare and each fetch is a few KB from a local sidecar.
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
	// ChallengeType first: JD rewrites Type to RecaptchaV2Challenge for an
	// hCaptcha when it believes MyJDownloader's web interface is asking.
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
	// Remaining <= 0 is JD saying "no timeout configured" or "already due",
	// left as the zero time rather than turned into an invented deadline.
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
			Vendor:      jdWidgetVendorByClass[className],
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

// normalizeImageDataURL covers both shapes ImageCaptchaChallenge.getAPIStorable
// produces: "image/xxx;base64,..." without the "data:" scheme, which no
// <img src> renders, and the complete data URL its fallback branch builds.
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
		// The answer arrived too late, which Source.Answer reports as
		// stillValid=false rather than as an error.
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
		// Gone already is the state Abort exists to reach.
		return nil
	}
	return fmt.Errorf("captcha: aborting JD challenge %s: %w", id, err)
}
