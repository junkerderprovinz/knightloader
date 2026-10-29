package main

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// popupName is the small window that pops up in a corner of the screen when
// the clipboard watch takes links into the collector, as JDownloader's link
// grabber does. Its page passes the same name to HubBridge; see
// web/src/lib/desktop.ts.
const popupName = "linkpopup"

// popupEvent hands the window's page the popupBatch it shows.
const popupEvent = "linkPopup"

// showPageEvent asks the main window's page to open the route it carries.
const showPageEvent = "showPage"

// The window is larger than the note the page draws in it, which needs room
// for its shadow and to rise into view from the window's lower edge.
const (
	popupWidth  = 360
	popupHeight = 140
)

// popupBatch is what the note says: how many links the take-ins since it
// appeared added, and the host they all came from where there is one. Seq
// counts take-ins and From is the Seq the batch began with, so the page can
// tell a new note from one that grew.
type popupBatch struct {
	Seq  int    `json:"seq"`
	From int    `json:"from"`
	N    int    `json:"n"`
	Host string `json:"host,omitempty"`
}

// LinkPopup decides when the note shows and folds take-ins that arrive while
// it is up into the one on screen. The page calls Current, Dismiss and Open as
// main.LinkPopup.Current and so on; the clipboard watch's goroutine calls
// offer.
type LinkPopup struct {
	// enabled reads the page's switch for the note, and whether the links go
	// to this instance's collector, which is what a click on it opens.
	enabled func() bool
	// tell hands the page a batch, and show and hide put the window on the
	// screen and take it away.
	tell func(popupBatch)
	show func()
	hide func()
	// openCollector shows the main window on the collector.
	openCollector func()

	mu    sync.Mutex
	batch popupBatch
	open  bool
}

func newLinkPopup(a *app.App, t *Tray) *LinkPopup {
	return &LinkPopup{
		enabled: func() bool {
			s := readClipSettings(a)
			return s.popup() && s.Target == ""
		},
		tell: func(b popupBatch) { t.emitTo(popupName, popupEvent, b) },
		openCollector: func() {
			t.ShowMain()
			t.emitTo("main", showPageEvent, "/collector")
		},
	}
}

// offer shows what the clipboard watch did, if it added links.
func (p *LinkPopup) offer(o clipOutcome) {
	if o.Kind != "staged" || o.N <= 0 || !p.enabled() {
		return
	}
	p.mu.Lock()
	fresh := !p.open
	seq := p.batch.Seq + 1
	if fresh {
		p.batch = popupBatch{From: seq, Host: o.Host}
	} else if p.batch.Host != o.Host {
		p.batch.Host = ""
	}
	p.batch.Seq = seq
	p.batch.N += o.N
	p.open = true
	b := p.batch
	p.mu.Unlock()

	// The page has its words before the window shows, so it never shows empty.
	p.tell(b)
	if fresh {
		p.show()
	}
}

// Current is the batch on screen, or a zero one, for a page that loaded after
// the batch was told.
func (p *LinkPopup) Current() popupBatch {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.open {
		return popupBatch{}
	}
	return p.batch
}

// Dismiss takes the note away once its time is up, unless a take-in has grown
// it since the page saw seq.
func (p *LinkPopup) Dismiss(seq int) {
	p.mu.Lock()
	gone := p.open && seq == p.batch.Seq
	if gone {
		p.open = false
	}
	p.mu.Unlock()
	if gone {
		p.hide()
	}
}

// Open takes the note away and shows the collector in the main window.
func (p *LinkPopup) Open() {
	p.close()
	p.openCollector()
}

func (p *LinkPopup) close() {
	p.mu.Lock()
	p.open = false
	p.mu.Unlock()
	p.hide()
}

// attach makes the window, hidden until the first take-in. It never takes the
// focus from the program somebody copied the link in. It runs before
// wails.Run.
func (p *LinkPopup) attach(wails *application.App, t *Tray) {
	w := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           popupName,
		Title:          "KnightLoader",
		Width:          popupWidth,
		Height:         popupHeight,
		URL:            "/linkpopup",
		Frameless:      true,
		AlwaysOnTop:    true,
		Hidden:         true,
		DisableResize:  true,
		BackgroundType: application.BackgroundTypeTransparent,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
			// The note draws its own shadow and corners.
			DisableFramelessWindowDecorations: true,
			ExStyle:                           quietExStyle(),
		},
		Mac: application.MacWindow{
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true},
			DisableShadow:    true,
		},
	})
	// Alt+F4 would otherwise destroy the window for good.
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if t.ending() {
			return
		}
		e.Cancel()
		p.close()
	})
	p.show = func() {
		if s := popupScreen(wails, t.window()); s != nil {
			w.SetBounds(popupBounds(s.WorkArea))
		}
		showQuietly(w)
	}
	p.hide = func() { w.Hide() }
}

// popupScreen is the screen under the pointer, where somebody has just copied
// a link, or where the pointer cannot be read, the main window's.
func popupScreen(wails *application.App, main *application.WebviewWindow) *application.Screen {
	if s := pointerScreen(); s != nil {
		return s
	}
	if main != nil {
		if s, err := main.GetScreen(); err == nil && s != nil {
			return s
		}
	}
	return wails.Screen.GetPrimary()
}

// popupBounds puts the window in the lower right corner of a work area, where
// the clock and the tray icons sit on most desktops.
func popupBounds(work application.Rect) application.Rect {
	return application.Rect{
		X:      work.X + work.Width - popupWidth - trayGap,
		Y:      work.Y + work.Height - popupHeight - trayGap,
		Width:  popupWidth,
		Height: popupHeight,
	}
}
