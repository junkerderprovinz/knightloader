package krakenfiles

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where Krakenfiles' pages and JSON answer.
const DefaultSite = "https://krakenfiles.com"

// maxAnswer caps how much of a page or an answer is read. A file page is
// about 45 KB.
const maxAnswer = 4 << 20

// maxCaptchaTries is how often one unlock asks for the Turnstile check again
// after the site turned an answer down.
const maxCaptchaTries = 3

// limitWait is how long a download waits once the address has used up the
// day's free transfer, which the page shows without saying when it renews.
// refusedWait is the pause after any other refusal of the form, which
// JDownloader's plugin takes for a passing one.
const (
	limitWait   = time.Hour
	refusedWait = 10 * time.Minute
)

// Client reads Krakenfiles' JSON and file pages and sends the download form.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// fileJSON is the embed player's description of a file.
type fileJSON struct {
	Title string `json:"title"`
	Size  string `json:"size"`
	Hash  string `json:"hash"`
}

// info describes the file id, nil for one Krakenfiles does not know, which it
// answers with an empty list.
func (c *Client) info(ctx context.Context, id string) (*fileJSON, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/json/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound || strings.TrimSpace(string(body)) == "[]" {
		return nil, nil
	}
	var f fileJSON
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("krakenfiles: the file's JSON answered %d in a shape it does not use", resp.StatusCode)
	}
	if !strings.EqualFold(f.Hash, id) {
		return nil, nil
	}
	return &f, nil
}

var (
	formPattern   = regexp.MustCompile(`(?is)<form\b([^>]*\bid="dl-form"[^>]*)>(.*?)</form>`)
	inputPattern  = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	attrPattern   = regexp.MustCompile(`(?s)([\w-]+)\s*=\s*"([^"]*)"`)
	hashPattern   = regexp.MustCompile(`data-file-hash="([^"]+)"`)
	widgetPattern = regexp.MustCompile(`(?s)<div\b[^>]*\bclass="cf-turnstile\b[^>]*>`)
	namePattern   = regexp.MustCompile(`class="coin-name"><h5>([^<]+)<`)
	sizePattern   = regexp.MustCompile(`>\s*File size\s*</div>\s*<div class="lead-text">([^<]+)<`)
	usagePattern  = regexp.MustCompile(`>\s*Used ([\d.,]+ ?[A-Za-z]+) of ([\d.,]+ ?[A-Za-z]+)\s*<`)
	gonePattern   = regexp.MustCompile(`File has been deleted or never existed`)
)

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = html.UnescapeString(m[2])
	}
	return out
}

// page is what a file page offers for the download.
type page struct {
	name    string
	size    int64
	action  string
	fields  url.Values
	hash    string
	siteKey string
}

func readPage(body string) (page, error) {
	p := page{fields: url.Values{}}
	if m := namePattern.FindStringSubmatch(body); m != nil {
		p.name = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := sizePattern.FindStringSubmatch(body); m != nil {
		p.size = parseSize(m[1])
	}
	if m := usagePattern.FindStringSubmatch(body); m != nil {
		if limit := parseSize(m[2]); limit > 0 && parseSize(m[1]) >= limit {
			return p, &hoster.WaitError{Wait: limitWait, Why: "Krakenfiles' daily free transfer for this address is used up", Reason: core.ReasonLimit}
		}
	}
	form := formPattern.FindStringSubmatch(body)
	hash := hashPattern.FindStringSubmatch(body)
	if form == nil || hash == nil {
		return p, fmt.Errorf("krakenfiles: the file page has no download form")
	}
	p.action, p.hash = attrs(form[1])["action"], hash[1]
	for _, in := range inputPattern.FindAllStringSubmatch(form[2], -1) {
		if a := attrs(in[1]); a["name"] != "" {
			p.fields.Set(a["name"], a["value"])
		}
	}
	if w := widgetPattern.FindString(form[2]); w != "" {
		p.siteKey = attrs(w)["data-sitekey"]
	}
	return p, nil
}

var _ hoster.AskingUnlocker = (*Client)(nil)

// Unlock is UnlockAsking with nobody to answer the Turnstile check.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	return c.UnlockAsking(ctx, raw, password, func(context.Context, hoster.Captcha) (string, error) {
		return "", hoster.ErrNoAnswer
	})
}

