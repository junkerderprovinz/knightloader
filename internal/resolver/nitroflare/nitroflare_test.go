package nitroflare

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
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare/nftest"
)

const (
	fileID   = "CDB87F04326E3EE"
	email    = "john@example.com"
	password = "hunter2"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://nitroflare.com/view/" + fileID, "https://nitroflare.com/view/" + fileID},
		{"https://nitroflare.com/view/" + fileID + "/Holiday.part1.rar", "https://nitroflare.com/view/" + fileID},
		{"http://www.nitroflare.com/view/" + fileID + "/", "https://nitroflare.com/view/" + fileID},
		{"https://nitroflare.com/watch/" + fileID, "https://nitroflare.com/view/" + fileID},
		{"https://nitroflare.net/view/" + fileID, "https://nitroflare.com/view/" + fileID},
		{"https://nitro.download/view/" + fileID + "/a.zip", "https://nitroflare.com/view/" + fileID},

		// A folder, the start page and the download servers are no file pages.
		{"https://nitroflare.com/folder/1234/QmFja3Vw", ""},
		{"https://nitroflare.com/", ""},
		{"https://nitroflare.com/view/", ""},
		{"https://nitroflare.com/view/cdb87f04326e3ee", ""},
		{"https://s41.nitroflare.com/d/" + fileID + "/a.rar", ""},
		{"https://notnitroflare.com/view/" + fileID, ""},
		{"ftp://nitroflare.com/view/" + fileID, ""},
	}
	for _, c := range cases {
		got, ok := Normalize(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
	}
}

func newServer(t *testing.T, acct nftest.Account) *nftest.Server {
	srv := nftest.New(t)
	srv.AddAccount(acct)
	return srv
}

func premium() nftest.Account {
	return nftest.Account{Email: email, Password: password, TrafficMax: 100 << 30, TrafficLeft: 80 << 30,
		Expires: time.Date(2030, 3, 1, 12, 30, 0, 0, time.UTC)}
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

// updates collects what the backend reports.
type updates struct {
	mu  sync.Mutex
	all []core.Update
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.all = append(u.all, up)
}

func (u *updates) list() []core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.all)
}

func (u *updates) last() core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.all) == 0 {
		return core.Update{}
	}
	return u.all[len(u.all)-1]
}

func newBackend(srv *nftest.Server) (*Backend, *fakeEngine, *updates) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{}
	b := NewBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
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
	link := "https://nitroflare.com/view/" + fileID
	cases := []struct {
		name  string
		acct  nftest.Account
		login string
		want  bool
	}{
		{"premium", premium(), password, true},
		{"no login", premium(), "", false},
		{"free account", nftest.Account{Email: email, Password: password}, password, false},
		{"premium ran out", nftest.Account{Email: email, Password: password, Expires: time.Now().Add(-time.Hour)}, password, false},
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
		})
	}
}

func TestResolvingAsksNothingAndLeavesTheConnectionsToTheUser(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	got, err := Resolver{Backend: b}.Resolve(context.Background(), resolver.Request{URL: "https://nitro.download/view/" + fileID + "/a.rar"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DirectURL != "https://nitroflare.com/view/"+fileID || got.Connections != 0 {
		t.Errorf("resolved to %+v, want the written-out link and no ceiling on the connections", got)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("resolving asked Nitroflare %v; it runs under the app's lock", srv.Requests())
	}
}

func TestEveryLinkIsNamedWithoutALoginInOneQuestion(t *testing.T) {
	srv := nftest.New(t)
	live := srv.Add(fileID, nftest.File{Name: "Holiday.part1.rar", Body: make([]byte, 700<<10+3)})
	offline := srv.Add("8F1C3BD420914B1", nftest.File{Name: "old.zip", Body: make([]byte, 10), Offline: true})
	b, _, _ := newBackend(srv)

	got := Resolver{Backend: b}.Inspect(context.Background(), []string{
		live, "https://nitroflare.com/view/A1B2C3D4E5F6071", offline, "https://1fichier.com/?abc",
	})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Holiday.part1.rar", Size: 700<<10 + 3},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOffline},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("inspected %+v, want %+v", got, want)
	}
	if reqs := srv.Requests(); !slices.Equal(reqs, []string{"GET /api/v2/getFileInfo"}) {
		t.Errorf("asked %v, want one getFileInfo and no login", reqs)
	}

	// With none of the files there the API sends a list rather than a map.
	got = Resolver{Backend: b}.Inspect(context.Background(), []string{"https://nitroflare.com/view/A1B2C3D4E5F6071"})
	if !slices.Equal(got, []resolver.LinkInfo{{Availability: core.AvailOffline}}) {
		t.Errorf("a link to nothing inspected as %+v", got)
	}
}

func TestAPremiumDownloadIsHandedOn(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, nftest.File{Name: "Holiday.part1.rar", Body: make([]byte, 3<<20)})
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 8)
	h := awaitHandoff(t, eng)

	if !strings.HasPrefix(h.url, srv.URL+"/d/"+fileID) || h.conns != 8 {
		t.Errorf("handed %q with %d connections, want the download link with the 8 asked for", h.url, h.conns)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool {
		return u.Status == core.StatusRunning && u.Name == "Holiday.part1.rar" && u.Size == 3<<20
	}) {
		t.Errorf("the task was never named from the API: %+v", ups.list())
	}

	fresh, err := h.relink(context.Background())
	if err != nil || !strings.HasPrefix(fresh, srv.URL+"/d/"+fileID) || fresh == h.url {
		t.Errorf("relink = %q, %v; want a fresh download link", fresh, err)
	}
}

