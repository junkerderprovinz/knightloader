package captcha

// The Anti-Captcha client, the second of the two automatic solvers. It shares
// Solver, solverPoint, encodeClickAnswer, decodeSolverImage, solverSleep,
// postJSON, ErrUnsupportedKind and the poll constants with solver_2captcha.go.
//
// The API has the same shape as 2Captcha's: createTask answers a task id,
// getTaskResult is polled until it reports "ready". Only "body", "comment" and,
// for a click task, "mode" are sent; the per-task hints need detail a Challenge
// does not carry. No polling interval is documented, so 2Captcha's five seconds
// is reused.
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
	Body    string `json:"body"`
	Comment string `json:"comment,omitempty"`
	// Mode applies to ImageToCoordinatesTask only, and is sent rather than
	// left to Anti-Captcha's default.
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
// every Anti-Captcha response carries.
type antiCaptchaEnvelope struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

// err turns a non-zero ErrorID into an error carrying Anti-Captcha's own code
// and description.
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

// antiCaptchaSolution covers both task types: Text for ImageToTextTask,
// Coordinates for ImageToCoordinatesTask. Coordinates arrive as rows of plain
// ints, [x1,y1,x2,y2] in rectangles mode and a pair in points mode.
type antiCaptchaSolution struct {
	Text        string  `json:"text,omitempty"`
	Coordinates [][]int `json:"coordinates,omitempty"`
}

type antiCaptchaResultResp struct {
	antiCaptchaEnvelope
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

// Solve implements Solver for Anti-Captcha.
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

// pollResult repeats getTaskResult until Anti-Captcha reports "ready", an error
// arrives or ctx ends.
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
