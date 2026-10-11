package api

// The way back in for somebody who lost the password: a code written where
// only a person with access to the machine can read it, then the code and a
// new password on the sign-in screen. internal/auth/reset.go holds the code
// and the argument for it; this file is where the code is shown and what else
// a reset takes away.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// resetCodeFile holds the code where the process has no output a person can
// read. It sits beside auth.json, whose signing key already opens the
// instance to anybody who can read the folder.
const resetCodeFile = "password-reset-code.txt"

// resetConsole is the process's own error output, or nil when nobody can read
// it. The code goes there directly and not through the log package, whose
// lines reach the log page, the log file and the diagnostics bundle. A
// variable so a test can stand in for it.
var resetConsole = func() io.Writer {
	if buildinfo.Deployment == "desktop" || !readable(os.Stderr) {
		return nil
	}
	return os.Stderr
}

// readable reports whether what is written to f ends up anywhere. A desktop
// program started from a shortcut has no handle at all, and a service can be
// started with its output sent to the null device.
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

// inContainer reports whether this process runs in a container, going by the
// marker files Docker and Podman leave and the variable Kubernetes sets. It
// only picks the sentence the sign-in screen shows.
func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// resetPlace is what the sign-in screen is told about where the code is. It
// never carries the code, and it is the same answer whether this request
// wrote one, an earlier one is still live or the gap between two codes held
// one back.
type resetPlace struct {
	// Place is "container" or "process" for the process's own output, "file"
	// for resetCodeFile.
	Place string `json:"place"`
	File  string `json:"file,omitempty"`
	// Dir is the data directory, named on the desktop build only. That build
	// opens no listener, so the path reaches nobody but the person at the
	// window, while a server's path would tell a stranger a user name.
	Dir string `json:"dir,omitempty"`
}

func resetPlaceOf(a *app.App, console io.Writer) resetPlace {
	if console == nil {
		p := resetPlace{Place: "file", File: resetCodeFile}
		if buildinfo.Deployment == "desktop" {
			p.Dir = a.DataDir
		}
		return p
	}
	if inContainer() {
		return resetPlace{Place: "container"}
	}
	return resetPlace{Place: "process"}
}

// shownCode is the code as a person reads it, in groups of four.
func shownCode(code string) string {
	return code[:4] + "-" + code[4:8] + "-" + code[8:]
}

// showResetCode writes code to console, or to resetCodeFile when there is no
// console. The block stands apart from the timestamped lines around it and
// carries no colour codes, which journalctl would hide the whole line for.
func showResetCode(a *app.App, console io.Writer, code string) error {
	text := fmt.Sprintf("  Password reset code: %s\n"+
		"  Valid for %d minutes. Enter it on the sign-in page.\n"+
		"  If nobody here asked for a reset, ignore this. The password is unchanged.\n",
		shownCode(code), int(auth.ResetCodeTTL.Minutes()))
	if console != nil {
		_, err := io.WriteString(console, "\n"+text+"\n")
		return err
	}
	text += "  Written " + time.Now().Format("2006-01-02 15:04:05") + ".\n"
	path := filepath.Join(a.DataDir, resetCodeFile)
	// Created anew, since a file that is already there keeps its own mode.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, text); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	time.AfterFunc(auth.ResetCodeTTL, func() { removeResetCodeFile(a, code) })
	return nil
}

// removeResetCodeFile removes resetCodeFile when it holds code, or whatever
// it holds when code is "". A newer code in the file is left alone.
func removeResetCodeFile(a *app.App, code string) {
	path := filepath.Join(a.DataDir, resetCodeFile)
	if code != "" {
		if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), shownCode(code)) {
			return
		}
	}
	_ = os.Remove(path)
}

// wrongGuesses reports wrong reset codes in the ordinary log, at most once a
// minute, so the host can see somebody guessing. It names no address: the log
// goes into diagnostics bundles that are attached to public bug reports.
type wrongGuesses struct {
	mu       sync.Mutex
	count    int
	reported time.Time
}