func TestTheAccountIsReportedAsNitroflareDescribesIt(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	st := b.Check(context.Background())

	want := Status{Checked: true, Premium: true, Expiry: time.Date(2030, 3, 1, 12, 30, 0, 0, time.UTC),
		TrafficLeft: 80 << 30, TrafficMax: 100 << 30}
	if st != want || b.Status() != want {
		t.Errorf("checked as %+v, want %+v", st, want)
	}
}

func TestARefusedLoginFailsTheDownloadAsAuth(t *testing.T) {
	cases := []struct {
		name    string
		acct    nftest.Account
		pass    string
		want    string
		captcha bool
	}{
		{"wrong password", premium(), "letmein", "email address and password", false},
		{"captcha", func() nftest.Account { a := premium(); a.Captcha = true; return a }(), password, "solveCaptcha", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.acct)
			link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
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
		})
	}
}

func TestAFreeAccountFailsSayingItNeedsPremium(t *testing.T) {
	srv := newServer(t, nftest.Account{Email: email, Password: password})
	link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonAuth || !strings.Contains(u.Err, "premium") {
		t.Errorf("settled as %+v, want a failure naming premium", u)
	}
	if slices.Contains(srv.Requests(), "GET /api/v2/getDownloadLink") {
		t.Error("a free account asked for a download link")
	}
}

func TestADeletedFileFailsAsGone(t *testing.T) {
	srv := newServer(t, premium())
	offline := srv.Add("8F1C3BD420914B1", nftest.File{Name: "old.zip", Body: make([]byte, 10), Offline: true})
	for _, link := range []string{"https://nitroflare.com/view/A1B2C3D4E5F6071", offline} {
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

func TestTrafficThatRunsOutFailsAsALimitAndBenchesTheLogin(t *testing.T) {
	t.Run("none left", func(t *testing.T) {
		acct := premium()
		acct.TrafficLeft = 0
		srv := newServer(t, acct)
		link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
		b, _, ups := newBackend(srv)
		b.SetLogin(email, password)

		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)

		if u.Status != core.StatusError || u.Reason != core.ReasonLimit || !strings.Contains(u.Err, "traffic") {
			t.Errorf("settled as %+v, want a traffic limit", u)
		}
		if slices.Contains(srv.Requests(), "GET /api/v2/getDownloadLink") {
			t.Error("a download link was asked for on a login without traffic")
		}
		if b.Ready() {
			t.Error("a login without traffic still claims links")
		}
	})
	t.Run("Nitroflare says so", func(t *testing.T) {
		acct := premium()
		acct.TrafficLeft = 1000
		srv := newServer(t, acct)
		link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
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

func TestTroubleAtNitroflareFailsAsUnavailable(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("getDownloadLink", http.StatusServiceUnavailable, 0, "Service temporarily unavailable")
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonUnavailable || !strings.Contains(u.Err, "temporarily unavailable") {
		t.Errorf("settled as %+v, want Nitroflare unavailable in its own words", u)
	}
	if !b.Ready() {
		t.Error("a hiccup at Nitroflare took the login out of routing")
	}
}

func TestAnAnswerTheClientCannotPlaceGivesTheLoginAndTheDownloadToJDownloader(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("getDownloadLink", http.StatusBadRequest, 99, "Something the API never said before")
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if !u.Unsupported || u.Takeover != "Nitroflare" {
		t.Errorf("settled as %+v, want the download handed on with Nitroflare named", u)
	}
	if st := b.Status(); !st.Handed || b.Ready() || len(eng.handoffs()) > 0 {
		t.Errorf("the login reads as %+v, want it with JDownloader and out of routing", st)
	}
}

func TestThePasswordNeverShowsInAnError(t *testing.T) {
	srv := nftest.New(t)
	c := NewClient(srv.URL, srv.Client())
	srv.Close()
	_, _, err := c.Login(context.Background(), email, password)
	if err == nil || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "premiumKey") {
		t.Errorf("a failed call reads %v; the API takes the password in the URL, which must not show", err)
	}
	var refused *LoginError
	if errors.As(err, &refused) {
		t.Error("a server that is not there turned the login down")
	}
}
