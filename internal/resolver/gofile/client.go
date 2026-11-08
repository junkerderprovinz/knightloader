package gofile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DefaultAPI and DefaultSite are where Gofile's API and website answer.
const (
	DefaultAPI  = "https://api.gofile.io"
	DefaultSite = "https://gofile.io"
)

// maxAnswer caps how much of one answer is read. A page of a hundred
// children is a few tens of kilobytes.
const maxAnswer = 8 << 20

// Gofile answers a burst of requests with 429. A few short pauses usually
// get through it, as JDownloader's plugin finds. After that the task waits
// longer, with the wait shown.
const (
	rateRetries = 3
	ratePause   = 5 * time.Second
	rateWait    = time.Minute
)

// Client asks Gofile about the contents behind its links, the way its website
// does, since the documented API lists contents for premium accounts only.
type Client struct {
	hc        *http.Client
	api, site string
	ua        string
	pageSize  int

	// Token returns the account token of the stored Gofile login, "" for
	// none. Without one the client makes a guest account, as the website
	// does for a visitor.
	Token func() string

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu    sync.Mutex
	guest string

	signMu sync.Mutex
	signer *signer
}

// NewClient talks to api and site, DefaultAPI and DefaultSite outside tests,
// through hc.
func NewClient(api, site string, hc *http.Client) *Client {
	return &Client{
		hc: hc, api: strings.TrimSuffix(api, "/"), site: strings.TrimSuffix(site, "/"),
		ua: httpx.UserAgent(), pageSize: 100, now: time.Now, sleep: sleep,
	}
}

// content is one file or folder as Gofile's contents endpoint describes it.
type content struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Name     string             `json:"name"`
	Size     int64              `json:"size"`
	Link     string             `json:"link"`
	Children map[string]content `json:"children"`
	// CanAccess is false behind a gate, which Password, PasswordStatus,
	// Public and Expire say more about. Absent means open.
	CanAccess      *bool  `json:"canAccess"`
	Password       bool   `json:"password"`
	PasswordStatus string `json:"passwordStatus"`
	Public         *bool  `json:"public"`
	Expire         int64  `json:"expire"`
}

