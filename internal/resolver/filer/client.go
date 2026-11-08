package filer

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

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// DefaultSite is where the API answers.
const DefaultSite = "https://filer.net"

// maxBody caps how much of an answer is read. An answer about a hundred
// files is some 20 kilobytes.
const maxBody = 1 << 20

// maxBatch is as many files as multi_status is asked about at once, as
// JDownloader's plugin asks.
const maxBatch = 100

// The answers that settle a download, whoever tries again.
var (
	ErrGone       = errors.New("filer.net: the file has been deleted or never existed")
	ErrNoLogin    = errors.New("filer.net: no filer.net login is stored")
	ErrNotPremium = errors.New("filer.net: the login has no premium, and KnightLoader downloads filer.net only on premium, leaving free downloads to JDownloader")
	ErrTraffic    = errors.New("filer.net: the account has no traffic left")
	ErrPassword   = errors.New("filer.net: the file sits in a folder locked with a password")
	ErrDenied     = errors.New("filer.net: filer.net refuses the download for this account or address")
	// ErrBusy is filer.net having no free slot or no download server for
	// the moment.
	ErrBusy = errors.New("filer.net: filer.net has no free download slot or server at the moment")
	// ErrSignIn is filer.net not getting through a sign-in, which its
	// documentation tells apart from a wrong password.
	ErrSignIn = errors.New("filer.net: filer.net could not complete the sign-in")
)

// LoginError is filer.net turning the login down.
type LoginError = ownlogin.LoginError

// APIError is an answer of the API nothing else here explains, in
// filer.net's own words.
type APIError struct {
	HTTP   int
	Code   int
	Status string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("filer.net: %s (status %d, code %d)", e.Status, e.HTTP, e.Code)
}

// Client speaks to filer.net's API.
type Client struct {
	hc *http.Client
	// direct is hc without following redirects, since the download link is
	// the redirect itself.
	direct *http.Client
	site   string
}

// NewClient asks site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	direct := *hc
	direct.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{hc: hc, direct: &direct, site: strings.TrimSuffix(site, "/")}
}

// Account is what filer.net says about a login.
type Account = ownlogin.Account

// File is what the API says about one file.
type File struct {
	Name string
	Size int64
	// PremiumOnly is a file its owner keeps for premium accounts.
	PremiumOnly bool
}

// number is a number the API writes as a number, as a string or as false.
type number int64

func (n *number) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" || s == "false" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	*n = number(v)
	return err
}

type fileInfo struct {
	Hash        string `json:"file_hash"`
	Name        string `json:"file_name"`
	Size        number `json:"file_size"`
	PremiumOnly bool   `json:"premium_only"`
}

func (f fileInfo) file() File {
	return File{Name: f.Name, Size: int64(f.Size), PremiumOnly: f.PremiumOnly}
}

// Login reads the account behind the login. The API keeps no session and
// takes the email address and password with every call that needs them.
func (c *Client) Login(ctx context.Context, user, password string) (string, Account, error) {
	var out struct {
		Premium     bool   `json:"premium"`
		Until       number `json:"until"`
		Traffic     number `json:"traffic"`
		TrafficLeft number `json:"traffic_left"`
	}
	req, err := c.request(ctx, http.MethodGet, "/profile.json", nil)
	if err != nil {
		return "", Account{}, err
	}
	req.SetBasicAuth(user, password)
	if _, err := c.call(c.hc, req, &out); err != nil {
		return "", Account{}, err
	}
	// The traffic left goes below zero after a download larger than what was
	// left, and fills up again over the day.
	a := Account{Premium: out.Premium, TrafficMax: int64(out.Traffic), TrafficLeft: max(int64(out.TrafficLeft), 0)}
	if out.Until > 0 {
		a.Expiry = time.Unix(int64(out.Until), 0).UTC()
	}
	return "", a, nil
}

