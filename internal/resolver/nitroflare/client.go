package nitroflare

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

	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// DefaultSite is where the API answers.
const DefaultSite = "https://nitroflare.com"

// maxBody caps how much of an answer is read. An answer about a hundred
// files is some 40 kilobytes.
const maxBody = 1 << 20

// maxBatch is as many files as getFileInfo is asked about at once, as
// JDownloader's plugin asks.
const maxBatch = 100

// siteKey is the key of the reCAPTCHA the API asks for, from JDownloader's
// plugin.
const siteKey = "6Lenx_USAAAAAF5L1pmTWvWcH73dipAEzNnmNLgy"

// captchaCode is the API asking for its captcha before it goes on.
const captchaCode = 12

// The answers that settle a download, whoever tries again.
var (
	ErrGone       = errors.New("nitroflare: the file has been deleted or never existed")
	ErrNoLogin    = errors.New("nitroflare: no Nitroflare login is stored")
	ErrNotPremium = errors.New("nitroflare: the login has no premium, and KnightLoader downloads Nitroflare only on premium, leaving free downloads to JDownloader")
	ErrTraffic    = errors.New("nitroflare: the account has no traffic left for today")
	// errCaptchaRefused is the API turning the captcha's answer down, or
	// asking for another right after taking one.
	errCaptchaRefused = errors.New("nitroflare: the answer to the captcha was not accepted")
)

// Ask has a captcha answered.
type Ask func(hoster.Captcha) (string, error)

// LoginError is Nitroflare turning the login down.
type LoginError = ownlogin.LoginError

// APIError is an answer of the API nothing else here explains, in
// Nitroflare's own words.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("nitroflare: %s (status %d, error %d)", e.Message, e.Status, e.Code)
}

// Client speaks to Nitroflare's API v2.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient asks site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// Account is what Nitroflare says about a login.
type Account = ownlogin.Account

// File is what getFileInfo says about one file.
type File struct {
	Name   string
	Size   int64
	Online bool
}

// count is a number Nitroflare writes as a number or as a string.
type count int64

func (c *count) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		*c = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	*c = count(n)
	return err
}

// Login checks the login and reads its account. The API keeps no session:
// every call that needs the account carries the email address and password.
// A captcha the API asks for goes to ask, and turns the login down where ask
// is nil.
func (c *Client) Login(ctx context.Context, user, password string, ask Ask) (string, Account, error) {
	var out struct {
		Status      string          `json:"status"`
		ExpiryDate  json.RawMessage `json:"expiryDate"`
		TrafficLeft count           `json:"trafficLeft"`
		TrafficMax  count           `json:"trafficMax"`
	}
	if err := c.call(ctx, "getKeyInfo", login(user, password), ask, &out); err != nil {
		return "", Account{}, err
	}
	a := Account{TrafficLeft: int64(out.TrafficLeft), TrafficMax: int64(out.TrafficMax)}
	// An account without premium writes 0 for the date.
	var date string
	if json.Unmarshal(out.ExpiryDate, &date) == nil && date != "0" {
		end, err := time.Parse(time.DateTime, date)
		if err != nil {
			return "", Account{}, fmt.Errorf("nitroflare: the end of premium %q does not read: %w", date, err)
		}
		a.Expiry = end.UTC()
	}
	a.Premium = strings.EqualFold(out.Status, "active") && a.Expiry.After(time.Now())
	return "", a, nil
}

// Files reads what the API knows about up to maxBatch ids, which needs no
// login. An id the API does not know is missing from the map.
func (c *Client) Files(ctx context.Context, ids []string) (map[string]File, error) {
	var out struct {
		Files json.RawMessage `json:"files"`
	}
	if err := c.call(ctx, "getFileInfo", url.Values{"files": {strings.Join(ids, ",")}}, nil, &out); err != nil {
		return nil, err
	}
	var listed map[string]struct {
		Status string `json:"status"`
		Name   string `json:"name"`
		Size   count  `json:"size"`
	}
	// With none of the ids known the files come as an empty list.
	if strings.HasPrefix(strings.TrimSpace(string(out.Files)), "[") {
		return map[string]File{}, nil
	}
	if err := json.Unmarshal(out.Files, &listed); err != nil {
		return nil, fmt.Errorf("nitroflare: the files getFileInfo lists do not read: %w", err)
	}
	files := make(map[string]File, len(listed))
	for id, f := range listed {
		files[id] = File{Name: f.Name, Size: int64(f.Size), Online: strings.EqualFold(f.Status, "online")}
	}
	return files, nil
}

