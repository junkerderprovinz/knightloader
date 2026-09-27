package main

// Wails' asset handler cannot upgrade a request to a WebSocket, so /api/ws
// never connects in a window. HubBridge carries the same stream over Wails
// events instead. Every stream a page opens is a hub connection of its own,
// started by api.OpenStream and fed the page's frames through
// api.StreamControl, so snapshots, subscriptions and visibility behave as they
// do for a socket in a browser.

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// HubBridge is bound to the frontend as main.HubBridge.
type HubBridge struct {
	app *app.App
	// emit sends one event to the named window only, so the main window and
	// the tray window each get the streams they opened.
	emit func(window, name string, data ...any)

	mu sync.Mutex
	// pages names the page instance in each window whose streams are open. A
	// page that reloads never closes its streams, so the first Open from a
	// new page in the same window drops them.
	pages   map[string]string
	streams map[string]*bridgeStream
	stopped bool
}

func newHubBridge(a *app.App, emit func(window, name string, data ...any)) *HubBridge {
	return &HubBridge{app: a, emit: emit, pages: map[string]string{}, streams: map[string]*bridgeStream{}}
}

// Open starts stream id for page in window. Its messages arrive as the Wails
// event "hub:<id>", and "hub:<id>:closed" says the hub dropped it.
func (b *HubBridge) Open(window, page, id string) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	var stale []*bridgeStream
	if page != b.pages[window] {
		for sid, s := range b.streams {
			if s.window == window {
				stale = append(stale, s)
				delete(b.streams, sid)
			}
		}
		b.pages[window] = page
	}
	s := &bridgeStream{b: b, id: id, window: window}
	b.streams[id] = s
	b.mu.Unlock()

	for _, old := range stale {
		b.app.Hub.Remove(old)
	}
	api.OpenStream(b.app, s)
}

// Send applies one control frame from the page to stream id, the frame a
// browser would send up its socket.
func (b *HubBridge) Send(id, frame string) {
	if s := b.lookup(id); s != nil {
		api.StreamControl(b.app, s, []byte(frame))
	}
}

// Close ends stream id.
func (b *HubBridge) Close(id string) {
	b.mu.Lock()
	s := b.streams[id]
	delete(b.streams, id)
	b.mu.Unlock()
	if s != nil {
		b.app.Hub.Remove(s)
	}
}

// stop drops every stream and refuses new ones. It runs from OnShutdown, so no
// hub writer emits into a window that is going away.
func (b *HubBridge) stop() {
	b.mu.Lock()
	b.stopped = true
	streams := b.streams
	b.streams = map[string]*bridgeStream{}
	b.mu.Unlock()
	for _, s := range streams {
		b.app.Hub.Remove(s)
	}
}

func (b *HubBridge) lookup(id string) *bridgeStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.streams[id]
}

// bridgeStream is one stream as the hub sees it: a hub.Conn whose writes
// become Wails events in the window that opened it.
type bridgeStream struct {
	b      *HubBridge
	id     string
	window string
}

// Write hands the frame on as the string a socket's onmessage would get, so
// the page parses it the same way.
func (s *bridgeStream) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	s.b.emit(s.window, "hub:"+s.id, string(p))
	return nil
}

// CloseNow runs when the hub lets go of the stream. Only a drop the page did
// not ask for, such as a full queue, is reported, so the page reconnects the
// way it would after a socket closed.
func (s *bridgeStream) CloseNow() error {
	s.b.mu.Lock()
	dropped := s.b.streams[s.id] == s
	if dropped {
		delete(s.b.streams, s.id)
	}
	s.b.mu.Unlock()
	if dropped {
		s.b.emit(s.window, "hub:"+s.id+":closed")
	}
	return nil
}
