package dropbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where Dropbox's shared links and website answer.
const DefaultSite = "https://www.dropbox.com"

// maxConns is the most connections one download opens. JDownloader's plugin
// opens one, and Dropbox pauses a link that draws too much traffic.
const maxConns = 1

// maxAnswer caps how much of a page or a listing is read. A live folder page
// is about 230 KB.
const maxAnswer = 4 << 20

// The waits Dropbox asks for without a figure, as JDownloader's plugin sits
// them out.
const (
	disabledWait = 10 * time.Minute
	trafficWait  = time.Hour
)

var (
	gonePattern     = regexp.MustCompile(`<title>Dropbox - Invalid Link|/images/sharing/error_|sharing/error_shmodel|class="not-found">`)
	disabledPattern = regexp.MustCompile(`>\s*Link Temporarily Disabled`)
	privatePattern  = regexp.MustCompile(`/images/precaution|dws_page_name=files_shared_content_link_login_page`)
)

// errPassword is a link behind Dropbox's password prompt that came without a
// password.
var errPassword = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "dropbox: the link is protected by a password, which goes in the link password of the add-links form"}

// Client follows Dropbox's shared links to their files and lists shared
// folders.
type Client struct {
	hc   *http.Client
	site string
	// settle is how long Dropbox takes to accept the session its password
	// prompt has just opened, as JDownloader's plugin found.
	settle time.Duration
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/"), settle: 5 * time.Second}
}

// at is where the client asks for l: its page, or with dl the file itself or
// the zip of a folder.
func (c *Client) at(l link, dl bool) string {
	q := url.Values{}
	for k, v := range l.query {
		q[k] = v
	}
	if dl {
		q.Set("dl", "1")
	}
	s := c.site + l.path
	if len(q) > 0 {
		s += "?" + q.Encode()
	}
	return s
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock returns where a shared file's dl=1 link leads, or the zip of a
// shared folder.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok {
		return hoster.Link{}, fmt.Errorf("dropbox: %s is not a shared link", raw)
	}
	return c.unlock(ctx, l, password)
}

func (c *Client) unlock(ctx context.Context, l link, password string) (hoster.Link, error) {
	hc := hoster.Session(c.hc)
	u := c.at(l, true)
	a, err := hoster.Peek(ctx, hc, u, nil)
	if err == nil && prompt(a.URL) {
		if err = c.signIn(ctx, hc, l, a.URL, password); err == nil {
			a, err = hoster.Peek(ctx, hc, u, nil)
		}
	}
	if err != nil {
		return hoster.Link{}, err
	}
	if !a.File() {
		return hoster.Link{}, refusal(a.Status, a.Page)
	}
	// The address it leads to is the content server's own and carries its
	// access, so the engine needs no cookie of the session.
	return hoster.Link{URL: a.URL.String(), Name: a.Name, Size: a.Size, Conns: maxConns}, nil
}

// prompt reports whether Dropbox sent a request to its password prompt.
func prompt(u *url.URL) bool { return strings.Contains(u.Path, "/sm/password") }

// refusal says why Dropbox answered with a page instead of the file.
func refusal(status int, page []byte) error {
	switch {
	case status == http.StatusNotFound || status == http.StatusGone || status == 460 || gonePattern.Match(page):
		return hoster.Gone("dropbox")
	case status == http.StatusTooManyRequests || disabledPattern.Match(page):
		return &hoster.WaitError{Wait: disabledWait, Why: "Dropbox has paused the link for drawing too much traffic", Reason: core.ReasonLimit}
	case status == 509:
		return &hoster.WaitError{Wait: trafficWait, Why: "Dropbox has paused the link for its owner's traffic", Reason: core.ReasonLimit}
	case privatePattern.Match(page):
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "dropbox: the owner made the link private"}
	}
	return fmt.Errorf("dropbox: the link answered %d with a page instead of the file", status)
}

