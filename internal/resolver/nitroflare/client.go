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

// The answers that settle a download, whoever tries again.
var (
	ErrGone       = errors.New("nitroflare: the file has been deleted or never existed")
	ErrNoLogin    = errors.New("nitroflare: no Nitroflare login is stored")
	ErrNotPremium = errors.New("nitroflare: the login has no premium, and KnightLoader downloads Nitroflare only on premium, leaving free downloads to JDownloader")
	ErrTraffic    = errors.New("nitroflare: the account has no traffic left for today")
)

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
func (c *Client) Login(ctx context.Context, user, password string) (string, Account, error) {
	var out struct {
		Status      string          `json:"status"`
		ExpiryDate  json.RawMessage `json:"expiryDate"`
		TrafficLeft count           `json:"trafficLeft"`
		TrafficMax  count           `json:"trafficMax"`
	}
	if err := c.call(ctx, "getKeyInfo", url.Values{"user": {user}, "premiumKey": {password}}, &out); err != nil {
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
	if err := c.call(ctx, "getFileInfo", url.Values{"files": {strings.Join(ids, ",")}}, &out); err != nil {
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

// Link asks for the download link of the file id.
func (c *Client) Link(ctx context.Context, user, password, id string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, "getDownloadLink", url.Values{"user": {user}, "premiumKey": {password}, "file": {id}}, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("nitroflare: the download was answered without a link")
	}
	return out.URL, nil
}

// call asks an API method with the parameters in the query string, the only
// place the API reads them, and decodes the answer's result into out. The
// URL then carries the password, so no error quotes it.
func (c *Client) call(ctx context.Context, method string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/api/v2/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("nitroflare: %s: %w", method, httpx.StripURL(err))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("nitroflare: %s: %w", method, httpx.StripURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("nitroflare: %s: %w", method, httpx.StripURL(err))
	}
	var env struct {
		Type    string          `json:"type"`
		Result  json.RawMessage `json:"result"`
		Code    int             `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		if resp.StatusCode != http.StatusOK {
			return &APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}
		return fmt.Errorf("nitroflare: %s answered with something that is not JSON", method)
	}
	if env.Type != "success" || resp.StatusCode != http.StatusOK {
		return classify(resp.StatusCode, env.Code, env.Message)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("nitroflare: the answer to %s does not read: %w", method, err)
	}
	return nil
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
