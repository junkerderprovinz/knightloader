// Package signinbrowser runs a headless Chromium that a user signs in to a
// site with from KnightLoader's web interface, for a server where no browser
// of the user's runs. The page is streamed to the interface as screencast
// frames and the user's pointer and keys are replayed into it, and the site's
// cookies are read back from the browser and handed on as its session.
//
// The browser is started on demand and stopped when the window closes or is
// left alone. Its top frame stays on the site and the providers it signs in
// with, and nothing it loads may reach this machine or the LAN.
package signinbrowser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// ErrNotOpen is a call for a window when none is open.
var ErrNotOpen = errors.New("signinbrowser: no sign-in window is open")

const (
	// idleAfter closes a window nobody has touched, such as one left open in
	// a tab that was forgotten.
	idleAfter = 5 * time.Minute
	// goneAfter is how long a window waits for its page to come back, so a
	// reload of the interface does not lose the sign-in under way.
	goneAfter = 30 * time.Second
	// pollEvery is how often the cookies are read while the window is open.
	pollEvery = 3 * time.Second

	minSide   = 320
	maxWidth  = 1600
	maxHeight = 1200
)

// Viewer is the web interface's end of the window. Both methods may be called
// from several goroutines.
type Viewer interface {
	Frame(jpeg []byte) error
	Send(msg any) error
}

// Config sets up a Manager.
type Config struct {
	// Binary is the Chromium to start.
	Binary string
	// Profile is the directory the browser keeps its profile in, so a sign-in
	// outlives the browser and KnightLoader.
	Profile string
	// NoSandbox starts Chromium without its sandbox, which cannot be built in
	// a container under Docker's default seccomp profile.
	NoSandbox bool
	// Session hears the site's cookies as a cookies.txt when the browser comes
	// to hold a sign-in and when the window closes holding one, and an empty
	// jar when a sign-in it held is gone. What it returns is shown in the
	// window.
	Session func(site sitelogin.Site, jar string) any

	start  func(ctx context.Context, args []string) (transport, func(), error)
	lookup func(context.Context, string) httpx.Scope
	now    func() time.Time
	every  time.Duration
	args   []string
}

// Manager runs one sign-in window at a time.
type Manager struct {
	ctx context.Context
	cfg Config

	// mu keeps one Open or Close at a time, and cmu guards cur, which input reads
	// while a window starts.
	mu  sync.Mutex
	cmu sync.Mutex
	cur *session
}

// NewManager makes a Manager whose browsers end with ctx.
func NewManager(ctx context.Context, cfg Config) *Manager {
	if cfg.start == nil {
		cfg.start = func(ctx context.Context, args []string) (transport, func(), error) {
			return launch(ctx, cfg.Binary, cfg.Profile, args)
		}
	}
	if cfg.lookup == nil {
		cfg.lookup = httpx.ScopeOfHost
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.every == 0 {
		cfg.every = pollEvery
	}
	return &Manager{ctx: ctx, cfg: cfg}
}

// Available reports whether the configured Chromium is there to start.
func (m *Manager) Available() bool {
	if m.cfg.Binary == "" {
		return false
	}
	_, err := exec.LookPath(m.cfg.Binary)
	return err == nil
}

// OpenSite is the id of the site whose window is open, or "".
func (m *Manager) OpenSite() string {
	if s := m.current(); s != nil {
		return s.site.ID
	}
	return ""
}

func (m *Manager) current() *session {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	return m.cur
}

// Open opens a window on the site's login page, sized width by height CSS
// pixels at the given pixel ratio. A window already open for the site is
// kept, and one for another site is closed first, so its session is handed on.
func (m *Manager) Open(ctx context.Context, site sitelogin.Site, width, height int, scale float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	vp := fit(width, height, scale)
	if s := m.current(); s != nil {
		if s.site.ID == site.ID {
			s.input(Input{Type: "resize", Width: vp.Width, Height: vp.Height, Scale: vp.Scale})
			return nil
		}
		m.stop(ctx, s, "replaced")
	}
	s, err := m.start(ctx, site, vp)
	if err != nil {
		return err
	}
	m.cmu.Lock()
	m.cur = s
	m.cmu.Unlock()
	go m.watchClock(s)
	return nil
}

// Close closes the open window, handing on the session it holds, and returns
// the id of its site, or "" when none was open.
func (m *Manager) Close(ctx context.Context) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.current()
	if s == nil {
		return ""
	}
	m.stop(ctx, s, "done")
	return s.site.ID
}

