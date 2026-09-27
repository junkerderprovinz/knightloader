package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

type emitted struct {
	name string
	typ  string // the hub message's type, empty for a close
}

// newTestBridge returns a bridge over a fresh app whose events land on the
// returned channel instead of in a window.
func newTestBridge(t *testing.T) (*HubBridge, *app.App, chan emitted) {
	t.Helper()
	t.Setenv("KL_JD", "")
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	events := make(chan emitted, 256)
	b := newHubBridge(a, func(name string, data ...any) {
		e := emitted{name: name}
		if len(data) == 1 {
			var msg struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(data[0].(string)), &msg); err != nil {
				t.Errorf("event %s carries %v, not a hub message", name, data[0])
			}
			e.typ = msg.Type
		}
		events <- e
	})
	return b, a, events
}

// next waits for the next event on name of type typ, skipping others.
func next(t *testing.T, events chan emitted, name, typ string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.name == name && e.typ == typ {
				return
			}
		case <-deadline:
			t.Fatalf("no %q event of type %q", name, typ)
		}
	}
}

func nothing(t *testing.T, events chan emitted, name, typ string) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case e := <-events:
			if e.name == name && e.typ == typ {
				t.Fatalf("got a %q event of type %q", name, typ)
			}
		case <-deadline:
			return
		}
	}
}

func TestStreamStartsWithTheSnapshotsASocketGets(t *testing.T) {
	b, a, events := newTestBridge(t)
	b.Open("p1", "p1.1")
	next(t, events, "hub:p1.1", "snapshot")
	next(t, events, "hub:p1.1", "activitySnapshot")

	a.Hub.Broadcast("queue", "x")
	next(t, events, "hub:p1.1", "queue")
}

func TestSubscribeFrameNarrowsTheStream(t *testing.T) {
	b, a, events := newTestBridge(t)
	b.Open("p1", "p1.1")
	next(t, events, "hub:p1.1", "activitySnapshot")

	b.Send("p1.1", `{"type":"subscribe","kinds":["task"]}`)
	a.Hub.Broadcast("queue", "x")
	a.Hub.Broadcast("task", "y")
	next(t, events, "hub:p1.1", "task")
	nothing(t, events, "hub:p1.1", "queue")
}

func TestStreamIsAViewerOnlyWhileItReportsVisible(t *testing.T) {
	b, a, _ := newTestBridge(t)
	b.Open("p1", "p1.1")
	b.Send("p1.1", `{"type":"subscribe","kinds":["captcha"]}`)
	if a.Hub.Watched("captcha", 0) {
		t.Fatal("watched before the stream reported anything")
	}
	b.Send("p1.1", `{"type":"visibility","visible":true}`)
	if !a.Hub.Watched("captcha", 0) {
		t.Fatal("not watched while visible")
	}
	b.Send("p1.1", `{"type":"visibility","visible":false}`)
	if a.Hub.Watched("captcha", 0) {
		t.Fatal("watched while hidden")
	}
	b.Send("p1.1", `{"type":"visibility","visible":true}`)
	b.Close("p1.1")
	if a.Hub.Watched("captcha", 0) {
		t.Fatal("watched after the stream closed")
	}
}

func TestReloadedPageDropsTheStreamsOfTheOldOne(t *testing.T) {
	b, a, events := newTestBridge(t)
	b.Open("p1", "p1.1")
	b.Open("p1", "p1.2")
	if n := a.Hub.Len(); n != 2 {
		t.Fatalf("hub holds %d connections, want 2", n)
	}
	b.Open("p2", "p2.1")
	if n := a.Hub.Len(); n != 1 {
		t.Fatalf("hub holds %d connections after the reload, want 1", n)
	}
	nothing(t, events, "hub:p1.1:closed", "")
}

func TestHubDropIsReportedToThePage(t *testing.T) {
	b, a, events := newTestBridge(t)
	b.Open("p1", "p1.1")
	a.Hub.Remove(b.lookup("p1.1"))
	next(t, events, "hub:p1.1:closed", "")
}

func TestClosedStreamIsNotReportedAsDropped(t *testing.T) {
	b, _, events := newTestBridge(t)
	b.Open("p1", "p1.1")
	b.Close("p1.1")
	nothing(t, events, "hub:p1.1:closed", "")
}
