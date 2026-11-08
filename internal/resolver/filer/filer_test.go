package filer

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer/filertest"
)

const (
	fileID   = "dv6qg8tfckhk7mja"
	email    = "john@example.com"
	password = "hunter2"
)

func TestEveryLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://filer.net/get/" + fileID, "https://filer.net/get/" + fileID},
		{"http://www.filer.net/get/" + fileID, "https://filer.net/get/" + fileID},
		{"https://filer.net/dl/" + fileID, "https://filer.net/get/" + fileID},
		{"https://filer.net/app.php/get/" + fileID + "/", "https://filer.net/get/" + fileID},

		// A folder, an id too short for the API and other sites are no files.
		{"https://filer.net/folder/" + fileID, ""},
		{"https://filer.net/get/abc123", ""},
		{"https://filer.net/", ""},
		{"https://notfiler.net/get/" + fileID, ""},
		{"ftp://filer.net/get/" + fileID, ""},
	}
	for _, c := range cases {
		got, ok := Normalize(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Normalize(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
	}
}

func premium() filertest.Account {
	return filertest.Account{Email: email, Password: password, Premium: true,
		Until: time.Date(2030, 3, 1, 12, 30, 0, 0, time.UTC), Traffic: 125 << 30, TrafficLeft: 80 << 30}
}

func newServer(t *testing.T, acct filertest.Account) *filertest.Server {
	srv := filertest.New(t)
	srv.AddAccount(acct)
	return srv
}

type handoff struct {
	url    string
	conns  int
	relink func(context.Context) (string, error)
}

type fakeEngine struct {
	mu     sync.Mutex
	handed []handoff
	got    chan handoff
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

type updates struct {
	mu  sync.Mutex
	all []core.Update
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.all = append(u.all, up)
}

func (u *updates) last() core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.all) == 0 {
		return core.Update{}
	}
	return u.all[len(u.all)-1]
}

func (u *updates) list() []core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.all)
}

