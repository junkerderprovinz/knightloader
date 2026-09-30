package captcha

// The Anti-Captcha client, which also serves CapMonster Cloud and CapSolver:
// both speak Anti-Captcha's createTask protocol and differ only in the base
// URL, the task names and which fields a task documents. Those differences
// are an antiCaptchaAPI each (solver_capmonster.go, solver_capsolver.go). It
// shares Solver, the refusal errors, the answer and image helpers, postJSON
// and the poll constants with solver_2captcha.go, and the token tasks with
// solver_token.go.
//
// The API has the same shape as 2Captcha's: createTask answers a task id,
// getTaskResult is polled until it reports "ready". An image task sends only
// "body", "comment" and, for a click task, "mode"; the per-task hints need
// detail a Challenge does not carry. No polling interval is documented, so
// 2Captcha's five seconds is reused.
//
//	https://anti-captcha.com/apidoc/methods/createTask
//	https://anti-captcha.com/apidoc/task-types/ImageToTextTask
//	https://anti-captcha.com/apidoc/task-types/ImageToCoordinatesTask
//	https://anti-captcha.com/apidoc/methods/getTaskResult
//	https://anti-captcha.com/apidoc/methods/getBalance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// antiCaptchaAPI is one provider of the createTask protocol.
type antiCaptchaAPI struct {
	// name is the provider in errors and logs, label in what a person reads.
	name  string
	label string
	base  string
	// comment is whether the image tasks document a "comment" for the prompt.
	comment bool
	// clickTask is the task that answers with coordinates, empty for a
	// provider without a general one.
	clickTask string
	// tokenTask turns the task tokenTask in solver_token.go named into the
	// provider's own, keeping only the fields its documentation lists. ok is
	// false for a task the provider does not take.
	tokenTask func(typ string, f tokenFields) (name string, fields tokenFields, ok bool)
}

var antiCaptcha = antiCaptchaAPI{
	name:      "anti-captcha",
	label:     "Anti-Captcha",
	base:      "https://api.anti-captcha.com",
	comment:   true,
	clickTask: "ImageToCoordinatesTask",
	tokenTask: func(typ string, f tokenFields) (string, tokenFields, bool) {
		return typ, f, antiCaptchaTokenTasks[typ]
	},
}

// antiCaptchaTokenTasks are the token task types Anti-Captcha takes; its task
// list has no hCaptcha. reCAPTCHA v3 Enterprise is the v3 type with
// isEnterprise set.
var antiCaptchaTokenTasks = map[string]bool{
	taskRecaptchaV2:           true,
	taskRecaptchaV2Enterprise: true,
	taskRecaptchaV3:           true,
	taskTurnstile:             true,
}

// AntiCaptchaSolver is a Solver backed by one account at Anti-Captcha or at a
// provider speaking its protocol.
type AntiCaptchaSolver struct {
	api  *antiCaptchaAPI
	key  string
	base string
	hc   *http.Client
}

// NewAntiCaptchaSolver builds a solver for one account key, which
// Anti-Captcha calls the client key and internal/accounts stores under
// catalogue id "anticaptcha".
func NewAntiCaptchaSolver(apiKey string) *AntiCaptchaSolver {
	return newAntiCaptchaSolver(&antiCaptcha, apiKey)
}

func newAntiCaptchaSolver(api *antiCaptchaAPI, apiKey string) *AntiCaptchaSolver {
	return &AntiCaptchaSolver{api: api, key: apiKey, base: api.base, hc: httpx.New(httpx.Options{Timeout: 20 * time.Second})}
}

type antiCaptchaTask struct {
	Type    string `json:"type"`
	Body    string `json:"body,omitempty"`
	Comment string `json:"comment,omitempty"`
	// Mode applies to ImageToCoordinatesTask only, and is sent rather than
	// left to Anti-Captcha's default.
	Mode string `json:"mode,omitempty"`
	tokenFields
}

type antiCaptchaCreateReq struct {
	ClientKey string          `json:"clientKey"`
	Task      antiCaptchaTask `json:"task"`
}

// antiCaptchaResultReq echoes the task id exactly as createTask wrote it:
// a number at Anti-Captcha and CapMonster Cloud, a string at CapSolver.
type antiCaptchaResultReq struct {
	ClientKey string          `json:"clientKey"`
	TaskID    json.RawMessage `json:"taskId"`
}

// antiCaptchaCreateResp carries a status and a solution only from CapSolver,
// whose ImageToTextTask answers in createTask itself.
type antiCaptchaCreateResp struct {
	solverEnvelope
	TaskID   json.RawMessage     `json:"taskId"`
	Status   string              `json:"status"`
	Solution antiCaptchaSolution `json:"solution"`
}

// antiCaptchaSolution covers every task type: Text for ImageToTextTask,
// Coordinates for ImageToCoordinatesTask, the token for the rest. Coordinates
// arrive as rows of plain ints, [x1,y1,x2,y2] in rectangles mode and a pair in
// points mode.
type antiCaptchaSolution struct {
	Text        string  `json:"text,omitempty"`
	Coordinates [][]int `json:"coordinates,omitempty"`
	solverTokenSolution
}

