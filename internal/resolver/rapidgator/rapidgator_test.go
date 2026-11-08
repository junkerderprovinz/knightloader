package rapidgator

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator/rgtest"
)

const (
	fileID   = "0001234422b3e37d648ea406d833c1d6"
	email    = "john@example.com"
	password = "hunter2"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://rapidgator.net/file/" + fileID, "https://rapidgator.net/file/" + fileID},
		{"https://rapidgator.net/file/" + fileID + "/Course.part69.rar.html", "https://rapidgator.net/file/" + fileID},
		{"http://www.rapidgator.net/file/" + fileID + "/Course.part69.rar", "https://rapidgator.net/file/" + fileID},
		{"https://rg.to/file/" + fileID + "/a.zip.html", "https://rapidgator.net/file/" + fileID},
		{"https://rapidgator.asia/file/" + fileID, "https://rapidgator.net/file/" + fileID},
		{"https://rapidgator.net/file/0001234422B3E37D648EA406D833C1D6", "https://rapidgator.net/file/" + fileID},
		{"http://rapidgator.net/file/10126555/ElBatallon.avi.html", "https://rapidgator.net/file/10126555"},

		// A folder, the start page and the download servers are no file pages.
		{"https://rapidgator.net/folder/7114133/Season1.html", ""},
		{"https://rapidgator.net/", ""},
		{"https://rapidgator.net/file/", ""},
		{"https://rapidgator.net/file/49b1b874", ""},
		{"https://pr3.rapidgator.net//?r=download/index&session_id=V7Zk", ""},
		{"https://1fichier.com/?k3m9x2p7q1", ""},
		{"https://notrapidgator.net/file/" + fileID, ""},
		{"ftp://rapidgator.net/file/" + fileID, ""},
	}
	for _, c := range cases {
		got, ok := Normalize(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
	}
}

func newServer(t *testing.T, acct rgtest.Account) *rgtest.Server {
	srv := rgtest.New(t)
	srv.AddAccount(acct)
	return srv
}

func premium() rgtest.Account {
	return rgtest.Account{Email: email, Password: password, Premium: true,
		TrafficTotal: 1 << 40, TrafficLeft: 1 << 40, PremiumEnd: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)}
}

func TestALinkIsClaimedOnlyWhileAPremiumLoginIsThere(t *testing.T) {
	link := "https://rapidgator.net/file/" + fileID
	cases := []struct {
		name  string
		acct  rgtest.Account
		login string
		want  bool
	}{
		{"premium", premium(), password, true},
		{"no login", premium(), "", false},
		{"free account", rgtest.Account{Email: email, Password: password}, password, false},
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
			if r.Match("https://rapidgator.net/folder/7114133/Season1.html") {
				t.Error("a folder link was claimed")
			}
		})
	}
}

