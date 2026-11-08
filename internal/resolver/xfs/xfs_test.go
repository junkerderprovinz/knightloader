package xfs_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

var site = &xfs.Site{ID: "example", Name: "Example", Host: "example.net", OwnLogin: true, FreeConns: 1, PremiumConns: 4}

type handoff struct {
	url   string
	conns int
}

type fakeEngine struct {
	got chan handoff
}

func (f *fakeEngine) Handover(_, url string, conns int, _ func(context.Context) (string, error)) {
	f.got <- handoff{url, conns}
}
func (f *fakeEngine) Pause(string)        {}
func (f *fakeEngine) Resume(string)       {}
func (f *fakeEngine) Remove(string, bool) {}

// harness collects what the backend reports, the waits it sits out and the
// captchas it asks for.
type harness struct {
	mu       sync.Mutex
	updates  []core.Update
	waits    []time.Duration
	captchas []xfs.Captcha
	// asked is how long had been waited when each captcha was asked.
	asked  []time.Duration
	answer string
	user   string
	pass   string
}

func (h *harness) add(_ string, u core.Update) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, u)
}

func (h *harness) sleep(ctx context.Context, d time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waits = append(h.waits, d)
	return ctx.Err()
}

func (h *harness) waitedLocked() time.Duration {
	var sum time.Duration
	for _, d := range h.waits {
		sum += d
	}
	return sum
}

func (h *harness) waited() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waitedLocked()
}

func (h *harness) solve(_ context.Context, _ string, c xfs.Captcha) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captchas = append(h.captchas, c)
	h.asked = append(h.asked, h.waitedLocked())
	if h.answer == "" {
		return "", errors.New("skipped")
	}
	return h.answer, nil
}

func (h *harness) last() core.Update {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.updates) == 0 {
		return core.Update{}
	}
	return h.updates[len(h.updates)-1]
}

func newBackend(srv *xfstest.Server) (*xfs.Backend, *fakeEngine, *harness) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	h := &harness{answer: xfstest.Token}
	b := xfs.NewBackend(xfs.NewClient(site, srv.URL, srv.Client()), eng, h.add)
	b.Sleep = h.sleep
	b.Solve = h.solve
	b.Login = func() (string, string, bool) { return h.user, h.pass, h.user != "" }
	return b, eng, h
}

func awaitHandoff(t *testing.T, eng *fakeEngine) handoff {
	t.Helper()
	select {
	case h := <-eng.got:
		return h
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was handed to the engine")
		return handoff{}
	}
}

func awaitFailure(t *testing.T, h *harness) core.Update {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if u := h.last(); u.Status == core.StatusError {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the download never failed")
	return core.Update{}
}

func TestAFreeDownloadPressesTheFreeButtonAnswersTheSecondFormAndHandsTheLinkOn(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 700<<10)})
	srv.Captcha(xfstest.Turnstile)
	srv.Countdown(30)
	b, eng, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	got := awaitHandoff(t, eng)

	if got.url != srv.URL+"/d/k3m9x2p7q1ab/Isle%20of%20Dogs.mkv" || got.conns != 1 {
		t.Errorf("handed on %+v, want the server's link for one connection", got)
	}
	posts := srv.Posts()
	if len(posts) != 2 {
		t.Fatalf("posts %v, want both forms", posts)
	}
	if first := posts[0]; first.Get("op") != "download1" || first.Get("method_free") != "Free Download" || first.Has("method_premium") {
		t.Errorf("the first form went in as %v, want it sent by its free button", first)
	}
	if second := posts[1]; second.Get("op") != "download2" || second.Get("cf-turnstile-response") != xfstest.Token || second.Get("adblock_detected") != "0" {
		t.Errorf("the second form went in as %v, want it with the captcha's answer", second)
	}
	if len(h.captchas) != 1 || h.captchas[0].Kind != xfs.Turnstile || h.captchas[0].SiteKey != xfstest.SiteKey || h.captchas[0].PageURL != srv.URL+"/k3m9x2p7q1ab" {
		t.Errorf("captchas asked %+v, want the second form's Turnstile check", h.captchas)
	}
	if w := h.waited(); w < 29*time.Second || w > 30*time.Second {
		t.Errorf("waited %s, want the countdown's half minute", w)
	}
	if h.updates[0].Name != "Isle of Dogs.mkv" || h.updates[0].Size != 700<<10 {
		t.Errorf("first update %+v, want the file named and sized", h.updates[0])
	}
}

