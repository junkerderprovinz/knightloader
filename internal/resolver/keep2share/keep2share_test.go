package keep2share

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share/k2stest"
)

const (
	fileID   = "2cfa1607527dc"
	email    = "john@example.com"
	password = "hunter2"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct {
		host Host
		in   string
		want string
	}{
		{Keep2Share, "https://k2s.cc/file/" + fileID, "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "https://k2s.cc/file/" + fileID + "/Holiday.part1.rar", "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "http://www.keep2share.cc/file/" + fileID + "/a.zip?site=example.com", "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "https://keep2s.cc/f/" + fileID, "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "https://k2share.cc/preview/" + fileID, "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "https://keep2.cc/file/info/" + fileID, "https://keep2share.cc/file/" + fileID},
		{Keep2Share, "https://k2s.cc/file/9f3c1a4b7e2d0-b81c", "https://keep2share.cc/file/9f3c1a4b7e2d0-b81c"},
		{FileBoom, "https://fboom.me/file/" + fileID + "/clip.mp4", "https://fileboom.me/file/" + fileID},
		{FileBoom, "https://fileboom.me/file/" + fileID, "https://fileboom.me/file/" + fileID},
		{TezFiles, "https://tezfiles.com/file/" + fileID, "https://tezfiles.com/file/" + fileID},
		{TezFiles, "https://publish2.me/file/" + fileID + "/a.rar", "https://tezfiles.com/file/" + fileID},

		// A folder, the start page, a short id and another family member's
		// link are none of the host's files.
		{Keep2Share, "https://k2s.cc/folder/0123456789abc", ""},
		{Keep2Share, "https://k2s.cc/", ""},
		{Keep2Share, "https://k2s.cc/file/abc123", ""},
		{Keep2Share, "https://fboom.me/file/" + fileID, ""},
		{FileBoom, "https://k2s.cc/file/" + fileID, ""},
		{Keep2Share, "https://notk2s.cc/file/" + fileID, ""},
		{Keep2Share, "ftp://k2s.cc/file/" + fileID, ""},
		{TezFiles, "https://prx-214.tezfiles.com/6b50f2add0ba8/a8db134b2b824", ""},
	}
	for _, c := range cases {
		got, ok := c.host.Normalize(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s.Normalize(%q) = %q, %v; want %q", c.host.ID, c.in, got, ok, c.want)
		}
	}
}

func newServer(t *testing.T, acct k2stest.Account) *k2stest.Server {
	srv := k2stest.New(t, "k2s.cc")
	srv.AddAccount(acct)
	return srv
}

func premium() k2stest.Account {
	return k2stest.Account{Email: email, Password: password, Traffic: 50 << 30,
		Expires: time.Date(2030, 3, 1, 0, 0, 0, 0, time.UTC)}
}

// fakeEngine records what the backend hands on.
type fakeEngine struct {
	mu     sync.Mutex
	handed []handoff
	got    chan handoff
}

type handoff struct {
	url    string
	conns  int
	relink func(context.Context) (string, error)
}

func (f *fakeEngine) Handover(_, url string, conns int, relink func(context.Context) (string, error)) {
	h := handoff{url, conns, relink}
	f.mu.Lock()
	f.handed = append(f.handed, h)
	f.mu.Unlock()
	f.got <- h
}

func (f *fakeEngine) Pause(string)        {}
func (f *fakeEngine) Resume(string)       {}
func (f *fakeEngine) Remove(string, bool) {}

func (f *fakeEngine) handoffs() []handoff {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.handed)
}

// updates collects what the backend reports, and the waits it asks for.
type updates struct {
	mu    sync.Mutex
	all   []core.Update
	waits []time.Duration
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.all = append(u.all, up)
}

func (u *updates) sleep(ctx context.Context, d time.Duration) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.waits = append(u.waits, d)
	return ctx.Err()
}

func (u *updates) list() []core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.all)
}

func (u *updates) waited() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	var sum time.Duration
	for _, d := range u.waits {
		sum += d
	}
	return sum
}

func (u *updates) last() core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.all) == 0 {
		return core.Update{}
	}
	return u.all[len(u.all)-1]
}

