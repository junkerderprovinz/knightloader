package captcha

// The 2Captcha client, plus the solver machinery it shares with
// solver_anticaptcha.go: the Solver contract, the errors a caller decides the
// next step by, the click-answer encoder and the HTTP and poll helpers neither
// provider's wire format owns. The token tasks for widget challenges are in
// solver_token.go.
//
// Each configured solver is tried in the order the user chose. Both handle
// image and click challenges, and reCAPTCHA and Turnstile widgets through the
// provider's own browsers. Neither takes hCaptcha, so an hCaptcha is refused
// before anything is sent and stays with the person at the prompt.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Solver is an automatic captcha-solving backend. Both TwoCaptchaSolver and
// AntiCaptchaSolver implement it, so a caller working through the configured
// order needs no type switch.
type Solver interface {
	// Takes reports whether the provider solves c's kind, without a network
	// call: nil if it does, a *Refusal with RefusalUnsupported if it does not.
	Takes(c Challenge) error

	// Solve submits c and returns the text to hand to Source.Answer unchanged:
	// recognized text for KindImage, JD's click-answer JSON for KindClick, the
	// token for KindWidget.
	//
	// An error wrapping ErrTaskTaken means the provider may hold the task and
	// bill it, so c must not go to another solver; it wraps the provider's
	// *Refusal as well when the provider gave up on a task it had accepted.
	// Any other error means no task was created, and the next solver may try.
	Solve(ctx context.Context, c Challenge) (string, error)
}

// Refusal is a provider declining a challenge: a kind it does not take, or an
// error its API answered with. One that createTask answers costs nothing,
// since no task exists; one that comes after the task was accepted arrives
// wrapped in ErrTaskTaken.
type Refusal struct {
	// Code is RefusalUnsupported or the provider's own error code.
	Code string
	// Detail is the provider's description of Code, if it gave one.
	Detail string
}

func (r *Refusal) Error() string {
	if r.Detail != "" {
		return fmt.Sprintf("captcha: solver refused: %s (%s)", r.Detail, r.Code)
	}
	return "captcha: solver refused: " + r.Code
}

// ErrTaskTaken is wrapped by every failure once the provider may hold the
// task: createTask answered a task id, or its request went out and no answer
// came back. The provider may charge for it whatever happens next, so a caller
// should not hand the same challenge to another solver.
var ErrTaskTaken = errors.New("captcha: the solver may hold the task and no usable answer came back")

// RefusalFor turns what Solve returned into the line a SolverReport shows for
// the solver named label.
func RefusalFor(label string, err error) SolverRefusal {
	out := SolverRefusal{Solver: label, Taken: errors.Is(err, ErrTaskTaken)}
	var r *Refusal
	switch {
	case errors.As(err, &r):
		out.Code, out.Detail = r.Code, r.Detail
	case out.Taken:
		out.Code = RefusalNoAnswer
	default:
		out.Code, out.Detail = RefusalFailed, err.Error()
	}
	return out
}

func unsupported(what string) *Refusal {
	return &Refusal{Code: RefusalUnsupported, Detail: what}
}

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

// solverResult is a ready task's solution, reduced to what solverAnswerFor
// reads.
type solverResult struct {
	text   string
	points []solverPoint
	token  string
}