func TestEveryKindOfCaptchaGoesInWhereItsWidgetOrFieldWould(t *testing.T) {
	for _, c := range []struct {
		kind   string
		want   xfs.CaptchaKind
		answer string
		field  string
	}{
		{xfstest.Recaptcha, xfs.Recaptcha, xfstest.Token, "g-recaptcha-response"},
		{xfstest.HCaptcha, xfs.HCaptcha, xfstest.Token, "h-captcha-response"},
		{xfstest.Image, xfs.Image, xfstest.Code, "code"},
		// The script's own digits are read off the page and asked of nobody.
		{xfstest.Plain, "", "", "code"},
	} {
		t.Run(c.kind, func(t *testing.T) {
			srv := xfstest.New(t, site.Host, nil)
			link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
			srv.Captcha(c.kind)
			b, eng, h := newBackend(srv)
			h.answer = c.answer

			b.Download("t1", link, nil, 1)
			awaitHandoff(t, eng)

			switch {
			case c.want == "" && len(h.captchas) > 0:
				t.Errorf("asked %+v for a captcha the page solves itself", h.captchas)
			case c.want != "" && (len(h.captchas) != 1 || h.captchas[0].Kind != c.want):
				t.Errorf("asked %+v, want one %s", h.captchas, c.want)
			}
			if c.want == xfs.Image && !bytes.Equal(h.captchas[0].Image, xfstest.Picture) {
				t.Errorf("the image captcha came with %q, want the server's picture", h.captchas[0].Image)
			}
			if posts := srv.Posts(); posts[1].Get(c.field) != xfstest.Code && posts[1].Get(c.field) != xfstest.Token {
				t.Errorf("the second form went in as %v, want the answer in %s", posts[1], c.field)
			}
		})
	}
}

func TestALongCountdownIsMostlySatOutBeforeTheCaptchaIsAsked(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Captcha(xfstest.Recaptcha)
	srv.Countdown(300)
	b, eng, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if len(h.asked) != 1 || h.asked[0] < 190*time.Second {
		t.Errorf("the captcha was asked after %v of waiting, want most of the five minutes sat out first", h.asked)
	}
	if w := h.waited(); w < 299*time.Second || w > 300*time.Second {
		t.Errorf("waited %s in all, want the countdown's five minutes", w)
	}
}

func TestAWaitBetweenFreeDownloadsIsSatOutAndShownOnTheTask(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Wait(125)
	b, eng, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if w := h.waited(); w != 125*time.Second {
		t.Errorf("waited %s, want the 2 min 5 s the page asked for", w)
	}
	if !slices.ContainsFunc(h.updates, func(u core.Update) bool { return u.Note == "Waiting for Example: 3 min left" }) {
		t.Errorf("no update showed the wait: %+v", h.updates)
	}
}

func TestEveryFailureNamesTheSiteAndItsCause(t *testing.T) {
	for _, c := range []struct {
		name   string
		file   *xfstest.File
		answer string
		want   core.Reason
	}{
		{name: "deleted", want: core.ReasonGone},
		{name: "premium only", file: &xfstest.File{Name: "a.bin", PremiumOnly: true}, want: core.ReasonAuth},
		{name: "no password", file: &xfstest.File{Name: "a.bin", Password: "hunter2"}, want: core.ReasonAuth},
		{name: "captcha wrong", file: &xfstest.File{Name: "a.bin"}, answer: "nope", want: core.ReasonCaptcha},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := xfstest.New(t, site.Host, nil)
			srv.Captcha(xfstest.Turnstile)
			link := "https://example.net/g0n3g0n3g0n3"
			if c.file != nil {
				link = srv.Add("k3m9x2p7q1ab", *c.file)
			}
			b, _, h := newBackend(srv)
			if c.answer != "" {
				h.answer = c.answer
			}

			b.Download("t1", link, nil, 1)
			u := awaitFailure(t, h)
			if u.Reason != c.want || !strings.HasPrefix(u.Err, "Example: ") {
				t.Errorf("failed with %q (%s), want %q named after the site", u.Reason, u.Err, c.want)
			}
		})
	}
}

func TestAnAdvertOnAPageThatStillHoldsTheFormIsNoDownloadLink(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, _ := newBackend(srv)

	b.Download("t1", link, nil, 1)
	if got := awaitHandoff(t, eng); strings.Contains(got.url, "ads.example.net") {
		t.Errorf("handed on the advert %q", got.url)
	}
}

