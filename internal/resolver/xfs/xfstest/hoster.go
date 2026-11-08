package xfstest

import (
	"context"
	"io/fs"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// Hoster is a hoster's site with the pages of its own that its checks run
// on.
type Hoster struct {
	Site *xfs.Site
	// Pages hold the hoster's own pages, in place of the script's.
	Pages fs.FS
	// GoneStatus is what the page of a deleted file answers with.
	GoneStatus int
	// LoginCaptcha is set for a sign-in form with a Turnstile check.
	LoginCaptcha bool
	// Countdown is what the hoster's own file page counts down.
	Countdown time.Duration
	// TrafficLeft is what the account page of a premium login with 20480 MB
	// on it says is left, in bytes.
	TrafficLeft int64
}

// CheckHoster runs the checks every hoster has to pass on its own pages: the
// link check, a free download and a premium login.
func CheckHoster(t *testing.T, h Hoster) {
	t.Run("link check", func(t *testing.T) {
		pause := xfs.InspectPause
		xfs.InspectPause = 0
		t.Cleanup(func() { xfs.InspectPause = pause })
		srv := h.server(t)
		live := srv.Add("k3m9x2p7q1ab", File{Name: "Fantastic Mr Fox (2009).mkv", Body: make([]byte, 700<<10)})
		r := xfs.Resolver{Site: h.Site, Client: xfs.NewClient(h.Site, srv.URL, srv.Client())}

		got := r.Inspect(context.Background(), []string{live, "https://" + h.Site.Host + "/g0n3g0n3g0n3"})
		want := []resolver.LinkInfo{
			{Availability: core.AvailOnline, Name: "Fantastic Mr Fox (2009).mkv", Size: 700 << 10},
			{Availability: core.AvailOffline},
		}
		if !slices.Equal(got, want) {
			t.Errorf("Inspect = %+v\nwant      %+v", got, want)
		}
	})

	t.Run("free download", func(t *testing.T) {
		srv := h.server(t)
		link := srv.Add("k3m9x2p7q1ab", File{Name: "a.bin", Body: make([]byte, 1024)})
		srv.Captcha(Recaptcha)
		srv.Countdown(20)
		b, eng, rec := h.backend(srv)

		b.Download("t1", link, nil, 1)
		got := await(t, eng, rec)
		if got.url != srv.URL+"/d/k3m9x2p7q1ab/a.bin" || got.conns != h.Site.FreeConns {
			t.Errorf("handed on %+v, want the link for %d connections", got, h.Site.FreeConns)
		}
		if w := rec.waited(); w < h.Countdown+19*time.Second || w > h.Countdown+20*time.Second {
			t.Errorf("waited %s, want the page's countdown and the second form's", w)
		}
		if len(rec.captchas) != 1 || rec.captchas[0].Kind != xfs.Recaptcha {
			t.Errorf("captchas asked %+v, want the second form's", rec.captchas)
		}
	})

	t.Run("premium login", func(t *testing.T) {
		srv := h.server(t)
		if h.LoginCaptcha {
			srv.LoginCaptcha()
		}
		link := srv.Add("k3m9x2p7q1ab", File{Name: "a.bin", Body: make([]byte, 1024)})
		until := time.Now().AddDate(0, 2, 0)
		srv.AddAccount("fox", Account{Password: "badger", Premium: true, Until: until, TrafficMB: 20480})
		b, eng, rec := h.backend(srv)
		rec.user, rec.pass = "fox", "badger"

		b.CheckLogin(context.Background(), true)
		st := rec.checked
		if !b.Premium() || len(st) == 0 || st[len(st)-1].Expires.Format(time.DateOnly) != until.Format(time.DateOnly) {
			t.Fatalf("the check found %+v, want premium until %s", st, until.Format(time.DateOnly))
		}
		if left := st[len(st)-1].TrafficLeft; left != h.TrafficLeft {
			t.Errorf("traffic left %d, want %d", left, h.TrafficLeft)
		}
		if h.LoginCaptcha != (len(rec.captchas) == 1) {
			t.Errorf("the sign-in asked %+v", rec.captchas)
		}

		b.Download("t1", link, nil, 1)
		if got := await(t, eng, rec); got.conns != h.Site.PremiumConns {
			t.Errorf("handed on %+v, want the premium's %d connections", got, h.Site.PremiumConns)
		}
	})
}

func (h Hoster) server(t *testing.T) *Server {
	srv := New(t, h.Site.Host, h.Pages)
	srv.GoneStatus = h.GoneStatus
	return srv
}

type handoff struct {
	url   string
	conns int
}

type engine struct{ got chan handoff }

func (e engine) Handover(_, url string, conns int, _ func(context.Context) (string, error)) {
	e.got <- handoff{url, conns}
}
func (engine) Pause(string)        {}
func (engine) Resume(string)       {}
func (engine) Remove(string, bool) {}

// recorder keeps what a backend reports, waits and asks.
type recorder struct {
	mu       sync.Mutex
	last     core.Update
	waits    time.Duration
	captchas []xfs.Captcha
	checked  []xfs.LoginState
	user     string
	pass     string
}

func (h Hoster) backend(srv *Server) (*xfs.Backend, engine, *recorder) {
	eng := engine{got: make(chan handoff, 1)}
	rec := &recorder{}
	b := xfs.NewBackend(xfs.NewClient(h.Site, srv.URL, srv.Client()), eng, func(_ string, u core.Update) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.last = u
	})
	b.Sleep = func(ctx context.Context, d time.Duration) error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.waits += d
		return ctx.Err()
	}
	b.Solve = func(_ context.Context, _ string, c xfs.Captcha) (string, error) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.captchas = append(rec.captchas, c)
		return Token, nil
	}
	b.Login = func() (string, string, bool) { return rec.user, rec.pass, rec.user != "" }
	b.OnLogin = func(st xfs.LoginState) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.checked = append(rec.checked, st)
	}
	return b, eng, rec
}

func (r *recorder) waited() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waits
}

func await(t *testing.T, eng engine, rec *recorder) handoff {
	t.Helper()
	select {
	case h := <-eng.got:
		return h
	case <-time.After(10 * time.Second):
		rec.mu.Lock()
		defer rec.mu.Unlock()
		t.Fatalf("nothing was handed to the engine, last update %+v", rec.last)
		return handoff{}
	}
}
