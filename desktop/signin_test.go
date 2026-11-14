package main

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func TestASignInStreamWithNoWindowOpenEndsAtOnce(t *testing.T) {
	t.Setenv("KL_JD", "")
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	events := make(chan emitted, 8)
	b := newSignInBridge(a, func(window, name string, _ ...any) { events <- emitted{window: window, name: name} })
	b.Watch("main", "s1")
	select {
	case e := <-events:
		if e.window != "main" || e.name != "signin:s1:closed" {
			t.Errorf("event %+v, want signin:s1:closed in the main window", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never ended")
	}
	b.Input("not even json")
}
