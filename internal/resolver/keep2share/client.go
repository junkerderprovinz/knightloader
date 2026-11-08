package keep2share

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// maxBody caps how much of an answer is read. An answer about a hundred
// files is some 50 kilobytes.
const maxBody = 1 << 20

// maxBatch is as many files as getFilesInfo is asked about at once, as
// JDownloader's plugin asks.
const maxBatch = 100

// The answers that settle a download, whoever tries again. They leave out
// the site, which the backend names in front of them.
var (
	ErrGone       = errors.New("the file has been deleted or never existed")
	ErrBlocked    = errors.New("the file has been blocked")
	ErrNoLogin    = errors.New("no login is stored")
	ErrNotPremium = errors.New("the login has no premium, and KnightLoader downloads from here only on premium, leaving free downloads to JDownloader")
	// ErrRestricted is a file only some accounts may download, such as a
	// private one.
	ErrRestricted = errors.New("this account may not download the file")
	ErrTraffic    = errors.New("the account has no traffic left for today")
	ErrQuota      = errors.New("the account has reached its download limit for today")
	ErrDenied     = errors.New("downloads from this network are refused")
	// ErrBusy is the site asking for a pause after too many sign-ins.
	ErrBusy = errors.New("too many sign-ins, the site asks for a pause")
)

// errSession is a token the site has stopped taking. Signing in again cures it,
// so it ends a download only when a fresh token is turned away too.
var errSession = errors.New("the session ended again right after signing in")

// LoginError is the site turning the login down.
type LoginError = ownlogin.LoginError

// APIError is an answer of the API nothing else here explains, in the site's
// own words.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (status %d, error %s)", e.Message, e.Status, e.Code)
}

// Client speaks to the API v2 of one site of the family.
type Client struct {
	hc  *http.Client
	api string
}

// NewClient asks the API at api, a Host's API outside tests, through hc.
func NewClient(api string, hc *http.Client) *Client {
	return &Client{hc: hc, api: strings.TrimSuffix(api, "/")}
}

// Account is what the site says about a login. It names no traffic total.
type Account = ownlogin.Account

// File is what getFilesInfo says about one file.
type File struct {
	Name      string
	Size      int64
	Available bool
	Folder    bool
}

// expiry is the end of premium, which the API writes as Unix seconds, as an
// ISO time or as false for an account that never had premium.
type expiry time.Time

func (e *expiry) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case float64:
		if v > 0 {
			*e = expiry(time.Unix(int64(v), 0).UTC())
		}
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return fmt.Errorf("the end of premium %q does not read: %w", v, err)
		}
		*e = expiry(t.UTC())
	}
	return nil
}

// Login signs in and reads the account the token belongs to.
func (c *Client) Login(ctx context.Context, user, password string) (string, Account, error) {
	var out struct {
		Token string `json:"auth_token"`
	}
	if err := c.call(ctx, "login", map[string]any{"username": user, "password": password}, &out); err != nil {
		return "", Account{}, err
	}
	if out.Token == "" {
		return "", Account{}, errors.New("the sign-in was answered without a token")
	}
	acct, err := c.Account(ctx, out.Token)
	return out.Token, acct, err
}

// Account reads the account behind token, which also proves the token still
// works.
func (c *Client) Account(ctx context.Context, token string) (Account, error) {
	var out struct {
		Traffic int64  `json:"available_traffic"`
		Expires expiry `json:"account_expires"`
	}
	if err := c.call(ctx, "accountInfo", map[string]any{"auth_token": token}, &out); err != nil {
		return Account{}, err
	}
	end := time.Time(out.Expires)
	// An account whose premium ran out keeps its old date.
	return Account{Premium: end.After(time.Now()), Expiry: end, TrafficLeft: out.Traffic}, nil
}

