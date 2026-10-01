package captcha

// The 9kw.eu client. 9kw has an API of its own: an upload answers a captcha
// id, and the answer is polled for by that id. Every call is answered in plain
// text, the API's default and the only format whose errors the documentation
// shows: a four-digit code and its description, such as "0002 API key not
// found". The key travels in the query of the GET calls, as documented, so a
// transport error is reported without its URL.
//
// 9kw's workers are people. An image goes up with the prompt as
// textinstructions, a click captcha as multimouse, the variant answered with
// one click or several, since a Challenge does not say how many. The widgets
// are reCAPTCHA v2 and hCaptcha, which a worker solves from the site key
// (oldsource "recaptchav2" and "hcaptcha"). Turnstile is not among the
// variants, and the upload has no field for a v3 action or for an invisible
// or Enterprise key, so those are refused.
//
//	https://www.9kw.eu/api.html

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

const nineKWBase = "https://www.9kw.eu"

// nineKWMaxTimeout is how long 9kw keeps an upload open, in seconds. Its
// default is ten minutes, but nobody polls after solverMaxWait, and 9kw does
// not charge for a captcha it closes unsolved.
const nineKWMaxTimeout = "180"

// nineKWError matches 9kw's wording of a failure.
var nineKWError = regexp.MustCompile(`^(\d{4}) ([A-Z].*)$`)

// NineKWSolver is a Solver backed by one 9kw.eu account.
type NineKWSolver struct {
	key  string
	base string
	hc   *http.Client
}

// NewNineKWSolver builds a solver for one API key, the one internal/accounts
// stores under catalogue id "9kw".
func NewNineKWSolver(apiKey string) *NineKWSolver {
	return &NineKWSolver{key: apiKey, base: nineKWBase, hc: httpx.New(httpx.Options{Timeout: 20 * time.Second})}
}

// upload builds the usercaptchaupload fields for c, or the reason 9kw would
// refuse it.
func (s *NineKWSolver) upload(c Challenge) (url.Values, error) {
	f := url.Values{"maxtimeout": {nineKWMaxTimeout}}
	switch c.Kind {
	case KindImage, KindClick:
		body, err := challengeImage(c)
		if err != nil {
			return nil, fmt.Errorf("9kw: %w", err)
		}
		f.Set("base64", "1")
		f.Set("file-upload-01", body)
		if c.Prompt != "" {
			f.Set("textinstructions", c.Prompt)
		}
		if c.Kind == KindClick {
			f.Set("multimouse", "1")
		}
		return f, nil
	case KindWidget:
		typ, t, err := tokenTask(c)
		if err != nil {
			return nil, err
		}
		switch {
		case typ == taskHCaptcha:
			f.Set("oldsource", "hcaptcha")
		case typ == taskRecaptchaV2 && t.IsInvisible:
			return nil, unsupported(typ + " invisible")
		case typ == taskRecaptchaV2:
			f.Set("oldsource", "recaptchav2")
		default:
			return nil, unsupported(typ)
		}
		f.Set("interactive", "1")
		f.Set("file-upload-01", t.WebsiteKey)
		f.Set("pageurl", t.WebsiteURL)
		return f, nil
	default:
		return nil, unsupported(string(c.Kind))
	}
}

// Takes implements Solver for 9kw.
func (s *NineKWSolver) Takes(c Challenge) error {
	_, err := s.upload(c)
	var r *Refusal
	if errors.As(err, &r) {
		return err
	}
	return nil
}

// Solve implements Solver for 9kw.
func (s *NineKWSolver) Solve(ctx context.Context, c Challenge) (string, error) {
	if s.key == "" {
		return "", errors.New("captcha: no 9kw API key configured")
	}
	fields, err := s.upload(c)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, solverMaxWait)
	defer cancel()

	id, err := s.submit(ctx, fields)
	if err != nil {
		return "", err
	}
	answer, err := s.pollAnswer(ctx, id)
	if err != nil {
		return "", err
	}
	if c.Kind == KindClick {
		return solverAnswerFor(c.Kind, solverResult{points: nineKWPoints(answer)})
	}
	return solverAnswerFor(c.Kind, solverResult{text: answer, token: answer})
}