func TestResolvingAsksNothingAndAllowsSeveralConnections(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	got, err := Resolver{Backend: b}.Resolve(context.Background(), resolver.Request{URL: "https://rg.to/file/" + fileID + "/a.rar.html"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DirectURL != "https://rapidgator.net/file/"+fileID || got.Connections != maxConns {
		t.Errorf("resolved to %+v, want the written-out link and %d connections", got, maxConns)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("resolving asked Rapidgator %v; it runs under the app's lock", srv.Requests())
	}
}

func TestWithoutALoginThePublicPageNamesTheFile(t *testing.T) {
	srv := rgtest.New(t)
	live := srv.Add(fileID, rgtest.File{Name: "Course.part69.rar", Body: make([]byte, 700<<10)})
	b, _, _ := newBackend(srv)

	got := Resolver{Backend: b}.Inspect(context.Background(), []string{
		live, "https://rapidgator.net/file/ffffffffffffffffffffffffffffffff", "https://1fichier.com/?abc",
	})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Course.part69.rar", Size: 700 << 10},
		{Availability: core.AvailOffline},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("inspected %+v, want %+v", got, want)
	}
	for _, req := range srv.Requests() {
		if strings.Contains(req, "/api/") {
			t.Errorf("the API was asked without a login: %s", req)
		}
	}
}

func TestTheFilePageIsReadAsTheSiteWritesIt(t *testing.T) {
	// Taken from a live page, whitespace and all.
	const body = `<title>Download file UD-Course1080p2026-2.part69.rar</title>    <!--[if lt IE 8]>
                    <strong>
                        Downloading:
                    </strong>
                    <a href="">
                    UD-Course1080p2026-2.part69.rar                    </a>
                    <br/>
                </p>
                <div>
                    File size:
                    <strong>1 GB</strong>`
	p := readPage(body)
	if p.name != "UD-Course1080p2026-2.part69.rar" || p.size != 1<<30 {
		t.Errorf("read %+v", p)
	}
	if p := readPage(`<title>Download file a &amp; b.zip</title>`); p.name != "a & b.zip" {
		t.Errorf("the title alone reads as %q", p.name)
	}
	sizes := map[string]int64{"831 B": 831, "12.5 KB": 12800, "700 MB": 700 << 20, "1.37 GB": 1471026299}
	for text, want := range sizes {
		if got := readPage("File size:\n <strong>" + text + "</strong>").size; got != want {
			t.Errorf("%s reads as %d, want %d", text, got, want)
		}
	}
}

func TestWithALoginTheAPINamesTheFileToTheByte(t *testing.T) {
	srv := newServer(t, premium())
	live := srv.Add(fileID, rgtest.File{Name: "Course.part69.rar", Body: make([]byte, 700<<10+3)})
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	got := Resolver{Backend: b}.Inspect(context.Background(), []string{live, "https://rapidgator.net/file/ffffffffffffffffffffffffffffffff"})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Course.part69.rar", Size: 700<<10 + 3},
		{Availability: core.AvailOffline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("inspected %+v, want %+v", got, want)
	}
	for _, req := range srv.Requests() {
		if strings.HasPrefix(req, "GET /file/") {
			t.Errorf("a page was read although the API answers: %s", req)
		}
	}
}

// fakeEngine records what the backend hands on.
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

// updates collects what the backend reports, and the waits it asks for.
type updates struct {
	mu    sync.Mutex
	all   []core.Update
	waits []time.Duration
	// block makes every wait last until it is called off.
	block   bool
	waiting chan struct{}
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.all = append(u.all, up)
}

func (u *updates) sleep(ctx context.Context, d time.Duration) error {
	u.mu.Lock()
	u.waits = append(u.waits, d)
	block := u.block
	u.mu.Unlock()
	if !block {
		return ctx.Err()
	}
	u.waiting <- struct{}{}
	<-ctx.Done()
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

func newBackend(srv *rgtest.Server) (*Backend, *fakeEngine, *updates) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{waiting: make(chan struct{}, 1)}
	b := NewBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.sleep = ups.sleep
	b.inspectPause = 0
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

func TestAPremiumDownloadIsHandedOnWithSeveralConnections(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "Course.part69.rar", Body: make([]byte, 3<<20)})
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, maxConns)
	h := awaitHandoff(t, eng)

	if !strings.HasPrefix(h.url, srv.URL+"/download/"+fileID) || h.conns != maxConns {
		t.Errorf("handed %q with %d connections, want the download link with %d", h.url, h.conns, maxConns)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool {
		return u.Status == core.StatusRunning && u.Name == "Course.part69.rar" && u.Size == 3<<20
	}) {
		t.Errorf("the task was never named from the API: %+v", ups.list())
	}
	for _, q := range srv.Queries() {
		if strings.Contains(q, password) || strings.Contains(q, "token") {
			t.Errorf("a secret went out in a query string: %s", q)
		}
	}

	// The engine comes back for a fresh link when the first stops working.
	fresh, err := h.relink(context.Background())
	if err != nil || !strings.HasPrefix(fresh, srv.URL+"/download/"+fileID) || fresh == h.url {
		t.Errorf("relink = %q, %v; want a fresh download link", fresh, err)
	}
}

func TestASmallFileGetsOneConnection(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.nfo", Body: make([]byte, 4096)})
	b, eng, _ := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, maxConns)
	if h := awaitHandoff(t, eng); h.conns != 1 {
		t.Errorf("a 4 KB file went out on %d connections, want one", h.conns)
	}
}

func TestTheSessionIsKeptAndRenewedWhenRapidgatorEndsIt(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
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
		t.Errorf("signed in %d times in all, want a second time once the session ended", n)
	}
}

