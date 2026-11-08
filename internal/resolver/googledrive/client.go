package googledrive

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// Sites are the hosts Drive answers on: its pages, its download server, the
// files API its pages call and the export of Google documents.
type Sites struct {
	Drive, Content, API, Docs string
}

// DefaultSites are Drive's own hosts.
var DefaultSites = Sites{
	Drive:   "https://drive.google.com",
	Content: "https://drive.usercontent.google.com",
	API:     "https://clients6.google.com",
	Docs:    "https://docs.google.com",
}

// maxConns is the most connections one download opens, as JDownloader's
// plugin allows after a user's report of Drive cutting more off.
const maxConns = 6

// maxPage caps how much of a folder page is read. A live one is about
// 470 KB.
const maxPage = 4 << 20

// The waits Drive asks for without a figure. A used up download quota lifts
// after hours, by Drive's own word within a day.
const (
	quotaWait = time.Hour
	busyWait  = 10 * time.Minute
)

// errQuota is the wait for a file whose download quota is used up, so
// Inspect can tell the file is there.
var errQuota = &hoster.WaitError{Wait: quotaWait, Why: "Google Drive has used up the file's download quota for now", Reason: core.ReasonLimit}

var (
	formPattern     = regexp.MustCompile(`(?s)<form id="download-form" action="([^"]+)"[^>]*>(.*?)</form>`)
	hiddenPattern   = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)"`)
	infectedPattern = regexp.MustCompile(`>\s*Sorry, this file is infected with a virus`)
	// The live titles have a no-break space inside the product's name, which
	// \s does not match.
	quotaPattern = regexp.MustCompile(`Too many users have viewed or downloaded this file recently|<title>Google[\s\x{a0}]+Drive[\s\x{a0}]+\S[\s\x{a0}]+(Quota|Cuota|Kuota|La quota|Quote)`)
	titlePattern = regexp.MustCompile(`<title>([^<]*?)[\s\x{a0}]+\S[\s\x{a0}]+Google[\s\x{a0}]+Drive\s*</title>`)
	// The folder page carries the files API's key as the first of a pair of
	// keys, as JDownloader's crawler finds it.
	keyPattern = regexp.MustCompile(`"(AIza[0-9A-Za-z_-]{35})"\s*,\s*"AIza[0-9A-Za-z_-]{35}"\s*,\s*null`)
)

// Client asks Drive for files, folders and documents.
type Client struct {
	hc    *http.Client
	sites Sites
}

// NewClient talks to sites, DefaultSites outside tests, through hc.
func NewClient(sites Sites, hc *http.Client) *Client {
	return &Client{hc: hc, sites: sites}
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock returns the download link of a file or the export of a document.
func (c *Client) Unlock(ctx context.Context, raw, _ string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok || l.kind == kindFolder {
		return hoster.Link{}, fmt.Errorf("googledrive: %s is not a link to a file", raw)
	}
	return c.peek(ctx, l)
}

// peek finds the address that hands out the file and what it is called.
func (c *Client) peek(ctx context.Context, l link) (hoster.Link, error) {
	if l.kind != kindFile {
		u := c.sites.Docs + "/" + l.kind + "/d/" + l.id + "/export?format=" + exports[l.kind]
		a, err := hoster.Peek(ctx, c.hc, u, nil)
		if err != nil {
			return hoster.Link{}, err
		}
		if !a.File() {
			return hoster.Link{}, refusal(a, "document")
		}
		return hoster.Link{URL: u, Name: a.Name, Size: a.Size, Conns: 1}, nil
	}
	q := url.Values{"id": {l.id}, "export": {"download"}}
	if l.key != "" {
		q.Set("resourcekey", l.key)
	}
	u := c.sites.Content + "/download?" + q.Encode()
	a, err := hoster.Peek(ctx, c.hc, u, nil)
	if err != nil {
		return hoster.Link{}, err
	}
	// A file too large to scan comes with a warning whose form leads on to
	// the file.
	if confirm, ok := confirmLink(a); ok {
		u = confirm
		if a, err = hoster.Peek(ctx, c.hc, u, nil); err != nil {
			return hoster.Link{}, err
		}
	}
	if !a.File() {
		return hoster.Link{}, refusal(a, "file")
	}
	return hoster.Link{URL: u, Name: a.Name, Size: a.Size, Conns: maxConns}, nil
}

// confirmLink is where the virus scan warning's form leads.
func confirmLink(a *hoster.Answer) (string, bool) {
	m := formPattern.FindSubmatch(a.Page)
	if m == nil {
		return "", false
	}
	action, err := a.URL.Parse(html.UnescapeString(string(m[1])))
	if err != nil {
		return "", false
	}
	q := url.Values{}
	for _, in := range hiddenPattern.FindAllSubmatch(m[2], -1) {
		q.Set(html.UnescapeString(string(in[1])), html.UnescapeString(string(in[2])))
	}
	action.RawQuery = q.Encode()
	return action.String(), true
}

// signIn reports whether Drive sent the request to its sign-in page, which
// it does for an item shared only with some Google accounts.
func signIn(u *url.URL) bool {
	return u.Hostname() == "accounts.google.com" || strings.Contains(u.Path, "ServiceLogin") || strings.Contains(u.Path, "signin")
}

// refusal says why Drive answered with something else than the file.
func refusal(a *hoster.Answer, what string) error {
	switch {
	case a.Status == http.StatusNotFound:
		return hoster.Gone("googledrive")
	case a.Status == http.StatusForbidden || signIn(a.URL):
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "googledrive: the owner shares the " + what + " only with some Google accounts"}
	case a.Status == http.StatusTooManyRequests:
		return &hoster.WaitError{Wait: busyWait, Why: "Google Drive limits requests from this address", Reason: core.ReasonLimit}
	case quotaPattern.Match(a.Page):
		return errQuota
	case infectedPattern.Match(a.Page):
		return &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "googledrive: Google Drive found a virus in the file and hands it out to its owner only"}
	}
	return fmt.Errorf("googledrive: Drive answered %d with a page instead of the %s", a.Status, what)
}