// solverAnswerFor turns a provider's solution into the string Source.Answer
// expects. A ready task has been charged for, so an unusable solution wraps
// ErrTaskTaken.
func solverAnswerFor(kind Kind, res solverResult) (string, error) {
	switch kind {
	case KindImage:
		if res.text == "" {
			return "", fmt.Errorf("%w: success with no text", ErrTaskTaken)
		}
		return res.text, nil
	case KindClick:
		answer, err := encodeClickAnswer(res.points)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
		return answer, nil
	case KindWidget:
		if res.token == "" {
			return "", fmt.Errorf("%w: success with no token", ErrTaskTaken)
		}
		return res.token, nil
	default:
		return "", unsupported(string(kind))
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

// ImageReadable reports whether dataURL holds image data a solver can be sent,
// what the image and click tasks are built from.
func ImageReadable(dataURL string) bool {
	_, err := decodeSolverImage(dataURL)
	return err == nil
}

// challengeImage is the base64 body an image or click task sends, read from
// c's payload.
func challengeImage(c Challenge) (string, error) {
	p, ok := c.Payload.(*ImagePayload)
	if !ok || p == nil {
		return "", errors.New("captcha: no image data to solve")
	}
	return decodeSolverImage(p.DataURL)
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
//
// sent reports whether the whole request went out. From then on the provider
// may have acted on it, even when no answer, or no answer this can read, came
// back.
func postJSON(ctx context.Context, hc *http.Client, url string, in, out any) (sent bool, err error) {
	b, err := json.Marshal(in)
	if err != nil {
		return false, err
	}
	var wrote atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			if i.Err == nil {
				wrote.Store(true)
			}
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return wrote.Load(), err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return true, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return true, fmt.Errorf("%s: HTTP %s: %w", url, resp.Status, err)
	}
	return true, nil
}

// createTaskFailed wraps a createTask call that brought back no answer. Once
// the request went out the provider may have created the task anyway, so the
// failure then counts as a taken task.
func createTaskFailed(provider string, sent bool, err error) error {
	if sent {
		return fmt.Errorf("%w: %s createTask: %v", ErrTaskTaken, provider, err)
	}
	return fmt.Errorf("%s createTask: %w", provider, err)
}

// solverEnvelope is the {errorId, errorCode, errorDescription} prefix every
// response of both providers carries, embedded in their response types.
type solverEnvelope struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

// refusal turns a non-zero ErrorID into a *Refusal carrying the provider's
// own code and description, which read better in a log than a paraphrase.
func (e solverEnvelope) refusal() error {
	if e.ErrorID == 0 {
		return nil
	}
	code := e.ErrorCode
	if code == "" {
		code = fmt.Sprintf("errorId %d", e.ErrorID)
	}
	return &Refusal{Code: code, Detail: e.ErrorDescription}
}

// solverTokenSolution is the part of a solution a token task fills. 2Captcha
// writes the token under both names for reCAPTCHA and only as token for
// Turnstile; Anti-Captcha uses gRecaptchaResponse for reCAPTCHA and token for
// Turnstile.
type solverTokenSolution struct {
	Token              string `json:"token,omitempty"`
	GRecaptchaResponse string `json:"gRecaptchaResponse,omitempty"`
}

func (s solverTokenSolution) value() string {
	if s.Token != "" {
		return s.Token
	}
	return s.GRecaptchaResponse
}

// The 2Captcha JSON API: createTask answers a task id, getTaskResult is polled
// until it reports "ready". An image task sends only "body" and "comment"; the
// per-task hints (phrase, numeric, minClicks and the rest) need detail a
// Challenge does not carry, so they stay at 2Captcha's defaults.
//
//	https://2captcha.com/api-docs/create-task
//	https://2captcha.com/api-docs/normal-captcha
//	https://2captcha.com/api-docs/coordinates
//	https://2captcha.com/api-docs/recaptcha-v2
//	https://2captcha.com/api-docs/recaptcha-v2-enterprise
//	https://2captcha.com/api-docs/recaptcha-v3
//	https://2captcha.com/api-docs/cloudflare-turnstile
//	https://2captcha.com/api-docs/get-task-result

const twoCaptchaBase = "https://api.2captcha.com"

// twoCaptchaTokenTasks are the token task types 2Captcha takes. hCaptcha is
// missing because 2Captcha's documentation does not list it.
var twoCaptchaTokenTasks = map[string]bool{
	taskRecaptchaV2:           true,
	taskRecaptchaV2Enterprise: true,
	taskRecaptchaV3:           true,
	taskTurnstile:             true,
}

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
	Body    string `json:"body,omitempty"`
	Comment string `json:"comment,omitempty"`
	tokenFields
}

type twoCaptchaCreateReq struct {
	ClientKey string         `json:"clientKey"`
	Task      twoCaptchaTask `json:"task"`
}

type twoCaptchaResultReq struct {
	ClientKey string `json:"clientKey"`
	TaskID    int64  `json:"taskId"`
}

type twoCaptchaCreateResp struct {
	solverEnvelope
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
	solverTokenSolution
}

type twoCaptchaResultResp struct {
	solverEnvelope
	Status   string             `json:"status"`
	Solution twoCaptchaSolution `json:"solution"`
}

// task builds the createTask task for c, or the reason 2Captcha would refuse
// it.
func (s *TwoCaptchaSolver) task(c Challenge) (twoCaptchaTask, error) {
	switch c.Kind {
	case KindImage, KindClick:
		body, err := challengeImage(c)
		if err != nil {
			return twoCaptchaTask{}, fmt.Errorf("2captcha: %w", err)
		}
		task := twoCaptchaTask{Type: "ImageToTextTask", Body: body, Comment: c.Prompt}
		if c.Kind == KindClick {
			task.Type = "CoordinatesTask"
		}
		return task, nil
	case KindWidget:
		typ, fields, err := tokenTask(c)
		if err != nil {
			return twoCaptchaTask{}, err
		}
		if !twoCaptchaTokenTasks[typ] {
			return twoCaptchaTask{}, unsupported(typ)
		}
		return twoCaptchaTask{Type: typ, tokenFields: fields}, nil
	default:
		return twoCaptchaTask{}, unsupported(string(c.Kind))
	}
}

// Takes implements Solver for 2Captcha.
func (s *TwoCaptchaSolver) Takes(c Challenge) error {
	_, err := s.task(c)
	var r *Refusal
	if errors.As(err, &r) {
		return err
	}
	return nil
}

// Solve implements Solver for 2Captcha.
func (s *TwoCaptchaSolver) Solve(ctx context.Context, c Challenge) (string, error) {
	if s.key == "" {
		return "", errors.New("captcha: no 2Captcha API key configured")
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

func (s *TwoCaptchaSolver) createTask(ctx context.Context, task twoCaptchaTask) (int64, error) {
	var resp twoCaptchaCreateResp
	if sent, err := postJSON(ctx, s.hc, s.base+"/createTask", twoCaptchaCreateReq{ClientKey: s.key, Task: task}, &resp); err != nil {
		return 0, createTaskFailed("2captcha", sent, err)
	}
	if err := resp.refusal(); err != nil {
		return 0, err
	}
	return resp.TaskID, nil
}

// pollResult repeats getTaskResult until 2Captcha reports "ready", an error
// arrives or ctx ends. The task exists at this point, so every failure wraps
// ErrTaskTaken, 2Captcha's own verdict included.
func (s *TwoCaptchaSolver) pollResult(ctx context.Context, taskID int64) (solverResult, error) {
	for {
		var resp twoCaptchaResultResp
		if _, err := postJSON(ctx, s.hc, s.base+"/getTaskResult", twoCaptchaResultReq{ClientKey: s.key, TaskID: taskID}, &resp); err != nil {
			return solverResult{}, fmt.Errorf("%w: 2captcha getTaskResult: %v", ErrTaskTaken, err)
		}
		if err := resp.refusal(); err != nil {
			return solverResult{}, fmt.Errorf("%w: %w", ErrTaskTaken, err)
		}
		if resp.Status == "ready" {
			pts := make([]solverPoint, len(resp.Solution.Coordinates))
			for i, p := range resp.Solution.Coordinates {
				pts[i] = solverPoint{X: p.X, Y: p.Y}
			}
			return solverResult{text: resp.Solution.Text, points: pts, token: resp.Solution.value()}, nil
		}
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return solverResult{}, fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
	}
}
