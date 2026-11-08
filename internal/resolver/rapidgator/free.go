package rapidgator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// FreeResolverID is the routing id of the free client and its backend.
const FreeResolverID = "rapidgator-free"

// freePrio puts the free client below every debrid service (43 and up),
// which downloads without the captcha and the wait, and above JDownloader's
// own free mode (41).
const freePrio = 42

// The free page's script and the captcha page, as JDownloader's plugin reads
// them.
var (
	startTimerPattern = regexp.MustCompile(`var startTimerUrl = '([^']+)';`)
	fidPattern        = regexp.MustCompile(`var fid = (\d+);`)
	secsPattern       = regexp.MustCompile(`var secs = (\d+);`)
	captchaForm       = regexp.MustCompile(`(?is)<form\b[^>]*\bid="captchaform"[^>]*>(.*?)</form>`)
	formAction        = regexp.MustCompile(`(?i)\baction="([^"]*)"`)
	hiddenInput       = regexp.MustCompile(`(?is)<input\b[^>]*\btype="hidden"[^>]*>`)
	inputName         = regexp.MustCompile(`(?i)\bname="([^"]*)"`)
	inputValue        = regexp.MustCompile(`(?i)\bvalue="([^"]*)"`)
	widget            = regexp.MustCompile(`(?is)<div\b[^>]*\bclass="[^"]*\b(cf-turnstile|g-recaptcha)\b[^"]*"[^>]*>`)
	siteKey           = regexp.MustCompile(`(?i)\bdata-sitekey="([^"]+)"`)
	captchaWrong      = regexp.MustCompile(`>\s*Please fix the following input errors|>\s*The verification code is incorrect`)
	freeLinks         = []*regexp.Regexp{
		regexp.MustCompile(`'(https?://[A-Za-z0-9\-_]+\.[^/]+//\?r=download/index&session_id=[A-Za-z0-9]+)'`),
		regexp.MustCompile(`location\.href\s*=\s*'(https?://[^']+)'`),
	}

	delayPattern  = regexp.MustCompile(`Delay between downloads must be not less than (\d+) min`)
	hourlyLimit   = regexp.MustCompile(`>\s*You have reached your (?:hourly|daily) downloads limit`)
	oneAtATime    = regexp.MustCompile("You can`t download more than 1 file at a time in free mode")
	freeSizeLimit = regexp.MustCompile(`You can download files up to ([\d.]+ ?(?:MB|GB)) in free mode`)
	premiumOnly   = regexp.MustCompile(`This file can be downloaded by premium only`)
)

var (
	// ErrPremiumOnly is a file Rapidgator keeps for premium accounts.
	ErrPremiumOnly = errors.New("rapidgator: only premium accounts may download this file")
	// errCaptchaRefused is Rapidgator turning the captcha's answer down.
	errCaptchaRefused = errors.New("rapidgator: the answer to the captcha was not accepted")
)

// FreeBackend downloads Rapidgator files for free the way its website does:
// the countdown the file page starts, the captcha after it, and the link the
// answer leads to.
type FreeBackend struct {
	*ownlogin.Free
	client   *Client
	onUpdate func(taskID string, u core.Update)
}

func NewFreeBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *FreeBackend {
	b := &FreeBackend{client: c, onUpdate: onUpdate}
	b.Free = ownlogin.NewFree(eng, onUpdate, ownlogin.FreeConfig{
		Name: "Rapidgator", Unlock: b.unlock, Refused: errCaptchaRefused,
		Reason: func(err error) core.Reason {
			if errors.Is(err, ErrPremiumOnly) {
				return core.ReasonAuth
			}
			return reason(err)
		},
	})
	return b
}

// session is one free download's way through the website, whose steps hang
// on its cookies.
type session struct {
	c  *Client
	hc http.Client
}

