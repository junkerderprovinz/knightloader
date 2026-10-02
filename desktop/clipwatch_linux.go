//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// openClipboard reads the clipboard through GTK, which on X11 sees every
// change. A Wayland compositor tells a program about the clipboard only while
// one of its windows has focus, so there wl-paste from wl-clipboard watches
// it instead, through the data-control protocol of wlroots compositors and
// KDE. Without wl-paste or that protocol, as on GNOME, the watch falls back to
// GTK and sees only what was copied by the time the window is in front.
func openClipboard(read func() (string, bool)) (clipboardReader, bool) {
	polled := &polledClipboard{read: read}
	if !onWayland() {
		return polled, true
	}
	w, err := startWlPaste(polled)
	if err != nil {
		log.Printf("desktop: clipboard watch: wl-paste cannot watch this session (%v); only a focused window sees the clipboard", err)
		return polled, false
	}
	return w, true
}

// onWayland reports whether GTK draws this program's windows on Wayland
// rather than through XWayland.
func onWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" && !strings.HasPrefix(os.Getenv("GDK_BACKEND"), "x11")
}

// wlPasteStart is how long wl-paste has to fail before it counts as
// watching. Without data-control it exits at once.
const wlPasteStart = time.Second

// wlPasteMax bounds one clipboard text read from wl-paste. A copied link list
// stays far below it, and a larger copy is skipped whole.
const wlPasteMax = 1 << 20

// wlPaste is wl-paste --watch, which runs a shell for every change that
// passes the text on with a NUL after it.
type wlPaste struct {
	cancel context.CancelFunc
	texts  chan string
	done   chan struct{}
	// fallback takes over if wl-paste ends while the watch is still on.
	fallback *polledClipboard
}

func startWlPaste(fallback *polledClipboard) (*wlPaste, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "wl-paste", "--type", "text", "--watch", "sh", "-c", `cat; printf '\0'`)
	// The shell wl-paste runs holds the pipe as well, so ending the watch
	// ends the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	w := &wlPaste{cancel: cancel, texts: make(chan string, 1), done: make(chan struct{}), fallback: fallback}
	go func() {
		defer close(w.done)
		w.forward(out)
		_ = cmd.Wait()
	}()

	select {
	case <-w.done:
		cancel()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, errors.New("wl-paste exited")
	case <-time.After(wlPasteStart):
		return w, nil
	}
}

// forward hands on each NUL-terminated text, keeping only the newest one
// unread.
func (w *wlPaste) forward(out io.Reader) {
	r := bufio.NewReaderSize(out, wlPasteMax)
	skipping := false
	for {
		chunk, err := r.ReadSlice(0)
		if err == bufio.ErrBufferFull {
			skipping = true
			continue
		}
		if err != nil {
			return
		}
		if skipping {
			skipping = false
			continue
		}
		text := string(chunk[:len(chunk)-1])
		select {
		case <-w.texts:
		default:
		}
		w.texts <- text
	}
}

func (w *wlPaste) latest() (string, bool) {
	select {
	case text := <-w.texts:
		return text, true
	default:
	}
	select {
	case <-w.done:
		return w.fallback.latest()
	default:
		return "", false
	}
}

func (w *wlPaste) close() {
	w.cancel()
	<-w.done
}
