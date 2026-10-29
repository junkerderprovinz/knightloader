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
// for its shadow and to slide into view from the window's edge.
const (
	popupWidth  = 360
	popupHeight = 140
)

// Where on the work area the popup sits, as the page's position picker names
// it.
const (
	placeBottomRight  = "bottom-right"
	placeBottomLeft   = "bottom-left"
	placeTopRight     = "top-right"
	placeTopLeft      = "top-left"
	placeBottomCenter = "bottom-center"
	placeTopCenter    = "top-center"
)

// popupSecondsOffered are the times the page offers, with 0 for a popup that
// stays until it is clicked.
var popupSecondsOffered = []int{3, 5, 8, 15, 0}

const defaultPopupSeconds = 5

// popup reports whether links taken in pop up on the screen.
func (s clipSettings) popup() bool {
	return s.Popup == nil || *s.Popup
}

// popupSeconds is how long the popup stays, 0 for until clicked. A time the
// page does not offer, from a hand edit or a newer version, is the default.
func (s clipSettings) popupSeconds() int {
	if s.PopupSeconds != nil {
		for _, n := range popupSecondsOffered {
			if *s.PopupSeconds == n {
				return n
			}
		}
	}
	return defaultPopupSeconds
}

// popupPlace is where the popup sits, the lower right corner unless the page
// picked another of the places it offers.
func (s clipSettings) popupPlace() string {
	switch s.PopupPlace {
	case placeBottomLeft, placeTopRight, placeTopLeft, placeBottomCenter, placeTopCenter:
		return s.PopupPlace
	}
	return placeBottomRight
}

// popupBatch is what the note says: how many links the take-ins since it
// appeared added, and the host they all came from where there is one. Seq
// counts take-ins and From is the Seq the batch began with, so the page can
// tell a new note from one that grew. Seconds and Place are the settings the
// note appeared with, which hold while it is up.
type popupBatch struct {
	Seq     int    `json:"seq"`
	From    int    `json:"from"`
	N       int    `json:"n"`
	Host    string `json:"host,omitempty"`
	Seconds int    `json:"seconds"`
	Place   string `json:"place"`
}

// LinkPopup decides when the note shows and folds take-ins that arrive while
// it is up into the one on screen. The page calls Current, Dismiss and Open as
// main.LinkPopup.Current and so on; the clipboard watch's goroutine calls
// offer.
type LinkPopup struct {
	settings func() clipSettings
	// tell hands the page a batch, and show and hide put the window on the
	// screen at a place and take it away.
	tell func(popupBatch)
	show func(place string)
	hide func()
	// openCollector shows the main window on the collector.
	openCollector func()

	mu    sync.Mutex
	batch popupBatch
	open  bool
}

func newLinkPopup(a *app.App, t *Tray) *LinkPopup {
	return &LinkPopup{
		settings: func() clipSettings { return readClipSettings(a) },
		tell:     func(b popupBatch) { t.emitTo(popupName, popupEvent, b) },
		openCollector: func() {
			t.ShowMain()
			t.emitTo("main", showPageEvent, "/collector")
		},
	}
}

// offer shows what the clipboard watch did, if it added links to this
// instance's collector, which is what a click on the popup opens.
func (p *LinkPopup) offer(o clipOutcome) {
	if o.Kind != "staged" || o.N <= 0 {
		return
	}
	s := p.settings()
	if !s.popup() || s.Target != "" {
		return
	}
	p.mu.Lock()
	fresh := !p.open
	seq := p.batch.Seq + 1
	if fresh {
		p.batch = popupBatch{From: seq, Host: o.Host, Seconds: s.popupSeconds(), Place: s.popupPlace()}
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
		p.show(b.Place)
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
	p.show = func(place string) {
		if s := popupScreen(wails, t.window()); s != nil {
			w.SetBounds(popupBounds(s.WorkArea, place))
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

// popupBounds puts the window at place on a work area, trayGap from its
// edges.
func popupBounds(work application.Rect, place string) application.Rect {
	b := application.Rect{
		X:      work.X + work.Width - popupWidth - trayGap,
		Y:      work.Y + work.Height - popupHeight - trayGap,
		Width:  popupWidth,
		Height: popupHeight,
	}
	switch place {
	case placeBottomLeft, placeTopLeft:
		b.X = work.X + trayGap
	case placeBottomCenter, placeTopCenter:
		b.X = work.X + (work.Width-popupWidth)/2
	}
	switch place {
	case placeTopRight, placeTopLeft, placeTopCenter:
		b.Y = work.Y + trayGap
	}
	return b
}
