package captcha

// Shared solver machinery: the Solver contract both automatic clients in
// this lane implement, the click-answer encoder they both need, and the
// small HTTP/poll helpers neither provider's wire format actually owns. It
// lives in this file rather than a third one because solver_2captcha.go and
// solver_anticaptcha.go are the whole of Wave 7 agent 7B's lane in this
// package (see docs/build-plan.md, Wave 7's "7B: captcha settings + solver
// order + two solver clients") - splitting shared code across two files one
// agent owns is an ordinary organisational choice, not the cross-lane reach
// jdclient.go (internal/hosterauth) declines for a different reason: that
// file keeps its own copy of a JD call() helper specifically BECAUSE
// internal/resolver/jd/client.go belongs to a different wave's agent, not
// because sharing is bad in general.
//
// "Solver order" (build-plan.md section 3, Wave 7) means: before a download
// ever reaches 7A's human prompt modal, KnightLoader tries each configured
// automatic solver in the user's chosen order and only falls through to a
// person when none are configured or every one of them declines or fails.
// Both solvers here are that automatic step - image and click challenges
// only. A widget challenge (site-key reCAPTCHA/hCaptcha) is out of scope:
// solving one needs a headless browser rendering the vendor's own JS on the
// hoster's page, which is a different, heavier integration on both
// providers' APIs than either exposes through a plain image/coordinates
// task, and it is not required by this wave's rows.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Solver is an automatic captcha-solving backend, tried in a configured
// order before KnightLoader ever shows a human the prompt modal - see this
// file's package comment and build-plan.md section 9 package 16. Both
// TwoCaptchaSolver and AntiCaptchaSolver implement it, so a caller holding a
// list built from settings.Settings' solver order can try each in turn
// without a type switch.
type Solver interface {
	// Solve submits image and returns the exact text to hand to
	// Source.Answer unchanged - recognized text for KindImage, JD's own
	// click-answer JSON for KindClick (see encodeClickAnswer). image is
	// Challenge.Payload's own DataURL field (ImagePayload/ClickPayload - a
	// full "data:image/...;base64,..." string) or a bare base64 string;
	// either is accepted, see decodeSolverImage. prompt is Challenge.Prompt,
	// "" when the challenge carried none - both services accept free-text
	// worker instructions and solve more accurately with one than without.
	//
	// kind must be KindImage or KindClick. Anything else (KindWidget,
	// KindUnsupported) returns ErrUnsupportedKind before any network call is
	// made - see this file's package comment for why widget solving is out
	// of scope here.
	Solve(ctx context.Context, kind Kind, image, prompt string) (string, error)
}

// ErrUnsupportedKind is what every Solve returns for a Kind neither solver
// in this package can act on - see Solver.Solve's own doc comment.
var ErrUnsupportedKind = errors.New("captcha: solver only handles KindImage and KindClick")

// solverPollInterval and solverMaxWait are var, not const, only so a test
// can shorten them - the same reason internal/app/app_accounts.go's
// accountHealthInterval is a var; this package's own tests are the ones
// that do it, shortening both to keep an httptest.Server round-trip test
// from taking real minutes.
var (
	// solverPollInterval is how long a Solve call waits between
	// getTaskResult polls. 2Captcha's own get-task-result documentation says
	// so directly ("wait at least 5 seconds and repeat the request");
	// Anti-Captcha's equivalent page states no interval of its own, so the
	// same, more conservative number is used for both rather than guessing a
	// shorter one that risks either service's own rate limit.
	solverPollInterval = 5 * time.Second

	// solverMaxWait bounds one Solve call's OWN poll loop, independent of
	// whatever deadline ctx already carries - context.WithTimeout takes
	// whichever of the two is sooner, so a caller with a tighter deadline
	// (say, one derived from Challenge.ExpiresAt) is never made to wait
	// longer than it asked for. This exists for the caller that passes
	// context.Background(): both services' own official client libraries
	// default to a comparable ceiling, and most image/click solves complete
	// in well under a minute, so this is a generous, purely defensive cap
	// against a queue-starved worker pool pinning the calling goroutine for
	// the life of the process - the same reasoning internal/httpx's own
	// ceilings document for exactly this failure shape.
	solverMaxWait = 180 * time.Second
)

// solverPoint is one resolved click location, in the pixel space of the
// image a Source handed out - the shape both solver clients reduce a
// provider's own coordinate answer to before handing it to
// encodeClickAnswer.
type solverPoint struct{ X, Y int }

