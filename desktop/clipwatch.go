package main

// The clipboard watch of the desktop build. A page can read the clipboard
// only while its window has focus, so here the watch runs in Go and keeps
// going while the window is hidden in the tray. Its switch and its target are
// the page's own, in the shared interface state (web/src/lib/clipboardWatch.ts),
// and the page leaves the watching to this side when it runs in the window.
//
// Only the links leave the clipboard, and the rule for what counts as one is
// the page's; clipboardWatch.cases.json holds both sides to it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/clipwatch"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// clipWatchEvent carries a clipOutcome to the main window's page.
const clipWatchEvent = "clipboardWatch"

// clipPollInterval matches the page's POLL_MS.
const clipPollInterval = 1200 * time.Millisecond

// clipLeaseRenewal matches the page's RENEW_MS.
const clipLeaseRenewal = time.Minute

// clipLeaseWait bounds a lease call to a peer, which holds up the polling.
const clipLeaseWait = 10 * time.Second

// linkSchemes are the starts a word needs to count as a link, as in the
// page's LOOKS_LIKE_A_LINK.
var linkSchemes = []string{"http://", "https://", "magnet:?", "ftp://"}

// clipboardLinks returns the words of text that are links, in order. Words
// are split on what a JavaScript \s matches, so the page and this side find
// the same ones.
func clipboardLinks(text string) []string {
	var links []string
	for _, word := range strings.FieldsFunc(text, isScriptSpace) {
		for _, s := range linkSchemes {
			if len(word) > len(s) && strings.EqualFold(word[:len(s)], s) {
				links = append(links, word)
				break
			}
		}
	}
	return links
}

// isScriptSpace is JavaScript's \s: Go's spaces without NEL, plus the byte
// order mark.
func isScriptSpace(r rune) bool {
	return r == 0xFEFF || (unicode.IsSpace(r) && r != 0x85)
}

