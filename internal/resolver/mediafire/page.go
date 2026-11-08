package mediafire

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// The page's own CSS styles a password prompt and a captcha form on every
// file page, so each is recognised by its markup, not by its name alone.
var (
	buttonPattern      = regexp.MustCompile(`href\s*=\s*"(https?://[^"]+)"\s*id\s*=\s*"downloadButton"`)
	scrambledPattern   = regexp.MustCompile(`data-scrambled-url\s*=\s*"([^"]+)"`)
	limitPattern       = regexp.MustCompile(`\bvar limitReachedTTL\s*=\s*(\d+)`)
	unavailablePattern = regexp.MustCompile(`class="error-title"[^>]*>\s*This download is currently unavailable\s*<`)
	retryPattern       = regexp.MustCompile(`(?i)we will retry your download again in (\d+) seconds`)
	outagePattern      = regexp.MustCompile(`class="error-title"[^>]*>\s*Temporarily Unavailable\s*<`)
	passwordPattern    = regexp.MustCompile(`aria-labelledby\s*=\s*"passwordmsg"|class\s*=\s*"passwordPrompt"`)
	captchaPattern     = regexp.MustCompile(`<form\b[^>]*\bname\s*=\s*"form_captcha"`)
	malwarePattern     = regexp.MustCompile(`class\s*=\s*"MalwareAdvisory"`)
)

// The waits a page asks for without a figure, as JDownloader's plugin sits
// them out.
const (
	outageWait = 30 * time.Minute
	repairWait = 5 * time.Minute
	retryWait  = time.Minute
)

// gone are the error numbers Mediafire's error page gives for a file it no
// longer hands out, with what each means.
var gone = map[int]string{
	320: "the owner or Mediafire removed the file",
	323: "Mediafire blocked the file as dangerous",
	326: "Google Safe Browsing marked the file as dangerous",
	378: "Mediafire removed the file for breaking its terms",
	380: "the file was taken down on a copyright claim",
	382: "the file belongs to a suspended account",
	386: "Mediafire blocked the file for breaking its terms",
	388: "the file was identified as copyrighted work",
}

// errorPage turns the error number Mediafire redirected to into a refusal.
func errorPage(n int) error {
	if why, ok := gone[n]; ok {
		return &hoster.Refusal{Reason: core.ReasonGone, Msg: "mediafire: " + why}
	}
	switch n {
	case 394:
		return &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "mediafire: the uploader's account allows no more downloads of this protected archive"}
	case 999:
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "mediafire: the owner made the file private"}
	}
	return fmt.Errorf("mediafire: the file page went to error number %d", n)
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock reads the file's page and returns the link behind its download
// button.
func (c *Client) Unlock(ctx context.Context, raw, _ string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok || l.folder {
		return hoster.Link{}, fmt.Errorf("mediafire: %s is not a link to a file", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/file/"+l.key+"/file", nil)
	if err != nil {
		return hoster.Link{}, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return hoster.Link{}, err
	}
	defer resp.Body.Close()
	final := resp.Request.URL
	if n, err := strconv.Atoi(final.Query().Get("errno")); err == nil && strings.Contains(final.Path, "error") {
		return hoster.Link{}, errorPage(n)
	}
	if strings.Contains(final.Path, "download_repair") {
		return hoster.Link{}, &hoster.WaitError{Wait: repairWait, Why: "Mediafire is making a new download key for the file", Reason: core.ReasonUnavailable}
	}
	if resp.StatusCode != http.StatusOK {
		return hoster.Link{}, fmt.Errorf("mediafire: the file page answered %d", resp.StatusCode)
	}
	// A file its owner lets be linked directly comes instead of the page.
	if d, _, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && d == "attachment" {
		return hoster.Link{URL: final.String(), Conns: maxConns}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return hoster.Link{}, err
	}
	link, err := readPage(string(body))
	if err != nil {
		return hoster.Link{}, err
	}
	if u, err := final.Parse(link); err == nil {
		link = u.String()
	}
	return hoster.Link{URL: link, Conns: maxConns}, nil
}

// readPage finds the download link on a file page, or why there is none.
func readPage(page string) (string, error) {
	if m := buttonPattern.FindStringSubmatch(page); m != nil {
		return html.UnescapeString(m[1]), nil
	}
	if m := scrambledPattern.FindStringSubmatch(page); m != nil {
		if b, err := base64.StdEncoding.DecodeString(m[1]); err == nil {
			if _, err := url.Parse(string(b)); err == nil {
				return string(b), nil
			}
		}
	}
	switch {
	case limitPattern.MatchString(page):
		n, _ := strconv.Atoi(limitPattern.FindStringSubmatch(page)[1])
		return "", &hoster.WaitError{Wait: time.Duration(n) * time.Second, Why: "Mediafire limits free downloads from this address", Reason: core.ReasonLimit}
	case unavailablePattern.MatchString(page):
		wait := retryWait
		if m := retryPattern.FindStringSubmatch(page); m != nil {
			n, _ := strconv.Atoi(m[1])
			wait = time.Duration(n) * time.Second
		}
		return "", &hoster.WaitError{Wait: wait, Why: "Mediafire has the download unavailable for the moment", Reason: core.ReasonUnavailable}
	case outagePattern.MatchString(page):
		return "", &hoster.WaitError{Wait: outageWait, Why: "Mediafire has the file unavailable for the moment", Reason: core.ReasonUnavailable}
	case passwordPattern.MatchString(page):
		return "", &hoster.Refusal{Reason: core.ReasonAuth, Msg: "mediafire: the file is protected by a password, which only JDownloader's Mediafire plugin can enter"}
	case captchaPattern.MatchString(page):
		return "", &hoster.Refusal{Reason: core.ReasonLimit, Msg: "mediafire: Mediafire wants a captcha from this address, which only its website shows"}
	case malwarePattern.MatchString(page):
		return "", &hoster.Refusal{Reason: core.ReasonUnavailable, Msg: "mediafire: Mediafire flagged the file as malware and hands it out only on its website"}
	}
	return "", fmt.Errorf("mediafire: the file page has no download link")
}
