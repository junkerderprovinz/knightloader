package onefichier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DefaultSite is where the file pages are read.
const DefaultSite = "https://1fichier.com"

// busyWait is how long a free download waits when 1fichier is holding back
// free downloads as a whole, which comes without a figure. pyLoad waits as
// long.
const busyWait = 5 * time.Minute

// blockedWait is how long a free download waits when 1fichier asks for a pause
// between downloads without saying how long.
const blockedWait = 5 * time.Minute

// maxPage caps how much of a page is read. A file page is a few kilobytes.
const maxPage = 2 << 20

// The reasons a free download cannot go ahead, whatever the wait.
var (
	ErrGone            = errors.New("1fichier: the file has been deleted or never existed")
	ErrPremium         = errors.New("1fichier: the owner keeps this file for subscribers, so it needs a 1fichier premium account")
	ErrPassword        = errors.New("1fichier: the file is protected by a password, and the download has no download password")
	ErrPasswordRefused = errors.New("1fichier: the download password was not accepted")
)

// WaitError is 1fichier asking for a pause before the free download.
type WaitError struct {
	Wait time.Duration
	// Busy is 1fichier holding back free downloads as a whole, rather than a
	// pause for this address.
	Busy bool
}

func (e *WaitError) Error() string {
	if e.Busy {
		return "1fichier: free downloads are limited at the moment"
	}
	return fmt.Sprintf("1fichier: this address has to wait %s before its next free download", e.Wait)
}

// Client reads 1fichier's file pages and sends their download form.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient reads pages from site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// Page is one read of a file's page, with the cookies it was read with.
type Page struct {
	Name string
	Size int64
	// Countdown is how long the page wants before its form goes in.
	Countdown time.Duration

	url  string
	form *form
	hc   *http.Client
}

// Open reads the page of a file link. ErrPremium, *WaitError and a page
// without a form come with the page, which still names the file. Every other
// error comes without one.
func (c *Client) Open(ctx context.Context, link string) (*Page, error) {
	norm, ok := Normalize(link)
	if !ok {
		return nil, fmt.Errorf("1fichier: %s is not a link to a file", link)
	}
	id := strings.TrimPrefix(norm, "https://"+site+"/?")
	jar, _ := cookiejar.New(nil)
	hc := *c.hc
	hc.Jar = jar
	pageURL := c.site + "/?" + id
	if u, err := url.Parse(pageURL); err == nil {
		// English pages, which the patterns are written for.
		jar.SetCookies(u, []*http.Cookie{{Name: "LG", Value: "en", Path: "/"}})
	}

	body, status, err := fetch(ctx, &hc, http.MethodGet, pageURL, nil, "")
	if err != nil {
		return nil, err
	}
	r := readPage(body)
	if r.gone || status == http.StatusNotFound || status == http.StatusGone {
		return nil, ErrGone
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("1fichier: the file page answered %d", status)
	}
	p := &Page{Name: r.name, Size: r.size, Countdown: r.countdown, url: pageURL, hc: &hc}
	if err := r.hold(); err != nil {
		return p, err
	}
	if p.form = r.formFor(id); p.form == nil {
		return p, errors.New("1fichier: the file page has no download form")
	}
	return p, nil
}

// hold is why a page holds the download back, nil when nothing stands in
// the way.
func (r page) hold() error {
	switch {
	case r.gone:
		return ErrGone
	case r.premium:
		return ErrPremium
	case r.coolDown > 0:
		return &WaitError{Wait: r.coolDown}
	case r.busy:
		return &WaitError{Wait: busyWait, Busy: true}
	}
	return nil
}

// Fetch sends the page's download form and returns the download link it
// answers with. password goes into the form's password field.
func (p *Page) Fetch(ctx context.Context, password string) (string, error) {
	fields := maps.Clone(p.form.fields)
	// The submit button, which pyLoad leaves out as well.
	fields.Del("save")
	fields.Set("did", "1")
	if p.form.pass {
		if password == "" {
			return "", ErrPassword
		}
		fields.Set("pass", password)
	}
	action, err := url.Parse(p.url)
	if err == nil {
		action, err = action.Parse(p.form.action)
	}
	if err != nil {
		return "", fmt.Errorf("1fichier: the download form goes nowhere: %w", err)
	}
	body, status, err := fetch(ctx, p.hc, http.MethodPost, action.String(), strings.NewReader(fields.Encode()), p.url)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("1fichier: the download form answered %d", status)
	}
	r := readPage(body)
	if r.link != "" {
		link, err := action.Parse(r.link)
		if err != nil {
			return "", fmt.Errorf("1fichier: the download link does not work: %w", err)
		}
		return link.String(), nil
	}
	if err := r.hold(); err != nil {
		return "", err
	}
	if r.countdown > 0 {
		return "", &WaitError{Wait: r.countdown}
	}
	if p.form.pass && slices.ContainsFunc(r.forms, func(f form) bool { return f.pass }) {
		return "", ErrPasswordRefused
	}
	return "", errors.New("1fichier: the answer to the download form held no download link")
}

func fetch(ctx context.Context, hc *http.Client, method, target string, body io.Reader, referer string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return "", 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return "", 0, err
	}
	return string(b), resp.StatusCode, nil
}
