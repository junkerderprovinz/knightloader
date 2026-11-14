package signinbrowser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

const headlessUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/158.0.0.0 Safari/537.36"

type sent struct {
	session string
	method  string
	params  map[string]any
}

// fakeChromium answers the calls a session makes the way Chromium does, and
// keeps them for the test to look at.
type fakeChromium struct {
	tr *chanTransport

	mu         sync.Mutex
	calls      []sent
	cookies    []map[string]any
	autoAttach bool
	stopped    bool
	args       []string
	changed    chan struct{}
}

func newFakeChromium(args []string) *fakeChromium {
	f := &fakeChromium{tr: newChanTransport(), args: args, changed: make(chan struct{}, 1)}
	go f.serve()
	return f
}

func (f *fakeChromium) serve() {
	for {
		var b []byte
		select {
		case b = <-f.tr.out:
		case <-f.tr.done:
			return
		}
		var m message
		_ = json.Unmarshal(b, &m)
		var p map[string]any
		_ = json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		f.calls = append(f.calls, sent{m.SessionID, m.Method, p})
		f.mu.Unlock()
		select {
		case f.changed <- struct{}{}:
		default:
		}

		var result any = map[string]any{}
		after := func() {}
		switch m.Method {
		case "Browser.getVersion":
			result = map[string]any{"product": "HeadlessChrome/158.0.8127.18", "userAgent": headlessUA}
		case "Target.setAutoAttach":
			f.mu.Lock()
			f.autoAttach = true
			f.mu.Unlock()
		case "Target.createTarget":
			if p["url"] == "chrome://version/" {
				result = map[string]any{"targetId": "PROBE"}
				break
			}
			result = map[string]any{"targetId": "T1"}
			after = func() { f.attach("T1", "", "about:blank") }
		case "Target.attachToTarget":
			result = map[string]any{"sessionId": "S-" + p["targetId"].(string)}
		case "Runtime.evaluate":
			result = map[string]any{"result": map[string]any{"value": map[string]any{
				"brands":          []map[string]string{{"brand": "Not.A/Brand", "version": "24"}, {"brand": "Chromium", "version": "158"}},
				"fullVersionList": []map[string]string{{"brand": "Chromium", "version": "158.0.8127.18"}},
				"platform":        "Linux", "platformVersion": "6.1.0", "architecture": "x86", "bitness": "64",
				"model": "", "mobile": false, "wow64": false,
			}}}
		case "Storage.getCookies":
			f.mu.Lock()
			result = map[string]any{"cookies": slices.Clone(f.cookies)}
			f.mu.Unlock()
		case "Page.getNavigationHistory":
			result = map[string]any{"currentIndex": 1, "entries": []map[string]any{
				{"id": 11, "url": "https://www.patreon.com/"}, {"id": 12, "url": "https://www.patreon.com/login"},
			}}
		case "Page.navigate":
			target := strings.TrimPrefix(m.SessionID, "S-")
			after = func() {
				f.emit(m.SessionID, "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": target, "url": p["url"]}})
			}
		case "Browser.close":
			after = func() { f.tr.close() }
		}
		raw, _ := json.Marshal(result)
		reply, _ := json.Marshal(message{ID: m.ID, Result: raw, SessionID: m.SessionID})
		f.tr.in <- reply
		after()
	}
}

func (f *fakeChromium) emit(session, method string, params any) {
	raw, _ := json.Marshal(params)
	b, _ := json.Marshal(message{Method: method, Params: raw, SessionID: session})
	f.tr.in <- b
}

// attach is Chromium attaching a new page to the browser session, paused
// until it is let go.
func (f *fakeChromium) attach(target, opener, url string) {
	info := map[string]any{"targetId": target, "type": "page", "url": url}
	if opener != "" {
		info["openerId"] = opener
	}
	f.emit("", "Target.attachedToTarget", map[string]any{"sessionId": "S-" + target, "targetInfo": info, "waitingForDebugger": true})
}

func (f *fakeChromium) setCookies(c ...map[string]any) {
	f.mu.Lock()
	f.cookies = c
	f.mu.Unlock()
}

