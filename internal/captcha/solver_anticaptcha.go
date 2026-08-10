package captcha

// AntiCaptchaSolver drives Anti-Captcha's current JSON API
// (api.anti-captcha.com) to solve an image or click captcha automatically -
// the second of this wave's two "solver order" clients (see
// solver_2captcha.go's package comment for what that means and why widget
// solving is out of scope for both). Shares Solver, solverPoint,
// encodeClickAnswer, decodeSolverImage, solverSleep, postJSON,
// ErrUnsupportedKind and the poll/wait constants with that file - see its
// own top comment for why splitting shared code across two files in one
// agent's own lane is a normal organisational choice.
//
// VERIFIED against Anti-Captcha's own current documentation (fetched
// 2026-08-10), not against JDownloader's JAntiCaptcha.java - that class
// talks to this same service, but its internals (retry counts, field
// choices) are JD's own implementation decisions, not the API contract:
//
//	https://anti-captcha.com/apidoc/methods/createTask
//	  POST https://api.anti-captcha.com/createTask,
//	  {"clientKey":...,"task":{"type":...,"body":...}} in;
//	  {"errorId":0,"taskId":N} out on success,
//	  {"errorId":N,"errorCode":"...","errorDescription":"..."} on failure -
//	  createTask itself never returns a solution, only a taskId, the same
//	  shape 2Captcha's own createTask answers with (independently verified
//	  per service - see solver_2captcha.go - not assumed from this one).
//	https://anti-captcha.com/apidoc/task-types/ImageToTextTask
//	  the task fields: this client sets only "body" (and "comment" when
//	  Challenge.Prompt is non-empty) and leaves case/numeric/math/phrase/
//	  minLength/maxLength/languagePool at their documented defaults - none of
//	  that per-challenge detail is available from a Challenge. Solution shape
//	  {"text":...,"url":...} - only text is used.
//	https://anti-captcha.com/apidoc/task-types/ImageToCoordinatesTask
//	  the click-equivalent task: "body" plus "mode" ("points" or
//	  "rectangles", default "points" - set explicitly here rather than
//	  relying on the documented default, the same reason jdsource.go sends
//	  JD's own format parameter explicitly rather than omitting it). The
//	  VERIFIED response example on this page is for "rectangles" mode only:
//	  {"coordinates":[[17,48,54,83],[76,93,140,164]]} - four numbers per
//	  entry, top-left to bottom-right. The page states in prose that "points"
//	  mode "returns (x,y) coordinate pairs" but its own worked JSON example
//	  was not published at the time this was written, so the exact shape of
//	  a points-mode row (two numbers - [x,y] - was inferred, not read
//	  verbatim) is the one field in this file not confirmed against a literal
//	  example - see antiCaptchaPoints for how that uncertainty is handled
//	  without guessing at a field name nothing on the page confirms.
//	https://anti-captcha.com/apidoc/methods/getTaskResult
//	  POST https://api.anti-captcha.com/getTaskResult,
//	  {"clientKey","taskId"} in; {"errorId":0,"status":"processing"} while
//	  unsolved, {"errorId":0,"status":"ready","solution":{...}} once solved.
//	  No polling interval is documented on this page, so solver_2captcha.go's
//	  solverPollInterval (2Captcha's own stated "at least 5 seconds") is
//	  reused here too, out of caution against a shorter guess tripping
//	  whatever rate limit Anti-Captcha applies to this endpoint.
//	  clientKey/errorId/errorCode/errorDescription vocabulary
//	  (ERROR_KEY_DOES_NOT_EXIST, ERROR_ZERO_BALANCE, ERROR_NO_SLOT_AVAILABLE
//	  among the confirmed ones) is Anti-Captcha's own, carried through
//	  verbatim in errors rather than translated into a private taxonomy - the
//	  same reasoning solver_2captcha.go's citation of 2Captcha's error-codes
//	  page gives, and the two vocabularies happen to rhyme (both services
//	  share this API design's lineage) without this file assuming they are
//	  identical anywhere it has not independently confirmed a value.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

const antiCaptchaBase = "https://api.anti-captcha.com"

// AntiCaptchaSolver is a Solver backed by one Anti-Captcha account.
type AntiCaptchaSolver struct {
	key  string
	base string
	hc   *http.Client
}

// NewAntiCaptchaSolver builds a solver for the given account key ("client
// key" in Anti-Captcha's own vocabulary) - the same value shown on
// https://anti-captcha.com/clients/settings/apisetup, and what
// internal/accounts stores under catalogue id "anticaptcha".
func NewAntiCaptchaSolver(apiKey string) *AntiCaptchaSolver {
	return &AntiCaptchaSolver{key: apiKey, base: antiCaptchaBase, hc: httpx.New(httpx.Options{Timeout: 20 * time.Second})}
}

