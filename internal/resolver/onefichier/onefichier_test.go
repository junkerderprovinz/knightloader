package onefichier

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
	"github.com/junkerderprovinz/knightloader/internal/resolver/onefichier/fichiertest"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://1fichier.com/?k3m9x2p7q1", "https://1fichier.com/?k3m9x2p7q1"},
		{"http://www.1fichier.com/?k3m9x2p7q1&af=12345", "https://1fichier.com/?k3m9x2p7q1"},
		{"https://k3m9x2p7q1.1fichier.com/", "https://1fichier.com/?k3m9x2p7q1"},
		{"https://k3m9x2p7q1.1fichier.com", "https://1fichier.com/?k3m9x2p7q1"},
		{"https://dl4free.com/?k3m9x2p7q1", "https://1fichier.com/?k3m9x2p7q1"},
		{"https://K3M9X2P7Q1.desfichiers.com/", "https://1fichier.com/?k3m9x2p7q1"},
		{"https://megadl.fr/?abc", "https://1fichier.com/?abc"},
		{"https://tenvoi.com/?abc", "https://1fichier.com/?abc"},
		{"https://alterupload.com/?abc", "https://1fichier.com/?abc"},
		{"https://cjoint.net/?abc", "https://1fichier.com/?abc"},
		{"https://dfichiers.com/?abc", "https://1fichier.com/?abc"},
		{"https://mesfichiers.org/?abc", "https://1fichier.com/?abc"},
		{"https://piecejointe.net/?abc", "https://1fichier.com/?abc"},
		{"https://pjointe.com/?abc", "https://1fichier.com/?abc"},

		// A folder, the start page and the download servers are no file pages.
		{"https://1fichier.com/dir/AbCdEf12", ""},
		{"https://1fichier.com/", ""},
		{"https://a-12.1fichier.com/c4719027611", ""},
		{"https://1fichier.com/console/abo.pl?k3m9x2p7q1", ""},
		{"https://rapidgator.net/file/abc", ""},
		{"https://not1fichier.com/?abc", ""},
		{"ftp://1fichier.com/?abc", ""},
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
	srv := fichiertest.New(t)
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	got, err := r.Resolve(context.Background(), resolver.Request{URL: "https://k3m9x2p7q1.1fichier.com/"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DirectURL != "https://1fichier.com/?k3m9x2p7q1" || got.Connections != 1 {
		t.Errorf("resolved to %+v, want the written-out link and one connection", got)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("resolving asked 1fichier %v; it runs under the app's lock", srv.Requests())
	}
}

func TestTheFilePageNamesAndSizesTheFile(t *testing.T) {
	srv := fichiertest.New(t)
	live := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "Isle of Dogs (2018) 1080p.mkv", Body: make([]byte, 700<<10)})
	gone := "https://1fichier.com/?g0n3"
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}

	got := r.Inspect(context.Background(), []string{live, gone, "https://rapidgator.net/file/abc"})

	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Isle of Dogs (2018) 1080p.mkv", Size: 700 << 10},
		{Availability: core.AvailOffline},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("inspected %+v, want %+v", got, want)
	}
	if asked := srv.WithoutEnglish(); len(asked) > 0 {
		t.Errorf("pages were asked for without the English cookie: %v", asked)
	}
}

func TestSizesAreReadInEveryUnitTheSiteUses(t *testing.T) {
	cases := []struct {
		row  string
		want int64
	}{
		{`<td class="normal"><span style="font-size:1.1em">1.37 GB</span></td>`, 1471026299},
		{`<td class="normal"><span style="font-size:1.1em">700 MB</span></td>`, 700 << 20},
		{`<td class="normal"><span style="font-size:1.1em">12,5 Ko</span></td>`, 12800},
		{`<td class="normal"><span style="font-size:1.1em">831 B</span></td>`, 831},
		{`<span class="tier-feat">42.99 GB</span>`, 46160161014},
		// A file name with figures in it is not the size.
		{`<td class="normal"><span style="font-weight:bold">Show 2018 1080p</span></td>`, 0},
	}
	for _, c := range cases {
		if got := readPage(c.row).size; got != c.want {
			t.Errorf("%s reads as %d bytes, want %d", c.row, got, c.want)
		}
	}
}

