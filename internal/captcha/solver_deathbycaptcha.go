package captcha

// The Death By Captcha client, over its HTTP API. The API keeps no session:
// every upload and the balance call carry the account's username and password,
// while a poll carries nothing but the captcha id. Death By Captcha's own
// clients take the authtoken from the account panel in place of the password
// when the username is "authtoken", and so does this one, so either login fits
// the one username and password form.
//
// An image goes up as a "base64:" captchafile, a reCAPTCHA as type 4 (v2,
// invisible included) or type 5 (v3, with its action and minimum score), and
// a Turnstile as type 12, all solved from Death By Captcha's own network. The
// rest is refused: the coordinates API (type 2) reads only reCAPTCHA
// screenshots, the API documents no hCaptcha type, v2 Enterprise (type 25)
// requires a proxy, and v3 has no Enterprise variant.
//
//	https://deathbycaptcha.com/api
//	https://deathbycaptcha.com/api/newtokenrecaptcha
//	https://deathbycaptcha.com/api/turnstile
//	https://deathbycaptcha.com/api/recaptcha-legacymethod
//	https://github.com/deathbycaptcha/deathbycaptcha-agent-api-metadata/blob/master/spec/openapi/http.yaml

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

const deathByCaptchaBase = "https://api.dbcapi.me/api"

// DeathByCaptchaSolver is a Solver backed by one Death By Captcha account.
type DeathByCaptchaSolver struct {
	username string
	password string
	base     string
	hc       *http.Client
}

// NewDeathByCaptchaSolver builds a solver for one account login, the one
// internal/accounts stores under catalogue id "deathbycaptcha".
func NewDeathByCaptchaSolver(username, password string) *DeathByCaptchaSolver {
	// An upload answers 303 with the captcha's fields in its body already, so
	// the redirect to the same fields is not followed.
	hc := httpx.New(httpx.Options{Timeout: 20 * time.Second, MaxRedirects: -1})
	return &DeathByCaptchaSolver{username: username, password: password, base: deathByCaptchaBase, hc: hc}
}

// dbcStatusText is how the documentation describes each HTTP status an
// upload can fail with.
var dbcStatusText = map[int]string{
	http.StatusBadRequest:          "the request or the image was rejected",
	http.StatusForbidden:           "the login was rejected, or there are not enough credits",
	http.StatusInternalServerError: "something on Death By Captcha's side prevented the upload",
	http.StatusNotImplemented:      "the captcha type or its parameters are not supported",
	http.StatusServiceUnavailable:  "the service is overloaded",
}

// dbcCaptcha is what an upload and a poll answer. Text stays empty until the
// captcha is solved, and "?" with IsCorrect false means it could not be.
type dbcCaptcha struct {
	Captcha   int64   `json:"captcha"`
	Text      string  `json:"text"`
	IsCorrect dbcFlag `json:"is_correct"`
}

// dbcFlag reads a boolean the documentation writes as true and false in one
// example and as 1 and 0 in another.
type dbcFlag bool

func (f *dbcFlag) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true", "1":
		*f = true
	case "false", "0", "null":
		*f = false
	default:
		return fmt.Errorf("not a boolean: %s", b)
	}
	return nil
}

// dbcTokenParams is token_params for types 4 and 5; only v3 has an action and
// a minimum score.
type dbcTokenParams struct {
	GoogleKey string  `json:"googlekey"`
	PageURL   string  `json:"pageurl"`
	Action    string  `json:"action,omitempty"`
	MinScore  float64 `json:"min_score,omitempty"`
}

type dbcTurnstileParams struct {
	SiteKey string `json:"sitekey"`
	PageURL string `json:"pageurl"`
}

// upload builds the upload fields for c, or the reason Death By Captcha would
// refuse it.
func (s *DeathByCaptchaSolver) upload(c Challenge) (url.Values, error) {
	switch c.Kind {
	case KindImage:
		body, err := challengeImage(c)
		if err != nil {
			return nil, fmt.Errorf("death by captcha: %w", err)
		}
		return url.Values{"captchafile": {"base64:" + body}}, nil
	case KindWidget:
		typ, t, err := tokenTask(c)
		if err != nil {
			return nil, err
		}
		switch {
		case typ == taskRecaptchaV2:
			return dbcTyped("4", "token_params", dbcTokenParams{GoogleKey: t.WebsiteKey, PageURL: t.WebsiteURL})
		case typ == taskRecaptchaV3 && t.IsEnterprise:
			return nil, unsupported(typ + " Enterprise")
		case typ == taskRecaptchaV3:
			return dbcTyped("5", "token_params", dbcTokenParams{GoogleKey: t.WebsiteKey, PageURL: t.WebsiteURL, Action: t.PageAction, MinScore: t.MinScore})
		case typ == taskTurnstile:
			return dbcTyped("12", "turnstile_params", dbcTurnstileParams{SiteKey: t.WebsiteKey, PageURL: t.WebsiteURL})
		}
		return nil, unsupported(typ)
	default:
		return nil, unsupported(string(c.Kind))
	}
}

// dbcTyped is the upload of an API type whose parameters travel as one JSON
// field.
func dbcTyped(typ, field string, params any) (url.Values, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return url.Values{"type": {typ}, field: {string(b)}}, nil
}

