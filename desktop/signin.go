package main

// Wails' asset handler cannot upgrade a request to a WebSocket, so the sign-in
// browser's stream (/api/signinbrowser/stream) travels over Wails events, as
// HubBridge carries /api/ws. The page names each stream it opens, so a stream
// replaced by the next one cannot end that one.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/signinbrowser"
)

// SignInBridge is bound to the frontend as main.SignInBridge.
type SignInBridge struct {
	app  *app.App
	emit func(window, name string, data ...any)

	mu      sync.Mutex
	current string
	stop    context.CancelFunc
}

func newSignInBridge(a *app.App, emit func(window, name string, data ...any)) *SignInBridge {
	return &SignInBridge{app: a, emit: emit}
}

// Watch streams the open sign-in window to window as stream id: frames as the
// event "signin:<id>:frame" holding a base64 JPEG, the page's state as
// "signin:<id>:msg" holding JSON, until "signin:<id>:closed". A stream already
// open ends.
func (b *SignInBridge) Watch(window, id string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.stop != nil {
		b.stop()
	}
	b.current, b.stop = id, cancel
	b.mu.Unlock()
	go func() {
		defer b.emit(window, "signin:"+id+":closed")
		_ = b.app.SignInBrowser().Watch(ctx, signInViewer{b: b, window: window, id: id})
	}()
}

// Input replays one thing the user did over the page, as a browser sends it
// up the socket.
func (b *SignInBridge) Input(raw string) {
	var in signinbrowser.Input
	if json.Unmarshal([]byte(raw), &in) == nil {
		b.app.SignInBrowser().Input(in)
	}
}

// Stop ends stream id, unless a newer stream has taken its place: the page
// queues the calls of each stream apart, so one stream's Stop can arrive after
// the next one's Watch.
func (b *SignInBridge) Stop(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == id && b.stop != nil {
		b.stop()
		b.current, b.stop = "", nil
	}
}

type signInViewer struct {
	b      *SignInBridge
	window string
	id     string
}

func (v signInViewer) Frame(jpeg []byte) error {
	v.b.emit(v.window, "signin:"+v.id+":frame", base64.StdEncoding.EncodeToString(jpeg))
	return nil
}

func (v signInViewer) Send(msg any) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	v.b.emit(v.window, "signin:"+v.id+":msg", string(raw))
	return nil
}
