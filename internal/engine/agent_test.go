package engine

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
	"github.com/junkerderprovinz/knightloader/internal/throttle"
)

// pickyOrigin serves body only to a client that names itself KnightLoader and
// hangs up on everyone else before answering, the way some speed-test mirrors
// treat a browser agent coming from a program.
type pickyOrigin struct {
	srv  *httptest.Server
	body []byte

	mu     sync.Mutex
	agents []string
}

func newPickyOrigin(t *testing.T, body []byte) *pickyOrigin {
	t.Helper()
	o := &pickyOrigin{body: body}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		o.agents = append(o.agents, r.UserAgent())
		o.mu.Unlock()
		if !strings.HasPrefix(r.UserAgent(), "KnightLoader/") {
			panic(http.ErrAbortHandler)
		}
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(o.body))
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *pickyOrigin) seen() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.agents...)
}

// run starts job on a fresh engine, through proxy when it is not empty, and
// waits for it to finish or fail.
func run(t *testing.T, job Job, proxy string) (*Engine, core.Update) {
	t.Helper()
	settled := make(chan core.Update, 1)
	e, err := New(t.TempDir(), func(_ string, u core.Update) {
		if u.Status == core.StatusDone || u.Status == core.StatusError {
			select {
			case settled <- u:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.UseProxy(proxy); err != nil {
		t.Fatal(err)
	}
	job.TaskID = "t1"
	e.Start(job)
	select {
	case u := <-settled:
		return e, u
	case <-time.After(30 * time.Second):
		t.Fatal("the download never settled")
		return nil, core.Update{}
	}
}

func TestAServerThatHangsUpOnTheBrowserAgentGetsKnightLoaders(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts; see settle")
	}
	px, err := netproxy.Start(throttle.New())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	for name, proxy := range map[string]string{"direct": "", "through the loopback proxy": px.Addr()} {
		t.Run(name, func(t *testing.T) {
			body := bytes.Repeat([]byte("speed test "), 50000)
			o := newPickyOrigin(t, body)

			e, u := run(t, Job{URL: o.srv.URL + "/f.bin", Conns: 4}, proxy)
			if u.Status != core.StatusDone {
				t.Fatalf("settled as %s (%s), want done", u.Status, u.Err)
			}
			got, err := os.ReadFile(u.File)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("wrote %d bytes, want the %d the server sent", len(got), len(body))
			}
			agents := o.seen()
			for i, a := range agents[1:] {
				if !strings.HasPrefix(a, "KnightLoader/") {
					t.Fatalf("request %d went out as %q after the server had hung up on that agent", i+2, a)
				}
			}
			e.mu.Lock()
			kept := e.jobs["t1"].Headers["User-Agent"]
			e.mu.Unlock()
			if !strings.HasPrefix(kept, "KnightLoader/") {
				t.Fatalf("the task kept agent %q, so mending it would hit the same wall", kept)
			}
		})
	}
}

func TestAServerThatHangsUpOnEveryAgentSaysSoInsteadOfEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)

	_, u := run(t, Job{URL: srv.URL + "/f.bin", Conns: 1}, "")
	if u.Status != core.StatusError {
		t.Fatalf("settled as %s, want an error", u.Status)
	}
	if !strings.Contains(u.Err, "the server closed the connection") || strings.Contains(u.Err, "EOF") {
		t.Fatalf("error %q, want it to say the server closed the connection", u.Err)
	}
}

func TestAnAgentTheCallerChoseIsNotSwappedForKnightLoaders(t *testing.T) {
	o := newPickyOrigin(t, []byte("x"))

	_, u := run(t, Job{URL: o.srv.URL + "/f.bin", Conns: 1, Headers: map[string]string{"user-agent": "HosterApp/2"}}, "")
	if u.Status != core.StatusError {
		t.Fatalf("settled as %s, want an error", u.Status)
	}
	for _, a := range o.seen() {
		if a != "HosterApp/2" {
			t.Fatalf("a request went out as %q, want only the caller's agent", a)
		}
	}
}

// A link whose server cannot be reached fails as a connection that could not
// be made, not as the 502 the loopback proxy answers for it.
func TestAnUnreachableServerIsNotReportedAsA502(t *testing.T) {
	px, err := netproxy.Start(throttle.New())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	link := "http://" + ln.Addr().String() + "/f.bin"
	ln.Close()

	settled := make(chan core.Update, 1)
	e, err := New(t.TempDir(), func(_ string, u core.Update) {
		if u.Status == core.StatusError {
			select {
			case settled <- u:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.UseProxy(px.Addr()); err != nil {
		t.Fatal(err)
	}
	e.ExplainBadGateway(px.Unreachable)
	e.Start(Job{TaskID: "t1", URL: link, Conns: 1})

	select {
	case u := <-settled:
		if badGateway(u.Err) || !strings.Contains(u.Err, "dial tcp") {
			t.Fatalf("failed with %q, want the dial failure", u.Err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the download never failed")
	}
}
