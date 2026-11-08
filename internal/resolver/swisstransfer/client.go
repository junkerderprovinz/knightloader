package swisstransfer

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where SwissTransfer's pages and API answer.
const DefaultSite = "https://www.swisstransfer.com"

// maxAnswer caps how much of a page or an answer is read. A live transfer
// page is about 60 KB.
const maxAnswer = 4 << 20

var pagePattern = regexp.MustCompile(`(?s)<script data-page="app"[^>]*>(.*?)</script>`)

var (
	errPassword = &hoster.Refusal{Reason: core.ReasonAuth, Msg: "swisstransfer: the transfer is protected by a password, which goes in the link password of the add-links form"}
	errExpired  = &hoster.Refusal{Reason: core.ReasonGone, Msg: "swisstransfer: the transfer has expired or was deleted"}
)

// Client reads SwissTransfer's transfer pages and asks its API for the
// files' links.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// file is one file of a transfer. Its path carries the folders it was sent
// in.
type file struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// transfer is the part of a transfer page's data the client reads.
type transfer struct {
	Title   string `json:"title"`
	Expires int64  `json:"expires_at"`
	Files   []file `json:"files"`
}

// find is the file with id, or a transfer's only file for no id.
func (t transfer) find(id string) (file, bool) {
	if id == "" && len(t.Files) == 1 {
		return t.Files[0], true
	}
	for _, f := range t.Files {
		if id != "" && f.ID == id {
			return f, true
		}
	}
	return file{}, false
}

// page is a transfer page's data, which the site's script renders.
type page struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Props     struct {
		Transfer *transfer `json:"transfer"`
	} `json:"props"`
}

// transfer reads a transfer off its page, through the password where the
// transfer is locked, and returns the session the file links are asked for
// in.
func (c *Client) transfer(ctx context.Context, l link, password string) (transfer, *http.Client, error) {
	hc := hoster.Session(c.hc)
	p, err := c.read(ctx, hc, l, "", "")
	if err == nil && p.Props.Transfer == nil && password != "" {
		p, err = c.read(ctx, hc, l, password, p.Version)
	}
	switch {
	case err != nil:
		return transfer{}, nil, err
	case p.Props.Transfer == nil && password != "":
		return transfer{}, nil, &hoster.Refusal{Reason: core.ReasonAuth, Msg: "swisstransfer: the link password does not open the transfer"}
	case p.Props.Transfer == nil:
		return transfer{}, nil, errPassword
	case p.Props.Transfer.Expires > 0 && time.Unix(p.Props.Transfer.Expires, 0).Before(time.Now()):
		return transfer{}, nil, errExpired
	}
	return *p.Props.Transfer, hc, nil
}

// read asks for a transfer page and decodes its data. A password goes in a
// post the way the site's script sends it, with the version of the page it
// was typed into, and is answered with the data alone.
func (c *Client) read(ctx context.Context, hc *http.Client, l link, password, version string) (page, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if password != "" {
		method, body = http.MethodPost, strings.NewReader(url.Values{"password": {password}}.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.site+"/dl/"+l.id, body)
	if err != nil {
		return page{}, err
	}
	if password != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", version)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Accept", "text/html, application/xhtml+xml")
		req.Header.Set("X-XSRF-TOKEN", xsrf(hc, c.site))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return page{}, err
	}
	if m := pagePattern.FindSubmatch(raw); m != nil {
		raw = []byte(html.UnescapeString(string(m[1])))
	}
	var p page
	if json.Unmarshal(raw, &p) != nil {
		if resp.StatusCode == http.StatusNotFound {
			return page{}, errExpired
		}
		return page{}, fmt.Errorf("swisstransfer: the transfer page answered %d without its data", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound || strings.HasPrefix(p.Component, "errors/") {
		return page{}, errExpired
	}
	return p, nil
}

// xsrf is the token the site's cookie holds, which its script sends back in
// a header, decoded as the script decodes it.
func xsrf(hc *http.Client, site string) string {
	u, err := url.Parse(site)
	if err != nil {
		return ""
	}
	for _, ck := range hc.Jar.Cookies(u) {
		if strings.HasSuffix(ck.Name, "XSRF-TOKEN") {
			v, err := url.QueryUnescape(ck.Value)
			if err != nil {
				return ck.Value
			}
			return v
		}
	}
	return ""
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock returns the signed link of one file of a transfer, or of a
// transfer's only file.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok {
		return hoster.Link{}, fmt.Errorf("swisstransfer: %s is not a link to a transfer", raw)
	}
	t, hc, err := c.transfer(ctx, l, password)
	if err != nil {
		return hoster.Link{}, err
	}
	f, ok := t.find(l.file)
	switch {
	case !ok && l.file == "":
		return hoster.Link{}, fmt.Errorf("swisstransfer: a transfer of several files opens into its files when it is added, and cannot be downloaded as one")
	case !ok:
		return hoster.Link{}, hoster.Gone("swisstransfer")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/api/1/links/"+l.id+"/files/"+f.ID, nil)
	if err != nil {
		return hoster.Link{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return hoster.Link{}, err
	}
	defer resp.Body.Close()
	var answer struct {
		Result  string `json:"result"`
		Message string `json:"message"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&answer); err != nil {
		return hoster.Link{}, fmt.Errorf("swisstransfer: the API answered %d in a shape it does not use", resp.StatusCode)
	}
	switch {
	case answer.Message == "Download Number Exceeded":
		return hoster.Link{}, &hoster.Refusal{Reason: core.ReasonGone, Msg: "swisstransfer: the transfer has been downloaded as often as its sender allowed"}
	case resp.StatusCode == http.StatusNotFound:
		return hoster.Link{}, errExpired
	case answer.Result != "success" || answer.Data.URL == "":
		return hoster.Link{}, fmt.Errorf("swisstransfer: the API answered %d: %s", resp.StatusCode, answer.Message)
	}
	return hoster.Link{URL: answer.Data.URL, Name: path.Base(f.Path), Size: f.Size}, nil
}