func newBackend(srv *k2stest.Server) (*Backend, *fakeEngine, *updates) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{}
	b := NewBackend(Keep2Share, NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.Tasks.Sleep = ups.sleep
	return b, eng, ups
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
func awaitSettled(t *testing.T, ups *updates) core.Update {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if u := ups.last(); u.Status == core.StatusError || u.Status == core.StatusPaused {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the task never settled: %+v", ups.list())
	return core.Update{}
}

func TestALinkIsClaimedOnlyWhileAPremiumLoginIsThere(t *testing.T) {
	link := "https://k2s.cc/file/" + fileID
	cases := []struct {
		name  string
		acct  k2stest.Account
		login string
		want  bool
	}{
		{"premium", premium(), password, true},
		{"no login", premium(), "", false},
		{"never premium", k2stest.Account{Email: email, Password: password, Traffic: 10 << 30}, password, false},
		{"premium ran out", k2stest.Account{Email: email, Password: password, Expires: time.Now().Add(-time.Hour)}, password, false},
		{"wrong password", premium(), "letmein", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.acct)
			b, _, _ := newBackend(srv)
			if c.login != "" {
				b.SetLogin(email, c.login)
			}
			r := Resolver{Backend: b}
			if c.login != "" && !r.Match(link) {
				t.Error("a stored login that was not checked yet does not claim the link")
			}
			b.Check(context.Background())
			if got := r.Match(link); got != c.want {
				t.Errorf("Match after the check = %v, want %v", got, c.want)
			}
			if r.Match("https://fboom.me/file/" + fileID) {
				t.Error("a Keep2Share login claimed a FileBoom link")
			}
		})
	}
}

func TestResolvingAsksNothingAndAllowsSeveralConnections(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	got, err := Resolver{Backend: b}.Resolve(context.Background(), resolver.Request{URL: "https://k2s.cc/file/" + fileID + "/a.rar"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DirectURL != "https://keep2share.cc/file/"+fileID || got.Connections != maxConns {
		t.Errorf("resolved to %+v, want the written-out link and %d connections", got, maxConns)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("resolving asked Keep2Share %v; it runs under the app's lock", srv.Requests())
	}
}

func TestEveryLinkIsNamedWithoutALoginInOneQuestion(t *testing.T) {
	srv := k2stest.New(t, "k2s.cc")
	live := srv.Add(fileID, k2stest.File{Name: "Holiday.part1.rar", Body: make([]byte, 700<<10+3)})
	blocked := srv.Add("96a9d7445484c", k2stest.File{Name: "taken-down.mp4", Body: make([]byte, 10), Blocked: true})
	b, _, _ := newBackend(srv)

	got := Resolver{Backend: b}.Inspect(context.Background(), []string{
		live, "https://k2s.cc/file/0000000000000", blocked, "https://1fichier.com/?abc", "https://keep2share.cc/file/" + fileID + "/again.rar",
	})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Holiday.part1.rar", Size: 700<<10 + 3},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOffline},
		{},
		{Availability: core.AvailOnline, Name: "Holiday.part1.rar", Size: 700<<10 + 3},
	}
	if !slices.Equal(got, want) {
		t.Errorf("inspected %+v, want %+v", got, want)
	}
	if reqs := srv.Requests(); !slices.Equal(reqs, []string{"POST /api/v2/getFilesInfo"}) {
		t.Errorf("asked %v, want one getFilesInfo and no sign-in", reqs)
	}
}

func TestAPremiumDownloadIsHandedOnWithSeveralConnections(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, k2stest.File{Name: "Holiday.part1.rar", Body: make([]byte, 3<<20)})
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, maxConns)
	h := awaitHandoff(t, eng)

	if !strings.HasPrefix(h.url, srv.URL+"/download/"+fileID) || h.conns != maxConns {
		t.Errorf("handed %q with %d connections, want the download link with %d", h.url, h.conns, maxConns)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool {
		return u.Status == core.StatusRunning && u.Name == "Holiday.part1.rar" && u.Size == 3<<20
	}) {
		t.Errorf("the task was never named from the API: %+v", ups.list())
	}

	// The engine comes back for a fresh link when the first stops working.
	fresh, err := h.relink(context.Background())
	if err != nil || !strings.HasPrefix(fresh, srv.URL+"/download/"+fileID) || fresh == h.url {
		t.Errorf("relink = %q, %v; want a fresh download link", fresh, err)
	}
}