// File reads the name and size of the file id, which needs no login.
func (c *Client) File(ctx context.Context, id string) (File, error) {
	var out fileInfo
	req, err := c.request(ctx, http.MethodGet, "/status/"+id+".json", nil)
	if err != nil {
		return File{}, err
	}
	if _, err := c.call(c.hc, req, &out); err != nil {
		return File{}, err
	}
	if out.Hash != id {
		return File{}, ErrGone
	}
	return out.file(), nil
}

// Files reads what the API knows about up to maxBatch ids, which needs no
// login. An id the API does not know is missing from the map.
func (c *Client) Files(ctx context.Context, ids []string) (map[string]File, error) {
	var req *http.Request
	var err error
	if len(ids) == 1 {
		req, err = c.request(ctx, http.MethodGet, "/multi_status/"+ids[0]+".json", nil)
	} else {
		body, _ := json.Marshal(map[string][]string{"hashes": ids})
		req, err = c.request(ctx, http.MethodPost, "/multi_status.json", body)
	}
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if _, err := c.call(c.hc, req, &raw); err != nil {
		return nil, err
	}
	files := map[string]File{}
	// With none of the ids known the data can come as an empty list.
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return files, nil
	}
	var listed map[string]fileInfo
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, fmt.Errorf("filer.net: the files multi_status lists do not read: %w", err)
	}
	for id, f := range listed {
		if f.Hash == id {
			files[id] = f.file()
		}
	}
	return files, nil
}

// Link asks for the download link of the file id, which the API answers with
// a redirect to it.
func (c *Client) Link(ctx context.Context, user, password, id string) (string, error) {
	var out struct {
		URL string `json:"download_url"`
	}
	req, err := c.request(ctx, http.MethodGet, "/dl/"+id+".json", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, password)
	resp, err := c.call(c.direct, req, &out)
	if err != nil {
		return "", err
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, nil
	}
	if out.URL == "" {
		return "", errors.New("filer.net: the download was answered without a link")
	}
	return out.URL, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.site+"/api"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// call sends req through hc and decodes the answer's data into out. Every
// answer has the same envelope, whose code is not always the HTTP status:
// a missing file is HTTP 500 with code 505.
func (c *Client) call(hc *http.Client, req *http.Request, out any) (*http.Response, error) {
	what := strings.TrimPrefix(req.URL.Path, "/api/")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filer.net: %s: %w", what, httpx.StripURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("filer.net: %s: %w", what, httpx.StripURL(err))
	}
	var env struct {
		Code   number          `json:"code"`
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, &APIError{HTTP: resp.StatusCode, Status: http.StatusText(resp.StatusCode)}
		}
		return nil, fmt.Errorf("filer.net: %s answered with something that is not JSON", what)
	}
	ok := env.Status == "success" || env.Status == "redirect" && resp.StatusCode == http.StatusFound
	if !ok {
		return nil, classify(resp.StatusCode, int(env.Code), env.Status)
	}
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return nil, fmt.Errorf("filer.net: the answer to %s does not read: %w", what, err)
		}
	}
	return resp, nil
}

// classify reads a refusal of the API, by the status text its documentation
// lists and, where that names nothing, by the code. Spent traffic is code
// 504, which only JDownloader's plugin knows of.
func classify(httpStatus, code int, status string) error {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "invalid credentials":
		return &LoginError{Msg: "filer.net: filer.net turned the login down, so check the email address and password"}
	case "authentication failed":
		return ErrSignIn
	case "account suspended":
		return &LoginError{Msg: "filer.net: filer.net does not take this account at the moment, so check it on filer.net's website"}
	case "account_breach_locked":
		return &LoginError{Msg: "filer.net: filer.net locked the account because its password turned up in a breach, so set a new one on filer.net's website"}
	case "premium required":
		return ErrNotPremium
	case "password required", "folder password required":
		return ErrPassword
	case "access denied", "download not allowed":
		return ErrDenied
	case "concurrent download limit reached", "no download server available":
		return ErrBusy
	case "file not found", "folder not found":
		return ErrGone
	}
	switch code {
	case 505, 506:
		return ErrGone
	case 504:
		return ErrTraffic
	}
	return &APIError{HTTP: httpStatus, Code: code, Status: status}
}
