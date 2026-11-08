package krakenfiles

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/krakenfiles/kftest"
)

func TestFileLinksAreClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://krakenfiles.com/view/DP3nGKJNsX/file.html":     "https://krakenfiles.com/view/DP3nGKJNsX/file.html",
		"http://www.krakenfiles.com/view/DP3nGKJNsX/file.html":  "https://krakenfiles.com/view/DP3nGKJNsX/file.html",
		"https://krakenfiles.com/embed-video/DP3nGKJNsX":        "https://krakenfiles.com/view/DP3nGKJNsX/file.html",
		"https://krakenfiles.com/view/DP3nGKJNsX/file.html?a=1": "https://krakenfiles.com/view/DP3nGKJNsX/file.html",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://krakenfiles.com/",
		"https://krakenfiles.com/view/DP3nGKJNsX",
		"https://krakenfiles.com/profiles/someone",
		"https://krakenfiles.com/view/DP3n-GKJNsX/file.html",
		"https://dl.krakenfiles.com/force-download/abc?fileHash=DP3nGKJNsX",
		"https://example.com/view/DP3nGKJNsX/file.html",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	r := Resolver{}
	res, err := r.Resolve(context.Background(), resolver.Request{URL: "https://krakenfiles.com/embed-video/DP3nGKJNsX"})
	if err != nil || res.DirectURL != "https://krakenfiles.com/view/DP3nGKJNsX/file.html" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
}

func TestTheEmbedJSONNamesSizesAndChecksEveryFile(t *testing.T) {
	srv := kftest.New(t)
	live := srv.Add("DP3nGKJNsX", kftest.File{Name: "Moonrise Kingdom (2012).mkv", Body: make([]byte, 5138022)})
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	pause := InspectPause
	InspectPause = 0
	t.Cleanup(func() { InspectPause = pause })

	got := r.Inspect(context.Background(), []string{live, "https://krakenfiles.com/view/zTpdkgdZY8/file.html", "https://example.com/x"})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Moonrise Kingdom (2012).mkv", Size: 5138022},
		{Availability: core.AvailOffline},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
}

