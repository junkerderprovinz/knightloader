package keep2share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// freePrio puts a free client below every debrid service (43 and up), which
// downloads without the captcha and the wait, and above JDownloader's own
// free mode (41).
const freePrio = 42

// maxFreeWaits is how many waits in a row getUrl may ask for before the
// download gives up, as JDownloader's plugin gives up.
const maxFreeWaits = 5

// maxImage caps the captcha's picture, which is a few kilobytes.
const maxImage = 1 << 20

var (
	// ErrPremiumOnly is a file its owner keeps for premium accounts, or one
	// too large for a free download.
	ErrPremiumOnly = errors.New("only premium accounts may download this file")
	// errCaptchaRefused is the site turning the captcha's answer down.
	errCaptchaRefused = errors.New("the answer to the captcha was not accepted")
)

// FreeBackend downloads the files of one site for free through its API:
// the image captcha, the wait the answer comes with and then the link.
type FreeBackend struct {
	*ownlogin.Free

	host     Host
	client   *Client
	onUpdate func(taskID string, u core.Update)
}

func NewFreeBackend(h Host, c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *FreeBackend {
	b := &FreeBackend{host: h, client: c, onUpdate: onUpdate}
	b.Free = ownlogin.NewFree(eng, onUpdate, ownlogin.FreeConfig{
		Name: h.Name, Unlock: b.unlock, Refused: errCaptchaRefused,
		Reason: func(err error) core.Reason {
			if errors.Is(err, ErrPremiumOnly) {
				return core.ReasonAuth
			}
			return reason(err)
		},
	})
	return b
}

func (b *FreeBackend) unlock(ctx context.Context, taskID, link string, ask func(hoster.Captcha) (string, error), delay func(time.Duration) error) (string, error) {
	norm, ok := b.host.Normalize(link)
	if !ok {
		return "", fmt.Errorf("%s: %s is not a link to a file", b.host.Name, link)
	}
	id := b.host.fileIDOf(norm)
	files, err := b.client.Files(ctx, []string{id})
	if err != nil {
		return "", b.named(err)
	}
	f, ok := files[id]
	if !ok || !f.Available || f.Folder {
		return "", b.named(ErrGone)
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: f.Name, Size: f.Size})
	challenge, image, err := b.client.captcha(ctx)
	if err != nil {
		return "", b.named(err)
	}
	answer, err := ask(hoster.Captcha{Kind: hoster.Image, Image: image, PageURL: norm})
	if err != nil {
		return "", err
	}
	in := map[string]any{"file_id": id, "captcha_challenge": challenge, "captcha_response": answer}
	for waits := 0; ; waits++ {
		direct, key, wait, err := b.client.freeURL(ctx, in)
		switch {
		case err != nil:
			return "", b.named(err)
		case direct != "":
			return direct, nil
		case key == "":
			return "", b.named(errors.New("getUrl answered neither with a link nor with a wait"))
		case waits == maxFreeWaits:
			return "", b.named(errors.New("getUrl kept asking for another wait"))
		}
		if err := delay(wait); err != nil {
			return "", err
		}
		in = map[string]any{"file_id": id, "free_download_key": key}
	}
}

func (b *FreeBackend) named(err error) error { return fmt.Errorf("%s: %w", b.host.Name, err) }

// captcha asks for a captcha and reads its picture.
func (c *Client) captcha(ctx context.Context) (string, []byte, error) {
	var out struct {
		Challenge string `json:"challenge"`
	}
	if err := c.call(ctx, "requestCaptcha", map[string]any{}, &out); err != nil {
		return "", nil, err
	}
	if out.Challenge == "" {
		return "", nil, errors.New("requestCaptcha was answered without a challenge")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+"/api/v2/captcha?"+url.Values{"id": {out.Challenge}}.Encode(), nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	image, err := io.ReadAll(io.LimitReader(resp.Body, maxImage))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode != http.StatusOK || len(image) == 0 {
		return "", nil, fmt.Errorf("the captcha's picture answered %d", resp.StatusCode)
	}
	return out.Challenge, image, nil
}

// freeURL asks getUrl for a free download: with the captcha's answer it
// answers with a key and a wait, with the key after the wait it answers with
// the link.
func (c *Client) freeURL(ctx context.Context, in map[string]any) (string, string, time.Duration, error) {
	var out struct {
		URL      string          `json:"url"`
		Key      string          `json:"free_download_key"`
		TimeWait json.RawMessage `json:"time_wait"`
	}
	refusal, err := c.do(ctx, "getUrl", in, &out)
	if refusal != nil {
		return "", "", 0, refusal.free()
	}
	if err != nil {
		return "", "", 0, err
	}
	var secs float64
	_ = json.Unmarshal(out.TimeWait, &secs)
	return out.URL, out.Key, time.Duration(secs) * time.Second, nil
}

// free reads a refusal of a free getUrl, in the codes the API documents
// for it and as JDownloader's plugin waits on them.
func (e apiError) free() error {
	var code int
	if json.Unmarshal(e.code, &code) != nil {
		var word string
		_ = json.Unmarshal(e.code, &word)
		switch word {
		case "captcha_need_wait":
			return &ownlogin.PauseError{Wait: 30 * time.Minute, Msg: "too many wrong captchas from this address, so the site makes it wait"}
		case "captcha_need_wait_daily":
			return &ownlogin.PauseError{Wait: 24 * time.Hour, Msg: "too many wrong captchas from this address, so the site makes it wait a day"}
		}
		return e.classify("getUrl")
	}
	if (code == 21 || code == 42) && e.sub != 0 {
		code = e.sub
	}
	switch code {
	case 1, 2:
		return &ownlogin.PauseError{Wait: time.Hour, Msg: "this address has had as many free downloads as the site allows at a time"}
	case 5:
		wait := e.wait
		if wait <= 0 {
			wait = 15 * time.Minute
		}
		return &ownlogin.PauseError{Wait: wait}
	case 6:
		return &ownlogin.PauseError{Wait: 15 * time.Minute, Msg: "the site takes one free download at a time from this address"}
	case 3, 7, 9:
		return fmt.Errorf("%w (%s)", ErrPremiumOnly, e.message)
	case 31:
		return errCaptchaRefused
	}
	return e.classify("getUrl")
}

// FreeResolver claims every file link of its site for a free download.
type FreeResolver struct {
	Host Host
}

var _ resolver.Resolver = FreeResolver{}

func (r FreeResolver) Info() resolver.Info {
	return resolver.Info{ID: r.Host.ID + "-free", Prio: freePrio}
}

func (r FreeResolver) Match(raw string) bool {
	_, ok := r.Host.Normalize(raw)
	return ok
}

// Resolve says what to fetch without asking the site, over the one
// connection a free download gets.
func (r FreeResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := r.Host.Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New(r.Host.Name + ": not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: 1}, nil
}
