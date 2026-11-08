package onedrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// maxHops caps the redirects a share link takes. A live one takes two or
// three.
const maxHops = 12

// guestLife is how long an opened share is used again, for the files of one
// folder and their link checks. A guest session lasts longer.
const guestLife = 10 * time.Minute

// throttleWait is the pause when OneDrive throttles without saying for how
// long.
const throttleWait = time.Minute

// maxAnswer caps how much of an answer of the REST API is read.
const maxAnswer = 16 << 20

var (
	errLogin    = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "onedrive: the owner shares it only with some Microsoft accounts"}
	errPassword = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "onedrive: the share is locked with a password, which only OneDrive's website takes"}

	passwordPattern = regexp.MustCompile(`(?:name|id)="txtPassword"`)
)

// Client opens OneDrive and SharePoint shares as a guest.
type Client struct {
	hc *http.Client
	// site stands in for the host of every share link, so tests can point
	// the client at a fake. Empty outside tests.
	site string

	mu     sync.Mutex
	guests map[string]*guest
}

// NewClient talks through hc to each share link's own host, or to site
// instead when it is set.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/"), guests: map[string]*guest{}}
}

// guest is an opened share: the session the site opened for it and where the
// shared item sits on the site.
type guest struct {
	hc *http.Client
	// web and path say where a shared folder sits: the site, such as
	// https://onedrive.live.com/personal/17e16246b3df8f06, and its path there.
	web, path string
	// file is where a shared file is downloaded from, with its name and size.
	file string
	name string
	size int64
	at   time.Time
}

// signIn reports whether a redirect goes to a sign-in page, which a share
// for some Microsoft accounts only and an expired guest session lead to.
func signIn(u *url.URL) bool {
	return strings.HasPrefix(strings.ToLower(u.Hostname()), "login.") || strings.Contains(u.Path, "/_forms/") ||
		strings.Contains(u.Path, "/_layouts/15/Authenticate.aspx")
}

// redeem opens a share, or hands back the share opened a moment ago.
func (c *Client) redeem(ctx context.Context, l link) (*guest, error) {
	c.mu.Lock()
	g := c.guests[l.share]
	c.mu.Unlock()
	if g != nil && time.Since(g.at) < guestLife {
		return g, nil
	}
	g, err := c.open(ctx, l)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, old := range c.guests {
		if time.Since(old.at) >= guestLife {
			delete(c.guests, k)
		}
	}
	c.guests[l.share] = g
	return g, nil
}

func (c *Client) forget(l link) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.guests, l.share)
}

// open follows a share link hop by hop, as a browser does, until a redirect
// says where the item sits. The site sets the guest session's cookie on the
// way.
func (c *Client) open(ctx context.Context, l link) (*guest, error) {
	sess := hoster.Session(c.hc)
	sess.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if signIn(req.URL) || len(via) >= maxHops {
			return http.ErrUseLastResponse
		}
		return nil
	}
	walker := *sess
	walker.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	u, err := url.Parse(l.share)
	if err != nil {
		return nil, err
	}
	// download=1 has a file share lead to the file rather than to its viewer.
	if !l.folder {
		q := u.Query()
		q.Set("download", "1")
		u.RawQuery = q.Encode()
	}
	if c.site != "" {
		s, err := url.Parse(c.site)
		if err != nil {
			return nil, err
		}
		u.Scheme, u.Host = s.Scheme, s.Host
	}
	for range maxHops {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := walker.Do(req)
		if err != nil {
			return nil, err
		}
		isPage := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html")
		var page []byte
		if isPage {
			page, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 3 {
			switch {
			case resp.StatusCode == http.StatusOK && passwordPattern.Match(page):
				return nil, errPassword
			case resp.StatusCode/100 == 2 && !isPage && !l.folder:
				// An older link may hand out the file without a redirect.
				return fileAt(ctx, sess, u)
			}
			return nil, refusal(resp, "share")
		}
		next, err := resp.Location()
		if err != nil {
			return nil, err
		}
		switch {
		case signIn(next):
			return nil, errLogin
		case l.folder && folderPath(next) != "":
			p := folderPath(next)
			return &guest{hc: sess, web: webOf(next, p), path: p, at: time.Now()}, nil
		case !l.folder && itemPath(next) != "":
			return fileAt(ctx, sess, next)
		}
		u = next
	}
	return nil, fmt.Errorf("onedrive: the share link took more than %d redirects", maxHops)
}

// fileAt is a shared file at u, named and sized from its first byte.
func fileAt(ctx context.Context, sess *http.Client, u *url.URL) (*guest, error) {
	a, err := hoster.Peek(ctx, sess, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if !a.File() {
		return nil, answerRefusal(a)
	}
	return &guest{hc: sess, file: a.URL.String(), name: a.Name, size: a.Size, at: time.Now()}, nil
}

// folderPath is the path of the shared folder a redirect names: in spopath
// on a personal OneDrive, in the id of SharePoint's folder view on a
// business site.
func folderPath(u *url.URL) string {
	q := u.Query()
	if p := q.Get("spopath"); p != "" {
		return p
	}
	if strings.HasSuffix(u.Path, "/_layouts/15/onedrive.aspx") && q.Get("parent") == "" {
		return q.Get("id")
	}
	return ""
}

// itemPath is a redirect's path when it leads to an item on a personal or a
// team site rather than to one of the site's own pages.
func itemPath(u *url.URL) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || (parts[0] != "personal" && parts[0] != "sites" && parts[0] != "teams") || strings.HasPrefix(parts[2], "_") {
		return ""
	}
	return u.Path
}