func newBackend(srv *filertest.Server) (*Backend, *fakeEngine, *updates) {
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
		if u := ups.last(); u.Status == core.StatusError {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the task never settled: %+v", ups.list())
	return core.Update{}
}

func TestALinkIsClaimedOnlyWhileAPremiumLoginIsThere(t *testing.T) {
	link := "https://filer.net/get/" + fileID
	cases := []struct {
		name  string
		acct  filertest.Account
		login string
		want  bool
	}{
		{"premium", premium(), password, true},
		{"no login", premium(), "", false},
		{"free account", filertest.Account{Email: email, Password: password}, password, false},
		{"wrong password", premium(), "letmein", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _, _ := newBackend(newServer(t, c.acct))
			if c.login != "" {
				b.SetLogin(email, c.login)
			}
			b.Check(context.Background())
			if got := (Resolver{Backend: b}).Match(link); got != c.want {
				t.Errorf("Match = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTheAccountIsReportedAsFilerDescribesIt(t *testing.T) {
	b, _, _ := newBackend(newServer(t, premium()))
	b.SetLogin(email, password)
	st := b.Check(context.Background())
	want := Status{Checked: true, Premium: true, Expiry: time.Date(2030, 3, 1, 12, 30, 0, 0, time.UTC),
		TrafficLeft: 80 << 30, TrafficMax: 125 << 30}
	if st != want {
		t.Errorf("status = %+v\nwant     %+v", st, want)
	}
}

func TestTrafficBelowZeroReadsAsNoneLeft(t *testing.T) {
	acct := premium()
	acct.TrafficLeft = -3 << 30
	srv := newServer(t, acct)
	link := srv.Add(fileID, filertest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)
	if st := b.Check(context.Background()); st.TrafficLeft != 0 || st.TrafficMax != 125<<30 {
		t.Errorf("status = %+v, want no traffic left of 125 GiB", st)
	}

	b.Download("t1", link, nil, 4)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonLimit || b.Ready() {
		t.Errorf("settled as %+v, want a traffic limit that benches the login", u)
	}
}

func TestAPremiumDownloadIsHandedOnOverOneConnectionWithoutTheLogin(t *testing.T) {
	srv := newServer(t, premium())
	body := bytes.Repeat([]byte("fantastic mr fox "), 4096)
	link := srv.Add(fileID, filertest.File{Name: "Fantastic Mr Fox.mkv", Body: body})
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 8)
	got := awaitHandoff(t, eng)

	if got.conns != 1 {
		t.Errorf("handed on for %d connections, want the one JDownloader's plugin opens", got.conns)
	}
	resp, err := srv.Client().Get(got.url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if fetched, _ := io.ReadAll(resp.Body); !bytes.Equal(fetched, body) {
		t.Errorf("the link handed on does not serve the file: %s", got.url)
	}
	named := slices.ContainsFunc(ups.list(), func(u core.Update) bool {
		return u.Name == "Fantastic Mr Fox.mkv" && u.Size == int64(len(body))
	})
	if !named {
		t.Errorf("the task was never named: %+v", ups.list())
	}
	if fresh, err := got.relink(context.Background()); err != nil || fresh == "" {
		t.Errorf("relink = %q, %v; want a fresh link", fresh, err)
	}
}

func TestEveryFailureReadsAsItsCause(t *testing.T) {
	cases := []struct {
		name   string
		acct   filertest.Account
		pass   string
		fail   func(*filertest.Server)
		reason core.Reason
		words  string
	}{
		{name: "wrong password", acct: premium(), pass: "letmein", reason: core.ReasonAuth, words: "email address and password"},
		{name: "free account", acct: filertest.Account{Email: email, Password: password}, pass: password, reason: core.ReasonAuth, words: "premium"},
		{name: "breached password", acct: premium(), pass: password, reason: core.ReasonAuth, words: "breach",
			fail: func(s *filertest.Server) { s.Fail("profile", http.StatusForbidden, 403, "account_breach_locked") }},
		{name: "folder password", acct: premium(), pass: password, reason: core.ReasonAuth, words: "password",
			fail: func(s *filertest.Server) { s.Fail("dl", http.StatusForbidden, 403, "folder password required") }},
		{name: "slots full", acct: premium(), pass: password, reason: core.ReasonUnavailable, words: "slot",
			fail: func(s *filertest.Server) {
				s.Fail("dl", http.StatusTooManyRequests, 429, "concurrent download limit reached")
			}},
		{name: "not allowed", acct: premium(), pass: password, reason: core.ReasonLimit, words: "refuses",
			fail: func(s *filertest.Server) { s.Fail("dl", http.StatusForbidden, 403, "download not allowed") }},
		{name: "server error", acct: premium(), pass: password, reason: core.ReasonUnavailable, words: "error",
			fail: func(s *filertest.Server) { s.Fail("dl", http.StatusInternalServerError, 500, "error") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.acct)
			link := srv.Add(fileID, filertest.File{Name: "a.bin", Body: make([]byte, 4096)})
			if c.fail != nil {
				c.fail(srv)
			}
			b, eng, ups := newBackend(srv)
			b.SetLogin(email, c.pass)

			b.Download("t1", link, nil, 1)
			u := awaitSettled(t, ups)
			if u.Reason != c.reason || u.Unsupported || !strings.Contains(u.Err, c.words) {
				t.Errorf("settled as %+v, want %q naming %q", u, c.reason, c.words)
			}
			if len(eng.handoffs()) > 0 {
				t.Error("a failed download reached the engine")
			}
		})
	}
}

func TestAMissingFileFailsAsGoneBeforeTheLoginIsAsked(t *testing.T) {
	srv := newServer(t, premium())
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", "https://filer.net/get/"+fileID, nil, 1)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonGone {
		t.Errorf("settled as %+v, want gone", u)
	}
	if slices.Contains(srv.Requests(), "GET /api/profile.json") {
		t.Error("the login was used for a file that is not there")
	}
}

func TestAnAnswerTheClientCannotPlaceGivesTheLoginAndTheDownloadToJDownloader(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, filertest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("dl", http.StatusForbidden, 403, "something the API never said before")
	b, _, ups := newBackend(srv)
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	if u := awaitSettled(t, ups); !u.Unsupported || u.Takeover != "filer.net" {
		t.Errorf("settled as %+v, want the download handed on with filer.net named", u)
	}
	if !b.Status().Handed || b.Ready() {
		t.Error("the login stayed with KnightLoader")
	}
}

func TestEveryLinkIsNamedWithoutALoginInOneQuestion(t *testing.T) {
	srv := filertest.New(t)
	first := srv.Add(fileID, filertest.File{Name: "Rushmore.mkv", Body: make([]byte, 3<<10)})
	second := srv.Add("q9w8e7r6t5y4u3i2", filertest.File{Name: "Bottle Rocket.mkv", Body: make([]byte, 2<<10), PremiumOnly: true})
	b, _, _ := newBackend(srv)

	got := Resolver{Backend: b}.Inspect(context.Background(),
		[]string{first, "https://filer.net/get/0000000000000000", second, "https://rapidgator.net/file/abc"})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Rushmore.mkv", Size: 3 << 10},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOnline, Name: "Bottle Rocket.mkv", Size: 2 << 10},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v\nwant      %+v", got, want)
	}
	if reqs := srv.Requests(); !slices.Equal(reqs, []string{"POST /api/multi_status.json"}) {
		t.Errorf("asked %v, want one question for the whole paste", reqs)
	}

	one := Resolver{Backend: b}.Inspect(context.Background(), []string{"https://filer.net/get/0000000000000000"})
	if one[0].Availability != core.AvailOffline {
		t.Errorf("a single unknown link reads %+v, want offline", one[0])
	}
}