type answer struct {
	Status   string          `json:"status"`
	Data     json.RawMessage `json:"data"`
	Metadata struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"metadata"`
}

// The gates of a content, as refusals.
var (
	errPassword        = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "gofile: the folder is protected by a password, and the download has no download password"}
	errPasswordRefused = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "gofile: the download password was not accepted"}
	errPrivate         = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "gofile: the owner keeps this content private"}
	errPremium         = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "gofile: the owner shares this content with premium accounts only, so it needs a Gofile premium login"}
	errTokenRefused    = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "gofile: the account token of the stored Gofile login was not accepted"}
)

// errGuestRefused is Gofile refusing a guest token, which a fresh one
// replaces.
var errGuestRefused = errors.New("gofile: the guest token was refused")

// token is the account token requests go out with: the stored login's, or a
// guest account's, made on first use.
func (c *Client) token(ctx context.Context) (string, bool, error) {
	if c.Token != nil {
		if t := c.Token(); t != "" {
			return t, true, nil
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guest != "" {
		return c.guest, false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/accounts", nil)
	if err != nil {
		return "", false, err
	}
	var data struct {
		Token string `json:"token"`
	}
	if _, err := c.do(req, &data); err != nil {
		return "", false, fmt.Errorf("gofile: no guest account: %w", err)
	}
	if data.Token == "" {
		return "", false, errors.New("gofile: the guest account came without a token")
	}
	c.guest = data.Token
	return c.guest, false, nil
}

func (c *Client) dropGuest(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guest == token {
		c.guest = ""
	}
}

// do sends req as the website would and decodes the data of an "ok" answer
// into out. It reports whether more pages follow.
func (c *Client) do(req *http.Request, out any) (bool, error) {
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Origin", c.site)
	req.Header.Set("Referer", c.site+"/")
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return false, err
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil {
		return false, fmt.Errorf("gofile: the API answered %d in a shape it does not use", resp.StatusCode)
	}
	switch a.Status {
	case "ok":
		if err := json.Unmarshal(a.Data, out); err != nil {
			return false, fmt.Errorf("gofile: the API answered in a shape it does not use: %w", err)
		}
		return a.Metadata.HasNextPage, nil
	case "error-notFound":
		return false, hoster.Gone("gofile")
	case "error-notPremium":
		return false, errPremium
	case "error-token":
		return false, errGuestRefused
	case "error-rateLimit":
		return false, errRateLimited
	}
	return false, fmt.Errorf("gofile: the API answered %d %s", resp.StatusCode, a.Status)
}

var errRateLimited = errors.New("gofile: rate limited")

// contents reads one page of the content id, with password as the folder's
// download password.
func (c *Client) contents(ctx context.Context, id, password string, page int) (content, bool, error) {
	for tries := 0; ; tries++ {
		token, stored, err := c.token(ctx)
		if err != nil {
			return content{}, false, err
		}
		wt, err := c.websiteToken(ctx, token)
		if err != nil {
			return content{}, false, err
		}
		q := url.Values{
			"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(c.pageSize)},
			"sortField": {"name"}, "sortDirection": {"1"},
		}
		if password != "" {
			sum := sha256.Sum256([]byte(password))
			q.Set("password", hex.EncodeToString(sum[:]))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+"/contents/"+url.PathEscape(id)+"?"+q.Encode(), nil)
		if err != nil {
			return content{}, false, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Website-Token", wt)
		req.Header.Set("X-BL", language)
		var ct content
		more, err := c.do(req, &ct)
		switch {
		case errors.Is(err, errGuestRefused) && stored:
			return content{}, false, errTokenRefused
		case errors.Is(err, errGuestRefused) && tries == 0:
			c.dropGuest(token)
			continue
		case errors.Is(err, errRateLimited) && tries < rateRetries:
			if err := c.sleep(ctx, ratePause); err != nil {
				return content{}, false, err
			}
			continue
		case errors.Is(err, errRateLimited):
			return content{}, false, &hoster.WaitError{Wait: rateWait, Why: "Gofile is turning away requests from this address", Reason: core.ReasonLimit}
		case err != nil:
			return content{}, false, err
		}
		return ct, more, gate(ct, password)
	}
}

// gate is the refusal a content that may not be opened comes with.
func gate(ct content, password string) error {
	if ct.CanAccess == nil || *ct.CanAccess {
		return nil
	}
	switch {
	case ct.Password && (ct.PasswordStatus == "passwordWrong" || password != ""):
		return errPasswordRefused
	case ct.Password:
		return errPassword
	case ct.Expire > 0 && (ct.Public == nil || *ct.Public):
		return &hoster.Refusal{Reason: core.ReasonGone, Msg: "gofile: the link has expired"}
	}
	return errPrivate
}

// open reads every page of a content and puts the children together.
func (c *Client) open(ctx context.Context, id, password string) (content, error) {
	var whole content
	for page := 1; ; page++ {
		ct, more, err := c.contents(ctx, id, password, page)
		if err != nil {
			return content{}, err
		}
		if page == 1 {
			whole = ct
			whole.Children = map[string]content{}
		}
		for k, v := range ct.Children {
			whole.Children[k] = v
		}
		if !more || ct.Type != "folder" {
			return whole, nil
		}
	}
}

// maxConns is the most connections one Gofile download opens, as
// JDownloader's plugin finds for free and premium alike.
const maxConns = 3

var _ hoster.Unlocker = (*Client)(nil)

// Unlock asks for the file behind a link and returns its download link, which
// Gofile's download servers hand out to a request carrying the account token
// the link was asked for with.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	id, ok := parse(raw)
	if !ok {
		return hoster.Link{}, fmt.Errorf("gofile: %s is not a Gofile link", raw)
	}
	ct, _, err := c.contents(ctx, id, password, 1)
	if err != nil {
		return hoster.Link{}, err
	}
	if ct.Type != "file" || ct.Link == "" {
		return hoster.Link{}, errors.New("gofile: a folder link opens into its files when it is added, and cannot be downloaded as one")
	}
	token, _, err := c.token(ctx)
	if err != nil {
		return hoster.Link{}, err
	}
	return hoster.Link{
		URL:     ct.Link,
		Headers: map[string]string{"Cookie": "accountToken=" + token, "Referer": c.site + "/"},
		Conns:   maxConns,
		Name:    ct.Name,
		Size:    ct.Size,
	}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
