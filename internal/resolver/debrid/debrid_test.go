package debrid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// handoff is what the backend hands the engine once the link is unlocked.
type handoff struct {
	url    string
	conns  int
	relink func(context.Context) (string, error)
}

// fakeEngine captures the handover.
type fakeEngine struct{ got chan handoff }

func (f *fakeEngine) Handover(_, url string, conns int, relink func(context.Context) (string, error)) {
	f.got <- handoff{url: url, conns: conns, relink: relink}
}
func (f *fakeEngine) Pause(string)        {}
func (f *fakeEngine) Resume(string)       {}
func (f *fakeEngine) Remove(string, bool) {}

func TestAllDebrid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/hosts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("agent"); got != "knightloader" {
			t.Errorf("agent = %q, want knightloader", got)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"hosts":{
			"1fichier":{"name":"1fichier","domains":["1fichier.com","dl4free.com"]},
			"rapidgator":{"name":"Rapidgator","domains":["rapidgator.net","RG.TO"]}}}}`))
	})
	mux.HandleFunc("/v4/link/unlock", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("apikey") != "AD-KEY" || q.Get("link") != "https://rapidgator.net/file/x" {
			t.Errorf("unlock params = %v", q)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"link":"https://cdn.alldebrid.com/dl/movie.mkv",
			"filename":"movie.mkv","filesize":4096}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ad := NewAllDebrid("AD-KEY")
	ad.base = srv.URL + "/v4"

	hosts, err := ad.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1fichier.com", "dl4free.com", "rapidgator.net", "rg.to"} {
		if !hosts[want] {
			t.Errorf("host set missing %q (got %v)", want, hosts)
		}
	}

	d, err := ad.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != "https://cdn.alldebrid.com/dl/movie.mkv" || d.Name != "movie.mkv" || d.Size != 4096 {
		t.Fatalf("unlock = %+v", d)
	}

	// The error envelope must surface as a Go error, not a silent empty result.
	mux.HandleFunc("/v4/link/unlock/bad", func(w http.ResponseWriter, r *http.Request) {})
	adErr := NewAllDebrid("")
	adErr.base = srv.URL + "/v4"
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"AUTH_BAD_APIKEY","message":"The auth apikey is invalid"}}`))
	}))
	defer errSrv.Close()
	adErr.base = errSrv.URL
	if _, err := adErr.Unlock(context.Background(), "https://x.example/f"); err == nil {
		t.Fatal("bad apikey did not produce an error")
	}
}

func TestRealDebrid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/hosts/domains", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`["1fichier.com","rapidgator.net","Uploaded.net"]`))
	})
	mux.HandleFunc("/unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer RD-TOKEN" {
			t.Errorf("auth = %q", got)
		}
		_ = r.ParseForm()
		if got := r.FormValue("link"); got != "https://rapidgator.net/file/y" {
			t.Errorf("link = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"ABC","filename":"show.mkv","filesize":2048,
			"host":"rapidgator.net","download":"https://cdn.real-debrid.com/d/ABC/show.mkv"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := NewRealDebrid("RD-TOKEN")
	rd.base = srv.URL

	hosts, err := rd.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hosts["rapidgator.net"] || !hosts["uploaded.net"] {
		t.Fatalf("host set = %v", hosts)
	}

	d, err := rd.Unlock(context.Background(), "https://rapidgator.net/file/y")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != "https://cdn.real-debrid.com/d/ABC/show.mkv" || d.Name != "show.mkv" || d.Size != 2048 {
		t.Fatalf("unlock = %+v", d)
	}
}

// After the unlock the engine gets the direct URL and the task the resolved
// name.
func TestBackendHandsOffToEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","filename":"f.bin","filesize":99,"download":"https://cdn.example/f.bin"}`))
	}))
	defer srv.Close()
	rd := NewRealDebrid("T")
	rd.base = srv.URL

	fe := &fakeEngine{got: make(chan handoff, 1)}
	names := make(chan string, 8)
	b := NewBackend(rd, fe, func(_ string, u core.Update) {
		if u.Name != "" {
			names <- u.Name
		}
	})
	// An odd count, so a default invented by the backend cannot match it.
	b.Download("t1", "https://rapidgator.net/file/z", nil, 5)

	select {
	case h := <-fe.got:
		if h.url != "https://cdn.example/f.bin" {
			t.Fatalf("engine got %q", h.url)
		}
		if h.conns != 5 {
			t.Errorf("engine opened %d connections, want the 5 the dispatcher decided on", h.conns)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("engine.Download never called")
	}
	var sawName bool
	for len(names) > 0 {
		if <-names == "f.bin" {
			sawName = true
		}
	}
	if !sawName {
		t.Error("resolved filename was never mirrored to the task")
	}
}

