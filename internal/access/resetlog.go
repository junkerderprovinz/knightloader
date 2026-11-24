package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ResetCodeFile holds the reset code where the process has no output a
// person can read.
const ResetCodeFile = "password-reset-code.txt"

// resetLife is how long the kit takes a reset code.
const resetLife = 10 * time.Minute

// Logger is the kit's logger for KnightLoader. Its lines go to the log, all
// but the reset code, which goes to console or, without one, to
// ResetCodeFile in dir. The log reaches the log page and the diagnostics
// bundle, which a code must not.
func Logger(dir string, console io.Writer) *slog.Logger {
	return slog.New(&kitLog{dir: dir, console: console})
}

// Console is the process's own error output, or nil where nobody reads it:
// the desktop app, or a service whose output goes to the null device.
func Console(desktop bool) io.Writer {
	if desktop || !readable(os.Stderr) {
		return nil
	}
	return os.Stderr
}

// RemoveResetCode takes away a code file a process that is gone left behind.
func RemoveResetCode(dir string) {
	_ = os.Remove(filepath.Join(dir, ResetCodeFile))
}

func readable(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	// On Windows a console and the null device stat alike, so only the
	// missing handle is told apart there.
	if runtime.GOOS == "windows" {
		return true
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

type kitLog struct {
	dir     string
	console io.Writer
	attrs   []slog.Attr
}

func (h *kitLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }

func (h *kitLog) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &kitLog{dir: h.dir, console: h.console, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *kitLog) WithGroup(string) slog.Handler { return h }

func (h *kitLog) Handle(_ context.Context, r slog.Record) error {
	var code string
	var b strings.Builder
	b.WriteString(r.Message)
	each := func(a slog.Attr) bool {
		if a.Key == "code" {
			code = a.Value.String()
			return true
		}
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	}
	for _, a := range h.attrs {
		each(a)
	}
	r.Attrs(each)
	if code != "" {
		return h.showCode(code)
	}
	log.Print(b.String())
	return nil
}

// showCode writes the code where only a person at the machine reads it. The
// block stands apart from the timestamped lines around it and carries no
// colour codes, which journalctl would hide the whole line for.
func (h *kitLog) showCode(code string) error {
	text := fmt.Sprintf("  Password reset code: %s\n"+
		"  Valid for %d minutes. Enter it on the sign-in page.\n"+
		"  If nobody here asked for a reset, ignore this. The password is unchanged.\n",
		code, int(resetLife.Minutes()))
	if h.console != nil {
		_, err := io.WriteString(h.console, "\n"+text+"\n")
		return err
	}
	text += "  Written " + time.Now().Format("2006-01-02 15:04:05") + ".\n"
	path := filepath.Join(h.dir, ResetCodeFile)
	// Made anew, since a file that is already there keeps its own mode.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return err
	}
	time.AfterFunc(resetLife, func() {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), code) {
			_ = os.Remove(path)
		}
	})
	return nil
}