func TestEveryWayThePageHoldsADownloadBackIsRead(t *testing.T) {
	cases := []struct {
		text string
		want error
	}{
		{"<div>You must wait 12 minutes to download again</div>", &WaitError{Wait: 12 * time.Minute}},
		{"Without premium, you must wait at least 30 minutes between each downloads", &WaitError{Wait: 30 * time.Minute}},
		{"<div>Vous devez attendre encore 7 minutes</div>", &WaitError{Wait: 7 * time.Minute}},
		{"<p>IP Locked</p>", &WaitError{Wait: time.Hour}},
		{"<div>You must wait for another download</div>", &WaitError{Wait: 5 * time.Minute}},
		{"Without Premium, you can only download one file at a time", &WaitError{Wait: 5 * time.Minute}},
		{"<b>High demand:</b> all free guest slots are currently in use.", &WaitError{Wait: busyWait, Busy: true}},
		{"<div>Free download is temporarily limited due to high demand</div>", &WaitError{Wait: busyWait, Busy: true}},
		{"It is not possible to free unregistered users to download this file", ErrPremium},
		{"<div>The owner of this file has reserved access to the subscribers of our services</div>", ErrPremium},
		{"<div>File not found !</div>", ErrGone},
	}
	for _, c := range cases {
		got := readPage(c.text).hold()
		var w, wantW *WaitError
		if errors.As(c.want, &wantW) {
			if !errors.As(got, &w) || *w != *wantW {
				t.Errorf("%q holds back with %v, want %v", c.text, got, c.want)
			}
		} else if !errors.Is(got, c.want) {
			t.Errorf("%q holds back with %v, want %v", c.text, got, c.want)
		}
	}
}

func TestACountdownIsReadInTheUnitItNames(t *testing.T) {
	cases := []struct {
		text string
		want time.Duration
	}{
		{"Free download in ⏳ 45", 45 * time.Second},
		{"Free download in ⏳ 45 seconds", 45 * time.Second},
		{"Free download in ⏳ 2 minutes", 2 * time.Minute},
		{"Free download in 30s", 30 * time.Second},
		{"<script>\n        var ct = 60;\n", 60 * time.Second},
	}
	for _, c := range cases {
		if got := readPage(c.text).countdown; got != c.want {
			t.Errorf("%q reads as %v, want %v", c.text, got, c.want)
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

func newBackend(srv *fichiertest.Server) (*Backend, *fakeEngine, *updates) {
	eng := newFakeEngine()
	ups := &updates{waiting: make(chan struct{}, 1)}
	b := NewBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.sleep = ups.sleep
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

func TestTheFormIsSentAsABrowserWouldAndTheLinkHandedOnForOneConnection(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 4096)})
	b, eng, ups := newBackend(srv)

	b.Download("t1", link, nil, 8)
	h := awaitHandoff(t, eng)

	if h.url != srv.URL+"/dl/k3m9x2p7q1" || h.conns != 1 {
		t.Errorf("handed %q with %d connections, want the download link with one", h.url, h.conns)
	}
	posts := srv.Posts()
	if len(posts) != 1 {
		t.Fatalf("%d forms went in, want 1", len(posts))
	}
	form := posts[0]
	if form.Get("did") != "1" || form.Has("dl_no_ssl") || form.Has("dlinline") || form.Has("save") || form.Has("pass") {
		t.Errorf("the form went in as %v, want did=1 and none of its unticked boxes", form)
	}
	if asked := srv.WithoutEnglish(); len(asked) > 0 {
		t.Errorf("1fichier was asked without the English cookie: %v", asked)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool {
		return u.Status == core.StatusRunning && u.Name == "Isle of Dogs.mkv" && u.Size == 4096
	}) {
		t.Errorf("the task was never named from the page: %+v", ups.list())
	}

	// The engine comes back for a fresh link when the first stops working.
	fresh, err := h.relink(context.Background())
	if err != nil || fresh != srv.URL+"/dl/k3m9x2p7q1" {
		t.Errorf("relink = %q, %v", fresh, err)
	}
}

func TestACoolDownIsWaitedOutAndTheFileFetchedAfterwards(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 4096)})
	srv.CoolDown(12, 3)
	b, eng, ups := newBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := ups.waited(); got != 15*time.Minute {
		t.Errorf("waited %v in all, want the 12 and then 3 minutes 1fichier asked for", got)
	}
	var notes []string
	for _, u := range ups.list() {
		if u.Status != core.StatusRunning && u.Status != "" {
			t.Errorf("a wait was reported as %q (%s); waiting is not failing", u.Status, u.Err)
		}
		if u.Note != "" {
			notes = append(notes, u.Note)
		}
	}
	if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, "12 min") }) {
		t.Errorf("the notes %q never showed the wait", notes)
	}
	if b.Waiting("t1") {
		t.Error("the task still counts as waiting after its link was handed on")
	}
}

func TestACoolDownAfterTheFormIsWaitedOutToo(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.CoolDownAfterForm(2)
	b, eng, ups := newBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := ups.waited(); got != 2*time.Minute {
		t.Errorf("waited %v, want 2 minutes", got)
	}
}

