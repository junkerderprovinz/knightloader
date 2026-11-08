package filer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// FreeResolverID is the routing id of the free client and its backend.
const FreeResolverID = "filer-free"

// freePrio puts the free client below every debrid service (43 and up),
// which downloads without the captcha and the wait, and above JDownloader's
// own free mode (41).
const freePrio = 42

// hCaptchaSite names the hCaptcha on filer.net's download page, as
// its script and JDownloader's plugin carry it.
const hCaptchaSite = "45623a98-7b08-43ae-b758-c21c13024e2a"

// ticketSlack is added to the wait a ticket comes with, as JDownloader's
// plugin adds it, since filer.net turns a ticket down that comes a moment
// early.
const ticketSlack = 10 * time.Second

var (
	ErrPremiumOnly = errors.New("filer.net: the owner keeps this file for premium accounts")
	// errCaptchaRefused is filer.net turning the captcha's answer down.
	errCaptchaRefused = errors.New("filer.net: the answer to the captcha was not accepted")
)

// FreeBackend downloads filer.net files for free the way its website does:
// the hCaptcha, a ticket and its wait, and the link the ticket buys.
type FreeBackend struct {
	*ownlogin.Free
	client *Client
}

func NewFreeBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *FreeBackend {
	b := &FreeBackend{client: c}
	b.Free = ownlogin.NewFree(eng, onUpdate, ownlogin.FreeConfig{
		Name: "filer.net", Unlock: b.unlock, Reason: freeReason, Refused: errCaptchaRefused,
	})
	return b
}

func (b *FreeBackend) unlock(ctx context.Context, _, link string, ask func(hoster.Captcha) (string, error), delay func(time.Duration) error) (string, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", fmt.Errorf("filer.net: %s is not a link to a file", link)
	}
	id := fileIDOf(norm)
	// The website ties the ticket to its session.
	jar, _ := cookiejar.New(nil)
	hc := *b.client.hc
	hc.Jar = jar
	var file struct {
		PremiumOnly bool `json:"premiumOnly"`
	}
	if err := b.client.web(ctx, &hc, http.MethodGet, "/file/"+id, nil, &file); err != nil {
		return "", err
	}
	if file.PremiumOnly {
		return "", ErrPremiumOnly
	}
	answer, err := ask(hoster.Captcha{Kind: hoster.HCaptcha, SiteKey: hCaptchaSite, PageURL: norm})
	if err != nil {
		return "", err
	}
	var ticket struct {
		T  string `json:"t"`
		WT number `json:"wt"`
	}
	path := "/file/request/" + id + "?" + url.Values{"hCaptchaToken": {answer}}.Encode()
	if err := b.client.web(ctx, &hc, http.MethodGet, path, nil, &ticket); err != nil {
		return "", err
	}
	if ticket.T == "" {
		return "", errors.New("filer.net: the captcha was answered without a ticket")
	}
	if err := delay(time.Duration(ticket.WT)*time.Second + ticketSlack); err != nil {
		return "", err
	}
	var dl struct {
		URL string `json:"downloadUrl"`
	}
	body, _ := json.Marshal(map[string]string{"ticket": ticket.T})
	if err := b.client.web(ctx, &hc, http.MethodPost, "/file/download", body, &dl); err != nil {
		return "", err
	}
	if dl.URL == "" {
		return "", errors.New("filer.net: the ticket was answered without a download link")
	}
	return dl.URL, nil
}

// web asks the API the website itself uses and decodes its answer into out.
// Its refusals carry an error, a word or a number, and a message.
func (c *Client) web(ctx context.Context, hc *http.Client, method, path string, body []byte, out any) error {
	req, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	what := strings.TrimPrefix(req.URL.Path, "/api/")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("filer.net: %s: %w", what, httpx.StripURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("filer.net: %s: %w", what, httpx.StripURL(err))
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("filer.net: the answer to %s does not read: %w", what, err)
		}
		return nil
	}
	var refusal struct {
		Error         json.RawMessage `json:"error"`
		Message       string          `json:"message"`
		RemainingTime number          `json:"remainingTime"`
	}
	if json.Unmarshal(raw, &refusal) != nil {
		return &APIError{HTTP: resp.StatusCode, Status: http.StatusText(resp.StatusCode)}
	}
	word := strings.Trim(string(refusal.Error), `"`)
	return classifyWeb(resp.StatusCode, word, refusal.Message, time.Duration(refusal.RemainingTime)*time.Second)
}

// classifyWeb reads a refusal of the website's API, by the words and numbers
// filer.net's error page and JDownloader's plugin know.
func classifyWeb(httpStatus int, word, message string, remaining time.Duration) error {
	key := strings.ToUpper(word)
	if n, err := strconv.Atoi(word); err == nil {
		key = map[int]string{500: "SERVICE_TEMPORARILY_UNAVAILABLE", 501: "SERVICE_BUSY", 502: "CONCURRENT_DOWNLOAD_LIMIT",
			503: "HOURLY_DOWNLOAD_LIMIT", 505: "PREMIUM_REQUIRED"}[n]
	}
	switch {
	case key == "" && httpStatus == http.StatusNotFound && strings.EqualFold(message, "File not found"):
		return ErrGone
	case key == "INVALID CAPTCHA":
		return errCaptchaRefused
	case key == "HOURLY_DOWNLOAD_LIMIT", key == "TICKET_LIMIT_REACHED":
		return &ownlogin.PauseError{Wait: time.Hour, Msg: "filer.net: filer.net makes this address wait an hour before the next free download"}
	case key == "CONCURRENT_DOWNLOAD_LIMIT":
		return &ownlogin.PauseError{Wait: 3 * time.Minute, Msg: "filer.net: filer.net takes one free download at a time from this address"}
	case key == "WAIT TIME NOT ELAPSED" && remaining > 0:
		return &ownlogin.PauseError{Wait: remaining + time.Second}
	case key == "PREMIUM_REQUIRED":
		return ErrPremiumOnly
	case key == "SERVICE_TEMPORARILY_UNAVAILABLE", key == "SERVICE_BUSY":
		return ErrBusy
	}
	return fmt.Errorf("filer.net: %s (status %d)", strings.TrimSpace(word+" "+message), httpStatus)
}

// freeReason is the cause a failed free download is reported with.
func freeReason(err error) core.Reason {
	if errors.Is(err, ErrPremiumOnly) {
		return core.ReasonAuth
	}
	return reason(err)
}

// FreeResolver claims every filer.net file link for a free download.
type FreeResolver struct{}

var _ resolver.Resolver = FreeResolver{}

func (FreeResolver) Info() resolver.Info { return resolver.Info{ID: FreeResolverID, Prio: freePrio} }

func (FreeResolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok
}

// Resolve says what to fetch without asking filer.net, over the one
// connection a free download gets.
func (FreeResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("filer.net: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: 1}, nil
}