// hangingServer answers no request until the test ends and signals each one
// that arrives, the way an overloaded login endpoint behaves.
func hangingServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	arrived := make(chan struct{}, 16)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv, arrived
}

func TestBackendReportsAnUnlockThatRanOutOfTime(t *testing.T) {
	srv, _ := hangingServer(t)
	updates := make(chan core.Update, 8)
	b := NewBackend(multiupAt(srv.URL), &fakeEngine{got: make(chan handoff, 1)}, func(_ string, u core.Update) { updates <- u })
	b.timeout = 100 * time.Millisecond

	b.Download("t1", "https://rapidgator.net/file/x", nil, 1)
	giveUp := time.After(10 * time.Second)
	for {
		select {
		case u := <-updates:
			if u.Status != core.StatusError {
				continue
			}
			if !strings.Contains(u.Err, "deadline exceeded") {
				t.Errorf("Err = %q, want it to say the unlock ran out of time", u.Err)
			}
			return
		case <-giveUp:
			t.Fatal("the task was never told that its unlock ran out of time and stays unlocking")
		}
	}
}

// Pause marks the task paused itself, so the cancelled unlock must not turn
// it into a failure.
func TestBackendReportsNoErrorForAPausedUnlock(t *testing.T) {
	srv, arrived := hangingServer(t)
	updates := make(chan core.Update, 8)
	b := NewBackend(multiupAt(srv.URL), &fakeEngine{got: make(chan handoff, 1)}, func(_ string, u core.Update) { updates <- u })

	b.Download("t1", "https://rapidgator.net/file/x", nil, 1)
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the unlock never sent its login")
	}
	b.Pause("t1")
	for giveUp := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		b.mu.Lock()
		_, unlocking := b.runs["t1"]
		b.mu.Unlock()
		if !unlocking {
			break
		}
		if time.Now().After(giveUp) {
			t.Fatal("the unlock kept running after the pause")
		}
	}
	for len(updates) > 0 {
		if u := <-updates; u.Status == core.StatusError {
			t.Errorf("a paused unlock reported the error %q", u.Err)
		}
	}
}

// heldService's Unlock returns only once the test releases it, the way a
// request already on the wire outlives its cancel for a moment.
type heldService struct {
	held chan chan struct{}
	// answered makes Unlock succeed even when cancelled, as when the answer
	// was already in when the cancel came.
	answered bool

	mu   sync.Mutex
	ctxs []context.Context
}

func (*heldService) ID() string                                     { return "held" }
func (*heldService) Label() string                                  { return "Held" }
func (*heldService) Hosts(context.Context) (map[string]bool, error) { return nil, nil }

func (s *heldService) Unlock(ctx context.Context, _ string) (Direct, error) {
	s.mu.Lock()
	s.ctxs = append(s.ctxs, ctx)
	s.mu.Unlock()
	release := make(chan struct{})
	s.held <- release
	<-release
	if err := ctx.Err(); err != nil && !s.answered {
		return Direct{}, err
	}
	return Direct{URL: "https://cdn.example/x"}, nil
}