// folderPage reads a folder's name and the key the files API takes off the
// folder's page.
func (c *Client) folderPage(ctx context.Context, l link) (name, key string, err error) {
	u := c.sites.Drive + "/drive/folders/" + l.id
	if l.key != "" {
		u += "?resourcekey=" + url.QueryEscape(l.key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", "", hoster.Gone("googledrive")
	case resp.StatusCode == http.StatusForbidden || signIn(resp.Request.URL):
		return "", "", &hoster.Refusal{Reason: core.ReasonAuth, Msg: "googledrive: the owner shares the folder only with some Google accounts"}
	case resp.StatusCode != http.StatusOK:
		return "", "", fmt.Errorf("googledrive: the folder page answered %d", resp.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return "", "", err
	}
	m := keyPattern.FindSubmatch(page)
	if m == nil {
		return "", "", fmt.Errorf("googledrive: the folder page carries no key for Drive's files API")
	}
	if t := titlePattern.FindSubmatch(page); t != nil {
		name = html.UnescapeString(string(t[1]))
	}
	if name == "" {
		name = l.id
	}
	return name, string(m[1]), nil
}

// item is one entry of a folder as the files API lists it. The API writes
// the size as a string.
type item struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Mime     string `json:"mimeType"`
	Size     string `json:"fileSize"`
	Key      string `json:"resourceKey"`
	Shortcut struct {
		Target string `json:"targetId"`
		Mime   string `json:"targetMimeType"`
	} `json:"shortcutDetails"`
}

func (it item) size() int64 {
	n, _ := strconv.ParseInt(it.Size, 10, 64)
	return n
}

// maxPages caps the answers read for one folder, fifty entries each.
const maxPages = 100

// children lists a folder's entries over every page of the files API.
func (c *Client) children(ctx context.Context, key string, folder link) ([]item, error) {
	q := url.Values{
		"openDrive": {"false"}, "reason": {"102"}, "syncType": {"0"}, "errorRecovery": {"false"},
		"q":             {"trashed = false and '" + folder.id + "' in parents"},
		"fields":        {"kind,nextPageToken,incompleteSearch,items(kind,mimeType,id,title,fileSize,resourceKey,shortcutDetails(targetId,targetMimeType))"},
		"appDataFilter": {"NO_APP_DATA"}, "spaces": {"drive"}, "maxResults": {"50"},
		"orderBy": {"folder,title_natural asc"}, "retryCount": {"0"}, "key": {key}, "supportsTeamDrives": {"true"},
	}
	var out []item
	for range maxPages {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sites.API+"/drive/v2beta/files?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		// The API answers the key only for requests that come from Drive's
		// own pages.
		req.Header.Set("Origin", "https://drive.google.com")
		req.Header.Set("Referer", "https://drive.google.com/")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("X-Javascript-User-Agent", "google-api-javascript-client/1.1.0")
		if folder.key != "" {
			req.Header.Set("X-Goog-Drive-Resource-Keys", folder.id+"/"+folder.key)
		}
		var page struct {
			Items []item `json:"items"`
			Next  string `json:"nextPageToken"`
		}
		if err := c.files(req, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if page.Next == "" || len(page.Items) == 0 {
			break
		}
		q.Set("pageToken", page.Next)
	}
	return out, nil
}

// files sends one request to the files API and decodes its answer.
func (c *Client) files(req *http.Request, out any) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return hoster.Gone("googledrive")
	default:
		return fmt.Errorf("googledrive: Drive's files API answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPage)).Decode(out); err != nil {
		return fmt.Errorf("googledrive: Drive's files API answered in a shape it does not use: %w", err)
	}
	return nil
}
