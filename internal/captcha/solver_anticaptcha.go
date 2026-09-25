package captcha

// The Anti-Captcha client, the second of the two automatic solvers. It shares
// Solver, the refusal errors, the answer and image helpers, postJSON and the
// poll constants with solver_2captcha.go, and the token tasks with
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

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

const antiCaptchaBase = "https://api.anti-captcha.com"

// antiCaptchaTokenTasks are the token task types Anti-Captcha takes; its task
// list has no hCaptcha. reCAPTCHA v3 Enterprise is the v3 type with
// isEnterprise set.
var antiCaptchaTokenTasks = map[string]bool{
	taskRecaptchaV2:           true,
	taskRecaptchaV2Enterprise: true,
	taskRecaptchaV3:           true,
	taskTurnstile:             true,
}

// AntiCaptchaSolver is a Solver backed by one Anti-Captcha account.
type AntiCaptchaSolver struct {
	key  string
	base string
	hc   *http.Client
}

// NewAntiCaptchaSolver builds a solver for one account key, which
// Anti-Captcha calls the client key and internal/accounts stores under
// catalogue id "anticaptcha".
func NewAntiCaptchaSolver(apiKey string) *AntiCaptchaSolver {
	return &AntiCaptchaSolver{key: apiKey, base: antiCaptchaBase, hc: httpx.New(httpx.Options{Timeout: 20 * time.Second})}
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

type antiCaptchaResultReq struct {
	ClientKey string `json:"clientKey"`
	TaskID    int64  `json:"taskId"`
}

type antiCaptchaCreateResp struct {
	solverEnvelope
	TaskID int64 `json:"taskId"`
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

// task builds the createTask task for c, or the reason Anti-Captcha would
// refuse it.
func (s *AntiCaptchaSolver) task(c Challenge) (antiCaptchaTask, error) {
	switch c.Kind {
	case KindImage, KindClick:
		body, err := challengeImage(c)
		if err != nil {
			return antiCaptchaTask{}, fmt.Errorf("anti-captcha: %w", err)
		}
		task := antiCaptchaTask{Type: "ImageToTextTask", Body: body, Comment: c.Prompt}
		if c.Kind == KindClick {
			task.Type, task.Mode = "ImageToCoordinatesTask", "points"
		}
		return task, nil
	case KindWidget:
		typ, fields, err := tokenTask(c)
		if err != nil {
			return antiCaptchaTask{}, err
		}
		if !antiCaptchaTokenTasks[typ] {
			return antiCaptchaTask{}, unsupported(typ)
		}
		return antiCaptchaTask{Type: typ, tokenFields: fields}, nil
	default:
		return antiCaptchaTask{}, unsupported(string(c.Kind))
	}
}

// Takes implements Solver for Anti-Captcha.
func (s *AntiCaptchaSolver) Takes(c Challenge) error {
	_, err := s.task(c)
	var r *Refusal
	if errors.As(err, &r) {
		return err
	}
	return nil
}

// Solve implements Solver for Anti-Captcha.
func (s *AntiCaptchaSolver) Solve(ctx context.Context, c Challenge) (string, error) {
	if s.key == "" {
		return "", errors.New("captcha: no Anti-Captcha API key configured")
	}
	task, err := s.task(c)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, solverMaxWait)
	defer cancel()

	taskID, err := s.createTask(ctx, task)
	if err != nil {
		return "", err
	}
	res, err := s.pollResult(ctx, taskID)
	if err != nil {
		return "", err
	}
	return solverAnswerFor(c.Kind, res)
}

func (s *AntiCaptchaSolver) createTask(ctx context.Context, task antiCaptchaTask) (int64, error) {
	var resp antiCaptchaCreateResp
	if sent, err := postJSON(ctx, s.hc, s.base+"/createTask", antiCaptchaCreateReq{ClientKey: s.key, Task: task}, &resp); err != nil {
		return 0, createTaskFailed("anti-captcha", sent, err)
	}
	if err := resp.refusal(); err != nil {
		return 0, err
	}
	return resp.TaskID, nil
}

// pollResult repeats getTaskResult until Anti-Captcha reports "ready", an error
// arrives or ctx ends. The task exists at this point, so every failure wraps
// ErrTaskTaken, Anti-Captcha's own verdict included: it charges for a task its
// workers gave up on (https://anti-captcha.com/apidoc/errors).
func (s *AntiCaptchaSolver) pollResult(ctx context.Context, taskID int64) (solverResult, error) {
	for {
		var resp antiCaptchaResultResp
		if _, err := postJSON(ctx, s.hc, s.base+"/getTaskResult", antiCaptchaResultReq{ClientKey: s.key, TaskID: taskID}, &resp); err != nil {
			return solverResult{}, fmt.Errorf("%w: anti-captcha getTaskResult: %v", ErrTaskTaken, err)
		}
		if err := resp.refusal(); err != nil {
			return solverResult{}, fmt.Errorf("%w: %w", ErrTaskTaken, err)
		}
		if resp.Status == "ready" {
			return solverResult{
				text:   resp.Solution.Text,
				points: antiCaptchaPoints(resp.Solution.Coordinates),
				token:  resp.Solution.value(),
			}, nil
		}
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return solverResult{}, fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
	}
}
