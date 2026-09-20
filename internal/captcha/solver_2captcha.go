package captcha

// The 2Captcha client, plus the solver machinery it shares with
// solver_anticaptcha.go: the Solver contract, the click-answer encoder and the
// HTTP and poll helpers neither provider's wire format owns.
//
// Before a download reaches the human prompt, each configured solver is tried
// in the order the user chose, and a person is asked only when none are
// configured or all of them decline. Both solvers handle image and click
// challenges. A widget challenge needs a headless browser running the vendor's
// JS on the hoster's page, which neither provider exposes through a plain image
// or coordinates task.

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

// Solver is an automatic captcha-solving backend, tried in a configured order
// before a human is shown the prompt. Both TwoCaptchaSolver and
// AntiCaptchaSolver implement it, so a caller working through the configured
// order needs no type switch.
type Solver interface {
	// Solve submits image and returns the text to hand to Source.Answer
	// unchanged: recognized text for KindImage, JD's click-answer JSON for
	// KindClick. image is a Challenge payload's DataURL or a bare base64
	// string, either of which decodeSolverImage accepts. prompt is
	// Challenge.Prompt, which both services pass to their workers as
	// instructions and which may be empty.
	//
	// A kind other than KindImage or KindClick returns ErrUnsupportedKind
	// before any network call.
	Solve(ctx context.Context, kind Kind, image, prompt string) (string, error)
}

// ErrUnsupportedKind is returned for a Kind neither solver can act on.
var ErrUnsupportedKind = errors.New("captcha: solver only handles KindImage and KindClick")

// Both are var rather than const so the tests can shorten them.
var (
	// solverPollInterval is how long a Solve call waits between getTaskResult
	// polls. 2Captcha's documentation asks for at least five seconds;
	// Anti-Captcha states no interval, so it gets the same one.
	solverPollInterval = 5 * time.Second

	// solverMaxWait bounds one Solve call's poll loop for a caller that passes
	// a context with no deadline of its own. A tighter caller deadline still
	// wins, since context.WithTimeout takes whichever is sooner.
	solverMaxWait = 180 * time.Second
)

// solverPoint is one resolved click location in the pixel space of the image a
// Source handed out, which is what both clients reduce a provider's coordinate
// answer to.
type solverPoint struct{ X, Y int }

// solverAnswerFor turns a provider's solved text or points into the string
// Source.Answer expects.
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

// encodeClickAnswer turns resolved click points into the string Source.Answer
// needs for a KindClick challenge. JD parses it as JSON, in one of two shapes:
//
//	ClickedPoint       {"x":<int>,"y":<int>}
//	MultiClickedPoint  {"x":[<int>,...],"y":[<int>,...]}
//
// Both click families classify to the one Kind and JD's CaptchaJob carries no
// expected click count, so the shape follows the number of points the solver
// resolved.
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

// decodeSolverImage turns a data URL or a bare base64 string into the
// standard-padded base64 both providers document. It decodes tolerantly and
// re-encodes rather than forwarding whatever followed "base64,", so a URL-safe
// or unpadded variant does not surface as an opaque provider-side error.
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

// solverSleep waits d or returns ctx's error, whichever comes first. A timer
// rather than time.After, which would stay alive past a cancellation.
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

// postJSON POSTs in as a JSON body to url and decodes the response into out,
// the one HTTP shape both providers' APIs share. What the response holds
// differs per provider and is decoded by the types below.
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

// The 2Captcha JSON API: createTask answers a task id, getTaskResult is polled
// until it reports "ready". Only "body" and "comment" are sent; the per-task
// hints (phrase, numeric, minClicks and the rest) need detail a Challenge does
// not carry, so they stay at 2Captcha's defaults.
//
//	https://2captcha.com/api-docs/create-task
//	https://2captcha.com/api-docs/normal-captcha
//	https://2captcha.com/api-docs/coordinates
//	https://2captcha.com/api-docs/get-task-result

const twoCaptchaBase = "https://api.2captcha.com"

// TwoCaptchaSolver is a Solver backed by one 2Captcha account.
type TwoCaptchaSolver struct {
	key  string
	base string
	hc   *http.Client
}

// NewTwoCaptchaSolver builds a solver for one account API key, the one
// internal/accounts stores under catalogue id "2captcha".
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

// twoCaptchaEnvelope is the {errorId, errorCode, errorDescription} prefix every
// 2Captcha response carries, embedded in both response types below.
type twoCaptchaEnvelope struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

// err turns a non-zero ErrorID into an error carrying 2Captcha's own code and
// description, which read better in a log than a paraphrase would.
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

// twoCaptchaPoint is CoordinatesTask's solution shape, an object with named x
// and y keys. Anti-Captcha answers coordinate pairs as arrays instead.
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

// Solve implements Solver for 2Captcha.
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

// pollResult repeats getTaskResult until 2Captcha reports "ready", an error
// arrives or ctx ends.
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