// Watch streams the open window to v until ctx ends, the window closes or
// another viewer takes it over.
func (m *Manager) Watch(ctx context.Context, v Viewer) error {
	s := m.current()
	if s == nil {
		return ErrNotOpen
	}
	s.watch(ctx, v)
	return nil
}

// Input replays one thing the user did into the page in front.
func (m *Manager) Input(in Input) {
	if s := m.current(); s != nil {
		s.input(in)
	}
}

// Jar reads the session the open window holds for a site, which is fresher
// than the one stored while the site keeps renewing its cookies.
func (m *Manager) Jar(ctx context.Context, siteID string) (string, bool) {
	s := m.current()
	if s == nil || s.site.ID != siteID {
		return "", false
	}
	return s.jar(ctx)
}

func (m *Manager) watchClock(s *session) {
	t := time.NewTicker(m.cfg.every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.tick(m.cfg.now())
		case <-s.done:
			return
		}
	}
}

// tick closes a window left alone and otherwise reads its cookies.
func (m *Manager) tick(now time.Time) {
	s := m.current()
	if s == nil {
		return
	}
	s.mu.Lock()
	idle := now.Sub(s.lastInput) >= idleAfter
	gone := s.viewer == nil && !s.leftAt.IsZero() && now.Sub(s.leftAt) >= goneAfter
	s.mu.Unlock()
	if idle || gone {
		reason := "gone"
		if idle {
			reason = "idle"
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.current() == s {
			m.stop(m.ctx, s, reason)
		}
		return
	}
	s.poll(m.ctx)
}

// stop closes s and forgets it. The caller holds m.mu.
func (m *Manager) stop(ctx context.Context, s *session, reason string) {
	s.stop(ctx, reason)
	m.cmu.Lock()
	if m.cur == s {
		m.cur = nil
	}
	m.cmu.Unlock()
}

func (m *Manager) switches() []string {
	args := []string{
		"--headless=new",
		"--user-data-dir=" + m.cfg.Profile,
		"--password-store=basic",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--deny-permission-prompts",
		// Without this navigator.webdriver is true, which a sign-in page
		// reads as a robot at the keys.
		"--disable-blink-features=AutomationControlled",
		// Docker gives a container 64 MB of /dev/shm, which a sign-in page
		// with a captcha outgrows.
		"--disable-dev-shm-usage",
	}
	if m.cfg.NoSandbox {
		args = append(args, "--no-sandbox")
	}
	return append(args, m.cfg.args...)
}

func (m *Manager) start(ctx context.Context, site sitelogin.Site, vp viewport) (*session, error) {
	if err := os.MkdirAll(m.cfg.Profile, 0o700); err != nil {
		return nil, err
	}
	// A browser killed with its container leaves its lock behind, and the
	// next one would take the profile for another computer's. Only one
	// browser of this Manager runs at a time.
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(m.cfg.Profile, name))
	}
	tr, kill, err := m.cfg.start(m.ctx, m.switches())
	if err != nil {
		return nil, err
	}
	s := &session{
		m:         m,
		site:      site,
		guard:     newGuard(site, m.cfg.lookup),
		c:         newClient(tr),
		kill:      kill,
		vp:        vp,
		lastInput: m.cfg.now(),
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
	}
	go s.handleEvents()
	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.setup(setupCtx); err != nil {
		s.c.close()
		kill()
		return nil, err
	}
	return s, nil
}

