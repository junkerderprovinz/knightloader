package workupload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where Workupload's pages and API answer.
const DefaultSite = "https://workupload.com"

// maxAnswer caps how much of a page or an answer is read. A file page is
// about 15 KB.
const maxAnswer = 4 << 20

// maxRange caps the numbers a puzzle is searched through. The site asks for
// three numbers below 10000.
const maxRange = 1_000_000

// The waits JDownloader's plugin sits out: a robot check that keeps coming
// back, an API that turns the download down, and one that names no server.
const (
	robotWait       = 10 * time.Minute
	unavailableWait = 30 * time.Minute
	noServerWait    = 10 * time.Minute
)

// Client reads Workupload's pages and API, past its robot check.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// visitor is one visit with its cookies, which carry the answered robot
// check from one request to the next.
type visitor struct {
	c   *Client
	hc  *http.Client
	jar http.CookieJar
}

func (c *Client) visit() *visitor {
	jar, _ := cookiejar.New(nil)
	hc := *c.hc
	hc.Jar = jar
	return &visitor{c: c, hc: &hc, jar: jar}
}

var robotPattern = regexp.MustCompile(`<title>\s*workupload - Are you a human\?|class="fa fa-shield-check"`)

// errRobot is the robot check standing in the way even after it was answered.
var errRobot = &hoster.WaitError{Wait: robotWait, Why: "Workupload's robot check does not let KnightLoader through", Reason: core.ReasonUnavailable}

// get reads path, answering the robot check when it comes up instead.
func (v *visitor) get(ctx context.Context, path, referer string, xhr bool) (string, int, error) {
	return v.do(ctx, http.MethodGet, path, nil, referer, xhr)
}

func (v *visitor) do(ctx context.Context, method, path string, form url.Values, referer string, xhr bool) (string, int, error) {
	for try := 0; ; try++ {
		body, status, err := v.request(ctx, method, path, form, referer, xhr)
		switch {
		case err != nil:
			return "", 0, err
		case !robotPattern.MatchString(body):
			return body, status, nil
		case try > 0:
			return "", 0, errRobot
		}
		if err := v.pass(ctx); err != nil {
			return "", 0, err
		}
	}
}