// webOf is the site an item path sits on, its first two segments on the
// host at answers on.
func webOf(at *url.URL, item string) string {
	parts := strings.SplitN(strings.Trim(item, "/"), "/", 3)
	return at.Scheme + "://" + at.Host + "/" + parts[0] + "/" + parts[1]
}

// refusal says why a share or the REST API answered with something else
// than what was asked for.
func refusal(resp *http.Response, what string) error {
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return hoster.Gone("onedrive")
	case http.StatusUnauthorized, http.StatusForbidden:
		return errLogin
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		wait := throttleWait
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 {
			wait = time.Duration(n) * time.Second
		}
		return &hoster.WaitError{Wait: wait, Why: "OneDrive is throttling requests from this address", Reason: core.ReasonLimit}
	}
	if resp.StatusCode/100 == 3 {
		return errLogin
	}
	return fmt.Errorf("onedrive: the %s answered %d", what, resp.StatusCode)
}

func answerRefusal(a *hoster.Answer) error {
	return refusal(&http.Response{StatusCode: a.Status, Header: a.Header}, "file")
}

// cookie is the Cookie header of the guest session for u, which the engine
// sends along with every request for the file.
func (g *guest) cookie(u string) map[string]string {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil
	}
	var pairs []string
	for _, ck := range g.hc.Jar.Cookies(parsed) {
		pairs = append(pairs, ck.Name+"="+ck.Value)
	}
	if len(pairs) == 0 {
		return nil
	}
	return map[string]string{"Cookie": strings.Join(pairs, "; ")}
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock returns where a shared file, or a file inside a shared folder, is
// downloaded from, with the guest session's cookie.
func (c *Client) Unlock(ctx context.Context, raw, _ string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok || (l.folder && l.rel == "") {
		return hoster.Link{}, fmt.Errorf("onedrive: %s is not a link to a file", raw)
	}
	return c.unlock(ctx, l)
}

// unlock opens the share again once when the session it was opened with a
// moment ago has run out.
func (c *Client) unlock(ctx context.Context, l link) (hoster.Link, error) {
	got, err := c.unlockOnce(ctx, l)
	if err == errLogin {
		c.forget(l)
		got, err = c.unlockOnce(ctx, l)
	}
	return got, err
}

func (c *Client) unlockOnce(ctx context.Context, l link) (hoster.Link, error) {
	g, err := c.redeem(ctx, l)
	if err != nil {
		return hoster.Link{}, err
	}
	if !l.folder {
		return hoster.Link{URL: g.file, Headers: g.cookie(g.file), Name: g.name, Size: g.size}, nil
	}
	u := g.web + "/_layouts/15/download.aspx?SourceUrl=" + url.QueryEscape(g.path+"/"+l.rel)
	a, err := hoster.Peek(ctx, g.hc, u, nil)
	if err != nil {
		return hoster.Link{}, err
	}
	if !a.File() {
		return hoster.Link{}, answerRefusal(a)
	}
	return hoster.Link{URL: u, Headers: g.cookie(u), Name: a.Name, Size: a.Size}, nil
}

// folderInfo is a folder as SharePoint's REST API lists it.
type folderInfo struct {
	Files   []fileEntry `json:"Files"`
	Folders []struct {
		Name string `json:"Name"`
		Path string `json:"ServerRelativeUrl"`
	} `json:"Folders"`
}

// fileEntry is one file of a folder. The API writes its length as a string.
type fileEntry struct {
	Name   string `json:"Name"`
	Length string `json:"Length"`
	Path   string `json:"ServerRelativeUrl"`
}

func (f fileEntry) size() int64 {
	n, _ := strconv.ParseInt(f.Length, 10, 64)
	return n
}

// folder asks the REST API for a folder's files and subfolders.
func (c *Client) folder(ctx context.Context, g *guest, dir string) (folderInfo, error) {
	u := g.web + "/_api/web/GetFolderByServerRelativeUrl('" + odata(dir) + "')?$expand=Files,Folders"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return folderInfo{}, err
	}
	req.Header.Set("Accept", "application/json;odata=nometadata")
	resp, err := g.hc.Do(req)
	if err != nil {
		return folderInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return folderInfo{}, refusal(resp, "folder listing")
	}
	var f folderInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&f); err != nil {
		return folderInfo{}, fmt.Errorf("onedrive: the folder listing answered in a shape it does not use: %w", err)
	}
	return f, nil
}

// odata writes a path into the quoted string of a REST call. The quote is
// doubled to stay inside the string, and a plus is escaped, since SharePoint
// would read it as a space.
func odata(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(strings.ReplaceAll(s, "'", "''")), "+", "%2B")
	}
	return strings.Join(parts, "/")
}