func (s antiCaptchaSolution) result() solverResult {
	return solverResult{text: s.Text, points: antiCaptchaPoints(s.Coordinates), token: s.value()}
}

type antiCaptchaResultResp struct {
	solverEnvelope
	Status   string              `json:"status"`
	Solution antiCaptchaSolution `json:"solution"`
}

// antiCaptchaPoints reduces ImageToCoordinatesTask's coordinate rows to
// solverPoint by reading the first two numbers of each row as x and y, which is
// the top-left corner in rectangles mode. A row with fewer than two numbers is
// dropped, so one malformed entry does not cost the other points on the image.
func antiCaptchaPoints(rows [][]int) []solverPoint {
	out := make([]solverPoint, 0, len(rows))
	for _, r := range rows {
		if len(r) >= 2 {
			out = append(out, solverPoint{X: r[0], Y: r[1]})
		}
	}
	return out
}

// task builds the createTask task for c, or the reason the provider would
// refuse it.
func (s *AntiCaptchaSolver) task(c Challenge) (antiCaptchaTask, error) {
	switch c.Kind {
	case KindImage, KindClick:
		if c.Kind == KindClick && s.api.clickTask == "" {
			return antiCaptchaTask{}, unsupported(string(c.Kind))
		}
		body, err := challengeImage(c)
		if err != nil {
			return antiCaptchaTask{}, fmt.Errorf("%s: %w", s.api.name, err)
		}
		task := antiCaptchaTask{Type: "ImageToTextTask", Body: body}
		if s.api.comment {
			task.Comment = c.Prompt
		}
		if c.Kind == KindClick {
			task.Type, task.Mode = s.api.clickTask, "points"
		}
		return task, nil
	case KindWidget:
		typ, fields, err := tokenTask(c)
		if err != nil {
			return antiCaptchaTask{}, err
		}
		name, fields, ok := s.api.tokenTask(typ, fields)
		if !ok {
			return antiCaptchaTask{}, unsupported(typ)
		}
		return antiCaptchaTask{Type: name, tokenFields: fields}, nil
	default:
		return antiCaptchaTask{}, unsupported(string(c.Kind))
	}
}

// Takes implements Solver.
func (s *AntiCaptchaSolver) Takes(c Challenge) error {
	_, err := s.task(c)
	var r *Refusal
	if errors.As(err, &r) {
		return err
	}
	return nil
}

// Solve implements Solver.
func (s *AntiCaptchaSolver) Solve(ctx context.Context, c Challenge) (string, error) {
	if s.key == "" {
		return "", fmt.Errorf("captcha: no %s API key configured", s.api.label)
	}
	task, err := s.task(c)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, solverMaxWait)
	defer cancel()

	created, err := s.createTask(ctx, task)
	if err != nil {
		return "", err
	}
	if created.Status == "ready" {
		return solverAnswerFor(c.Kind, created.Solution.result())
	}
	res, err := s.pollResult(ctx, created.TaskID)
	if err != nil {
		return "", err
	}
	return solverAnswerFor(c.Kind, res)
}

// Balance returns what the account holds, and is how a key is checked without
// paying for a captcha.
func (s *AntiCaptchaSolver) Balance(ctx context.Context) (float64, error) {
	return getBalance(ctx, s.hc, s.base, s.key, s.api.name)
}

func (s *AntiCaptchaSolver) createTask(ctx context.Context, task antiCaptchaTask) (antiCaptchaCreateResp, error) {
	var resp antiCaptchaCreateResp
	if sent, err := postJSON(ctx, s.hc, s.base+"/createTask", antiCaptchaCreateReq{ClientKey: s.key, Task: task}, &resp); err != nil {
		return resp, createTaskFailed(s.api.name, sent, err)
	}
	return resp, resp.refusal()
}

// pollResult repeats getTaskResult until the provider reports "ready", an
// error arrives or ctx ends. The task exists at this point, so every failure
// wraps ErrTaskTaken, the provider's own verdict included: Anti-Captcha
// charges for a task its workers gave up on
// (https://anti-captcha.com/apidoc/errors).
func (s *AntiCaptchaSolver) pollResult(ctx context.Context, taskID json.RawMessage) (solverResult, error) {
	for {
		var resp antiCaptchaResultResp
		if _, err := postJSON(ctx, s.hc, s.base+"/getTaskResult", antiCaptchaResultReq{ClientKey: s.key, TaskID: taskID}, &resp); err != nil {
			return solverResult{}, fmt.Errorf("%w: %s getTaskResult: %v", ErrTaskTaken, s.api.name, err)
		}
		if err := resp.refusal(); err != nil {
			return solverResult{}, fmt.Errorf("%w: %w", ErrTaskTaken, err)
		}
		if resp.Status == "ready" {
			return resp.Solution.result(), nil
		}
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return solverResult{}, fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
	}
}