// Link asks for the download link of the file id, with a captcha the API
// asks for going to ask as in Login.
func (c *Client) Link(ctx context.Context, user, password, id string, ask Ask) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	q := login(user, password)
	q.Set("file", id)
	if err := c.call(ctx, "getDownloadLink", q, ask, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("nitroflare: the download was answered without a link")
	}
	return out.URL, nil
}

// login is the query of a call on the account. The address goes out in
// lower case, as JDownloader's plugin writes it.
func login(user, password string) url.Values {
	return url.Values{"user": {strings.ToLower(user)}, "premiumKey": {password}}
}

// call asks an API method with the parameters in the query string, the only
// place the API reads them, and decodes the answer's result into out. The
// URL then carries the password, so no error quotes it. A captcha the API
// asks for on a call on the account goes to ask, once, and the call is made
// again, as JDownloader's plugin does.
func (c *Client) call(ctx context.Context, method string, q url.Values, ask Ask, out any) error {
	code, err := c.get(ctx, method, q, out)
	if code != captchaCode || ask == nil {
		return err
	}
	if err := c.solveCaptcha(ctx, q.Get("user"), ask); err != nil {
		return err
	}
	if code, err = c.get(ctx, method, q, out); code == captchaCode {
		return errCaptchaRefused
	}
	return err
}

// solveCaptcha has ask solve the API's reCAPTCHA for user, posts the answer
// and confirms it.
func (c *Client) solveCaptcha(ctx context.Context, user string, ask Ask) error {
	page := c.site + "/api/v2/solveCaptcha?" + url.Values{"user": {user}}.Encode()
	token, err := ask(hoster.Captcha{Kind: hoster.Recaptcha, SiteKey: siteKey, PageURL: page})
	if err != nil {
		return fmt.Errorf("nitroflare: %w", err)
	}
	_, body, err := c.fetch(ctx, http.MethodPost, page, url.Values{"response": {token}})
	if err != nil {
		return fmt.Errorf("nitroflare: solveCaptcha: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(body)), "passed") {
		return errCaptchaRefused
	}
	status, _, err := c.fetch(ctx, http.MethodGet, page+"&solved=1", nil)
	if err != nil {
		return fmt.Errorf("nitroflare: solveCaptcha: %w", err)
	}
	if status != http.StatusOK {
		return &APIError{Status: status, Message: "solveCaptcha did not confirm the answer"}
	}
	return nil
}

// fetch makes one request and reads its answer, posting form where it is
// set. Its errors leave the URL out.
func (c *Client) fetch(ctx context.Context, method, target string, form url.Values) (int, []byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return 0, nil, httpx.StripURL(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, httpx.StripURL(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, httpx.StripURL(err)
	}
	return resp.StatusCode, raw, nil
}

// get is one call of call, which also returns the API's error code, 0 for
// none.
func (c *Client) get(ctx context.Context, method string, q url.Values, out any) (int, error) {
	status, body, err := c.fetch(ctx, http.MethodGet, c.site+"/api/v2/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("nitroflare: %s: %w", method, err)
	}
	var env struct {
		Type    string          `json:"type"`
		Result  json.RawMessage `json:"result"`
		Code    int             `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		if status != http.StatusOK {
			return 0, &APIError{Status: status, Message: http.StatusText(status)}
		}
		return 0, fmt.Errorf("nitroflare: %s answered with something that is not JSON", method)
	}
	if env.Type != "success" || status != http.StatusOK {
		return env.Code, classify(status, env.Code, env.Message)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return 0, fmt.Errorf("nitroflare: the answer to %s does not read: %w", method, err)
	}
	return 0, nil
}

// classify reads a refusal of the API. Spent traffic has no code of its own
// that is known, so its words decide, the ones the site shows in a browser.
func classify(status, code int, message string) error {
	m := strings.ToLower(message)
	switch {
	case code == 4:
		return ErrGone
	case code == 8:
		return &LoginError{Msg: "nitroflare: Nitroflare turned the login down, so check the email address and password"}
	case code == 12:
		return &LoginError{Captcha: true, Msg: "nitroflare: Nitroflare's API asks for a captcha before it takes this login, which KnightLoader cannot answer, " +
			"so open https://nitroflare.com/api/v2/solveCaptcha?user= followed by the account's email address in a browser, " +
			"solve it there and then switch the login off and on again"}
	case code == 1:
		return fmt.Errorf("%w (%s)", ErrNotPremium, message)
	case strings.Contains(m, "maximum volume"), strings.Contains(m, "daily download limit"):
		return ErrTraffic
	}
	return &APIError{Status: status, Code: code, Message: message}
}
