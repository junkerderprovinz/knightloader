package ddownload

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload/ddltest"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://ddownload.com/k3m9x2p7q1ab", "https://ddownload.com/k3m9x2p7q1ab"},
		{"https://ddownload.com/k3m9x2p7q1ab/Isle_of_Dogs.mkv", "https://ddownload.com/k3m9x2p7q1ab"},
		{"https://ddownload.com/k3m9x2p7q1ab/Isle_of_Dogs.mkv.html", "https://ddownload.com/k3m9x2p7q1ab"},
		{"http://www.ddownload.com/K3M9X2P7Q1AB", "https://ddownload.com/k3m9x2p7q1ab"},
		{"https://ddl.to/k3m9x2p7q1ab", "https://ddownload.com/k3m9x2p7q1ab"},
		{"https://ddl.to/k3m9x2p7q1ab/a.bin", "https://ddownload.com/k3m9x2p7q1ab"},

		// The site's own pages, the download servers and other hosters are no
		// file pages.
		{"https://ddownload.com/", ""},
		{"https://ddownload.com/login", ""},
		{"https://ddownload.com/pricing", ""},
		{"https://ddownload.com/?op=my_account", ""},
		{"https://ddownload.com/k3m9x2p7q1abc", ""},
		{"https://www2012.dstorage.to:183/d/av6atdjbzid6/a.rar", ""},
		{"https://rapidgator.net/file/k3m9x2p7q1ab", ""},
		{"https://notddownload.com/k3m9x2p7q1ab", ""},
		{"ftp://ddownload.com/k3m9x2p7q1ab", ""},
	}
	r := Resolver{}
	for _, c := range cases {
		got, ok := Normalize(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
		if r.Match(c.in) != (c.want != "") {
			t.Errorf("Match(%q) = %v", c.in, c.want == "")
		}
	}
}

func TestResolvingAsksNothingAndAllowsOneConnection(t *testing.T) {
	srv := ddltest.New(t)
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	got, err := r.Resolve(context.Background(), resolver.Request{URL: "https://ddl.to/k3m9x2p7q1ab/a.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DirectURL != "https://ddownload.com/k3m9x2p7q1ab" || got.Connections != 1 {
		t.Errorf("resolved to %+v, want the written-out link and one connection", got)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("resolving asked ddownload %v; it runs under the app's lock", srv.Requests())
	}
}

func TestAPremiumLoginNamesTheHostItRanksFor(t *testing.T) {
	premium := false
	r := Resolver{Premium: func() bool { return premium }}
	if got := r.LoginHost(); got != "" {
		t.Errorf("LoginHost without premium = %q, want none", got)
	}
	premium = true
	if got := r.LoginHost(); got != "ddownload.com" {
		t.Errorf("LoginHost with premium = %q, want ddownload.com", got)
	}
}

func TestTheFilePageNamesAndSizesTheFile(t *testing.T) {
	inspectPause = 0
	srv := ddltest.New(t)
	live := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "Isle of Dogs (2018) 1080p.mkv", Body: make([]byte, 700<<10)})
	locked := srv.Add("pr3m1umonly0", ddltest.File{Name: "Fantastic Mr Fox.mkv", Body: make([]byte, 2<<10), PremiumOnly: true})
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}

	got := r.Inspect(context.Background(), []string{live, "https://ddownload.com/g0n3g0n3g0n3", locked, "https://rapidgator.net/file/abc"})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Isle of Dogs (2018) 1080p.mkv", Size: 700 << 10},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOnline, Name: "Fantastic Mr Fox.mkv", Size: 2 << 10},
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
		if got := parseSize(figure, unit); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

type fakeEngine struct {
	mu      sync.Mutex
	handed  []handoff
	got     chan handoff
	paused  []string
	removed []string
}

type handoff struct {
	url    string
	conns  int
	relink func(context.Context) (string, error)
}

func newFakeEngine() *fakeEngine { return &fakeEngine{got: make(chan handoff, 4)} }