func TestAFileFetchedBeforeGoesOutAgainOnLessTrafficThanItsSize(t *testing.T) {
	acct := premium()
	acct.Traffic = 5 << 20
	srv := newServer(t, acct)
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 3<<20)})
	b, eng, _ := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	h := awaitHandoff(t, eng)
	// Keep2Share took the size off once. What is left is less than the
	// file, but the file was paid for.
	if _, err := h.relink(context.Background()); err != nil {
		t.Errorf("relink with 2 MB left for a 3 MB file Keep2Share already counted: %v", err)
	}
}

func TestTheTokenIsKeptAndRenewedWhenKeep2ShareForgetsIt(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, eng, _ := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	b.Download("t2", link, nil, 1)
	awaitHandoff(t, eng)
	if n := srv.Logins(); n != 1 {
		t.Errorf("two downloads signed in %d times, want once", n)
	}

	srv.Expire()
	b.Download("t3", link, nil, 1)
	awaitHandoff(t, eng)
	if n := srv.Logins(); n != 2 {
		t.Errorf("signed in %d times in all, want a second time once the token was forgotten", n)
	}
}

func TestTheAccountIsReportedAsKeep2ShareDescribesIt(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	checked := make(chan struct{}, 1)
	b.Checked = func() { checked <- struct{}{} }

	st := b.Check(context.Background())

	want := Status{Checked: true, Premium: true, Expiry: time.Date(2030, 3, 1, 0, 0, 0, 0, time.UTC), TrafficLeft: 50 << 30}
	if st != want || b.Status() != want {
		t.Errorf("checked as %+v, want %+v", st, want)
	}
	select {
	case <-checked:
	default:
		t.Error("Checked was not called")
	}
}

func TestTheExpiryReadsInEveryFormTheAPIWritesIt(t *testing.T) {
	cases := map[string]time.Time{
		`1898208000`:                 time.Date(2030, 2, 25, 0, 0, 0, 0, time.UTC),
		`"2030-02-25T00:00:00.000Z"`: time.Date(2030, 2, 25, 0, 0, 0, 0, time.UTC),
		`false`:                      {},
		`null`:                       {},
		`0`:                          {},
	}
	for raw, want := range cases {
		var e expiry
		if err := e.UnmarshalJSON([]byte(raw)); err != nil || !time.Time(e).Equal(want) {
			t.Errorf("%s reads as %v, %v; want %v", raw, time.Time(e), err, want)
		}
	}
}

func TestARefusedLoginFailsTheDownloadAsAuth(t *testing.T) {
	cases := []struct {
		name    string
		acct    k2stest.Account
		pass    string
		want    string
		captcha bool
	}{
		{"wrong password", premium(), "letmein", "email address and password", false},
		{"captcha", func() k2stest.Account { a := premium(); a.Captcha = true; return a }(), password, "browser", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.acct)
			link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
			b, eng, ups := newBackend(srv)
			b.SetLogin(email, c.pass)

			b.Download("t1", link, nil, 1)
			u := awaitSettled(t, ups)

			if u.Status != core.StatusError || u.Reason != core.ReasonAuth || !strings.Contains(u.Err, c.want) {
				t.Errorf("settled as %+v, want an auth failure naming %q", u, c.want)
			}
			if st := b.Status(); !st.Refused || !st.Checked || st.Captcha != c.captcha {
				t.Errorf("the login reads as %+v, want it refused with captcha %v", st, c.captcha)
			}
			if b.Ready() || len(eng.handoffs()) > 0 {
				t.Error("a refused login still downloads")
			}
			if n := srv.Logins(); n != 1 {
				t.Errorf("signed in %d times, want once: Keep2Share asks for captchas at addresses that keep trying", n)
			}
		})
	}
}

func TestAFreeAccountFailsSayingItNeedsPremium(t *testing.T) {
	srv := newServer(t, k2stest.Account{Email: email, Password: password, Traffic: 10 << 30})
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonAuth || !strings.Contains(u.Err, "premium") {
		t.Errorf("settled as %+v, want a failure naming premium", u)
	}
	if slices.Contains(srv.Requests(), "POST /api/v2/getUrl") {
		t.Error("a free account asked for a download link")
	}
}

func TestADeletedOrBlockedFileFailsAsGone(t *testing.T) {
	srv := newServer(t, premium())
	blocked := srv.Add("96a9d7445484c", k2stest.File{Name: "a.bin", Body: make([]byte, 10), Blocked: true})
	for _, link := range []string{"https://k2s.cc/file/0000000000000", blocked} {
		b, eng, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)

		if u.Status != core.StatusError || u.Reason != core.ReasonGone {
			t.Errorf("%s settled as %+v, want gone", link, u)
		}
		if len(eng.handoffs()) > 0 {
			t.Errorf("%s reached the engine", link)
		}
	}
}