// solverAnswerFor turns one provider's solved text/points into the exact
// string Source.Answer expects, dispatching on kind so neither client has to
// repeat this decision.
func solverAnswerFor(kind Kind, text string, points []solverPoint) (string, error) {
	switch kind {
	case KindImage:
		if text == "" {
			return "", errors.New("captcha: solver reported success with no text")
		}
		return text, nil
	case KindClick:
		return encodeClickAnswer(points)
	default:
		return "", ErrUnsupportedKind
	}
}

// encodeClickAnswer turns resolved click points into the exact string
// Source.Answer must be given for a KindClick challenge to be understood on
// the JD side - JSON, not a delimited list, and one of two shapes depending
// on how many points there are.
//
// VERIFIED against JD's own source (fetched 2026-08-10), the same rigor
// jdsource.go applies to the rest of this package - jdsource.go's own
// citations establish THAT click/multi-click parseAPIAnswer exist and that
// resultFormat is irrelevant to them; these four pin the exact string they
// parse, which jdsource.go's own comment does not go into:
//
//	https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/clickcaptcha/ClickCaptchaChallenge.java
//	  parseAPIAnswer: JSonStorage.restoreFromString(result, TypeRef<ClickedPoint>) -
//	  the answer text is parsed as JSON, not a plain string.
//	https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/clickcaptcha/ClickedPoint.java
//	  one point, two int fields with the obvious getX()/getY() -> "x"/"y" JSON keys:
//	  {"x":<int>,"y":<int>}
//	https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/multiclickcaptcha/MultiClickCaptchaChallenge.java
//	  parseAPIAnswer: JSonStorage.restoreFromString(result, TypeRef<MultiClickedPoint>)
//	https://github.com/mirror/jdownloader/blob/master/src/org/jdownloader/captcha/v2/challenge/multiclickcaptcha/MultiClickedPoint.java
//	  several points as PARALLEL int arrays, not an array of pairs:
//	  {"x":[<int>,...],"y":[<int>,...]}
//
// Challenge/ClickPayload (challenge.go) carry no field saying which of the
// two challenge families produced a given KindClick instance - both
// ClickCaptchaChallenge and MultiClickCaptchaChallenge classify to the one
// Kind (see jdsource.go's jdKindByClass), and JD's own CaptchaJob never
// surfaces an expected click count either. So this picks the shape from the
// number of points a solver actually resolved: exactly one point encodes as
// ClickedPoint, more than one as MultiClickedPoint. That is the best signal
// available without a change to the shared type, and it is not a gap this
// solver invented alone - whoever builds the human-click side of the prompt
// modal (7A) faces the identical ambiguity for a person's own clicks and
// needs the same answer.
func encodeClickAnswer(points []solverPoint) (string, error) {
	if len(points) == 0 {
		return "", errors.New("captcha: solver returned no click points")
	}
	if len(points) == 1 {
		b, err := json.Marshal(struct {
			X int `json:"x"`
			Y int `json:"y"`
		}{points[0].X, points[0].Y})
		return string(b), err
	}
	xs := make([]int, len(points))
	ys := make([]int, len(points))
	for i, p := range points {
		xs[i], ys[i] = p.X, p.Y
	}
	b, err := json.Marshal(struct {
		X []int `json:"x"`
		Y []int `json:"y"`
	}{xs, ys})
	return string(b), err
}

// decodeSolverImage turns image - a full "data:image/...;base64,..." string
// (ImagePayload.DataURL/ClickPayload.DataURL, already normalized that way by
// jdsource.go's normalizeImageDataURL) or a bare base64 string, either is
// accepted - into the canonical, standard-padded base64 both providers'
// current docs describe accepting: "Image encoded into Base64 format...
// Data-URI format is also supported" (2Captcha's normal-captcha page) and a
// plain "Base64-encoded image file" (Anti-Captcha's ImageToTextTask page).
//
// Decoding tolerantly and re-encoding with the one flavour both services'
// own examples show - rather than forwarding the substring after "base64,"
// verbatim - guards against a URL-safe or unpadded variant a future Source
// might hand out. That costs a few dozen CPU cycles on an image capped at
// 100 kB (2Captcha's own documented limit) and removes the question
// entirely rather than leaving it to surface as an opaque provider-side
// decode error.
func decodeSolverImage(image string) (string, error) {
	s := image
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("captcha: no image data to solve")
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(s); err == nil {
			return base64.StdEncoding.EncodeToString(raw), nil
		}
	}
	return "", errors.New("captcha: image data is not valid base64")
}