func (f *fakeEngine) Handover(_, url string, conns int, relink func(context.Context) (string, error)) {
	h := handoff{url, conns, relink}
	f.mu.Lock()
	f.handed = append(f.handed, h)
	f.mu.Unlock()
	f.got <- h
}

func (f *fakeEngine) Pause(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, id)
}

func (f *fakeEngine) Resume(string) {}

func (f *fakeEngine) Remove(id string, _ bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
}

func (f *fakeEngine) handoffs() []handoff {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.handed)
}

// harness collects what the backend reports, the waits it asks for and the
// captchas it wants answered, and holds the stored login and session.
type harness struct {
	mu      sync.Mutex
	all     []core.Update
	waits   []time.Duration
	asked   []string
	checked []LoginState
	session string
	// block makes every wait last until it is called off.
	block   bool
	waiting chan struct{}
	// answer is what a captcha gets answered with, "" to skip it.
	answer string
	user   string
	pass   string
}

func (h *harness) add(_ string, up core.Update) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.all = append(h.all, up)
}

func (h *harness) sleep(ctx context.Context, d time.Duration) error {
	h.mu.Lock()
	h.waits = append(h.waits, d)
	block := h.block
	h.mu.Unlock()
	if !block {
		return ctx.Err()
	}
	h.waiting <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func (h *harness) solve(_ context.Context, taskID, pageURL, siteKey string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, taskID+" "+pageURL+" "+siteKey)
	if h.answer == "" {
		return "", errors.New("skipped")
	}
	return h.answer, nil
}

func (h *harness) login() (string, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.user, h.pass, h.user != ""
}

func (h *harness) list() []core.Update {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.all)
}

func (h *harness) last() core.Update {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.all) == 0 {
		return core.Update{}
	}
	return h.all[len(h.all)-1]
}

func (h *harness) waited() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	var sum time.Duration
	for _, d := range h.waits {
		sum += d
	}
	return sum
}

func (h *harness) captchas() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.asked)
}

func (h *harness) lastCheck() LoginState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.checked) == 0 {
		return LoginState{}
	}
	return h.checked[len(h.checked)-1]
}

func newBackend(srv *ddltest.Server) (*Backend, *fakeEngine, *harness) {
	eng := newFakeEngine()
	h := &harness{waiting: make(chan struct{}, 1), answer: ddltest.Token}
	b := NewBackend(NewClient(srv.URL, srv.Client()), eng, h.add)
	b.sleep = h.sleep
	b.Solve = h.solve
	b.Login = h.login
	b.LoadSession = func() string {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.session
	}
	b.SaveSession = func(s string) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.session = s
	}
	b.OnLogin = func(st LoginState) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.checked = append(h.checked, st)
	}
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

// awaitSettled waits for the backend's last word on a task that does not
// reach the engine.
func awaitSettled(t *testing.T, h *harness) core.Update {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if u := h.last(); u.Status == core.StatusError || u.Status == core.StatusPaused {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the task never settled: %+v", h.list())
	return core.Update{}
}

func TestAFreeDownloadAnswersTheCaptchaSitsOutTheCountdownAndHandsTheLinkOn(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 4096)})
	srv.Countdown(60)
	b, eng, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	got := awaitHandoff(t, eng)

	if got.url != srv.URL+"/d/k3m9x2p7q1ab/Isle%20of%20Dogs.mkv" || got.conns != 1 {
		t.Errorf("handed on %q for %d connections, want the download link for one", got.url, got.conns)
	}
	want := "t1 " + srv.URL + "/k3m9x2p7q1ab " + ddltest.SiteKey
	if asked := h.captchas(); len(asked) != 1 || asked[0] != want {
		t.Errorf("captchas asked %q, want %q", asked, want)
	}
	if w := h.waited(); w < 59*time.Second || w > 60*time.Second {
		t.Errorf("waited %s before the form went in, want the countdown's minute", w)
	}
	posts := srv.Posts()
	if len(posts) != 1 || posts[0].Get("op") != "download2" || posts[0].Get("id") != "k3m9x2p7q1ab" || posts[0].Get("cf-turnstile-response") != ddltest.Token {
		t.Errorf("the form went in as %v, want download2 with the captcha's answer", posts)
	}
	if u := h.list(); u[0].Name != "Isle of Dogs.mkv" || u[0].Size != 4096 {
		t.Errorf("first update %+v, want the file named and sized", u[0])
	}
	if b.Waiting("t1") {
		t.Error("the task still counts as waiting after the hand-over")
	}
	if b.Premium() {
		t.Error("a backend without a login reads as premium")
	}
}