func TestSpentTrafficFailsAsALimitAndBenchesTheLogin(t *testing.T) {
	acct := premium()
	acct.Traffic = 1000
	srv := newServer(t, acct)
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonLimit || !strings.Contains(u.Err, "traffic") {
		t.Errorf("settled as %+v, want a traffic limit", u)
	}
	if b.Ready() {
		t.Error("a login without traffic still claims links")
	}
}

func TestTroubleAtKeep2ShareFailsAsUnavailable(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("getUrl", http.StatusServiceUnavailable, 503, "Service temporarily unavailable", 0)
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonUnavailable || !strings.Contains(u.Err, "temporarily unavailable") {
		t.Errorf("settled as %+v, want Keep2Share unavailable in its own words", u)
	}
	if !b.Ready() {
		t.Error("a hiccup at Keep2Share took the login out of routing")
	}
}

func TestTooManySignInsWaitAndShowOnTheTask(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("login", http.StatusNotAcceptable, 71, "Login attempt was exceed, please wait or verify your request via captcha challenge", 0)
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := ups.waited(); got != loginWait {
		t.Errorf("waited %v, want %v", got, loginWait)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool { return strings.Contains(u.Note, "Keep2Share") }) {
		t.Errorf("the wait never showed: %+v", ups.list())
	}
}

func TestTheAPIsRefusalsReadAsTheirCause(t *testing.T) {
	cases := []struct {
		method string
		status int
		code   string
		sub    int
		want   error
	}{
		{"getUrl", 406, `20`, 0, ErrGone},
		{"getUrl", 406, `23`, 0, ErrGone},
		{"getUrl", 406, `22`, 0, ErrBlocked},
		{"getUrl", 400, `2`, 0, ErrGone},
		{"getUrl", 406, `2`, 0, ErrTraffic},
		{"getUrl", 406, `21`, 2, ErrTraffic},
		{"getUrl", 406, `42`, 7, ErrNotPremium},
		{"getUrl", 406, `21`, 3, ErrNotPremium},
		{"getUrl", 406, `21`, 8, ErrRestricted},
		{"getUrl", 406, `30`, 0, errSession},
		{"getUrl", 403, `10`, 0, errSession},
		{"getUrl", 403, `75`, 0, errSession},
		{"getUrl", 403, `73`, 0, ErrDenied},
		{"login", 403, `77`, 0, ErrDenied},
		{"login", 406, `71`, 0, ErrBusy},
		{"login", 400, `"captcha_need_wait"`, 0, ErrBusy},
	}
	for _, c := range cases {
		e := apiError{status: c.status, code: []byte(c.code), message: "x"}
		if c.sub != 0 {
			e.sub = c.sub
		}
		if got := e.classify(c.method); !errors.Is(got, c.want) {
			t.Errorf("%s %d errorCode %s/%d reads as %v, want %v", c.method, c.status, c.code, c.sub, got, c.want)
		}
	}
	var refused *LoginError
	if err := (apiError{status: 406, code: []byte(`70`)}).classify("login"); !errors.As(err, &refused) || refused.Captcha {
		t.Errorf("a wrong password reads as %v", err)
	}
	if err := (apiError{status: 406, code: []byte(`33`)}).classify("login"); !errors.As(err, &refused) || !refused.Captcha {
		t.Errorf("a captcha at sign-in reads as %v", err)
	}
}

func TestAFailureNamesTheSiteThatAnswered(t *testing.T) {
	srv := k2stest.New(t, "fboom.me")
	srv.AddAccount(premium())
	srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 10)})
	for link, want := range map[string]string{
		"https://fboom.me/file/0000000000000": "FileBoom: the file has been deleted or never existed",
		"https://fboom.me/file/" + fileID:     "FileBoom: the login was turned down, so check the email address and password",
	} {
		ups := &updates{}
		b := NewBackend(FileBoom, NewClient(srv.URL, srv.Client()), &fakeEngine{got: make(chan handoff, 1)}, ups.add)
		b.SetLogin(email, "letmein")

		b.Download("t1", link, nil, 1)
		if u := awaitSettled(t, ups); u.Err != want {
			t.Errorf("%s failed with %q, want %q", link, u.Err, want)
		}
	}
}
