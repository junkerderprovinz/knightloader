package pixeldrain

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultAPI is where Pixeldrain's API answers.
const DefaultAPI = "https://pixeldrain.com/api"

// maxAnswer caps how much of an answer is read. A list of a few thousand
// files stays well below it.
const maxAnswer = 8 << 20

// Client asks Pixeldrain's API about files and lists.
type Client struct {
	hc  *http.Client
	api string

	// Key returns the API key of the stored Pixeldrain login, "" for none.
	Key func() string

	mu sync.Mutex
	// paid remembers, per key, whether its account pays, which only the
	// account itself says.
	paid map[string]bool
}

// NewClient talks to api, DefaultAPI outside tests, through hc.
func NewClient(api string, hc *http.Client) *Client {
	return &Client{hc: hc, api: strings.TrimSuffix(api, "/"), paid: map[string]bool{}}
}

// file is the part of Pixeldrain's file info the client reads.
type file struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Availability string `json:"availability"`
	AbuseType    string `json:"abuse_type"`
}

type list struct {
	Title string `json:"title"`
	Files []file `json:"files"`
}

// apiError is how Pixeldrain turns a request down.
type apiError struct {
	Status  int    `json:"-"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return "pixeldrain: " + e.Message
	}
	return fmt.Sprintf("pixeldrain: the API answered %d %s", e.Status, e.Value)
}

func (c *Client) key() string {
	if c.Key == nil {
		return ""
	}
	return c.Key()
}

// auth is the header that sends key, which Pixeldrain takes as the password
// of HTTP basic authentication with any user name.
func auth(key string) map[string]string {
	if key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+key))}
}

// get fetches path and decodes a successful answer into out. A refusal comes
// back as *apiError.
func (c *Client) get(ctx context.Context, path string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	fine := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
	if fine && out == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return err
	}
	if !fine {
		e := &apiError{Status: resp.StatusCode}
		json.Unmarshal(body, e)
		return e
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("pixeldrain: the API answered in a shape it does not use: %w", err)
	}
	return nil
}

// infos asks for the files of ids in one request. A file that does not
// exist is left out of the answer, as Pixeldrain leaves it out.
func (c *Client) infos(ctx context.Context, ids []string) (map[string]file, error) {
	var raw json.RawMessage
	err := c.get(ctx, "/file/"+strings.Join(ids, ",")+"/info", auth(c.key()), &raw)
	var e *apiError
	if errors.As(err, &e) && e.Value == "not_found" {
		return map[string]file{}, nil
	}
	if err != nil {
		return nil, err
	}
	// One id is answered with an object, several with an array.
	var files []file
	if len(ids) == 1 {
		var f file
		err = json.Unmarshal(raw, &f)
		files = []file{f}
	} else {
		err = json.Unmarshal(raw, &files)
	}
	if err != nil {
		return nil, fmt.Errorf("pixeldrain: the file info came in a shape it does not use: %w", err)
	}
	out := make(map[string]file, len(files))
	for _, f := range files {
		out[f.ID] = f
	}
	return out, nil
}

func (c *Client) list(ctx context.Context, id string) (list, error) {
	var l list
	err := c.get(ctx, "/list/"+id, auth(c.key()), &l)
	return l, refusal(err)
}

// paidPlan reports whether the account behind key pays for Pixeldrain, which
// is what lifts the cap on connections. An answer that could not be read
// counts as free.
func (c *Client) paidPlan(ctx context.Context, key string) bool {
	c.mu.Lock()
	paid, known := c.paid[key]
	c.mu.Unlock()
	if known {
		return paid
	}
	var user struct {
		Subscription struct {
			Type string `json:"type"`
		} `json:"subscription"`
	}
	if err := c.get(ctx, "/user", auth(key), &user); err != nil {
		return false
	}
	paid = user.Subscription.Type != "" && user.Subscription.Type != "free"
	c.mu.Lock()
	c.paid[key] = paid
	c.mu.Unlock()
	return paid
}

// The waits a refusal that passes with time is sat out for. A day's limit
// is counted over the last 24 hours, so it frees up bit by bit.
const (
	limitWait = 30 * time.Minute
	slotWait  = time.Minute
)

// refusal turns Pixeldrain's reasons into the ones a task fails or waits
// with, and leaves any other error as it is.
func refusal(err error) error {
	var e *apiError
	if !errors.As(err, &e) {
		return err
	}
	switch e.Value {
	case "not_found":
		return hoster.Gone("pixeldrain")
	case "unavailable_for_legal_reasons":
		return &hoster.Refusal{Reason: core.ReasonGone, Msg: "pixeldrain: the file was taken down after a report"}
	case "authentication_failed":
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "pixeldrain: the API key of the stored Pixeldrain login was not accepted"}
	case "file_rate_limited_captcha_required":
		return &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "pixeldrain: the file is downloaded so often that Pixeldrain asks for a captcha on its page, which only a paid Pixeldrain account skips"}
	case "virus_detected_captcha_required":
		return &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "pixeldrain: Pixeldrain marked the file as malware and lets it through only after a captcha on its page"}
	case "hotlink_detected":
		return &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "pixeldrain: Pixeldrain took the download for hotlinking, which it allows only when the uploader or the downloader pays"}
	case "ip_download_limited_captcha_required", "transfer_limit_exceeded", "download_limit_exceeded":
		return &hoster.WaitError{Wait: limitWait, Why: "Pixeldrain's free daily limit for this address is used up", Reason: core.ReasonLimit}
	case "max_concurrent_downloads":
		return &hoster.WaitError{Wait: slotWait, Why: "Pixeldrain allows this address no more downloads at once", Reason: core.ReasonLimit}
	}
	return err
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock checks the file and asks for its first byte, so that a limit or a
// refusal is told on the task rather than as a failed transfer, and returns
// the address the file downloads from. A free download gets one connection,
// as Pixeldrain counts a free address's connections.
func (c *Client) Unlock(ctx context.Context, raw, _ string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok || l.list {
		return hoster.Link{}, fmt.Errorf("pixeldrain: %s is not a link to a file", raw)
	}
	key := c.key()
	headers := auth(key)
	var f file
	if err := c.get(ctx, "/file/"+l.id+"/info", headers, &f); err != nil {
		return hoster.Link{}, refusal(err)
	}
	if f.AbuseType != "" {
		return hoster.Link{}, refusal(&apiError{Value: "unavailable_for_legal_reasons"})
	}
	if f.Availability != "" {
		return hoster.Link{}, refusal(&apiError{Value: f.Availability})
	}
	target := "/file/" + l.id + "?download"
	probe := map[string]string{"Range": "bytes=0-0"}
	for k, v := range headers {
		probe[k] = v
	}
	if err := c.get(ctx, target, probe, nil); err != nil {
		return hoster.Link{}, refusal(err)
	}
	conns := 1
	if key != "" && c.paidPlan(ctx, key) {
		conns = 0
	}
	return hoster.Link{URL: c.api + target, Headers: headers, Conns: conns, Name: f.Name, Size: f.Size}, nil
}