func TestAPremiumLoginSkipsTheFirstFormAndTheCaptcha(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Captcha(xfstest.Turnstile)
	srv.LoginCaptcha()
	until := time.Now().AddDate(0, 2, 0)
	srv.AddAccount("fox", xfstest.Account{Password: "badger", Premium: true, Until: until, TrafficMB: 51200})
	b, eng, h := newBackend(srv)
	h.user, h.pass = "fox", "badger"

	b.CheckLogin(context.Background(), true)
	if !b.Premium() {
		t.Fatal("a confirmed premium login does not read as premium")
	}
	if len(h.captchas) != 1 || h.captchas[0].PageURL != srv.URL+"/login.html" {
		t.Errorf("captchas %+v, want the sign-in's", h.captchas)
	}

	b.Download("t1", link, nil, 1)
	if got := awaitHandoff(t, eng); got.conns != 4 {
		t.Errorf("handed on for %d connections, want the premium's four", got.conns)
	}
	posts := srv.Posts()
	if len(posts) != 1 || posts[0].Get("op") != "download2" || posts[0].Has("cf-turnstile-response") {
		t.Errorf("posts %v, want the second form alone and without a captcha", posts)
	}
}

func TestAPremiumFormThatAsksForACaptchaToo(t *testing.T) {
	own := *site
	own.PremiumCaptcha = true
	srv := xfstest.New(t, site.Host, nil)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Captcha(xfstest.Recaptcha)
	srv.PremiumCaptcha()
	srv.AddAccount("fox", xfstest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(0, 2, 0), TrafficMB: 51200})
	eng := &fakeEngine{got: make(chan handoff, 1)}
	h := &harness{answer: xfstest.Token, user: "fox", pass: "badger"}
	b := xfs.NewBackend(xfs.NewClient(&own, srv.URL, srv.Client()), eng, h.add)
	b.Sleep, b.Solve = h.sleep, h.solve
	b.Login = func() (string, string, bool) { return h.user, h.pass, true }
	b.CheckLogin(context.Background(), true)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if !b.Premium() || len(h.captchas) != 1 || h.captchas[0].Kind != xfs.Recaptcha {
		t.Errorf("premium %v after captchas %+v, want the premium form's captcha answered", b.Premium(), h.captchas)
	}
}

// noLink is a page after the download form the client cannot read a link
// from.
var noLink = fstest.MapFS{"link.html": {Data: []byte(`<html><body><h2>Download File</h2><p>Your link is on its way</p></body></html>`)}}

func TestASignInPageTheClientCannotReadGivesTheLoginToJDownloader(t *testing.T) {
	srv := xfstest.New(t, site.Host, fstest.MapFS{"login.html": {Data: []byte(`<html><body>New sign-in coming soon</body></html>`)}})
	srv.AddAccount("fox", xfstest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(0, 2, 0)})
	b, _, h := newBackend(srv)
	h.user, h.pass = "fox", "badger"
	moved := 0
	b.Moved = func() { moved++ }

	b.CheckLogin(context.Background(), true)
	if !b.Handed() || b.Premium() || moved != 1 {
		t.Errorf("handed %v, premium %v after %d moves, want the login with JDownloader", b.Handed(), b.Premium(), moved)
	}
	b.Forget()
	if b.Handed() {
		t.Error("a login that was changed stayed with JDownloader")
	}
}

func TestAWrongPasswordStaysWithTheSiteClient(t *testing.T) {
	srv := xfstest.New(t, site.Host, nil)
	srv.AddAccount("fox", xfstest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(0, 2, 0)})
	b, _, h := newBackend(srv)
	h.user, h.pass = "fox", "weasel"

	b.CheckLogin(context.Background(), true)
	if st, _ := b.LoginStatus(); !st.Refused || b.Handed() {
		t.Errorf("state %+v, handed %v, want a refusal kept from JDownloader", st, b.Handed())
	}
}

