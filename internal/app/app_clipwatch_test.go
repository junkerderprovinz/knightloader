package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/clipwatch"
)

func TestAClipboardStopThatRanOutIsGoneAfterARestart(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := clipwatch.Watcher{ID: "ext", Kind: clipwatch.KindExtension}
	asked := time.Now().Add(-clipwatch.Lease - time.Second)
	if _, err := a.ClipWatch.Renew(w, asked); err != nil {
		t.Fatal(err)
	}
	a.ClipWatch.Stop("ext", asked)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	if stop, _ := b.ClipWatch.Renew(w, time.Now()); stop {
		t.Fatal("a stop that ran out before the restart reached the watcher switched on again")
	}
}
