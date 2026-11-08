package rapidgator

import (
	"cmp"
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
)

// DefaultSite is where the API and the file pages are.
const DefaultSite = "https://rapidgator.net"

// maxBody caps how much of an answer is read. An API answer is a few hundred
// bytes and a file page some 25 kilobytes.
const maxBody = 2 << 20

// The answers that settle a download, whoever tries again.
var (
	ErrGone       = errors.New("rapidgator: the file has been deleted or never existed")
	ErrNoLogin    = errors.New("rapidgator: no Rapidgator login is stored")
	ErrNotPremium = errors.New("rapidgator: the login has no premium, and KnightLoader downloads Rapidgator only on premium, leaving free downloads to JDownloader")
	ErrTraffic    = errors.New("rapidgator: the account has no traffic left")
	ErrDenied     = errors.New("rapidgator: Rapidgator refuses downloads from this address")
	// ErrBusy is Rapidgator asking for a pause after too many sign-ins.
	ErrBusy = errors.New("rapidgator: Rapidgator asks for a pause between sign-ins")
)

// errSession is a token Rapidgator no longer knows. Signing in again cures
// it, so it ends a download only when a fresh token is turned away too.
var errSession = errors.New("rapidgator: Rapidgator ended the session again right after signing in")

// LoginError is Rapidgator turning the login down. The same login would be
// turned down again, and Rapidgator blocks addresses that keep trying.
type LoginError struct{ msg string }

func (e *LoginError) Error() string { return e.msg }

// APIError is an answer of the API nothing else here explains, in
// Rapidgator's own words.
type APIError struct {
	Status  int
	Details string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("rapidgator: %s (status %d)", strings.TrimPrefix(e.Details, "Error: "), e.Status)
}

// Client speaks to Rapidgator's API v2 and reads its public file pages.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient asks site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// Account is what Rapidgator says about a login.
type Account struct {
	Premium bool
	Expiry  time.Time
	// TrafficLeft and TrafficMax are bytes, both 0 for a plan without a
	// traffic quota.
	TrafficLeft, TrafficMax int64
}

// File is what the API says about one file.
type File struct {
	Name string
	Size int64
}

// count is a number Rapidgator writes as a number, as a string or as null.
type count int64

func (c *count) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		*c = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	*c = count(n)
	return err
}

type user struct {
	IsPremium  bool  `json:"is_premium"`
	PremiumEnd count `json:"premium_end_time"`
	Traffic    struct {
		Total count `json:"total"`
		Left  count `json:"left"`
	} `json:"traffic"`
}

func (u user) account() Account {
	a := Account{Premium: u.IsPremium, TrafficMax: int64(u.Traffic.Total), TrafficLeft: int64(u.Traffic.Left)}
	if u.PremiumEnd > 0 {
		a.Expiry = time.Unix(int64(u.PremiumEnd), 0).UTC()
	}
	return a
}

// Login signs in and returns the token the other calls go with.
func (c *Client) Login(ctx context.Context, email, password string) (string, Account, error) {
	var out struct {
		Token string `json:"token"`
		User  user   `json:"user"`
	}
	if err := c.call(ctx, "user/login", url.Values{"login": {email}, "password": {password}}, &out); err != nil {
		return "", Account{}, err
	}
	if out.Token == "" {
		return "", Account{}, errors.New("rapidgator: the sign-in was answered without a token")
	}
	return out.Token, out.User.account(), nil
}

// User reads the account behind token, which also proves the token still
// works.
func (c *Client) User(ctx context.Context, token string) (Account, error) {
	var out struct {
		User user `json:"user"`
	}
	err := c.call(ctx, "user/info", url.Values{"token": {token}}, &out)
	return out.User.account(), err
}

// File reads the name and size of the file id.
func (c *Client) File(ctx context.Context, token, id string) (File, error) {
	var out struct {
		File struct {
			Name string `json:"name"`
			Size count  `json:"size"`
		} `json:"file"`
	}
	err := c.call(ctx, "file/info", url.Values{"token": {token}, "file_id": {id}}, &out)
	return File{Name: out.File.Name, Size: int64(out.File.Size)}, err
}

