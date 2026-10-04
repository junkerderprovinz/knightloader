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
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// clipWatchEvent carries a clipOutcome to the main window's page.
const clipWatchEvent = "clipboardWatch"

// clipPollInterval matches the page's POLL_MS.
const clipPollInterval = 1200 * time.Millisecond

// linkSchemes are the starts a word needs to count as a link, as in the
// page's LINK_WORD.
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

	reader clipboardReader
	last   string

	// mu keeps a delivery from overlapping stop, so none reaches the app
	// after it has closed.
	mu      sync.Mutex
	stopped bool
}

func newClipWatch(a *app.App, read func() (string, bool), report func(clipOutcome)) *clipWatch {
	return &clipWatch{
		settings: func() clipSettings { return readClipSettings(a) },
		open:     func() (clipboardReader, bool) { return openClipboard(read) },
		deliver: func(ctx context.Context, target string, links []string) (int, error) {
			return deliverLinks(ctx, a, target, links)
		},
		report: report,
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

// stop waits out a delivery under way and refuses any after it.
func (w *clipWatch) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
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