func TestAWaitBetweenFreeDownloadsShowsOnTheTaskAndIsSatOut(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Wait(3725)
	b, eng, h := newBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if w := h.waited(); w != 3725*time.Second {
		t.Errorf("waited %s, want the 1 h 2 min 5 s the page asked for", w)
	}
	if !slices.ContainsFunc(h.list(), func(u core.Update) bool {
		return u.Status == core.StatusRunning && u.Note == "Waiting for ddownload: 1 h 3 min left"
	}) {
		t.Errorf("no update showed the wait: %+v", h.list())
	}
}

func TestEveryFailureNamesItsCause(t *testing.T) {
	for _, c := range []struct {
		name   string
		file   ddltest.File
		id     string
		answer string
		pass   string
		want   core.Reason
	}{
		{name: "deleted", id: "g0n3g0n3g0n3", want: core.ReasonGone},
		{name: "premium only", file: ddltest.File{Name: "a.bin", PremiumOnly: true}, want: core.ReasonAuth},
		{name: "no password", file: ddltest.File{Name: "a.bin", Password: "hunter2"}, want: core.ReasonAuth},
		{name: "wrong password", file: ddltest.File{Name: "a.bin", Password: "hunter2"}, pass: "hunter3", want: core.ReasonAuth},
		{name: "captcha skipped", file: ddltest.File{Name: "a.bin"}, answer: "-", want: core.ReasonCaptcha},
		{name: "captcha wrong", file: ddltest.File{Name: "a.bin"}, answer: "nope", want: core.ReasonCaptcha},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := ddltest.New(t)
			link := "https://ddownload.com/" + c.id
			if c.id == "" {
				link = srv.Add("k3m9x2p7q1ab", c.file)
			}
			b, eng, h := newBackend(srv)
			switch c.answer {
			case "-":
				h.answer = ""
			case "":
			default:
				h.answer = c.answer
			}
			b.Password = func(string) string { return c.pass }

			b.Download("t1", link, nil, 1)
			u := awaitSettled(t, h)
			if u.Status != core.StatusError || u.Reason != c.want {
				t.Errorf("settled as %q, %q (%s); want an error for %q", u.Status, u.Reason, u.Err, c.want)
			}
			if len(eng.handoffs()) > 0 {
				t.Error("a link was handed on")
			}
		})
	}
}

func TestAPasswordGoesIntoThePasswordField(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024), Password: "hunter2"})
	b, eng, _ := newBackend(srv)
	b.Password = func(string) string { return "hunter2" }

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if posts := srv.Posts(); posts[0].Get("password") != "hunter2" {
		t.Errorf("the form went in as %v, want the download password", posts)
	}
}

func signedIn(t *testing.T, srv *ddltest.Server, a ddltest.Account) (*Backend, *fakeEngine, *harness) {
	t.Helper()
	srv.AddAccount("fox", a)
	b, eng, h := newBackend(srv)
	h.user, h.pass = "fox", a.Password
	b.CheckLogin(context.Background(), true)
	return b, eng, h
}