// A paused unlock that ends only after the task was resumed must leave the
// resumed unlock where the next pause can reach it.
func TestAResumedUnlockCanBePausedAgain(t *testing.T) {
	svc := &heldService{held: make(chan chan struct{}, 2)}
	fe := &fakeEngine{got: make(chan handoff, 2)}
	b := NewBackend(svc, fe, func(string, core.Update) {})

	b.Download("t1", "https://rapidgator.net/file/x", nil, 1)
	first := <-svc.held
	b.Pause("t1")
	b.Resume("t1")
	second := <-svc.held
	close(first)
	// Give the first run the moment it needs to wind down.
	for giveUp := time.Now().Add(200 * time.Millisecond); time.Now().Before(giveUp); time.Sleep(5 * time.Millisecond) {
		b.mu.Lock()
		_, tracked := b.runs["t1"]
		b.mu.Unlock()
		if !tracked {
			break
		}
	}

	b.Pause("t1")
	close(second)
	svc.mu.Lock()
	resumed := svc.ctxs[1]
	svc.mu.Unlock()
	if resumed.Err() == nil {
		t.Fatal("the second pause did not reach the resumed unlock")
	}
	select {
	case h := <-fe.got:
		t.Fatalf("a paused task was handed to the engine: %+v", h)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAPauseAfterTheAnswerArrivedKeepsTheTaskOutOfTheEngine(t *testing.T) {
	svc := &heldService{held: make(chan chan struct{}, 1), answered: true}
	fe := &fakeEngine{got: make(chan handoff, 1)}
	b := NewBackend(svc, fe, func(string, core.Update) {})

	b.Download("t1", "https://rapidgator.net/file/x", nil, 1)
	release := <-svc.held
	b.Pause("t1")
	close(release)
	select {
	case h := <-fe.got:
		t.Fatalf("a paused task was handed to the engine: %+v", h)
	case <-time.After(100 * time.Millisecond):
	}
}

// The first unlock's login hangs and holds the lock until the client's own
// timeout. An unlock queued behind it has to give up at its own deadline, not
// start its login with whatever time is left once the lock comes free.
func TestUnlockWaitingForAnotherLoginEndsAtItsOwnDeadline(t *testing.T) {
	clients := map[string]func(base string) Service{
		"MultiUp":   func(base string) Service { return multiupAt(base) },
		"NeoDebrid": func(base string) Service { return neodebridAt(base) },
		"Mega-Debrid": func(base string) Service {
			m := NewMegaDebrid("amy", "secret-pass")
			m.base = base + "/api.php"
			return m
		},
	}
	for name, newService := range clients {
		t.Run(name, func(t *testing.T) {
			srv, arrived := hangingServer(t)
			svc := newService(srv.URL)

			firstCtx, stopFirst := context.WithCancel(context.Background())
			first := make(chan error, 1)
			go func() {
				_, err := svc.Unlock(firstCtx, "https://rapidgator.net/file/a")
				first <- err
			}()
			defer func() {
				stopFirst()
				<-first
			}()
			select {
			case <-arrived:
			case <-time.After(10 * time.Second):
				t.Fatal("the first unlock never sent its login")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			second := make(chan error, 1)
			go func() {
				_, err := svc.Unlock(ctx, "https://rapidgator.net/file/b")
				second <- err
			}()
			select {
			case err := <-second:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("second Unlock = %v, want it to end with its own deadline", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("the second unlock was still waiting for the first one's login after its own deadline")
			}
		})
	}
}

func TestHostInSet(t *testing.T) {
	set := map[string]bool{"rapidgator.net": true}
	for _, h := range []string{"rapidgator.net", "www.rapidgator.net", "dl5.rapidgator.net"} {
		if !HostInSet(h, set) {
			t.Errorf("HostInSet(%q) = false, want true", h)
		}
	}
	if HostInSet("example.com", set) {
		t.Error("unrelated host matched")
	}
}

// countingService unlocks every link to a direct URL of its own.
type countingService struct {
	mu      sync.Mutex
	unlocks int
}

func (*countingService) ID() string                                     { return "counting" }
func (*countingService) Label() string                                  { return "Counting" }
func (*countingService) Hosts(context.Context) (map[string]bool, error) { return nil, nil }

func (s *countingService) Unlock(context.Context, string) (Direct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unlocks++
	return Direct{URL: "https://cdn.example/" + strconv.Itoa(s.unlocks)}, nil
}

// A direct link can expire before the file is complete. The engine gets a way
// back to the service, which unlocks the same hoster link again for the rest.
func TestTheEngineCanAskTheServiceForAFreshLink(t *testing.T) {
	fe := &fakeEngine{got: make(chan handoff, 1)}
	b := NewBackend(&countingService{}, fe, func(string, core.Update) {})
	b.Download("t1", "https://rapidgator.net/file/x", nil, 1)

	var h handoff
	select {
	case h = <-fe.got:
	case <-time.After(10 * time.Second):
		t.Fatal("the link was never handed over")
	}
	if h.relink == nil {
		t.Fatal("the engine was given no way to a fresh link")
	}
	url, err := h.relink(context.Background())
	if err != nil || url != "https://cdn.example/2" {
		t.Errorf("relink = %q, %v; want the second unlock's https://cdn.example/2", url, err)
	}
}