func (g *wrongGuesses) add() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	if time.Since(g.reported) < time.Minute {
		return
	}
	log.Printf("auth: %d wrong password reset code(s) entered since the last report", g.count)
	g.count = 0
	g.reported = time.Now()
}

func registerPasswordReset(reg *Registry, a *app.App) {
	// A file left by a process that is gone names a code nobody holds.
	removeResetCodeFile(a, "")

	// A throttle of its own, as the passkey sign-in has, and one that only
	// slows down wrong codes. Behind a reverse proxy every caller shares one
	// bucket, so a gate in front of the right code would let anybody who
	// reaches the page keep the owner out; the code's sixty bits are what
	// stops a guesser.
	gate := newLoginGate(trustedProxies(a))
	var wrong wrongGuesses

	reg.AddOpen(http.MethodPost, "/api/auth/reset",
		"have a password reset code written to the process's own output, or to a file in the data folder where there is none; "+
			"answers where it is and never the code. Open because it is for somebody who cannot sign in",
		func(w http.ResponseWriter, r *http.Request) {
			console := resetConsole()
			code, err := a.Auth.StartReset()
			if errors.Is(err, auth.ErrNothingToReset) {
				writeRefusal(w, http.StatusBadRequest, "noPassword", err.Error(), nil)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if code != "" {
				// Logged first, so the code is the last thing in the output.
				log.Printf("auth: a password reset code was asked for")
				if err := showResetCode(a, console, code); err != nil {
					a.Auth.CancelReset()
					log.Printf("auth: the password reset code could not be written: %v", err)
					writeRefusal(w, http.StatusServiceUnavailable, "resetUnavailable",
						"the reset code could not be written anywhere", nil)
					return
				}
			}
			writeJSON(w, resetPlaceOf(a, console))
		})

	reg.AddOpen(http.MethodPost, "/api/auth/reset/finish",
		"set a new password with the reset code and take the session cookie; turns the second factor off, "+
			"removes every passkey and API token and ends every other session. Open because it is for somebody who cannot sign in",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Code     string `json:"code"`
				Password string `json:"password"`
			}
			if !decodeJSONUpTo(w, r, &body, maxAuthBody) {
				return
			}
			passkeys := 0
			// Runs while nothing can be minted: whoever resets from the machine
			// ends up holding the only way in, and a token or a passkey
			// somebody else got hold of or registered would otherwise outlive
			// the reset. The password is stored by then, so nothing here may
			// fail the request.
			endTheRest := func() {
				if err := a.APITokens.RevokeAll(); err != nil {
					log.Printf("auth: password reset: revoking the API tokens: %v", err)
				}
				n, err := a.Store.DeleteAllPasskeys()
				if err != nil {
					log.Printf("auth: password reset: removing the passkeys: %v", err)
				}
				passkeys = n
				// A live stream was let in when it opened and would keep
				// delivering to a session that has just ended.
				a.Hub.DropAll()
				removeResetCodeFile(a, "")
			}
			reset, err := a.Auth.FinishReset(body.Code, body.Password, endTheRest)
			switch {
			case refusePasswordLength(w, err):
				return
			case errors.Is(err, auth.ErrNothingToReset):
				writeRefusal(w, http.StatusBadRequest, "noPassword", err.Error(), nil)
				return
			case errors.Is(err, auth.ErrCodeRejected):
				wrong.add()
				if !gate.try(r) {
					http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
					return
				}
				writeRefusal(w, http.StatusUnauthorized, "codeRejected", err.Error(), nil)
				return
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			gate.pass(r)
			log.Printf("auth: the password was reset with a reset code; the second factor is off, "+
				"%d passkey(s) and every API token are removed and every other session has ended", passkeys)

			if tok, ok := a.Auth.IssueFor(reset); ok {
				setSession(w, r, tok)
			}
			learnDomain(a, reg, r)
			writeJSON(w, map[string]any{"enabled": true, "authenticated": true})
		})
}