// page is a tab of the window: its own page and the sign-in popups it opens,
// the last of which is in front.
type page struct {
	target  string
	session string
}

type session struct {
	m     *Manager
	site  sitelogin.Site
	guard *guard
	c     *client
	kill  func()

	ua   string
	meta *uaMetadata

	// showMu keeps one restart of the stream at a time, since a commit and
	// a new viewer can ask for one together.
	showMu sync.Mutex

	mu        sync.Mutex
	vp        viewport
	pages     []page
	viewer    Viewer
	kicked    chan struct{}
	leftAt    time.Time
	lastInput time.Time
	signedIn  bool
	closed    bool

	ready chan struct{}
	done  chan struct{}
}

type brand struct {
	Brand   string `json:"brand"`
	Version string `json:"version"`
}

// uaMetadata is the client hints Chromium sends, as the DevTools protocol
// takes them back.
type uaMetadata struct {
	Brands          []brand `json:"brands"`
	FullVersionList []brand `json:"fullVersionList"`
	Platform        string  `json:"platform"`
	PlatformVersion string  `json:"platformVersion"`
	Architecture    string  `json:"architecture"`
	Model           string  `json:"model"`
	Mobile          bool    `json:"mobile"`
	Bitness         string  `json:"bitness"`
	Wow64           bool    `json:"wow64"`
}

func (s *session) setup(ctx context.Context) error {
	var version struct {
		UserAgent string `json:"userAgent"`
	}
	if err := s.c.call(ctx, "", "Browser.getVersion", nil, &version); err != nil {
		return err
	}
	// Headless Chromium calls itself HeadlessChrome in its user agent.
	// Overriding the name drops the client hints unless they are passed
	// back, so they are read first from a page that never touches the
	// network.
	if meta := s.probeClientHints(ctx); meta != nil {
		s.ua = strings.Replace(version.UserAgent, "HeadlessChrome/", "Chrome/", 1)
		s.meta = meta
	}
	for _, c := range []struct {
		method string
		params any
	}{
		{"Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}},
		// Every request of every target stops here for the guard.
		{"Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": "*", "requestStage": "Request"}}}},
		// A new page or worker waits until it is set up, so a popup cannot
		// load before the guard knows it is a page.
		{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
		{"Target.createTarget", map[string]any{"url": "about:blank"}},
	} {
		if err := s.c.call(ctx, "", c.method, c.params, nil); err != nil {
			return err
		}
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.c.call(ctx, s.front().session, "Page.navigate", map[string]string{"url": s.site.LoginURL}, nil)
}

// probeClientHints reads the browser's own client hints from chrome://version,
// a secure context, which a blank page is not. Nil leaves the user agent
// alone.
func (s *session) probeClientHints(ctx context.Context) *uaMetadata {
	var target struct {
		TargetID string `json:"targetId"`
	}
	if s.c.call(ctx, "", "Target.createTarget", map[string]string{"url": "chrome://version/"}, &target) != nil {
		return nil
	}
	defer func() {
		_ = s.c.call(ctx, "", "Target.closeTarget", map[string]string{"targetId": target.TargetID}, nil)
	}()
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if s.c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attached) != nil {
		return nil
	}
	const expr = `navigator.userAgentData ? navigator.userAgentData.getHighEntropyValues(` +
		`["architecture", "bitness", "fullVersionList", "model", "platformVersion", "wow64"]) : null`
	for range 40 {
		var res struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
		}
		// Until the page has committed there is no context to ask in.
		err := s.c.call(ctx, attached.SessionID, "Runtime.evaluate",
			map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true}, &res)
		var meta uaMetadata
		if err == nil && json.Unmarshal(res.Result.Value, &meta) == nil && len(meta.Brands) > 0 {
			return &meta
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func (s *session) front() page {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pages) == 0 {
		return page{}
	}
	return s.pages[len(s.pages)-1]
}

// isPage reports whether a frame is the top frame of one of the tabs, whose
// id is its target's.
func (s *session) isPage(frameID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.pages, func(p page) bool { return p.target == frameID })
}