// clipOutcome is what the page shows for one copied link list, with the kinds
// of the page's WatchOutcome. "limited" says the clipboard is visible only
// while the window has focus.
type clipOutcome struct {
	Kind   string `json:"kind"`
	N      int    `json:"n,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// clipSettings are the two interface-state fields the page writes.
type clipSettings struct {
	On     bool   `json:"clipboardWatch"`
	Target string `json:"clipboardWatchTarget"`
}

// clipboardReader reads the system clipboard for the watch. latest returns
// fresh false when the clipboard cannot have changed since the last call, or
// when it could not be read.
type clipboardReader interface {
	latest() (text string, fresh bool)
	close()
}

// polledClipboard reads the clipboard on every call, unless stamp, the
// system's change counter where it has one, says nothing has changed.
type polledClipboard struct {
	read  func() (string, bool)
	stamp func() (uint64, bool)

	last  uint64
	known bool
}

func (p *polledClipboard) latest() (string, bool) {
	var now uint64
	stamped := false
	if p.stamp != nil {
		now, stamped = p.stamp()
		if stamped && p.known && now == p.last {
			return "", false
		}
	}
	text, ok := p.read()
	if ok && stamped {
		p.last, p.known = now, true
	}
	return text, ok
}

func (p *polledClipboard) close() {}

// clipWatch runs the watch. The fields other than state are its links to the
// app, the window and the clipboard, so a test can stand in for each.
type clipWatch struct {
	settings func() clipSettings
	// open starts reading the clipboard when the watch is switched on.
	// background is false where only a focused window sees it change.
	open    func() (r clipboardReader, background bool)
	deliver func(ctx context.Context, target string, links []string) (int, error)
	report  func(clipOutcome)
	// lease keeps the watch on the list of clipboard watchers of the instance
	// target names and reports whether another device asked it to stop;
	// leave takes it off that list.
	lease func(ctx context.Context, target string) (stop bool)
	leave func(ctx context.Context, target string)
	// switchOff turns the page's switch off.
	switchOff func()

	reader clipboardReader
	last   string

	// mu keeps a delivery or a lease call from overlapping stop, so none
	// reaches the app after it has closed and stop sees the lease as held.
	mu      sync.Mutex
	stopped bool
	// leased is when the lease was last renewed, zero while none is held, and
	// leasedTarget the instance it is held with.
	leased       time.Time
	leasedTarget string
}

func newClipWatch(a *app.App, read func() (string, bool), report func(clipOutcome)) *clipWatch {
	return &clipWatch{
		settings: func() clipSettings { return readClipSettings(a) },
		open:     func() (clipboardReader, bool) { return openClipboard(read) },
		deliver: func(ctx context.Context, target string, links []string) (int, error) {
			return deliverLinks(ctx, a, target, links)
		},
		report: report,
		lease:  func(ctx context.Context, target string) bool { return leaseClip(ctx, a, target) },
		leave:  func(ctx context.Context, target string) { leaveClip(ctx, a, target) },
		switchOff: func() {
			if err := switchClipOff(a); err != nil {
				log.Printf("desktop: clipboard watch: switching off: %v", err)
			}
		},
	}
}

// run checks the switch and the clipboard every clipPollInterval until ctx
// ends.
func (w *clipWatch) run(ctx context.Context) {
	tick := time.NewTicker(clipPollInterval)
	defer tick.Stop()
	defer w.closeReader()
	for {
		w.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// step is one round of run. Switched off, the clipboard is not read at all.
// The first read after switching on is only remembered, as on the page, so
// what was copied an hour ago does not arrive.
func (w *clipWatch) step(ctx context.Context) {
	s := w.settings()
	if w.keepLease(ctx, s) {
		w.closeReader()
		w.report(clipOutcome{Kind: "stopped"})
		return
	}
	if !s.On {
		w.closeReader()
		return
	}
	if w.reader == nil {
		r, background := w.open()
		w.reader = r
		text, _ := r.latest()
		w.last = strings.TrimSpace(text)
		if !background {
			w.report(clipOutcome{Kind: "limited"})
		}
		return
	}

	text, fresh := w.reader.latest()
	if !fresh {
		return
	}
	text = strings.TrimSpace(text)
	if text == w.last {
		return
	}
	w.last = text
	links := clipboardLinks(text)
	if len(links) == 0 {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || ctx.Err() != nil {
		return
	}
	n, err := w.deliver(ctx, s.Target, links)
	switch {
	case err != nil:
		log.Printf("desktop: clipboard watch: %v", err)
		w.report(clipOutcome{Kind: "failed", Reason: err.Error()})
	case n > 0:
		w.report(clipOutcome{Kind: "staged", N: n})
	default:
		// The links were in the collector already.
		w.report(clipOutcome{Kind: "none"})
	}
}

func (w *clipWatch) closeReader() {
	if w.reader != nil {
		w.reader.close()
		w.reader = nil
	}
}

// keepLease holds the lease where s says, renewing it every clipLeaseRenewal,
// and reports whether another device asked the watch to stop, which switches
// it off.
func (w *clipWatch) keepLease(ctx context.Context, s clipSettings) (stopped bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return false
	}
	if !w.leased.IsZero() && (!s.On || s.Target != w.leasedTarget) {
		w.leased = time.Time{}
		// Leaving drops a stop that waits there, so a watch that stays on
		// asks for it first.
		if s.On && w.lease(ctx, w.leasedTarget) {
			w.switchOff()
			return true
		}
		w.leave(ctx, w.leasedTarget)
	}
	if !s.On || time.Since(w.leased) < clipLeaseRenewal {
		return false
	}
	w.leased, w.leasedTarget = time.Now(), s.Target
	if !w.lease(ctx, s.Target) {
		return false
	}
	w.switchOff()
	w.leased = time.Time{}
	return true
}

// stop waits out a delivery or lease call under way, refuses any after it and
// takes the watch off the list, where a closed app would otherwise stay until
// its lease ran out. A stop another device asked for since the last renewal
// switches the watch off instead.
func (w *clipWatch) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.leased.IsZero() {
		return
	}
	w.leased = time.Time{}
	// The run's context has already ended. One wait covers both calls, so a
	// peer that does not answer holds up quitting for clipLeaseWait at most.
	ctx, cancel := context.WithTimeout(context.Background(), clipLeaseWait)
	defer cancel()
	// Leaving drops a stop that waits there, and the switch would stay on for
	// the next start, so the watch asks for it first.
	if w.lease(ctx, w.leasedTarget) {
		w.switchOff()
		return
	}
	w.leave(ctx, w.leasedTarget)
}

// readClipSettings reads the switch and the target from the interface state
// the page stores on this instance. Unreadable means off.
func readClipSettings(a *app.App) clipSettings {
	raw, err := a.UIState(store.UIStateKey)
	if err != nil || raw == "" {
		return clipSettings{}
	}
	var s clipSettings
	if json.Unmarshal([]byte(raw), &s) != nil {
		return clipSettings{}
	}
	return s
}

// clipWatcher is how the watch appears on a list of clipboard watchers, named
// the way the group names this instance.
func clipWatcher(a *app.App) clipwatch.Watcher {
	cfg := a.Settings.Get()
	name := cfg.InstanceName
	if name == "" {
		name, _ = os.Hostname()
	}
	return clipwatch.Watcher{ID: "desktop-" + cfg.InstanceID, Name: name, Kind: clipwatch.KindDesktop}
}

func clipWatcherPath(id string) string {
	return "/api/clipboard-watchers/" + url.PathEscape(id)
}

// leaseClip renews the watch's lease with this instance, or with the peer
// named target, and reports whether another device asked it to stop. A
// renewal that fails counts as no stop; the next one tries again.
func leaseClip(ctx context.Context, a *app.App, target string) bool {
	me := clipWatcher(a)
	if target == "" {
		stop, err := a.ClipWatch.Renew(me, time.Now())
		if err != nil {
			log.Printf("desktop: clipboard watch: %v", err)
		}
		return stop
	}
	if a.ModuleOff("federation") {
		return false
	}
	body, _ := json.Marshal(map[string]string{"name": me.Name, "kind": me.Kind})
	ctx, cancel := context.WithTimeout(ctx, clipLeaseWait)
	defer cancel()
	resp, code, err := a.Federation.Proxy(ctx, target, http.MethodPut, clipWatcherPath(me.ID), body)
	if err != nil || code != http.StatusOK {
		return false
	}
	var answer struct {
		Stop bool `json:"stop"`
	}
	return json.Unmarshal(resp, &answer) == nil && answer.Stop
}

// leaveClip takes the watch off the list it was leased on. A peer that does
// not hear it drops the lease once it runs out.
func leaveClip(ctx context.Context, a *app.App, target string) {
	id := clipWatcher(a).ID
	if target == "" {
		a.ClipWatch.Leave(id)
		return
	}
	if a.ModuleOff("federation") {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, clipLeaseWait)
	defer cancel()
	_, _, _ = a.Federation.Proxy(ctx, target, http.MethodDelete, clipWatcherPath(id), nil)
}

// switchClipOff turns the switch off in the interface state and leaves the
// page's other fields as they are.
func switchClipOff(a *app.App) error {
	doc := map[string]json.RawMessage{}
	raw, err := a.UIState(store.UIStateKey)
	if err != nil {
		return err
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return err
		}
	}
	doc["clipboardWatch"] = json.RawMessage("false")
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return a.SetUIState(store.UIStateKey, string(out))
}

// deliverLinks stages links on this instance, or on the peer named target,
// the way POST /api/links and its forward to a peer do, and returns how many
// tasks they made.
func deliverLinks(ctx context.Context, a *app.App, target string, links []string) (int, error) {
	if target == "" {
		// POST /api/links runs the paste through the pre-parser when it is
		// on; see extractLinks in internal/api/routes_links.go.
		if a.Settings.Get().PreParserEnabled {
			links = linkscan.Extract(strings.Join(links, "\n"))
		}
		created, err := a.AddLinksWithOptions(links, "", app.OriginPaste, app.LinkBatchOptions{})
		return len(created), err
	}
	if a.ModuleOff("federation") {
		return 0, errors.New(`"Instances" is switched off on the Modules page`)
	}
	body, err := json.Marshal(map[string]string{"links": strings.Join(links, "\n")})
	if err != nil {
		return 0, err
	}
	resp, code, err := a.Federation.Proxy(ctx, target, http.MethodPost, "/api/links", body)
	if err != nil {
		return 0, err
	}
	if code >= 300 {
		return 0, fmt.Errorf("%s answered %d: %s", target, code, strings.TrimSpace(string(resp)))
	}
	var created []json.RawMessage
	if err := json.Unmarshal(resp, &created); err != nil {
		return 0, fmt.Errorf("%s answered with something other than a task list", target)
	}
	return len(created), nil
}