// signIn enters password at the prompt Dropbox sent the request to, which
// leaves the session's cookies opening the link.
func (c *Client) signIn(ctx context.Context, hc *http.Client, l link, at *url.URL, password string) error {
	if password == "" {
		return errPassword
	}
	form := url.Values{
		"is_xhr": {"true"}, "t": {token(hc, c.site)}, "content_id": {at.Query().Get("content_id")},
		"password": {password}, "url": {l.path},
	}
	var answer struct {
		Status string `json:"status"`
	}
	status, err := c.post(ctx, hc, "/sm/auth", form, &answer)
	if err != nil {
		return err
	}
	if status != http.StatusOK || answer.Status == "error" {
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "dropbox: the link password does not open the link"}
	}
	return hoster.Sleep(ctx, c.settle)
}

// token is the t cookie the website sets, which its own posts carry back.
func token(hc *http.Client, site string) string {
	u, err := url.Parse(site)
	if err != nil {
		return ""
	}
	for _, ck := range hc.Jar.Cookies(u) {
		if ck.Name == "t" {
			return ck.Value
		}
	}
	return ""
}

// post sends form to the website as its own script does and decodes the
// answer into out, which a status other than 200 leaves alone.
func (c *Client) post(ctx context.Context, hc *http.Client, path string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.site+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", c.site)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(out); err != nil {
		return 0, fmt.Errorf("dropbox: the website answered in a shape it does not use: %w", err)
	}
	return resp.StatusCode, nil
}

// openFolder reads a folder's page, through the password prompt where the
// link has one, and returns the session the listing goes on in.
func (c *Client) openFolder(ctx context.Context, l link, password string) (*http.Client, error) {
	hc := hoster.Session(c.hc)
	status, at, page, err := c.page(ctx, hc, l)
	if err == nil && prompt(at) {
		if err = c.signIn(ctx, hc, l, at, password); err == nil {
			status, _, page, err = c.page(ctx, hc, l)
		}
	}
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || gonePattern.Match(page) {
		return nil, refusal(status, page)
	}
	return hc, nil
}

func (c *Client) page(ctx context.Context, hc *http.Client, l link) (int, *url.URL, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.at(l, false), nil)
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	return resp.StatusCode, resp.Request.URL, page, err
}

// entry is one file or subfolder of a folder listing.
type entry struct {
	Name  string `json:"filename"`
	Bytes int64  `json:"bytes"`
	Dir   bool   `json:"is_dir"`
	Href  string `json:"href"`
}

// maxPages caps the answers read for one folder, thirty entries each.
const maxPages = 200

// entries lists a folder's files and subfolders over every page of the
// listing, with the folder's name.
func (c *Client) entries(ctx context.Context, hc *http.Client, l link) (string, []entry, error) {
	form := url.Values{
		"is_xhr": {"true"}, "link_key": {l.key}, "link_type": {l.kind}, "secure_hash": {l.hash},
		"sub_path": {l.sub}, "t": {token(hc, c.site)},
	}
	if k := l.query.Get("rlkey"); k != "" {
		form.Set("rlkey", k)
	}
	var name string
	var out []entry
	for range maxPages {
		var page struct {
			Entries []entry `json:"entries"`
			More    bool    `json:"has_more_entries"`
			Voucher string  `json:"next_request_voucher"`
			Folder  struct {
				Name string `json:"filename"`
			} `json:"folder"`
		}
		status, err := c.post(ctx, hc, "/list_shared_link_folder_entries", form, &page)
		switch {
		case err != nil:
			return "", nil, err
		case status == http.StatusNotFound:
			return "", nil, hoster.Gone("dropbox")
		case status == http.StatusForbidden:
			return "", nil, &hoster.Refusal{Reason: core.ReasonAuth, Msg: "dropbox: the owner shares the folder only with some Dropbox accounts"}
		case status != http.StatusOK:
			return "", nil, fmt.Errorf("dropbox: the folder listing answered %d", status)
		}
		if name == "" {
			name = page.Folder.Name
		}
		out = append(out, page.Entries...)
		if !page.More || page.Voucher == "" {
			break
		}
		form.Set("voucher", page.Voucher)
	}
	if name == "" {
		name = l.key
	}
	return name, out, nil
}