func (b *FreeBackend) unlock(ctx context.Context, taskID, link string, ask func(hoster.Captcha) (string, error), delay func(time.Duration) error) (string, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", fmt.Errorf("rapidgator: %s is not a link to a file", link)
	}
	jar, _ := cookiejar.New(nil)
	s := &session{c: b.client, hc: *b.client.hc}
	s.hc.Jar = jar

	body, err := s.page(ctx, "/file/"+fileIDOf(norm))
	if err != nil {
		return "", err
	}
	if p := readPage(body); p.name != "" {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: p.name, Size: p.size})
	}
	start, fid, secs := startTimerPattern.FindStringSubmatch(body), fidPattern.FindStringSubmatch(body), secsPattern.FindStringSubmatch(body)
	if start == nil || fid == nil || secs == nil {
		return "", errors.New("rapidgator: the free page shows no countdown")
	}
	var started struct {
		State string `json:"state"`
		SID   string `json:"sid"`
	}
	if err := s.ajax(ctx, start[1]+"?"+url.Values{"fid": {fid[1]}}.Encode(), &started); err != nil {
		return "", err
	}
	if !strings.EqualFold(started.State, "started") || started.SID == "" {
		return "", fmt.Errorf("rapidgator: the countdown did not start (state %q)", started.State)
	}
	wait, _ := strconv.Atoi(secs[1])
	// A second more, as JDownloader's plugin waits, since the server counts
	// the countdown too.
	if err := delay(time.Duration(wait)*time.Second + time.Second); err != nil {
		return "", err
	}
	var done struct {
		State string `json:"state"`
	}
	if err := s.ajax(ctx, "/download/AjaxGetDownloadLink?"+url.Values{"sid": {started.SID}}.Encode(), &done); err != nil {
		return "", err
	}
	if !strings.EqualFold(done.State, "done") {
		return "", fmt.Errorf("rapidgator: the countdown did not end (state %q)", done.State)
	}
	body, err = s.page(ctx, "/download/captcha")
	if err != nil {
		return "", err
	}
	form := captchaForm.FindStringSubmatch(body)
	if form == nil {
		return freeLink(body)
	}
	fields := url.Values{}
	for _, input := range hiddenInput.FindAllString(form[1], -1) {
		if n := inputName.FindStringSubmatch(input); n != nil {
			v := ""
			if m := inputValue.FindStringSubmatch(input); m != nil {
				v = html.UnescapeString(m[1])
			}
			fields.Set(html.UnescapeString(n[1]), v)
		}
	}
	w := widget.FindStringSubmatch(form[1])
	var key []string
	if w != nil {
		key = siteKey.FindStringSubmatch(w[0])
	}
	if key == nil {
		return "", errors.New("rapidgator: the captcha page asks for a captcha KnightLoader does not know")
	}
	kind := hoster.Turnstile
	if w[1] == "g-recaptcha" {
		kind = hoster.Recaptcha
	}
	answer, err := ask(hoster.Captcha{Kind: kind, SiteKey: key[1], PageURL: "https://" + Site + "/download/captcha"})
	if err != nil {
		return "", err
	}
	// The form takes the answer of either kind in both fields.
	fields.Set("DownloadCaptchaForm[verifyCode]", answer)
	fields.Set("g-recaptcha-response", answer)
	action := "/download/captcha"
	if a := formAction.FindStringSubmatch(form[0]); a != nil && a[1] != "" {
		action = html.UnescapeString(a[1])
	}
	body, err = s.post(ctx, action, fields)
	if err != nil {
		return "", err
	}
	if captchaWrong.MatchString(body) {
		return "", errCaptchaRefused
	}
	return freeLink(body)
}

// freeLink finds the download link on the page the captcha's answer leads
// to.
func freeLink(body string) (string, error) {
	for _, p := range freeLinks {
		if m := p.FindStringSubmatch(body); m != nil {
			return m[1], nil
		}
	}
	if err := freeRefusal(body); err != nil {
		return "", err
	}
	return "", errors.New("rapidgator: the page after the captcha holds no download link")
}

// freeRefusal reads why a page lets no free download through, nil for no
// reason it knows.
func freeRefusal(body string) error {
	switch {
	case delayPattern.MatchString(body):
		mins, _ := strconv.Atoi(delayPattern.FindStringSubmatch(body)[1])
		return &ownlogin.PauseError{Wait: time.Duration(mins) * time.Minute}
	case hourlyLimit.MatchString(body):
		return &ownlogin.PauseError{Wait: time.Hour, Msg: "rapidgator: this address has had as many free downloads as Rapidgator allows at a time"}
	case oneAtATime.MatchString(body):
		return &ownlogin.PauseError{Wait: 5 * time.Minute, Msg: "rapidgator: Rapidgator takes one free download at a time from this address"}
	case freeSizeLimit.MatchString(body):
		return fmt.Errorf("%w, free downloads take files up to %s", ErrPremiumOnly, freeSizeLimit.FindStringSubmatch(body)[1])
	case premiumOnly.MatchString(body):
		return ErrPremiumOnly
	}
	return nil
}

// page reads a page of the website in English, failing with what it says
// against a free download.
func (s *session) page(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.c.site+path, nil)
	if err != nil {
		return "", err
	}
	return s.read(req, path)
}

func (s *session) post(ctx context.Context, path string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.c.site+path, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.read(req, path)
}

func (s *session) read(req *http.Request, path string) (string, error) {
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.AddCookie(&http.Cookie{Name: "lang", Value: "en"})
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", err
	}
	body := string(raw)
	// A missing file sends the browser on to the premium page.
	if strings.HasPrefix(path, "/file/") && (resp.StatusCode == http.StatusNotFound ||
		!strings.HasPrefix(resp.Request.URL.Path, "/file/") || notFound.MatchString(body)) {
		return "", ErrGone
	}
	if err := freeRefusal(body); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Details: path + " answered " + http.StatusText(resp.StatusCode)}
	}
	return body, nil
}

// ajax asks one of the free page's scripts' calls and decodes its answer.
func (s *session) ajax(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.c.site+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	resp, err := s.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Details: http.StatusText(resp.StatusCode)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("rapidgator: the answer to %s does not read: %w", strings.SplitN(path, "?", 2)[0], err)
	}
	return nil
}

// FreeResolver claims every Rapidgator file link for a free download.
type FreeResolver struct{}

var _ resolver.Resolver = FreeResolver{}

func (FreeResolver) Info() resolver.Info { return resolver.Info{ID: FreeResolverID, Prio: freePrio} }

func (FreeResolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok
}

// Resolve says what to fetch without asking Rapidgator, over the one
// connection a free download gets.
func (FreeResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("rapidgator: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: 1}, nil
}