func (s *session) handleEvents() {
	for ev := range s.c.events() {
		switch ev.method {
		case "Target.attachedToTarget":
			s.attached(ev.params)
		case "Target.detachedFromTarget":
			s.detached(ev.params)
		case "Fetch.requestPaused":
			go s.decide(ev.params)
		case "Page.screencastFrame":
			s.frame(ev.session, ev.params)
		case "Page.frameNavigated":
			s.navigated(ev.session, ev.params)
		case "Page.navigatedWithinDocument":
			if ev.session == s.front().session {
				go s.sendPage()
			}
		case "Page.javascriptDialogOpening":
			// Nobody sees the dialog, which would hold the page up. An
			// alert or a leave-this-page question is answered yes, any
			// other question no.
			var d struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(ev.params, &d)
			go s.call(ev.session, "Page.handleJavaScriptDialog", map[string]bool{"accept": d.Type == "alert" || d.Type == "beforeunload"})
		}
	}
}

// call sends a command whose answer nobody waits for.
func (s *session) call(session, method string, params any) {
	ctx, cancel := context.WithTimeout(s.m.ctx, 10*time.Second)
	defer cancel()
	_ = s.c.call(ctx, session, method, params, nil)
}

func (s *session) attached(raw json.RawMessage) {
	var ev struct {
		SessionID  string `json:"sessionId"`
		TargetInfo struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfo"`
		Waiting bool `json:"waitingForDebugger"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	// A target that is not waiting was there before auto-attach, such as the
	// blank tab Chromium starts with, and is left alone.
	if !ev.Waiting {
		return
	}
	sess := ev.SessionID
	// The commands go out in order without waiting for their answers: a
	// target held for the debugger answers some of them only once it runs.
	switch ev.TargetInfo.Type {
	case "page":
		s.mu.Lock()
		s.pages = append(s.pages, page{target: ev.TargetInfo.TargetID, session: sess})
		first := len(s.pages) == 1
		vp := s.vp
		s.mu.Unlock()
		s.setupPage(sess, vp)
		_ = s.c.send(sess, "Runtime.runIfWaitingForDebugger", nil)
		// The window's own page starts streaming when the login page
		// commits. Before that Chromium has nothing to stream.
		if first {
			close(s.ready)
			return
		}
		go s.showFront()
	case "worker", "shared_worker", "service_worker":
		if s.ua != "" {
			_ = s.c.send(sess, "Network.setUserAgentOverride", map[string]any{"userAgent": s.ua, "userAgentMetadata": s.meta})
		}
		fallthrough
	default:
		_ = s.c.send(sess, "Runtime.runIfWaitingForDebugger", nil)
	}
}

func (s *session) setupPage(sess string, vp viewport) {
	_ = s.c.send(sess, "Page.enable", nil)
	// A file picker would open nowhere, and intercepting it keeps the page from
	// waiting for one.
	_ = s.c.send(sess, "Page.setInterceptFileChooserDialog", map[string]bool{"enabled": true})
	if s.ua != "" {
		_ = s.c.send(sess, "Emulation.setUserAgentOverride", map[string]any{"userAgent": s.ua, "userAgentMetadata": s.meta})
	}
	_ = s.c.send(sess, "Emulation.setDeviceMetricsOverride", metrics(vp))
	// A headless page never has the focus, and a sign-in field that waits
	// for it would never show its caret.
	_ = s.c.send(sess, "Emulation.setFocusEmulationEnabled", map[string]bool{"enabled": true})
}

func metrics(vp viewport) map[string]any {
	return map[string]any{"width": vp.Width, "height": vp.Height, "deviceScaleFactor": vp.Scale, "mobile": false}
}

func (s *session) detached(raw json.RawMessage) {
	var ev struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	s.mu.Lock()
	i := slices.IndexFunc(s.pages, func(p page) bool { return p.session == ev.SessionID })
	if i < 0 {
		s.mu.Unlock()
		return
	}
	wasFront := i == len(s.pages)-1
	s.pages = slices.Delete(s.pages, i, i+1)
	empty := len(s.pages) == 0
	s.mu.Unlock()
	if empty {
		// The window's own page is gone, which only a crash does.
		go func() {
			s.m.mu.Lock()
			defer s.m.mu.Unlock()
			if s.m.current() == s {
				s.m.stop(s.m.ctx, s, "crashed")
			}
		}()
		return
	}
	if wasFront {
		go s.showFront()
	}
}

// showFront streams the page in front, whose frames are the only ones the
// viewer sees.
func (s *session) showFront() {
	s.showMu.Lock()
	defer s.showMu.Unlock()
	s.mu.Lock()
	vp := s.vp
	pages := slices.Clone(s.pages)
	s.mu.Unlock()
	if len(pages) == 0 {
		return
	}
	for _, p := range pages[:len(pages)-1] {
		s.call(p.session, "Page.stopScreencast", nil)
	}
	front := pages[len(pages)-1]
	ctx, cancel := context.WithTimeout(s.m.ctx, 5*time.Second)
	defer cancel()
	// Right after a page commits, Chromium answers for a moment as if the
	// tab had no page.
	for {
		_ = s.c.call(ctx, front.session, "Page.stopScreencast", nil, nil)
		err := s.c.call(ctx, front.session, "Page.startScreencast", map[string]any{
			"format":    "jpeg",
			"quality":   80,
			"maxWidth":  int(float64(vp.Width) * vp.Scale),
			"maxHeight": int(float64(vp.Height) * vp.Scale),
		}, nil)
		if err == nil {
			break
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	s.sendPage()
	// The screencast sends a frame only when the page paints, and a page
	// that has finished loading may not paint again for a new viewer.
	var shot struct {
		Data string `json:"data"`
	}
	if s.c.call(ctx, front.session, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 80}, &shot) != nil {
		return
	}
	s.mu.Lock()
	v := s.viewer
	s.mu.Unlock()
	if b, err := base64.StdEncoding.DecodeString(shot.Data); err == nil && v != nil && len(b) > 0 {
		_ = v.Frame(b)
	}
}

// sendPage tells the viewer where the page in front is.
func (s *session) sendPage() {
	front := s.front()
	ctx, cancel := context.WithTimeout(s.m.ctx, 5*time.Second)
	defer cancel()
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if s.c.call(ctx, front.session, "Page.getNavigationHistory", nil, &h) != nil || h.CurrentIndex >= len(h.Entries) {
		return
	}
	host := ""
	if u, err := url.Parse(h.Entries[h.CurrentIndex].URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
		host = u.Hostname()
	}
	s.mu.Lock()
	popup := len(s.pages) > 1
	s.mu.Unlock()
	s.send(map[string]any{"type": "page", "host": host, "canGoBack": h.CurrentIndex > 0 || popup})
}

func (s *session) send(msg any) {
	s.mu.Lock()
	v := s.viewer
	s.mu.Unlock()
	if v != nil {
		_ = v.Send(msg)
	}
}

func (s *session) decide(raw json.RawMessage) {
	var ev struct {
		RequestID    string `json:"requestId"`
		FrameID      string `json:"frameId"`
		ResourceType string `json:"resourceType"`
		Request      struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.m.ctx, 10*time.Second)
	defer cancel()
	top := s.isPage(ev.FrameID)
	if s.guard.allows(ctx, ev.Request.URL, ev.ResourceType, top) {
		_ = s.c.call(ctx, "", "Fetch.continueRequest", map[string]string{"requestId": ev.RequestID}, nil)
		return
	}
	if top && ev.ResourceType == "Document" {
		// No Content keeps the page where it was, where an error page would put the
		// blocked address in the bar and in the history.
		_ = s.c.call(ctx, "", "Fetch.fulfillRequest", map[string]any{"requestId": ev.RequestID, "responseCode": 204}, nil)
		s.refuse(ev.FrameID, ev.Request.URL)
		return
	}
	_ = s.c.call(ctx, "", "Fetch.failRequest", map[string]string{"requestId": ev.RequestID, "errorReason": "BlockedByClient"}, nil)
}

// refuse tells the viewer a page was kept from the top frame, and closes a
// popup that has nothing else to show.
func (s *session) refuse(target, rawurl string) {
	host := rawurl
	if u, err := url.Parse(rawurl); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	s.send(map[string]string{"type": "blocked", "host": host})
	s.mu.Lock()
	popup := len(s.pages) > 0 && s.pages[0].target != target
	s.mu.Unlock()
	if popup {
		s.call("", "Target.closeTarget", map[string]string{"targetId": target})
	}
}

func (s *session) frame(sess string, raw json.RawMessage) {
	var ev struct {
		Data      string `json:"data"`
		SessionID int    `json:"sessionId"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	s.mu.Lock()
	v := s.viewer
	s.mu.Unlock()
	// Chromium sends a page's next frame only once this one is acknowledged,
	// so acknowledging after the viewer took it gives a slow connection
	// fewer frames instead of a backlog, and the events keep flowing.
	go func() {
		if v != nil && sess == s.front().session {
			if b, err := base64.StdEncoding.DecodeString(ev.Data); err == nil {
				_ = v.Frame(b)
			}
		}
		s.call(sess, "Page.screencastFrameAck", map[string]int{"sessionId": ev.SessionID})
	}()
}

func (s *session) navigated(sess string, raw json.RawMessage) {
	var ev struct {
		Frame struct {
			ID       string `json:"id"`
			ParentID string `json:"parentId"`
			URL      string `json:"url"`
		} `json:"frame"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Frame.ParentID != "" {
		return
	}
	u := ev.Frame.URL
	// The guard sees only what goes over the network, so a top frame that
	// lands on a local page, a data: URL or a browser page is sent back.
	if u != "about:blank" && !strings.HasPrefix(u, "chrome-error://") && !s.guard.list.topLevel(u) {
		s.mu.Lock()
		own := len(s.pages) > 0 && s.pages[0].session == sess
		s.mu.Unlock()
		if own {
			go s.call(sess, "Page.navigate", map[string]string{"url": s.site.LoginURL})
			s.send(map[string]string{"type": "blocked", "host": ev.Frame.URL})
			return
		}
		go s.refuse(ev.Frame.ID, u)
		return
	}
	if p, err := url.Parse(u); err == nil && p.Hostname() == "accounts.google.com" && strings.Contains(p.Path, "rejected") {
		s.send(map[string]string{"type": "refused"})
	}
	// A page that moves to another site's process may stop streaming, so
	// each page it commits starts the stream again.
	if sess == s.front().session {
		go s.showFront()
	}
}

func (s *session) watch(ctx context.Context, v Viewer) {
	kicked := make(chan struct{})
	s.mu.Lock()
	if s.kicked != nil {
		close(s.kicked)
	}
	s.viewer, s.kicked = v, kicked
	s.lastInput = s.m.cfg.now()
	s.mu.Unlock()
	go s.showFront()

	select {
	case <-ctx.Done():
	case <-kicked:
		_ = v.Send(map[string]string{"type": "closed", "reason": "elsewhere"})
	case <-s.done:
	}
	s.mu.Lock()
	if s.kicked == kicked {
		s.viewer, s.kicked = nil, nil
		s.leftAt = s.m.cfg.now()
	}
	s.mu.Unlock()
}

func (s *session) input(in Input) {
	s.mu.Lock()
	s.lastInput = s.m.cfg.now()
	vp := s.vp
	s.mu.Unlock()
	front := s.front()
	switch in.Type {
	case "back":
		ctx, cancel := context.WithTimeout(s.m.ctx, 5*time.Second)
		defer cancel()
		var h struct {
			CurrentIndex int `json:"currentIndex"`
			Entries      []struct {
				ID int `json:"id"`
			} `json:"entries"`
		}
		if s.c.call(ctx, front.session, "Page.getNavigationHistory", nil, &h) != nil {
			return
		}
		if h.CurrentIndex > 0 && h.CurrentIndex <= len(h.Entries) {
			_ = s.c.call(ctx, front.session, "Page.navigateToHistoryEntry", map[string]int{"entryId": h.Entries[h.CurrentIndex-1].ID}, nil)
			return
		}
		// A popup on its first page goes back to the page that opened it.
		s.mu.Lock()
		popup := len(s.pages) > 1
		s.mu.Unlock()
		if popup {
			_ = s.c.call(ctx, "", "Target.closeTarget", map[string]string{"targetId": front.target}, nil)
		}
	case "reload":
		s.call(front.session, "Page.reload", nil)
	case "resize":
		vp = fit(in.Width, in.Height, in.Scale)
		s.mu.Lock()
		s.vp = vp
		pages := slices.Clone(s.pages)
		s.mu.Unlock()
		for _, p := range pages {
			s.call(p.session, "Emulation.setDeviceMetricsOverride", metrics(vp))
		}
		s.showFront()
	default:
		if method, params, ok := in.command(vp); ok {
			s.call(front.session, method, params)
		}
	}
}

// fit keeps a requested size within what a sign-in page needs and a screen
// shows.
func fit(width, height int, scale float64) viewport {
	return viewport{
		Width:  min(max(width, minSide), maxWidth),
		Height: min(max(height, minSide), maxHeight),
		Scale:  min(max(scale, 1), 3),
	}
}

// jar reads the site's cookies from the browser as a cookies.txt.
func (s *session) jar(ctx context.Context) (string, bool) {
	var res struct {
		Cookies []struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Domain  string  `json:"domain"`
			Path    string  `json:"path"`
			Expires float64 `json:"expires"`
			Secure  bool    `json:"secure"`
			Session bool    `json:"session"`
		} `json:"cookies"`
	}
	if s.c.call(ctx, "", "Storage.getCookies", nil, &res) != nil {
		return "", false
	}
	cookies := make([]sitelogin.BrowserCookie, 0, len(res.Cookies))
	for _, c := range res.Cookies {
		cookies = append(cookies, sitelogin.BrowserCookie{
			Domain: c.Domain,
			// DevTools writes a domain cookie with its leading dot and a
			// host's own cookie without.
			HostOnly: !strings.HasPrefix(c.Domain, "."),
			Path:     c.Path,
			Secure:   c.Secure,
			Session:  c.Session,
			Expires:  max(c.Expires, 0),
			Name:     c.Name,
			Value:    c.Value,
		})
	}
	return sitelogin.JarFromCookies(s.site, cookies, s.m.cfg.now()), true
}

// poll hands the session on when a sign-in appears in the browser or one it
// held goes.
func (s *session) poll(ctx context.Context) {
	jar, ok := s.jar(ctx)
	if !ok {
		return
	}
	holds := sitelogin.HoldsSignIn(s.site, jar, s.m.cfg.now())
	s.mu.Lock()
	changed := holds != s.signedIn
	s.signedIn = holds
	s.mu.Unlock()
	if !changed {
		return
	}
	if !holds {
		jar = ""
	}
	s.hand(jar)
}

func (s *session) hand(jar string) {
	if s.m.cfg.Session == nil {
		return
	}
	s.send(map[string]any{"type": "login", "login": s.m.cfg.Session(s.site, jar)})
}

func (s *session) stop(ctx context.Context, reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if jar, ok := s.jar(ctx); ok && sitelogin.HoldsSignIn(s.site, jar, s.m.cfg.now()) {
		s.hand(jar)
	}
	s.send(map[string]string{"type": "closed", "reason": reason})
	// Closed from inside, Chromium writes its cookies out before it exits.
	_ = s.c.call(ctx, "", "Browser.close", nil, nil)
	s.kill()
	s.c.close()
	close(s.done)
}