func TestTheAccountIsReportedAsRapidgatorDescribesIt(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	checked := make(chan struct{}, 1)
	b.Checked = func() { checked <- struct{}{} }

	st := b.Check(context.Background())

	want := Status{Checked: true, Premium: true, Expiry: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC),
		TrafficLeft: 1 << 40, TrafficMax: 1 << 40}
	if st != want || b.Status() != want {
		t.Errorf("checked as %+v, want %+v", st, want)
	}
	select {
	case <-checked:
	default:
		t.Error("Checked was not called")
	}
	if b.SetLogin(email, password) {
		t.Error("storing the same login again counts as a change")
	}
	if !b.SetLogin(email, "changed") || b.Status().Checked {
		t.Error("a changed password kept the old check")
	}
}

func TestARefusedLoginFailsTheDownloadAsAuth(t *testing.T) {
	cases := []struct {
		name string
		acct rgtest.Account
		pass string
		want string
	}{
		{"wrong password", premium(), "letmein", "email address and password"},
		{"two-factor", func() rgtest.Account { a := premium(); a.TwoFactor = true; return a }(), password, "two-factor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.acct)
			link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
			b, eng, ups := newBackend(srv)
			b.SetLogin(email, c.pass)

			b.Download("t1", link, nil, 1)
			u := awaitSettled(t, ups)

			if u.Status != core.StatusError || u.Reason != core.ReasonAuth || !strings.Contains(u.Err, c.want) {
				t.Errorf("settled as %+v, want an auth failure naming %q", u, c.want)
			}
			if st := b.Status(); !st.Refused || !st.Checked {
				t.Errorf("the login reads as %+v, want it refused", st)
			}
			if b.Ready() || len(eng.handoffs()) > 0 {
				t.Error("a refused login still downloads")
			}
			if n := srv.Logins(); n != 1 {
				t.Errorf("signed in %d times, want once: Rapidgator blocks addresses that keep guessing", n)
			}
		})
	}
}

func TestAFreeAccountFailsSayingItNeedsPremium(t *testing.T) {
	srv := newServer(t, rgtest.Account{Email: email, Password: password})
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonAuth || !strings.Contains(u.Err, "premium") {
		t.Errorf("settled as %+v, want a failure naming premium", u)
	}
	if slices.Contains(srv.Requests(), "POST /api/v2/file/download") {
		t.Error("a free account asked for a premium download")
	}
	if b.Ready() {
		t.Error("a free account still claims links")
	}
}

func TestADeletedFileFailsAsGone(t *testing.T) {
	srv := newServer(t, premium())
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", "https://rapidgator.net/file/ffffffffffffffffffffffffffffffff", nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonGone {
		t.Errorf("settled as %+v, want gone", u)
	}
	if len(eng.handoffs()) > 0 {
		t.Error("a deleted file reached the engine")
	}
}

func TestTrafficThatRunsOutFailsAsALimitAndBenchesTheLogin(t *testing.T) {
	t.Run("too little left for the file", func(t *testing.T) {
		acct := premium()
		acct.TrafficLeft = 1000
		srv := newServer(t, acct)
		link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
		b, _, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)

		if u.Status != core.StatusError || u.Reason != core.ReasonLimit || !strings.Contains(u.Err, "traffic") {
			t.Errorf("settled as %+v, want a traffic limit", u)
		}
		if slices.Contains(srv.Requests(), "POST /api/v2/file/download") {
			t.Error("a download link was asked for a file the traffic does not cover")
		}
	})
	t.Run("Rapidgator says so", func(t *testing.T) {
		srv := newServer(t, premium())
		link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
		srv.Fail("file/download", http.StatusLocked, "Error: Exceeded traffic")
		b, _, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)

		if u.Status != core.StatusError || u.Reason != core.ReasonLimit {
			t.Errorf("settled as %+v, want a traffic limit", u)
		}
		if b.Ready() {
			t.Error("a login without traffic still claims links")
		}
	})
}

func TestTroubleAtRapidgatorFailsAsUnavailable(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("file/download", http.StatusServiceUnavailable, "Download temporarily unavailable")
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonUnavailable || !strings.Contains(u.Err, "temporarily unavailable") {
		t.Errorf("settled as %+v, want Rapidgator unavailable in its own words", u)
	}
	if !b.Ready() {
		t.Error("a hiccup at Rapidgator took the login out of routing")
	}
}