func (f *fakeChromium) sentCalls() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// waitFor waits until a call matching ok has been sent and returns it.
func (f *fakeChromium) waitFor(t *testing.T, what string, ok func(sent) bool) sent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		for _, c := range f.sentCalls() {
			if ok(c) {
				return c
			}
		}
		select {
		case <-f.changed:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("%s was never sent; sent: %v", what, f.sentCalls())
		}
	}
}

func (f *fakeChromium) index(ok func(sent) bool) int {
	return slices.IndexFunc(f.sentCalls(), ok)
}

func method(session, name string) func(sent) bool {
	return func(c sent) bool { return c.session == session && c.method == name }
}

// clock is a time the test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// harness is a Manager over fake browsers, one per start.
type harness struct {
	m        *Manager
	clock    *clock
	mu       sync.Mutex
	browsers []*fakeChromium
	handed   chan handed
}

type handed struct {
	site string
	jar  string
}

func newHarness(t *testing.T) *harness {
	h := &harness{clock: &clock{t: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}, handed: make(chan handed, 16)}
	h.m = NewManager(context.Background(), Config{
		Binary:  "chromium",
		Profile: t.TempDir(),
		Session: func(s sitelogin.Site, jar string) any {
			h.handed <- handed{s.ID, jar}
			return map[string]string{"state": "seen"}
		},
	})
	h.m.cfg.start = func(_ context.Context, args []string) (transport, func(), error) {
		f := newFakeChromium(args)
		h.mu.Lock()
		h.browsers = append(h.browsers, f)
		h.mu.Unlock()
		return f.tr, func() {
			f.mu.Lock()
			f.stopped = true
			f.mu.Unlock()
			f.tr.close()
		}, nil
	}
	h.m.cfg.lookup = func(context.Context, string) httpx.Scope { return httpx.ScopePublic }
	h.m.cfg.now = h.clock.now
	h.m.cfg.every = time.Hour
	t.Cleanup(func() { h.m.Close(context.Background()) })
	return h
}

func (h *harness) browser(t *testing.T, i int) *fakeChromium {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if i >= len(h.browsers) {
		t.Fatalf("browser %d was never started", i)
	}
	return h.browsers[i]
}

// open opens a site's window and waits for its login page to load.
func (h *harness) open(t *testing.T, id string) *fakeChromium {
	t.Helper()
	if err := h.m.Open(context.Background(), site(t, id), 800, 600, 1); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	f := h.browsers[len(h.browsers)-1]
	h.mu.Unlock()
	f.waitFor(t, "the login page", method("S-T1", "Page.navigate"))
	return f
}

// viewer is the web UI's end of the window.
type viewer struct {
	frames chan []byte
	msgs   chan map[string]any
}

func newViewer() *viewer {
	return &viewer{frames: make(chan []byte, 16), msgs: make(chan map[string]any, 256)}
}

func (v *viewer) Frame(b []byte) error {
	select {
	case v.frames <- b:
	default:
	}
	return nil
}

func (v *viewer) Send(msg any) error {
	raw, _ := json.Marshal(msg)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	v.msgs <- m
	return nil
}

// next waits for a message of the given type.
func (v *viewer) next(t *testing.T, kind string) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-v.msgs:
			if m["type"] == kind {
				return m
			}
		case <-deadline:
			t.Fatalf("no %q message reached the viewer", kind)
		}
	}
}

// watch attaches v until the returned stop is called.
func (h *harness) watch(t *testing.T, v *viewer) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.m.Watch(ctx, v)
	}()
	v.next(t, "page")
	return func() {
		cancel()
		<-done
	}
}

func cookie(domain, name, value string) map[string]any {
	return map[string]any{"name": name, "value": value, "domain": domain, "path": "/", "expires": -1, "secure": true, "session": true}
}

func frame(data string) map[string]any {
	return map[string]any{"data": base64.StdEncoding.EncodeToString([]byte(data)), "sessionId": 7, "metadata": map[string]any{}}
}

func hasArg(args []string, prefix string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, prefix) })
}