// Link asks for the download link of the file id and how long to wait before
// using it.
func (c *Client) Link(ctx context.Context, token, id string) (string, time.Duration, error) {
	var out struct {
		URL   string `json:"download_url"`
		Delay count  `json:"delay"`
	}
	if err := c.call(ctx, "file/download", url.Values{"token": {token}, "file_id": {id}}, &out); err != nil {
		return "", 0, err
	}
	if out.URL == "" {
		return "", 0, errors.New("rapidgator: the download was answered without a link")
	}
	return out.URL, time.Duration(out.Delay) * time.Second, nil
}

// call posts form to an API method and decodes the answer's response into
// out. The API takes POST as well as GET, and a POST keeps the password and
// the token out of every log a URL ends up in.
func (c *Client) call(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.site+"/api/v2/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	// Older answers name their fields response_status and response_details,
	// and some refusals come as {"error": ..., "success": false}.
	var env struct {
		Response        json.RawMessage `json:"response"`
		Status          *int            `json:"status"`
		ResponseStatus  *int            `json:"response_status"`
		Details         string          `json:"details"`
		ResponseDetails string          `json:"response_details"`
		Error           string          `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		if resp.StatusCode != http.StatusOK {
			return classify(method, resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return fmt.Errorf("rapidgator: %s answered with something that is not JSON", method)
	}
	status := resp.StatusCode
	if env.Status != nil {
		status = *env.Status
	} else if env.ResponseStatus != nil {
		status = *env.ResponseStatus
	}
	details := cmp.Or(env.Details, env.ResponseDetails, env.Error)
	if status != http.StatusOK || env.Error != "" {
		return classify(method, status, details)
	}
	if err := json.Unmarshal(env.Response, out); err != nil {
		return fmt.Errorf("rapidgator: the answer to %s does not read: %w", method, err)
	}
	return nil
}

// classify reads a refusal of the API. The wording decides before the
// status, which Rapidgator does not keep apart: a 401 is a wrong password at
// sign-in and an ended session everywhere else, and a 404 from file/download
// can be an ended session as well, as JDownloader's plugin found.
func classify(method string, status int, details string) error {
	d := strings.ToLower(details)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(d, s) {
				return true
			}
		}
		return false
	}
	switch {
	case status == http.StatusLocked || has("exceeded traffic"):
		return ErrTraffic
	case has("denied by ip"):
		return ErrDenied
	case has("please wait"):
		return ErrBusy
	case has("user is not premium", "can be downloaded by premium only", "you can download files up to"):
		return fmt.Errorf("%w (%s)", ErrNotPremium, strings.TrimPrefix(details, "Error: "))
	case has("invalid auth code"):
		return &LoginError{"rapidgator: the account signs in with a two-factor code, which KnightLoader cannot enter, so switch two-factor sign-in off for it at Rapidgator"}
	case has("unusual ip"):
		return &LoginError{"rapidgator: Rapidgator wants this address confirmed through the link it emailed to the account"}
	case has("login or password is wrong", "e-mail or password", "password cannot be blank", "login or password is missing"):
		return &LoginError{"rapidgator: Rapidgator turned the login down, so check the email address and password"}
	case has("frozen", "account locked"):
		return &LoginError{"rapidgator: Rapidgator turned the login down: " + strings.TrimPrefix(details, "Error: ")}
	case has("session"):
		return errSession
	case status == http.StatusUnauthorized && method == "user/login":
		return &LoginError{"rapidgator: Rapidgator turned the login down, so check the email address and password"}
	case status == http.StatusUnauthorized:
		return errSession
	case status == http.StatusNotFound && method == "file/info":
		return ErrGone
	case status == http.StatusNotFound:
		return errSession
	}
	return &APIError{Status: status, Details: details}
}

// Page reads the public page of a file link, which needs no login.
func (c *Client) Page(ctx context.Context, link string) (page, error) {
	norm, ok := Normalize(link)
	if !ok {
		return page{}, fmt.Errorf("rapidgator: %s is not a link to a file", link)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/file/"+fileIDOf(norm), nil)
	if err != nil {
		return page{}, err
	}
	// English pages, which the patterns are written for.
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.AddCookie(&http.Cookie{Name: "lang", Value: "en"})
	resp, err := c.hc.Do(req)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return page{}, err
	}
	// A missing file sends the browser on to the premium page.
	if resp.StatusCode == http.StatusNotFound || !strings.HasPrefix(resp.Request.URL.Path, "/file/") || notFound.Match(body) {
		return page{}, ErrGone
	}
	if resp.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("rapidgator: the file page answered %d", resp.StatusCode)
	}
	return readPage(string(body)), nil
}
