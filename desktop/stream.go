package main

// Wails' asset handler cannot upgrade a request to a WebSocket, so /api/ws
// never connects in the window. HubBridge carries the same stream over Wails
// events instead. Every stream the page opens is a hub connection of its own,
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

// HubBridge is bound to the frontend as window.go.main.HubBridge.
type HubBridge struct {
	app  *app.App
	emit func(name string, data ...any)

	mu sync.Mutex
	// page names the page instance whose streams are open. A page that
	// reloads never closes its streams, so the first Open from a new page
	// drops them.
	page    string
	streams map[string]*bridgeStream
	stopped bool
}

func newHubBridge(a *app.App, emit func(name string, data ...any)) *HubBridge {
	return &HubBridge{app: a, emit: emit, streams: map[string]*bridgeStream{}}
}

// Open starts stream id for page. Its messages arrive as the Wails event
// "hub:<id>", and "hub:<id>:closed" says the hub dropped it.
func (b *HubBridge) Open(page, id string) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	var stale []*bridgeStream
	if page != b.page {
		for _, s := range b.streams {
			stale = append(stale, s)
		}
		b.streams = map[string]*bridgeStream{}
		b.page = page
	}
	s := &bridgeStream{b: b, id: id}
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
// become Wails events.
type bridgeStream struct {
	b  *HubBridge
	id string
}

// Write hands the frame on as the string a socket's onmessage would get, so
// the page parses it the same way.
func (s *bridgeStream) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	s.b.emit("hub:"+s.id, string(p))
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
		s.b.emit("hub:" + s.id + ":closed")
	}
	return nil
}