func TestAnAnswerTheClientCannotPlaceGivesTheLoginAndTheDownloadToJDownloader(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("file/download", http.StatusBadRequest, "Error: Something the API never said before")
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if !u.Unsupported || u.Takeover != "Rapidgator" {
		t.Errorf("settled as %+v, want the download handed on with Rapidgator named", u)
	}
	if st := b.Status(); !st.Handed || b.Ready() || len(eng.handoffs()) > 0 {
		t.Errorf("the login reads as %+v, want it with JDownloader and out of routing", st)
	}
}

func TestAWrongPasswordIsNotHandedToJDownloader(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, "letmein")
	if st := b.Check(context.Background()); !st.Refused || st.Handed {
		t.Errorf("the login reads as %+v, want it refused and kept from JDownloader", st)
	}
}

func TestWaitsRapidgatorAsksForShowOnTheTask(t *testing.T) {
	t.Run("frequent logins", func(t *testing.T) {
		srv := newServer(t, premium())
		link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
		srv.Fail("user/login", http.StatusBadRequest, "Error: Please wait")
		b, eng, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		awaitHandoff(t, eng)

		if got := ups.waited(); got != loginWait {
			t.Errorf("waited %v, want %v", got, loginWait)
		}
		if !slices.ContainsFunc(ups.list(), func(u core.Update) bool { return strings.Contains(u.Note, "Rapidgator") }) {
			t.Errorf("the wait never showed: %+v", ups.list())
		}
	})
	t.Run("a delay on the link", func(t *testing.T) {
		srv := newServer(t, premium())
		link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
		srv.Delay(30)
		b, eng, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		awaitHandoff(t, eng)

		if got := ups.waited(); got != 30*time.Second {
			t.Errorf("waited %v, want the 30 seconds the link names", got)
		}
		if b.Waiting("t1") {
			t.Error("the task still counts as waiting after its link was handed on")
		}
	})
}

func TestPausingOrRemovingDuringAWaitCallsItOff(t *testing.T) {
	for _, stop := range []string{"pause", "remove"} {
		t.Run(stop, func(t *testing.T) {
			srv := newServer(t, premium())
			link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
			srv.Delay(90)
			b, eng, ups := newBackend(srv)
			b.SetLogin(email, password)
			ups.block = true

			b.Download("t1", link, nil, 1)
			<-ups.waiting
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
			if stop == "pause" {
				if got := ups.last().Status; got != core.StatusPaused {
					t.Errorf("last status %q, want paused", got)
				}
				srv.Delay(0)
				ups.mu.Lock()
				ups.block = false
				ups.mu.Unlock()
				b.Resume("t1")
				awaitHandoff(t, eng)
			}
		})
	}
}

func TestPauseAndRemoveReachTheEngineOnceTheLinkIsHandedOn(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, eng, _ := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	b.Pause("t1")
	b.Remove("t1", true)

	eng.mu.Lock()
	defer eng.mu.Unlock()
	if !slices.Equal(eng.paused, []string{"t1"}) || !slices.Equal(eng.removed, []string{"t1"}) {
		t.Errorf("the engine saw pauses %v and removals %v", eng.paused, eng.removed)
	}
}

func TestAWaitResumedAfterAPauseStillCountsAsWaitingWhenThePausedOneWindsDown(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Delay(90)
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	waiting := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	b.sleep = func(ctx context.Context, _ time.Duration) error {
		first := calls.Add(1) == 1
		waiting <- struct{}{}
		<-ctx.Done()
		if first {
			// The paused run lingers until the resumed one is waiting too.
			<-release
		}
		return ctx.Err()
	}
	t.Cleanup(func() { b.Remove("t1", false) })

	b.Download("t1", link, nil, 1)
	<-waiting
	b.Pause("t1")
	if b.Waiting("t1") {
		t.Error("a paused task still counts as waiting")
	}
	b.Resume("t1")
	<-waiting
	close(release)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !b.Waiting("t1") {
			t.Fatal("the paused run winding down took the resumed run's wait with it")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