func (v *visitor) request(ctx context.Context, method, path string, form url.Values, referer string, xhr bool) (string, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, v.c.site+path, body)
	if err != nil {
		return "", 0, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if xhr {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := v.hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	return string(b), resp.StatusCode, err
}

// puzzle is the robot check: the numbers below Range whose SHA-256, written
// after Puzzle, is one of Find.
type puzzle struct {
	Puzzle string   `json:"puzzle"`
	Range  int      `json:"range"`
	Find   []string `json:"find"`
}

// pass answers the robot check as the check's own page does.
func (v *visitor) pass(ctx context.Context) error {
	body, _, err := v.request(ctx, http.MethodGet, "/puzzle", nil, v.c.site+"/", true)
	if err != nil {
		return err
	}
	var a struct {
		Data puzzle `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return fmt.Errorf("workupload: the robot check's puzzle came in a shape it does not use")
	}
	answer, err := solve(a.Data)
	if err != nil {
		return err
	}
	_, _, err = v.request(ctx, http.MethodPost, "/captcha", url.Values{"captcha": {answer}}, v.c.site+"/", true)
	return err
}

// solve finds the puzzle's numbers and writes them as the check's page does,
// smallest first, each followed by a space.
func solve(p puzzle) (string, error) {
	if p.Range <= 0 || p.Range > maxRange || len(p.Find) == 0 {
		return "", fmt.Errorf("workupload: the robot check asks for a puzzle of a range it does not use (%d)", p.Range)
	}
	var b strings.Builder
	found := 0
	for i := 0; i < p.Range && found < len(p.Find); i++ {
		n := strconv.Itoa(i)
		sum := sha256.Sum256([]byte(p.Puzzle + n))
		if slices.Contains(p.Find, hex.EncodeToString(sum[:])) {
			b.WriteString(n + " ")
			found++
		}
	}
	if found < len(p.Find) {
		return "", errors.New("workupload: the robot check's puzzle has no answer")
	}
	return b.String(), nil
}

var (
	namePattern     = regexp.MustCompile(`<td>\s*(?:Dateiname|Filename|File name):\s*(?:&nbsp;|\x{a0})?\s*</td>\s*<td[^>]*>\s*([^<>]+?)\s*</td>`)
	sizePattern     = regexp.MustCompile(`<td>\s*(?:Dateigröße|Filesize|File size):\s*(?:&nbsp;|\x{a0})?\s*</td>\s*<td[^>]*>\s*(\d+)\s*\(Bytes?\)`)
	headingPattern  = regexp.MustCompile(`<h1[^>]*>\s*([^<>]+?)\s*\([\d.,]+\s*[KMGT]?B\)\s*</h1>`)
	gonePattern     = regexp.MustCompile(`<title>\s*(?:Datei nicht gefunden|File not found)|img/404\.jpg"|>\s*Whoops! 404|>\s*Datei gesperrt`)
	passwordPattern = regexp.MustCompile(`id="passwordprotected_file_password"`)
)

// filePage is what a file page says.
type filePage struct {
	name   string
	size   int64
	locked bool
}

func readFilePage(body string) filePage {
	var p filePage
	if m := namePattern.FindStringSubmatch(body); m != nil {
		p.name = html.UnescapeString(m[1])
	} else if m := headingPattern.FindStringSubmatch(body); m != nil {
		p.name = html.UnescapeString(m[1])
	}
	if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size, _ = strconv.ParseInt(m[1], 10, 64)
	}
	p.locked = passwordPattern.MatchString(body)
	return p
}

func (v *visitor) filePage(ctx context.Context, id string) (filePage, error) {
	body, status, err := v.get(ctx, "/file/"+id, "", false)
	switch {
	case err != nil:
		return filePage{}, err
	case status == http.StatusNotFound || status == http.StatusGone || gonePattern.MatchString(body):
		return filePage{}, hoster.Gone("workupload")
	case status != http.StatusOK:
		return filePage{}, fmt.Errorf("workupload: the file page answered %d", status)
	}
	return readFilePage(body), nil
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock reads the file's page, enters the download password a protected
// file asks for, opens the start page and asks the API it calls for the
// download server.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	id := fileID(raw)
	if id == "" {
		return hoster.Link{}, fmt.Errorf("workupload: %s is not a link to a file", raw)
	}
	v := c.visit()
	page := c.site + "/file/" + id
	p, err := v.filePage(ctx, id)
	if err != nil {
		return hoster.Link{}, err
	}
	if p.locked {
		if password == "" {
			return hoster.Link{}, &hoster.Refusal{Reason: core.ReasonAuth, Msg: "workupload: the file is protected by a password, which goes in as the download password"}
		}
		form := url.Values{
			"passwordprotected_file[password]": {password},
			"passwordprotected_file[submit]":   {""},
			"passwordprotected_file[key]":      {id},
		}
		body, _, err := v.do(ctx, http.MethodPost, "/file/"+id, form, page, false)
		if err != nil {
			return hoster.Link{}, err
		}
		if passwordPattern.MatchString(body) {
			return hoster.Link{}, &hoster.Refusal{Reason: core.ReasonAuth, Msg: "workupload: the download password was not accepted"}
		}
	}
	if _, _, err := v.get(ctx, "/start/"+id, page, false); err != nil {
		return hoster.Link{}, err
	}
	body, _, err := v.get(ctx, "/api/file/getDownloadServer/"+id, c.site+"/start/"+id, true)
	if err != nil {
		return hoster.Link{}, err
	}
	var a struct {
		Success bool `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	switch {
	case json.Unmarshal([]byte(body), &a) != nil:
		return hoster.Link{}, fmt.Errorf("workupload: the API answered in a shape it does not use")
	case !a.Success:
		return hoster.Link{}, &hoster.WaitError{Wait: unavailableWait, Why: "Workupload has the download unavailable for the moment", Reason: core.ReasonUnavailable}
	case !strings.HasPrefix(a.Data.URL, "http"):
		return hoster.Link{}, &hoster.WaitError{Wait: noServerWait, Why: "Workupload named no download server", Reason: core.ReasonUnavailable}
	}
	l := hoster.Link{URL: a.Data.URL, Conns: maxConns, Name: p.name, Size: p.size, Headers: map[string]string{"Referer": page}}
	if u, err := url.Parse(a.Data.URL); err == nil {
		var cookies []string
		for _, ck := range v.jar.Cookies(u) {
			cookies = append(cookies, ck.Name+"="+ck.Value)
		}
		if len(cookies) > 0 {
			l.Headers["Cookie"] = strings.Join(cookies, "; ")
		}
	}
	return l, nil
}