// Files reads what the API knows about up to maxBatch ids, which needs no
// login. An id the API does not know at all is missing from the map.
func (c *Client) Files(ctx context.Context, ids []string) (map[string]File, error) {
	var out struct {
		Files []struct {
			ID          string  `json:"id"`
			RequestedID string  `json:"requested_id"`
			Name        *string `json:"name"`
			Size        *int64  `json:"size"`
			Available   *bool   `json:"is_available"`
			Folder      *bool   `json:"is_folder"`
		} `json:"files"`
	}
	err := c.call(ctx, "getFilesInfo", map[string]any{"ids": ids}, &out)
	if errors.Is(err, ErrGone) {
		// The API refuses a question without a single valid id.
		return map[string]File{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := map[string]File{}
	for _, f := range out.Files {
		file := File{Available: f.Available != nil && *f.Available, Folder: f.Folder != nil && *f.Folder}
		if f.Name != nil {
			file.Name = *f.Name
		}
		if f.Size != nil {
			file.Size = *f.Size
		}
		files[f.ID] = file
		// A tracking id comes back under the file's own id.
		if f.RequestedID != "" {
			files[f.RequestedID] = file
		}
	}
	return files, nil
}

// Link asks for the download link of the file id.
func (c *Client) Link(ctx context.Context, token, id string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, "getUrl", map[string]any{"auth_token": token, "file_id": id}, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("the download was answered without a link")
	}
	return out.URL, nil
}

// call posts in as JSON to an API method and decodes the answer into out.
func (c *Client) call(ctx context.Context, method string, in, out any) error {
	refusal, err := c.do(ctx, method, in, out)
	if refusal != nil {
		return refusal.classify(method)
	}
	return err
}

// do is call with a refusal left as the API wrote it.
func (c *Client) do(ctx context.Context, method string, in, out any) (*apiError, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/api/v2/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	var env struct {
		Status    string          `json:"status"`
		ErrorCode json.RawMessage `json:"errorCode"`
		Message   string          `json:"message"`
		Errors    []struct {
			Code          int    `json:"code"`
			TimeRemaining string `json:"timeRemaining"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, &APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}
		return nil, fmt.Errorf("%s answered with something that is not JSON", method)
	}
	if env.Status == "error" || resp.StatusCode != http.StatusOK {
		e := &apiError{status: resp.StatusCode, code: env.ErrorCode, message: env.Message}
		if len(env.Errors) > 0 {
			e.sub = env.Errors[0].Code
			// The seconds come as a decimal in a string.
			if secs, err := strconv.ParseFloat(env.Errors[0].TimeRemaining, 64); err == nil {
				e.wait = time.Duration(secs) * time.Second
			}
		}
		return e, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("the answer to %s does not read: %w", method, err)
	}
	return nil, nil
}

// apiError is a refusal as the API writes it. errorCode is mostly a number,
// and a word for the waits after too many captchas.
type apiError struct {
	status  int
	code    json.RawMessage
	sub     int
	message string
	// wait is the time a free download is asked to wait, 0 for none.
	wait time.Duration
}

// classify reads a refusal of the API. Codes 21 and 42 only say the download
// is not possible, and the code in their errors list says why, in the same
// numbers the API uses on their own. Code 2 is a malformed request at status
// 400 and spent traffic at 406.
func (e apiError) classify(method string) error {
	var code int
	if json.Unmarshal(e.code, &code) != nil {
		var word string
		_ = json.Unmarshal(e.code, &word)
		if strings.HasPrefix(word, "captcha_need_wait") {
			return ErrBusy
		}
		return &APIError{Status: e.status, Code: word, Message: e.message}
	}
	if (code == 21 || code == 42) && e.sub != 0 {
		code = e.sub
	}
	login := method == "login"
	switch {
	case code == 2 && e.status == http.StatusBadRequest && login, code == 70:
		return &LoginError{Msg: "the login was turned down, so check the email address and password"}
	case code == 2 && e.status == http.StatusBadRequest, code == 20, code == 23:
		return ErrGone
	case code == 22:
		return ErrBlocked
	case code == 2:
		return ErrTraffic
	case code == 1, code == 6:
		return ErrQuota
	case code == 3, code == 7:
		return ErrNotPremium
	case code == 4, code == 8, code == 9:
		return fmt.Errorf("%w (%s)", ErrRestricted, e.message)
	case login && (code == 30 || code == 31 || code == 33):
		return &LoginError{Captcha: true, Msg: "the sign-in asks for a captcha, which KnightLoader cannot answer, so sign in once on the site in a browser on this network and then switch the login off and on again"}
	case code == 10, code == 11, code == 30, code == 75:
		// A premium download asked for a captcha means the token was not
		// taken, as JDownloader's plugin found.
		return errSession
	case code == 71:
		return ErrBusy
	case code == 73, code == 77:
		return ErrDenied
	case code == 72, code == 74, code == 76:
		return &LoginError{Msg: "the login was turned down: " + e.message}
	}
	return &APIError{Status: e.status, Code: string(e.code), Message: e.message}
}