// solverSleep waits d or returns ctx's error, whichever comes first - the
// poll-loop primitive both clients share. A plain time.After here would leak
// its timer for up to d past every ctx cancellation; there are at most a
// few dozen of these per Solve call (solverMaxWait / solverPollInterval), so
// it would not matter in practice, but Stop costs one line and removes the
// question entirely.
func solverSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// postJSON POSTs in as a JSON body to url and decodes the JSON response into
// out - the one HTTP shape both 2Captcha's and Anti-Captcha's current JSON
// APIs share (POST, application/json, a small control-plane response body),
// even though what is IN that response differs per provider and per call.
// Kept generic rather than duplicated per client: this part carries no
// provider-specific knowledge at all, unlike the task/solution shapes below,
// which genuinely differ per service and are verified independently for
// each rather than assumed from the other.
func postJSON(ctx context.Context, hc *http.Client, url string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: HTTP %s: %w", url, resp.Status, err)
	}
	return nil
}

// ---- 2Captcha ---------------------------------------------------------
//
// TwoCaptchaSolver drives 2Captcha's current JSON API (api.2captcha.com) to
// solve an image or click captcha automatically.
//
// VERIFIED against 2Captcha's own current documentation (fetched
// 2026-08-10), not against a third-party wrapper or JDownloader's
// JAntiCaptcha.java, whose internals may differ from the public API
// surface:
//
//	https://2captcha.com/api-docs/create-task
//	  the generic createTask envelope: POST https://api.2captcha.com/createTask,
//	  {"clientKey":...,"task":{...}} in; {"errorId":0,"taskId":N} out on
//	  success, {"errorId":N,"errorCode":"...","errorDescription":"..."} on
//	  failure - createTask itself never returns a solution, only a taskId.
//	https://2captcha.com/api-docs/normal-captcha
//	  ImageToTextTask's fields: this client sets only "body" (and "comment"
//	  when Challenge.Prompt is non-empty) and leaves phrase/case/numeric/
//	  math/minLength/maxLength/languagePool at their documented defaults -
//	  none of that per-challenge detail is available from a Challenge.
//	https://2captcha.com/api-docs/coordinates
//	  CoordinatesTask: the same "body"/"comment" fields, solution shape
//	  {"coordinates":[{"x":N,"y":N}, ...]} - object pairs, distinct from
//	  Anti-Captcha's own array-pair shape (solver_anticaptcha.go).
//	  minClicks/maxClicks are left unset: nothing this package has access to
//	  (Challenge carries no expected-click-count field - see
//	  encodeClickAnswer) says how many clicks a given challenge wants.
//	https://2captcha.com/api-docs/get-task-result
//	  POST https://api.2captcha.com/getTaskResult, {"clientKey","taskId"} in;
//	  {"errorId":0,"status":"processing"} while unsolved,
//	  {"errorId":0,"status":"ready","solution":{...}} once solved. The
//	  page's own guidance - "wait at least 5 seconds and repeat the
//	  request" - is solverPollInterval.
//	https://2captcha.com/api-docs/error-codes
//	  the errorCode vocabulary (ERROR_ZERO_BALANCE, ERROR_KEY_DOES_NOT_EXIST,
//	  ERROR_CAPTCHA_UNSOLVABLE among them) this file's errors carry through
//	  verbatim rather than translating into a private taxonomy - 7A's
//	  fallthrough only needs "this solver did not produce an answer", and the
//	  service's own words are more useful in a log line than a
//	  KnightLoader-invented paraphrase would be.

const twoCaptchaBase = "https://api.2captcha.com"

// TwoCaptchaSolver is a Solver backed by one 2Captcha account.
type TwoCaptchaSolver struct {
	key  string
	base string
	hc   *http.Client
}

// NewTwoCaptchaSolver builds a solver for the given account API key - the
// same key 2Captcha's own dashboard (https://2captcha.com/enterpage, linked
// from https://2captcha.com/api-docs/quick-start as "obtain your API key
// from the Dashboard") shows, and what internal/accounts stores under
// catalogue id "2captcha".
func NewTwoCaptchaSolver(apiKey string) *TwoCaptchaSolver {
	return &TwoCaptchaSolver{key: apiKey, base: twoCaptchaBase, hc: httpx.New(httpx.Options{Timeout: 20 * time.Second})}
}