func TestAPremiumLoginSignsInOnceAndTakesTheLinkTheFilePageSendsOn(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 4096)})
	until := time.Now().AddDate(0, 3, 0)
	b, eng, h := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: until, TrafficMB: 150000})

	st := h.lastCheck()
	if st.Refused || st.Err != nil || !st.Premium || st.Expires.Format(time.DateOnly) != until.Format(time.DateOnly) {
		t.Fatalf("the check found %+v, want premium until %s", st, until.Format(time.DateOnly))
	}
	if st.TrafficLeft != 150000e6 || st.TrafficMax != 200000e6 {
		t.Errorf("traffic %d of %d, want 150000 of 200000 MB", st.TrafficLeft, st.TrafficMax)
	}
	if want := " " + srv.URL + "/login " + ddltest.SiteKey; !slices.Equal(h.captchas(), []string{want}) {
		t.Errorf("captchas asked %q, want the sign-in's with no task", h.captchas())
	}
	if !b.Premium() {
		t.Fatal("a confirmed premium login does not read as premium")
	}

	b.Download("t1", link, nil, 1)
	got := awaitHandoff(t, eng)
	if got.url != srv.URL+"/d/k3m9x2p7q1ab/Isle%20of%20Dogs.mkv" || got.conns != 1 {
		t.Errorf("handed on %q for %d connections, want the download link", got.url, got.conns)
	}
	if srv.SignIns() != 1 || len(h.captchas()) != 1 {
		t.Errorf("signed in %d times with %d captchas, want the one sign-in", srv.SignIns(), len(h.captchas()))
	}
	if !strings.Contains(h.session, "xfss=") {
		t.Errorf("the saved session %q holds no session cookie", h.session)
	}
}

func TestAPremiumAccountWithoutDirectDownloadsSendsTheForm(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, _ := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000, FormDownloads: true})

	b.Download("t1", link, nil, 1)
	if got := awaitHandoff(t, eng); got.url != srv.URL+"/d/k3m9x2p7q1ab/a.bin" {
		t.Errorf("handed on %q, want the link on the form's answer", got.url)
	}
	if posts := srv.Posts(); len(posts) != 1 || posts[0].Get("cf-turnstile-response") != "" {
		t.Errorf("the form went in as %v, want it once and without a captcha", posts)
	}
}

func TestAWrongLoginIsReportedAndLeavesTheDownloadsFree(t *testing.T) {
	srv := ddltest.New(t)
	srv.AddAccount("fox", ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})
	b, _, h := newBackend(srv)
	h.user, h.pass = "fox", "weasel"

	b.CheckLogin(context.Background(), true)
	if st := h.lastCheck(); !st.Refused {
		t.Errorf("the check found %+v, want the login refused", st)
	}
	if b.Premium() {
		t.Error("a refused login reads as premium")
	}
}

func TestAnAccountWhosePremiumRanOutIsNoPremium(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, h := signedIn(t, srv, ddltest.Account{Password: "badger"})

	if st := h.lastCheck(); st.Refused || st.Err != nil || st.Premium {
		t.Errorf("the check found %+v, want a free account", st)
	}
	if b.Premium() {
		t.Fatal("an expired premium reads as premium")
	}
	// Without premium the download goes the free way.
	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if len(srv.Posts()) != 1 || srv.Posts()[0].Get("cf-turnstile-response") != ddltest.Token {
		t.Errorf("posts %v, want the free form with the captcha's answer", srv.Posts())
	}
}

func TestASessionThatRanOutIsSignedInAgain(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, h := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})
	srv.SignOutAll()

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if srv.SignIns() != 2 {
		t.Errorf("signed in %d times, want once more after the session ran out", srv.SignIns())
	}
	if asked := h.captchas(); len(asked) != 2 || !strings.HasPrefix(asked[1], "t1 ") {
		t.Errorf("captchas asked %q, want the second sign-in's on the task", asked)
	}
}

func TestASavedSessionNeedsNoNewSignIn(t *testing.T) {
	srv := ddltest.New(t)
	_, _, first := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})

	b, _, h := newBackend(srv)
	h.user, h.pass, h.session = "fox", "badger", first.session
	b.CheckLogin(context.Background(), false)
	if !b.Premium() || srv.SignIns() != 1 || len(h.captchas()) != 0 {
		t.Errorf("premium %v after %d sign-ins and %d captchas, want the saved session used as it is",
			b.Premium(), srv.SignIns(), len(h.captchas()))
	}
}