// UnlockAsking reads the file's page, has ask answer its Turnstile check and
// sends the download form, whose answer holds the link. A refused answer is
// asked for again on a fresh page.
func (c *Client) UnlockAsking(ctx context.Context, raw, _ string, ask func(context.Context, hoster.Captcha) (string, error)) (hoster.Link, error) {
	id := fileID(raw)
	if id == "" {
		return hoster.Link{}, fmt.Errorf("krakenfiles: %s is not a link to a file", raw)
	}
	jar, _ := cookiejar.New(nil)
	hc := *c.hc
	hc.Jar = jar
	for try := 1; try <= maxCaptchaTries; try++ {
		l, refused, err := c.unlockOnce(ctx, &hc, id, ask)
		if !refused {
			return l, err
		}
	}
	return hoster.Link{}, &hoster.Refusal{Reason: core.ReasonCaptcha, Msg: "krakenfiles: the answer to the captcha was not accepted"}
}

// unlockOnce goes through the page and its form once. refused is an answer
// to the captcha the site did not take.
func (c *Client) unlockOnce(ctx context.Context, hc *http.Client, id string, ask func(context.Context, hoster.Captcha) (string, error)) (l hoster.Link, refused bool, err error) {
	at := c.site + "/view/" + id + "/file.html"
	body, status, err := get(ctx, hc, at)
	switch {
	case err != nil:
		return l, false, err
	case status == http.StatusNotFound || gonePattern.MatchString(body):
		return l, false, hoster.Gone("krakenfiles")
	case status != http.StatusOK:
		return l, false, fmt.Errorf("krakenfiles: the file page answered %d", status)
	}
	p, err := readPage(body)
	if err != nil {
		return l, false, err
	}
	fields := p.fields
	if p.siteKey != "" {
		answer, err := ask(ctx, hoster.Captcha{Kind: hoster.Turnstile, SiteKey: p.siteKey, PageURL: pageURL(id)})
		if err != nil {
			return l, false, fmt.Errorf("krakenfiles: %w", err)
		}
		fields.Set("cf-turnstile-response", answer)
	}
	action, err := url.Parse(at)
	if err == nil {
		action, err = action.Parse(p.action)
	}
	if err != nil {
		return l, false, fmt.Errorf("krakenfiles: the download form goes nowhere: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action.String(), strings.NewReader(fields.Encode()))
	if err != nil {
		return l, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", at)
	// The page's script sends the file's hash as a header of its own.
	req.Header.Set("hash", p.hash)
	resp, err := hc.Do(req)
	if err != nil {
		return l, false, err
	}
	defer resp.Body.Close()
	var a struct {
		Status string `json:"status"`
		URL    string `json:"url"`
		Msg    string `json:"msg"`
	}
	// A refusal comes as a server error with the same JSON.
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&a); err != nil {
		return l, false, fmt.Errorf("krakenfiles: the download form answered %d in a shape it does not use", resp.StatusCode)
	}
	switch {
	case strings.EqualFold(a.Status, "ok") && strings.HasPrefix(a.URL, "http"):
		l = hoster.Link{URL: a.URL, Conns: maxConns, Name: p.name, Size: p.size, Headers: map[string]string{"Referer": at}}
		if u, err := url.Parse(a.URL); err == nil {
			var cookies []string
			for _, ck := range hc.Jar.Cookies(u) {
				cookies = append(cookies, ck.Name+"="+ck.Value)
			}
			if len(cookies) > 0 {
				l.Headers["Cookie"] = strings.Join(cookies, "; ")
			}
		}
		return l, false, nil
	case strings.EqualFold(a.Msg, "captcha not valid"):
		return l, true, nil
	case a.Msg != "":
		return l, false, &hoster.WaitError{Wait: refusedWait, Why: "Krakenfiles turned the download down (" + a.Msg + ")", Reason: core.ReasonUnavailable}
	}
	return l, false, fmt.Errorf("krakenfiles: the download form answered %d without a link", resp.StatusCode)
}

func get(ctx context.Context, hc *http.Client, target string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	return string(b), resp.StatusCode, err
}

// parseSize reads a size as the site writes it, such as "4.90 MB", in binary
// units. 0 is a size it could not read.
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, unicode.IsLetter)
	if i < 0 {
		i = len(s)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s[:i]), ",", ""), 64)
	if err != nil {
		return 0
	}
	shift := 0
	switch strings.ToUpper(strings.TrimSpace(s[i:])) {
	case "", "B", "BYTES":
	case "KB":
		shift = 1
	case "MB":
		shift = 2
	case "GB":
		shift = 3
	case "TB":
		shift = 4
	default:
		return 0
	}
	return int64(math.Round(v * math.Pow(1024, float64(shift))))
}