func TestAPremiumDownloadTheClientCannotReadGoesToJDownloaderWithTheLogin(t *testing.T) {
	srv := xfstest.New(t, site.Host, noLink)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.AddAccount("fox", xfstest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(0, 2, 0), TrafficMB: 51200})
	b, _, h := newBackend(srv)
	h.user, h.pass = "fox", "badger"
	b.CheckLogin(context.Background(), true)

	b.Download("t1", link, nil, 1)
	if u := awaitFailure(t, h); !u.Unsupported || u.Takeover != "Example" {
		t.Errorf("failed with %+v, want the download handed on with the site named", u)
	}
	if !b.Handed() || b.Premium() {
		t.Error("the login stayed with the site's client")
	}
}

func TestAFreeDownloadTheClientCannotReadLeavesTheLoginAlone(t *testing.T) {
	srv := xfstest.New(t, site.Host, noLink)
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, _, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	if u := awaitFailure(t, h); u.Unsupported {
		t.Errorf("failed with %+v, want a plain failure", u)
	}
	if b.Handed() {
		t.Error("a free download handed a login over")
	}
}

func TestTheLinkCheckReadsNameSizeAndStateFromTheFilePage(t *testing.T) {
	xfs.InspectPause = 0
	srv := xfstest.New(t, site.Host, nil)
	srv.GoneStatus = 404
	live := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "Fantastic Mr Fox.mkv", Body: make([]byte, 3<<20)})
	locked := srv.Add("pr3m1umonly0", xfstest.File{Name: "Rushmore.mkv", Body: make([]byte, 2<<10), PremiumOnly: true})
	r := xfs.Resolver{Site: site, Client: xfs.NewClient(site, srv.URL, srv.Client())}

	got := r.Inspect(context.Background(), []string{live, "https://www.example.net/g0n3g0n3g0n3", locked, "https://rapidgator.net/file/abc"})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Fantastic Mr Fox.mkv", Size: 3 << 20},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOnline, Name: "Rushmore.mkv", Size: 2 << 10},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v\nwant      %+v", got, want)
	}
}

func TestSizesAreReadInTheBinaryUnitsTheSiteWrites(t *testing.T) {
	for in, want := range map[string]int64{
		"442.40 MB": 463890022,
		"700.00 KB": 700 << 10,
		"1.5 GB":    3 << 29,
		"1,020 MB":  1020 << 20,
		"512 B":     512,
	} {
		figure, unit, _ := strings.Cut(in, " ")
		if got := xfs.ParseSize(figure, unit); got != want {
			t.Errorf("ParseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTheScriptsAccountPageNamesThePlanItsEndAndTheTrafficLeft(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		page string
		want xfs.Account
	}{
		{
			name: "premium",
			page: `<tr><td>Premium account expire:</td><td><b>12 January 2027</b></td></tr><tr><td>Traffic available today:</td><td><b>50 GB</b></td></tr>`,
			want: xfs.Account{Premium: true, Expires: time.Date(2027, 1, 12, 23, 59, 59, 0, time.UTC), TrafficLeft: 50 << 30, TrafficMax: 50 << 30},
		},
		{
			name: "premium to the second",
			page: `<span class="profile-ud-label">Premium account expire:</span> <span class="profile-ud-value">2027-02-07 19:58:15</span>`,
			want: xfs.Account{Premium: true, Expires: time.Date(2027, 2, 7, 19, 58, 15, 0, time.UTC)},
		},
		{
			name: "run out",
			page: `<TD>Premium account expire</TD><TD><b>1 October 2026</b></TD>`,
			want: xfs.Account{Expires: time.Date(2026, 10, 1, 23, 59, 59, 0, time.UTC)},
		},
		{
			name: "free",
			page: `<tr><td>Account type:</td><td><b>Free</b></td></tr><tr><td>Traffic available today:</td><td><b>Unlimited</b></td></tr>`,
		},
	} {
		if got := xfs.ReadAccount(c.page, now); got != c.want {
			t.Errorf("%s: ReadAccount = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLinksOfTheSitesDomainsAreWrittenTheSameWay(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.net/k3m9x2p7q1ab":               "https://example.net/k3m9x2p7q1ab",
		"http://www.example.net/K3M9X2P7Q1AB/a.rar.html": "https://example.net/k3m9x2p7q1ab",
		"https://example.net/k3m9x2p7q1ab.html":          "https://example.net/k3m9x2p7q1ab",
		"https://example.net/?op=login":                  "",
		"https://example.net/k3m9x2p7q1abc":              "",
		"https://example.org/k3m9x2p7q1ab":               "",
	} {
		got, ok := site.Normalize(in)
		if got != want || ok != (want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