// Takes implements Solver for Death By Captcha.
func (s *DeathByCaptchaSolver) Takes(c Challenge) error {
	_, err := s.upload(c)
	var r *Refusal
	if errors.As(err, &r) {
		return err
	}
	return nil
}

// Solve implements Solver for Death By Captcha.
func (s *DeathByCaptchaSolver) Solve(ctx context.Context, c Challenge) (string, error) {
	if s.username == "" || s.password == "" {
		return "", errors.New("captcha: no Death By Captcha login configured")
	}
	fields, err := s.upload(c)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, solverMaxWait)
	defer cancel()

	got, err := s.submit(ctx, fields)
	if err != nil {
		return "", err
	}
	for got.Text == "" {
		if err := solverSleep(ctx, solverPollInterval); err != nil {
			return "", fmt.Errorf("%w: %v", ErrTaskTaken, err)
		}
		if got, err = s.poll(ctx, got.Captcha); err != nil {
			return "", fmt.Errorf("%w: %w", ErrTaskTaken, err)
		}
	}
	if !got.IsCorrect {
		return "", fmt.Errorf("%w: death by captcha found no answer", ErrTaskTaken)
	}
	return solverAnswerFor(c.Kind, solverResult{text: got.Text, token: got.Text})
}

// Balance returns the account's balance in US cents, and is how a login is
// checked without paying for a captcha.
func (s *DeathByCaptchaSolver) Balance(ctx context.Context) (float64, error) {
	form := s.login(url.Values{})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	status, raw, _, err := roundTrip(s.hc, req)
	if err != nil {
		return 0, fmt.Errorf("death by captcha balance: %w", err)
	}
	var user struct {
		Balance  float64 `json:"balance"`
		IsBanned dbcFlag `json:"is_banned"`
	}
	err = dbcRead(status, raw, &user)
	if err == nil && user.IsBanned {
		err = &Refusal{Code: "is_banned", Detail: "the account is banned"}
	}
	var r *Refusal
	if errors.As(err, &r) {
		return 0, fmt.Errorf("captcha: death by captcha refused the login: %s", r.reason())
	}
	if err != nil {
		return 0, fmt.Errorf("death by captcha balance: %w", err)
	}
	return user.Balance, nil
}

func (s *DeathByCaptchaSolver) login(v url.Values) url.Values {
	if s.username == "authtoken" {
		v.Set("authtoken", s.password)
	} else {
		v.Set("username", s.username)
		v.Set("password", s.password)
	}
	return v
}

// submit uploads the captcha as the multipart POST the API asks for. A
// refusal the API answered with means no captcha was created.
func (s *DeathByCaptchaSolver) submit(ctx context.Context, fields url.Values) (dbcCaptcha, error) {
	body, contentType, err := multipartBody(s.login(fields))
	if err != nil {
		return dbcCaptcha{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/captcha", body)
	if err != nil {
		return dbcCaptcha{}, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	status, raw, sent, err := roundTrip(s.hc, req)
	if err != nil {
		if sent {
			return dbcCaptcha{}, fmt.Errorf("%w: death by captcha upload: %v", ErrTaskTaken, err)
		}
		return dbcCaptcha{}, fmt.Errorf("death by captcha upload: %w", err)
	}
	var got dbcCaptcha
	err = dbcRead(status, raw, &got)
	var r *Refusal
	switch {
	case errors.As(err, &r):
		return dbcCaptcha{}, err
	case err != nil:
		return dbcCaptcha{}, fmt.Errorf("%w: death by captcha upload: HTTP %d: %v", ErrTaskTaken, status, err)
	case got.Captcha == 0:
		return dbcCaptcha{}, fmt.Errorf("%w: death by captcha upload answered HTTP %d without a captcha id", ErrTaskTaken, status)
	}
	return got, nil
}

// poll reads the captcha's state. An id of 0 in the answer is the API saying
// it knows no such captcha.
func (s *DeathByCaptchaSolver) poll(ctx context.Context, id int64) (dbcCaptcha, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/captcha/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return dbcCaptcha{}, err
	}
	req.Header.Set("Accept", "application/json")
	status, raw, _, err := roundTrip(s.hc, req)
	if err != nil {
		return dbcCaptcha{}, fmt.Errorf("death by captcha poll: %w", err)
	}
	var got dbcCaptcha
	if err := dbcRead(status, raw, &got); err != nil {
		return dbcCaptcha{}, err
	}
	if got.Captcha == 0 {
		return dbcCaptcha{}, fmt.Errorf("death by captcha knows no captcha %d", id)
	}
	return got, nil
}

// dbcRead decodes an answer into out. A failed request, an HTTP error status
// or status 255 in the body, is a *Refusal coded with the error text the body
// carries, or else with the HTTP status.
func dbcRead(status int, raw []byte, out any) error {
	var st struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	// An error page need not be JSON; the HTTP status still says enough.
	_ = json.Unmarshal(raw, &st)
	if status >= 400 || st.Status == 255 {
		code := st.Error
		if code == "" {
			code = "HTTP " + strconv.Itoa(status)
		}
		return &Refusal{Code: code, Detail: dbcStatusText[status]}
	}
	return json.Unmarshal(raw, out)
}