func TestSizesAreReadInBinaryUnits(t *testing.T) {
	for in, want := range map[string]int64{"4.90 MB": 5138022, "313.30 MB": 328518861, "1.2 GB": 1288490189, "512 B": 512, "12 KB": 12288, "big": 0} {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

// answering returns an ask that answers every captcha with token and keeps
// what it was asked.
func answering(token string, asked *[]hoster.Captcha) func(context.Context, hoster.Captcha) (string, error) {
	return func(_ context.Context, c hoster.Captcha) (string, error) {
		*asked = append(*asked, c)
		return token, nil
	}
}

func TestTheDownloadFormGoesInWithTheTurnstileAnswerAndHandsOutTheLink(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar")})
	c := NewClient(srv.URL, srv.Client())
	var asked []hoster.Captcha

	l, err := c.UnlockAsking(context.Background(), link, "", answering(kftest.Token, &asked))
	if err != nil {
		t.Fatal(err)
	}
	if l.URL != srv.URL+"/force-download/DP3nGKJNsX?fileHash=DP3nGKJNsX" || l.Conns != maxConns || l.Name != "a.rar" {
		t.Errorf("link = %+v", l)
	}
	if l.Headers["Referer"] != srv.URL+"/view/DP3nGKJNsX/file.html" {
		t.Errorf("headers = %v, want the file page as referer", l.Headers)
	}
	if len(asked) != 1 || asked[0].Vendor != "turnstile" || asked[0].SiteKey != kftest.SiteKey || asked[0].PageURL != "https://krakenfiles.com/view/DP3nGKJNsX/file.html" {
		t.Errorf("asked %+v, want the page's Turnstile widget", asked)
	}
	posts := srv.Posts()
	if len(posts) != 1 || !posts[0].XHR || posts[0].Hash != "DP3nGKJNsX" || posts[0].Token != "dl-token-DP3nGKJNsX" {
		t.Errorf("posts = %+v, want the form as the page's script sends it", posts)
	}
}

func TestARefusedAnswerIsAskedAgainBeforeTheDownloadFailsAsACaptchaOne(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar")})
	var asked []hoster.Captcha

	_, err := NewClient(srv.URL, srv.Client()).UnlockAsking(context.Background(), link, "", answering("wrong", &asked))
	var r *hoster.Refusal
	if !errors.As(err, &r) || r.Reason != core.ReasonCaptcha {
		t.Errorf("err = %v, want a captcha refusal", err)
	}
	if len(asked) != maxCaptchaTries {
		t.Errorf("asked %d times, want %d", len(asked), maxCaptchaTries)
	}
}

func TestACaptchaNobodyAnswersStopsTheUnlock(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar")})
	_, err := NewClient(srv.URL, srv.Client()).UnlockAsking(context.Background(), link, "", func(context.Context, hoster.Captcha) (string, error) {
		return "", hoster.ErrNoAnswer
	})
	if !errors.Is(err, hoster.ErrNoAnswer) || len(srv.Posts()) != 0 {
		t.Errorf("err = %v after %d posts, want no answer and no form sent", err, len(srv.Posts()))
	}
}

func TestAGoneFileIsGone(t *testing.T) {
	srv := kftest.New(t)
	_, err := NewClient(srv.URL, srv.Client()).UnlockAsking(context.Background(), "https://krakenfiles.com/view/zTpdkgdZY8/file.html", "", nil)
	if !hoster.IsGone(err) {
		t.Errorf("err = %v, want gone", err)
	}
}

func TestAUsedUpDailyTransferIsAWaitWithoutACaptcha(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar"), Used: "5 GB"})
	var asked []hoster.Captcha
	_, err := NewClient(srv.URL, srv.Client()).UnlockAsking(context.Background(), link, "", answering(kftest.Token, &asked))
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Reason != core.ReasonLimit || w.Wait != limitWait || !strings.Contains(w.Why, "daily free transfer") {
		t.Errorf("err = %v, want a wait for the daily limit", err)
	}
	if len(asked) != 0 {
		t.Errorf("asked %+v for a download that cannot go ahead", asked)
	}
}

func TestAnotherRefusalOfTheFormIsAWaitWithItsMessage(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar"), Refuse: "Server overloaded"})
	var asked []hoster.Captcha
	_, err := NewClient(srv.URL, srv.Client()).UnlockAsking(context.Background(), link, "", answering(kftest.Token, &asked))
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Wait != refusedWait || !strings.Contains(w.Why, "Server overloaded") {
		t.Errorf("err = %v, want a wait naming the site's message", err)
	}
}

func TestUnlockWithoutAWayToAskFailsAsACaptchaOne(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar")})
	_, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
	if !errors.Is(err, hoster.ErrNoAnswer) {
		t.Errorf("err = %v, want no answer", err)
	}
}

func TestTheBackendDownloadsAKrakenfilesLinkThroughTheCaptcha(t *testing.T) {
	srv := kftest.New(t)
	link := srv.Add("DP3nGKJNsX", kftest.File{Name: "a.rar", Body: []byte("rar")})
	got := make(chan string, 1)
	b := hoster.NewBackend("Krakenfiles", NewClient(srv.URL, srv.Client()), engine{got}, func(string, core.Update) {})
	b.Solve = func(context.Context, string, hoster.Captcha) (string, error) { return kftest.Token, nil }

	b.Download("t1", link, nil, 4)
	select {
	case u := <-got:
		if !strings.HasPrefix(u, srv.URL+"/force-download/") {
			t.Errorf("handed on %q", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was handed on")
	}
}

type engine struct{ got chan string }

func (e engine) HandoverHeaders(_, url string, _ map[string]string, _ int, _ func(context.Context) (string, error)) {
	e.got <- url
}
func (engine) Pause(string)        {}
func (engine) Resume(string)       {}
func (engine) Remove(string, bool) {}