func TestACheckThatMaySignInNoMoreLeavesAMissingSessionAlone(t *testing.T) {
	srv := ddltest.New(t)
	srv.AddAccount("fox", ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})
	b, _, h := newBackend(srv)
	h.user, h.pass = "fox", "badger"

	b.CheckLogin(context.Background(), false)
	if srv.SignIns() != 0 || len(h.captchas()) != 0 {
		t.Errorf("a check that may not sign in signed in %d times", srv.SignIns())
	}
}

func TestUsedUpTrafficFailsTheDownloadAsALimit(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, _, h := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 10})
	srv.TrafficOut()

	b.Download("t1", link, nil, 1)
	if u := awaitSettled(t, h); u.Reason != core.ReasonLimit {
		t.Errorf("settled as %q (%s), want a limit", u.Reason, u.Err)
	}
	if b.Premium() {
		t.Error("a login with its traffic used up still reads as premium")
	}
	if st := h.lastCheck(); st.TrafficLeft > 0 || st.TrafficMax == 0 {
		t.Errorf("the login was reported as %+v, want its traffic used up", st)
	}
}

func TestForgettingTheLoginEndsThePremium(t *testing.T) {
	srv := ddltest.New(t)
	b, _, h := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})

	b.Forget()
	if b.Premium() || h.session != "" {
		t.Errorf("premium %v with session %q after Forget, want neither", b.Premium(), h.session)
	}
}

func TestPausingOrRemovingDuringAWaitCallsItOff(t *testing.T) {
	for _, stop := range []string{"pause", "remove"} {
		t.Run(stop, func(t *testing.T) {
			srv := ddltest.New(t)
			link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
			srv.Wait(90)
			b, eng, h := newBackend(srv)
			h.block = true

			b.Download("t1", link, nil, 1)
			<-h.waiting
			if !b.Waiting("t1") {
				t.Error("a task in a wait does not count as waiting")
			}
			if stop == "pause" {
				b.Pause("t1")
			} else {
				b.Remove("t1", true)
			}

			deadline := time.Now().Add(5 * time.Second)
			for b.Waiting("t1") && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if b.Waiting("t1") {
				t.Fatal("the wait went on after it was called off")
			}
			if len(eng.handoffs()) > 0 {
				t.Error("a link was handed on after the wait was called off")
			}
			for _, u := range h.list() {
				if u.Status == core.StatusError {
					t.Errorf("calling the wait off reported a failure: %s", u.Err)
				}
			}
			if stop == "pause" {
				if got := h.last().Status; got != core.StatusPaused {
					t.Errorf("last status %q, want paused", got)
				}
				h.mu.Lock()
				h.block = false
				h.mu.Unlock()
				b.Resume("t1")
				awaitHandoff(t, eng)
			}
		})
	}
}

func TestPauseAndRemoveReachTheEngineOnceTheLinkIsHandedOn(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, _ := newBackend(srv)

	b.Download("t1", link, nil, 1)
	h := awaitHandoff(t, eng)
	b.Pause("t1")
	b.Remove("t1", true)
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if !slices.Equal(eng.paused, []string{"t1"}) || !slices.Equal(eng.removed, []string{"t1"}) {
		t.Errorf("engine saw pause %v and remove %v, want both for t1", eng.paused, eng.removed)
	}
	if h.relink == nil {
		t.Error("no way to fetch a fresh link was handed on")
	}
}

func TestAPremiumThatRanOutSinceTheCheckFailsTheDownloadAndEndsThePremium(t *testing.T) {
	srv := ddltest.New(t)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, h := signedIn(t, srv, ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})
	srv.AddAccount("fox", ddltest.Account{Password: "badger"})

	b.Download("t1", link, nil, 1)
	if u := awaitSettled(t, h); u.Reason != core.ReasonAuth || !strings.Contains(u.Err, "premium") {
		t.Errorf("settled as %q (%s), want the premium named as run out", u.Reason, u.Err)
	}
	if b.Premium() || h.lastCheck().Premium {
		t.Error("the login still reads as premium")
	}
	if srv.SignIns() != 1 || len(eng.handoffs()) > 0 {
		t.Errorf("%d sign-ins and %d hand-overs, want the one sign-in and none", srv.SignIns(), len(eng.handoffs()))
	}
}
