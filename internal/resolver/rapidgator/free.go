package rapidgator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// answerLife is how long a captcha's answer is sure to be taken. A countdown
// longer than that is sat out mostly before the captcha is asked, so the
// answer is still fresh when the form goes in.
const answerLife = 100 * time.Second

// maxAnswers is how many answers one captcha page is given before the
// download fails as a captcha.
const maxAnswers = 3

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
	pageError         = regexp.MustCompile(`(?is)<div\b[^>]*\bclass="(?:error|errorSummary)"[^>]*>(.*?)</div>|<strong style="color:red">(.*?)</strong>`)
	markup            = regexp.MustCompile(`<[^>]*>`)
	pageTitle         = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	scriptCall        = regexp.MustCompile(`'(/download/[A-Za-z]+)'`)
	pageName          = regexp.MustCompile(`^[A-Za-z]{1,24}$`)
	// Each is a way the page after the answer carries the link, as
	// JDownloader's plugin and other clients read it. The last takes a link
	// to the file anywhere on the page, such as on the download button.
	freeLinks = []*regexp.Regexp{
		regexp.MustCompile(`'(https?://[A-Za-z0-9\-_]+\.[^/]+//\?r=download/index&session_id=[A-Za-z0-9]+)'`),
		regexp.MustCompile(`location\.href\s*=\s*'(https?://[^']+)'`),
		regexp.MustCompile(`var download_link\s*=\s*'(https?://[^']+)'`),
		regexp.MustCompile(`(https?://[^/'"<>\s]+/download/[^'"<>\s]+)`),
	}
	// stepLink is a link back into the steps before the download rather
	// than to the file.
	stepLink = regexp.MustCompile(`^https?://[^/]+/download/(?:captcha|Ajax)`)

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
	now      func() time.Time

	mu sync.Mutex
	// last is the captcha the last captcha page asked, the zero Captcha
	// before the first.
	last hoster.Captcha
}

func NewFreeBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *FreeBackend {
	b := &FreeBackend{client: c, onUpdate: onUpdate, now: time.Now}
	b.Free = ownlogin.NewFree(eng, onUpdate, ownlogin.FreeConfig{
		Name: "Rapidgator", Unlock: b.unlock,
		Reason: func(err error) core.Reason {
			switch {
			case errors.Is(err, ErrPremiumOnly):
				return core.ReasonAuth
			case errors.Is(err, errCaptchaRefused):
				return core.ReasonCaptcha
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
	countdown := time.Duration(wait)*time.Second + time.Second
	// Rapidgator times the answer from the end of the countdown and bars the
	// address for an hour when it comes late, as one from a phone easily
	// does. Its captcha keeps its key from one download to the next, so the
	// last one is answered during the countdown, as JDownloader's plugin
	// does with the type it last met.
	b.mu.Lock()
	early := b.last
	b.mu.Unlock()
	var answer string
	if early.SiteKey != "" {
		if err := sitOut(countdown-answerLife, delay); err != nil {
			return "", err
		}
		began := b.now()
		if answer, err = ask(early); err != nil {
			return "", err
		}
		countdown = min(countdown, answerLife) - b.now().Sub(began)
	}
	if err := sitOut(countdown, delay); err != nil {
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
	ready := b.now()
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
	c, ok := widgetCaptcha(form[1])
	if !ok {
		return "", errors.New("rapidgator: the captcha page asks for a captcha KnightLoader does not know")
	}
	b.mu.Lock()
	b.last = c
	b.mu.Unlock()
	if answer == "" || c.Kind != early.Kind || c.SiteKey != early.SiteKey {
		if answer, err = ask(c); err != nil {
			return "", err
		}
	}
	action := "/download/captcha"
	if a := formAction.FindStringSubmatch(form[0]); a != nil && a[1] != "" {
		action = html.UnescapeString(a[1])
	}
	// The form takes another answer after one it turned down, also when its
	// own check of the answer failed, so a refusal is answered on the same
	// page rather than with a new countdown.
	for try := 1; ; try++ {
		// The form takes the answer of either kind in its own field and the
		// one the widget adds, as a browser sends them.
		fields.Set("DownloadCaptchaForm[verifyCode]", answer)
		fields.Set("g-recaptcha-response", answer)
		if c.Kind == hoster.Turnstile {
			fields.Set("cf-turnstile-response", answer)
		}
		sent := b.now().Sub(ready)
		body, resp, err := s.post(ctx, action, fields)
		if err != nil {
			return "", err
		}
		if !captchaWrong.MatchString(body) {
			direct, err := freeLink(body)
			if err == nil && resp.StatusCode != http.StatusOK {
				err = &APIError{Status: resp.StatusCode, Details: action + " answered " + http.StatusText(resp.StatusCode)}
			}
			if err != nil {
				s.explain("no download link after the captcha", fileIDOf(norm), sent, body, resp)
			}
			return direct, err
		}
		s.explain("Rapidgator refused the captcha answer", fileIDOf(norm), sent, body, resp)
		if try == maxAnswers {
			return "", errCaptchaRefused
		}
		if answer, err = ask(c); err != nil {
			return "", err
		}
	}
}

// explain logs why an answer to the captcha led to no download, so a report
// tells an answer Rapidgator found too late from one it found wrong.
func (s *session) explain(what, id string, sent time.Duration, body string, resp *http.Response) {
	failed := "not set"
	u, _ := url.Parse(s.c.site)
	for _, c := range s.hc.Jar.Cookies(u) {
		if c.Name == "failed_on_captcha" && c.Value == "1" {
			failed = "set"
		}
	}
	says := "the page shows no error"
	if text := pageText(body); text != "" {
		says = strconv.Quote(text)
	}
	log.Printf("rapidgator: %s for file %s, answer sent %ds after the countdown (failed_on_captcha %s): %s. Page: %s",
		what, id, int(sent.Seconds()), failed, says, pageSummary(body, resp))
}

// pageSummary says what a page is without a session id, token or query, since
// the line ends up in public issues: its status and path, its title, whether
// the captcha form is back and which of the site's calls its scripts make.
func pageSummary(body string, resp *http.Response) string {
	title := "no title"
	if m := pageTitle.FindStringSubmatch(body); m != nil {
		title = "title " + strconv.Quote(strings.Join(strings.Fields(html.UnescapeString(m[1])), " "))
	}
	form := "no captcha form"
	if captchaForm.MatchString(body) {
		form = "captcha form shown"
	}
	calls := "no script calls under /download/"
	var seen []string
	for _, m := range scriptCall.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(seen, m[1]) {
			seen = append(seen, m[1])
		}
	}
	if len(seen) > 0 {
		calls = "script calls " + strings.Join(seen, ", ")
	}
	return fmt.Sprintf("status %d at %s, %s, %s, %s", resp.StatusCode, sitePath(resp.Request.URL.Path), title, form, calls)
}

// sitePath is path up to its first part that names no page of the site, such
// as a file id or a session on a download server.
func sitePath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return "/"
	}
	var kept []string
	for _, part := range strings.Split(path, "/") {
		if !pageName.MatchString(part) {
			kept = append(kept, "...")
			break
		}
		kept = append(kept, part)
	}
	return "/" + strings.Join(kept, "/")
}

// pageText is the first error a page shows, in plain words and cut short.
func pageText(body string) string {
	for _, m := range pageError.FindAllStringSubmatch(body, -1) {
		text := strings.Join(strings.Fields(html.UnescapeString(markup.ReplaceAllString(m[1]+m[2], " "))), " ")
		if r := []rune(text); len(r) > 200 {
			text = string(r[:200])
		}
		if text != "" {
			return text
		}
	}
	return ""
}

// widgetCaptcha is the first captcha widget in body, as the captcha page
// asks it to be answered.
func widgetCaptcha(body string) (hoster.Captcha, bool) {
	w := widget.FindStringSubmatch(body)
	if w == nil {
		return hoster.Captcha{}, false
	}
	key := siteKey.FindStringSubmatch(w[0])
	if key == nil {
		return hoster.Captcha{}, false
	}
	kind := hoster.Turnstile
	if w[1] == "g-recaptcha" {
		kind = hoster.Recaptcha
	}
	return hoster.Captcha{Kind: kind, SiteKey: key[1], PageURL: "https://" + Site + "/download/captcha"}, true
}

// sitOut waits d, if there is anything to wait.
func sitOut(d time.Duration, delay func(time.Duration) error) error {
	if d > 0 {
		return delay(d)
	}
	return nil
}

// freeLink finds the download link on the page the captcha's answer leads
// to.
func freeLink(body string) (string, error) {
	for _, p := range freeLinks {
		for _, m := range p.FindAllStringSubmatch(body, -1) {
			if !stepLink.MatchString(m[1]) {
				return strings.ReplaceAll(m[1], "&amp;", "&"), nil
			}
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

// post sends form and returns the page and the answer it came with, whatever
// they say, since the page after the captcha is read for why it holds no
// link.
func (s *session) post(ctx context.Context, path string, form url.Values) (string, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.c.site+path, strings.NewReader(form.Encode()))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body, err := s.do(req)
	if err != nil {
		return "", nil, err
	}
	return body, resp, nil
}

func (s *session) read(req *http.Request, path string) (string, error) {
	resp, body, err := s.do(req)
	if err != nil {
		return "", err
	}
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

// do sends req as a browser set to English would.
func (s *session) do(req *http.Request) (*http.Response, string, error) {
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.AddCookie(&http.Cookie{Name: "lang", Value: "en"})
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", err
	}
	return resp, string(raw), nil
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