// Balance returns the account's credits, and is how a key is checked without
// paying for a captcha.
func (s *NineKWSolver) Balance(ctx context.Context) (float64, error) {
	answer, err := s.get(ctx, url.Values{"action": {"usercaptchaguthaben"}})
	var r *Refusal
	if errors.As(err, &r) {
		return 0, keyRefused("9kw", r)
	}
	if err != nil {
		return 0, err
	}
	credits, err := strconv.ParseFloat(answer, 64)
	if err != nil {
		return 0, fmt.Errorf("9kw usercaptchaguthaben answered %q", answer)
	}
	return credits, nil
}

// submit uploads the captcha, which 9kw takes only as a multipart POST, and
// returns its id.
func (s *NineKWSolver) submit(ctx context.Context, fields url.Values) (string, error) {
	fields.Set("action", "usercaptchaupload")
	fields.Set("apikey", s.key)
	body, contentType, err := multipartBody(fields)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/index.cgi", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	status, raw, sent, err := roundTrip(s.hc, req)
	if err != nil {
		if sent {
			return "", fmt.Errorf("%w: 9kw upload: %v", ErrTaskTaken, err)
		}
		return "", fmt.Errorf("9kw upload: %w", err)
	}
	answer := strings.TrimSpace(string(raw))
	if r := nineKWRefusal(answer); r != nil {
		return "", r
	}
	if _, err := strconv.ParseUint(answer, 10, 64); err != nil {
		return "", fmt.Errorf("%w: 9kw upload answered HTTP %d without a captcha id", ErrTaskTaken, status)
	}
	return answer, nil
}

// pollAnswer asks for the answer until there is one. 9kw asks for five to ten
// seconds before the first request, and answers nothing while no worker has
// solved the captcha.
func (s *NineKWSolver) pollAnswer(ctx context.Context, id string) (string, error) {
	for {
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return "", fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
		answer, err := s.get(ctx, url.Values{"action": {"usercaptchacorrectdata"}, "id": {id}})
		var status *nineKWStatus
		if errors.As(err, &status) && status.code >= 500 {
			// A front end that is briefly down; the captcha is paid for, and
			// its answer is still to be had once 9kw is back.
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrTaskTaken, err)
		}
		if answer != "" {
			return answer, nil
		}
	}
}

// get calls one of the GET actions and returns its answer, or 9kw's error as
// a *Refusal.
func (s *NineKWSolver) get(ctx context.Context, params url.Values) (string, error) {
	action := params.Get("action")
	params.Set("apikey", s.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/index.cgi?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("9kw %s: %w", action, err)
	}
	status, raw, _, err := roundTrip(s.hc, req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("9kw %s: %w", action, err)
	}
	answer := strings.TrimSpace(string(raw))
	if r := nineKWRefusal(answer); r != nil {
		return "", r
	}
	// The body of an error status is a page from 9kw's front end or a proxy
	// in front of it, and handed on it would be typed into the captcha.
	if status >= http.StatusBadRequest {
		return "", fmt.Errorf("9kw %s: %w", action, &nineKWStatus{code: status})
	}
	return answer, nil
}

// nineKWStatus is an HTTP error status from 9kw.
type nineKWStatus struct{ code int }

func (e *nineKWStatus) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func nineKWRefusal(answer string) *Refusal {
	m := nineKWError.FindStringSubmatch(answer)
	if m == nil {
		return nil
	}
	return &Refusal{Code: m[1], Detail: m[2]}
}

// nineKWPoints reads a click answer, "324x184" for one click and
// "68x149;81x192" for several. A pair that does not parse is dropped, as in
// antiCaptchaPoints.
func nineKWPoints(answer string) []solverPoint {
	var out []solverPoint
	for pair := range strings.SplitSeq(answer, ";") {
		xs, ys, ok := strings.Cut(strings.TrimSpace(pair), "x")
		x, errX := strconv.Atoi(xs)
		y, errY := strconv.Atoi(ys)
		if ok && errX == nil && errY == nil {
			out = append(out, solverPoint{X: x, Y: y})
		}
	}
	return out
}