func TestTheCountdownRunsOutBeforeTheFormGoesIn(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
	srv.Countdown(45)
	b, eng, ups := newBackend(srv)
	postsAtWait := -1
	b.sleep = func(ctx context.Context, d time.Duration) error {
		postsAtWait = len(srv.Posts())
		return ups.sleep(ctx, d)
	}

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := ups.waited(); got != 45*time.Second {
		t.Errorf("waited %v, want the 45 seconds of the countdown", got)
	}
	if postsAtWait != 0 {
		t.Errorf("%d forms had gone in when the countdown was waited, want none", postsAtWait)
	}
	if n := len(srv.Posts()); n != 1 {
		t.Errorf("%d forms went in, want 1: the form from the page the countdown ran on", n)
	}
}

func TestAPasswordGoesIntoThePasswordField(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024), Password: "hunter2"})
	b, eng, _ := newBackend(srv)
	b.Password = func(id string) string {
		if id == "t1" {
			return "hunter2"
		}
		return ""
	}

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := srv.Posts()[0].Get("pass"); got != "hunter2" {
		t.Errorf("pass = %q, want the task's download password", got)
	}
}

func TestAProtectedFileFailsSayingWhichPasswordIsMissing(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024), Password: "hunter2"})

	t.Run("none set", func(t *testing.T) {
		b, eng, ups := newBackend(srv)
		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)
		if u.Status != core.StatusError || !strings.Contains(u.Err, "download password") || u.Reason != core.ReasonAuth {
			t.Errorf("settled as %+v, want a failure that asks for the download password", u)
		}
		if len(eng.handoffs()) > 0 || len(srv.Posts()) > 0 {
			t.Error("a form went in without the password it asks for")
		}
	})
	t.Run("a wrong one", func(t *testing.T) {
		b, _, ups := newBackend(srv)
		b.Password = func(string) string { return "letmein" }
		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)
		if u.Status != core.StatusError || !strings.Contains(u.Err, "not accepted") || u.Reason != core.ReasonAuth {
			t.Errorf("settled as %+v, want the password turned down", u)
		}
	})
}

func TestADeletedFileFailsAsGone(t *testing.T) {
	srv := fichiertest.New(t)
	b, eng, ups := newBackend(srv)

	b.Download("t1", "https://1fichier.com/?g0n3", nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || u.Reason != core.ReasonGone {
		t.Errorf("settled as %+v, want gone", u)
	}
	if len(eng.handoffs()) > 0 {
		t.Error("a deleted file reached the engine")
	}
}

func TestAFileForSubscribersOnlyFailsSayingItNeedsPremium(t *testing.T) {
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024), PremiumOnly: true})
	b, _, ups := newBackend(srv)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Status != core.StatusError || !strings.Contains(u.Err, "premium account") || u.Retry != 0 {
		t.Errorf("settled as %+v, want a failure naming the premium account", u)
	}
	if ups.waited() != 0 {
		t.Errorf("waited %v for a file no wait opens", ups.waited())
	}
}

func TestBusyFreeDownloadsAreTriedAgainEveryFiveMinutesThenGivenUp(t *testing.T) {
	t.Run("then free", func(t *testing.T) {
		srv := fichiertest.New(t)
		link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
		srv.Busy(2)
		b, eng, ups := newBackend(srv)
		b.Download("t1", link, nil, 1)
		awaitHandoff(t, eng)
		if got := ups.waited(); got != 10*time.Minute {
			t.Errorf("waited %v, want two rounds of five minutes", got)
		}
	})
	t.Run("never free", func(t *testing.T) {
		srv := fichiertest.New(t)
		link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
		srv.Busy(1000)
		b, _, ups := newBackend(srv)
		b.Download("t1", link, nil, 1)
		u := awaitSettled(t, ups)
		if u.Status != core.StatusError || u.Reason != core.ReasonUnavailable {
			t.Errorf("settled as %+v, want 1fichier unavailable", u)
		}
		if got := ups.waited(); got != 25*time.Minute {
			t.Errorf("waited %v before giving up, want five rounds of five minutes", got)
		}
	})
}

func TestPausingOrRemovingDuringAWaitCallsItOff(t *testing.T) {
	for _, stop := range []string{"pause", "remove"} {
		t.Run(stop, func(t *testing.T) {
			srv := fichiertest.New(t)
			link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
			srv.CoolDown(90)
			b, eng, ups := newBackend(srv)
			ups.block = true

			b.Download("t1", link, nil, 1)
			<-ups.waiting
			if !b.Waiting("t1") {
				t.Error("a task in a cool-down does not count as waiting")
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
			for _, u := range ups.list() {
				if u.Status == core.StatusError {
					t.Errorf("calling the wait off reported a failure: %s", u.Err)
				}
			}
			if stop == "pause" {
				if got := ups.last().Status; got != core.StatusPaused {
					t.Errorf("last status %q, want paused", got)
				}
				// Resuming reads the page again, and this time it asks for no wait.
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
	srv := fichiertest.New(t)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "a.bin", Body: make([]byte, 1024)})
	b, eng, _ := newBackend(srv)

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