type twoCaptchaTask struct {
	Type    string `json:"type"`
	Body    string `json:"body"`
	Comment string `json:"comment,omitempty"`
}

type twoCaptchaCreateReq struct {
	ClientKey string         `json:"clientKey"`
	Task      twoCaptchaTask `json:"task"`
}

type twoCaptchaResultReq struct {
	ClientKey string `json:"clientKey"`
	TaskID    int64  `json:"taskId"`
}

// twoCaptchaEnvelope is the {errorId, errorCode, errorDescription} prefix
// every 2Captcha response carries - see err, called by both createTask and
// getTaskResult's own response types below through embedding.
type twoCaptchaEnvelope struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

// err turns a non-zero ErrorID into a Go error carrying 2Captcha's own code
// and description verbatim (see this file's citation of api-docs/error-codes
// for why that beats a private taxonomy), or nil for a genuinely successful
// response.
func (e twoCaptchaEnvelope) err(op string) error {
	if e.ErrorID == 0 {
		return nil
	}
	if e.ErrorDescription != "" {
		return fmt.Errorf("2captcha %s: %s (%s)", op, e.ErrorDescription, e.ErrorCode)
	}
	return fmt.Errorf("2captcha %s: errorId %d", op, e.ErrorID)
}

type twoCaptchaCreateResp struct {
	twoCaptchaEnvelope
	TaskID int64 `json:"taskId"`
}

// twoCaptchaPoint is CoordinatesTask's own solution shape - an object with
// named x/y keys, verified distinct from Anti-Captcha's array-pair shape
// (see solver_anticaptcha.go's antiCaptchaPoints).
type twoCaptchaPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type twoCaptchaSolution struct {
	Text        string            `json:"text,omitempty"`
	Coordinates []twoCaptchaPoint `json:"coordinates,omitempty"`
}

type twoCaptchaResultResp struct {
	twoCaptchaEnvelope
	Status   string             `json:"status"`
	Solution twoCaptchaSolution `json:"solution"`
}

// Solve implements Solver for 2Captcha - see that interface's own doc
// comment for the contract every caller relies on.
func (s *TwoCaptchaSolver) Solve(ctx context.Context, kind Kind, image, prompt string) (string, error) {
	if s.key == "" {
		return "", errors.New("captcha: no 2Captcha API key configured")
	}
	body, err := decodeSolverImage(image)
	if err != nil {
		return "", fmt.Errorf("2captcha: %w", err)
	}
	task := twoCaptchaTask{Body: body, Comment: prompt}
	switch kind {
	case KindImage:
		task.Type = "ImageToTextTask"
	case KindClick:
		task.Type = "CoordinatesTask"
	default:
		return "", ErrUnsupportedKind
	}

	ctx, cancel := context.WithTimeout(ctx, solverMaxWait)
	defer cancel()

	taskID, err := s.createTask(ctx, task)
	if err != nil {
		return "", err
	}
	text, points, err := s.pollResult(ctx, taskID)
	if err != nil {
		return "", err
	}
	return solverAnswerFor(kind, text, points)
}

func (s *TwoCaptchaSolver) createTask(ctx context.Context, task twoCaptchaTask) (int64, error) {
	var resp twoCaptchaCreateResp
	if err := postJSON(ctx, s.hc, s.base+"/createTask", twoCaptchaCreateReq{ClientKey: s.key, Task: task}, &resp); err != nil {
		return 0, err
	}
	if err := resp.err("createTask"); err != nil {
		return 0, err
	}
	return resp.TaskID, nil
}

// pollResult repeats getTaskResult until 2Captcha reports "ready" (or an
// error, or ctx ends) - see solverPollInterval/solverMaxWait for the pacing
// and the ceiling.
func (s *TwoCaptchaSolver) pollResult(ctx context.Context, taskID int64) (text string, points []solverPoint, err error) {
	for {
		var resp twoCaptchaResultResp
		if err := postJSON(ctx, s.hc, s.base+"/getTaskResult", twoCaptchaResultReq{ClientKey: s.key, TaskID: taskID}, &resp); err != nil {
			return "", nil, err
		}
		if err := resp.err("getTaskResult"); err != nil {
			return "", nil, err
		}
		if resp.Status == "ready" {
			pts := make([]solverPoint, len(resp.Solution.Coordinates))
			for i, p := range resp.Solution.Coordinates {
				pts[i] = solverPoint{X: p.X, Y: p.Y}
			}
			return resp.Solution.Text, pts, nil
		}
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return "", nil, err
		}
	}
}