type antiCaptchaTask struct {
	Type    string `json:"type"`
	Body    string `json:"body"`
	Comment string `json:"comment,omitempty"`
	// Mode only ever applies to ImageToCoordinatesTask (KindClick) - see
	// this file's package comment on why it is sent explicitly rather than
	// left to the documented default.
	Mode string `json:"mode,omitempty"`
}

type antiCaptchaCreateReq struct {
	ClientKey string          `json:"clientKey"`
	Task      antiCaptchaTask `json:"task"`
}

type antiCaptchaResultReq struct {
	ClientKey string `json:"clientKey"`
	TaskID    int64  `json:"taskId"`
}

// antiCaptchaEnvelope is the {errorId, errorCode, errorDescription} prefix
// every Anti-Captcha response carries - see err.
type antiCaptchaEnvelope struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

// err turns a non-zero ErrorID into a Go error carrying Anti-Captcha's own
// code and description verbatim, or nil for a genuinely successful response.
func (e antiCaptchaEnvelope) err(op string) error {
	if e.ErrorID == 0 {
		return nil
	}
	if e.ErrorDescription != "" {
		return fmt.Errorf("anti-captcha %s: %s (%s)", op, e.ErrorDescription, e.ErrorCode)
	}
	return fmt.Errorf("anti-captcha %s: errorId %d", op, e.ErrorID)
}

type antiCaptchaCreateResp struct {
	antiCaptchaEnvelope
	TaskID int64 `json:"taskId"`
}

// antiCaptchaSolution covers both task types this file uses: Text for
// ImageToTextTask, Coordinates for ImageToCoordinatesTask. Coordinates is
// decoded as rows of plain ints - VERIFIED as [x1,y1,x2,y2] for "rectangles"
// mode; see antiCaptchaPoints for how "points" mode's own, less certain
// shape is handled.
type antiCaptchaSolution struct {
	Text        string  `json:"text,omitempty"`
	Coordinates [][]int `json:"coordinates,omitempty"`
}

type antiCaptchaResultResp struct {
	antiCaptchaEnvelope
	Status   string              `json:"status"`
	Solution antiCaptchaSolution `json:"solution"`
}

// antiCaptchaPoints reduces ImageToCoordinatesTask's own coordinate rows to
// solverPoint, reading the first two numbers of every row as (x, y).
//
// That read is confirmed correct for the one VERIFIED shape (rectangles
// mode: [x1,y1,x2,y2], top-left corner first - see this file's package
// comment) and is the only sensible read of a genuine two-number points-mode
// row too. A row with fewer than two numbers is dropped rather than causing
// the whole answer to fail - Anti-Captcha's own worker interface allows up
// to six points per image, and one malformed entry among several must not
// cost the rest.
func antiCaptchaPoints(rows [][]int) []solverPoint {
	out := make([]solverPoint, 0, len(rows))
	for _, r := range rows {
		if len(r) >= 2 {
			out = append(out, solverPoint{X: r[0], Y: r[1]})
		}
	}
	return out
}

// Solve implements Solver for Anti-Captcha - see that interface's own doc
// comment for the contract every caller relies on.
func (s *AntiCaptchaSolver) Solve(ctx context.Context, kind Kind, image, prompt string) (string, error) {
	if s.key == "" {
		return "", errors.New("captcha: no Anti-Captcha API key configured")
	}
	body, err := decodeSolverImage(image)
	if err != nil {
		return "", fmt.Errorf("anti-captcha: %w", err)
	}
	task := antiCaptchaTask{Body: body, Comment: prompt}
	switch kind {
	case KindImage:
		task.Type = "ImageToTextTask"
	case KindClick:
		task.Type, task.Mode = "ImageToCoordinatesTask", "points"
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

func (s *AntiCaptchaSolver) createTask(ctx context.Context, task antiCaptchaTask) (int64, error) {
	var resp antiCaptchaCreateResp
	if err := postJSON(ctx, s.hc, s.base+"/createTask", antiCaptchaCreateReq{ClientKey: s.key, Task: task}, &resp); err != nil {
		return 0, err
	}
	if err := resp.err("createTask"); err != nil {
		return 0, err
	}
	return resp.TaskID, nil
}

// pollResult repeats getTaskResult until Anti-Captcha reports "ready" (or an
// error, or ctx ends) - see solverPollInterval/solverMaxWait (defined in
// solver_2captcha.go) for the pacing and the ceiling.
func (s *AntiCaptchaSolver) pollResult(ctx context.Context, taskID int64) (text string, points []solverPoint, err error) {
	for {
		var resp antiCaptchaResultResp
		if err := postJSON(ctx, s.hc, s.base+"/getTaskResult", antiCaptchaResultReq{ClientKey: s.key, TaskID: taskID}, &resp); err != nil {
			return "", nil, err
		}
		if err := resp.err("getTaskResult"); err != nil {
			return "", nil, err
		}
		if resp.Status == "ready" {
			return resp.Solution.Text, antiCaptchaPoints(resp.Solution.Coordinates), nil
		}
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return "", nil, err
		}
	}
}
