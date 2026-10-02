//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeWlPaste puts a wl-paste on PATH that runs script instead.
func fakeWlPaste(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("GDK_BACKEND", "")
}

func TestOpenClipboardOnWaylandWatchesThroughWlPaste(t *testing.T) {
	// The real wl-paste runs its last arguments for every change; this one
	// runs them twice and then waits like a watch does.
	fakeWlPaste(t, `
shift 3
printf 'old' | "$@"
sleep 1.5
printf 'https://host.example/a' | "$@"
exec sleep 30`)
	polled := false
	r, background := openClipboard(func() (string, bool) { polled = true; return "", true })
	defer r.close()
	if !background {
		t.Fatal("wl-paste watched, but the reader reports only a focused window")
	}
	if text, fresh := r.latest(); !fresh || text != "old" {
		t.Fatalf("first text %q fresh %v, want the one there at the start", text, fresh)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if text, fresh := r.latest(); fresh {
			if text != "https://host.example/a" {
				t.Fatalf("got %q", text)
			}
			if polled {
				t.Fatal("GTK was read although wl-paste watched")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the copied link never arrived")
}

func TestOpenClipboardOnWaylandFallsBackWithoutDataControl(t *testing.T) {
	fakeWlPaste(t, `echo "Watch mode requires a compositor that supports the wlroots data-control protocol" >&2; exit 1`)
	r, background := openClipboard(func() (string, bool) { return "gtk", true })
	defer r.close()
	if background {
		t.Fatal("a failed wl-paste still counts as watching in the background")
	}
	if text, _ := r.latest(); text != "gtk" {
		t.Fatalf("read %q, want the GTK clipboard", text)
	}
}

func TestWlPasteSkipsATextOverTheLimit(t *testing.T) {
	w := &wlPaste{texts: make(chan string, 1)}
	huge := strings.Repeat("x", wlPasteMax+10)
	w.forward(strings.NewReader(huge + "\x00https://host.example/a\x00"))
	if text := <-w.texts; text != "https://host.example/a" {
		t.Fatalf("got %d bytes starting %q", len(text), text[:min(len(text), 20)])
	}
}
