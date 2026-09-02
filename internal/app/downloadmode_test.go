package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// The download mode: whether a transfer goes out on an account or anonymously.
//
// It exists because the answer was invisible (jdp, 2026-09-02: "Wenn man links
// runterladen möchte für die kein premium account hinterlegt ist muss das
// angezeigt werden un der link im free modus heruntergeladen werden. wie in
// JD"). A hoster link with no account behind it looked exactly like one with an
// account behind it, right up until it was slow, queued behind a countdown, or
// asking for a captcha.

// TestModeIsUnknownForAnOrdinaryFile: the common case answers nothing at all.
// A file on a plain web server is neither free nor premium, and a label there
// would put a word on screen that answers a question nobody asked.
func TestModeIsUnknownForAnOrdinaryFile(t *testing.T) {
	a := newOrderApp(t)
	task := &core.Task{ID: "x", URL: "https://example.com/holiday.zip"}

	a.mu.Lock()
	got := a.modeForLocked(task, "direct")
	a.mu.Unlock()

	if got != core.ModeUnknown {
		t.Errorf("mode for a plain file = %q, want %q - only a hoster link gets a label", got, core.ModeUnknown)
	}
}

// TestModeIsPremiumThroughADebridService: a debrid resolver IS the account, so
// anything routed to one is premium by construction.
func TestModeIsPremiumThroughADebridService(t *testing.T) {
	a := newOrderApp(t)
	task := &core.Task{ID: "x", URL: "https://rapidgator.net/file/abc"}

	a.mu.Lock()
	got := a.modeForLocked(task, "torbox")
	a.mu.Unlock()

	if got != core.ModePremium {
		t.Errorf("mode through torbox = %q, want %q", got, core.ModePremium)
	}
}

// TestModeTellsFreeFromPremiumOnTheSameHost is the distinction the whole field
// exists for: the SAME url, the SAME resolver, and the only difference is
// whether a login has been confirmed on the sidecar.
func TestModeTellsFreeFromPremiumOnTheSameHost(t *testing.T) {
	a := newOrderApp(t)
	task := &core.Task{ID: "x", URL: "https://rapidgator.net/file/abc"}
	t.Cleanup(func() {
		jdresolver.SetKnownHosts(nil)
		jdresolver.SetHostActive("rapidgator.net", false)
	})

	// JD has a plugin for the host and nobody has a login: free mode.
	jdresolver.SetKnownHosts([]string{"rapidgator.net"})
	a.mu.Lock()
	free := a.modeForLocked(task, "jd")
	a.mu.Unlock()
	if free != core.ModeFree {
		t.Errorf("mode with a known host and no login = %q, want %q", free, core.ModeFree)
	}

	// A login the sidecar has confirmed: premium, same url, same resolver.
	jdresolver.SetHostActive("rapidgator.net", true)
	a.mu.Lock()
	paid := a.modeForLocked(task, "jd")
	a.mu.Unlock()
	if paid != core.ModePremium {
		t.Errorf("mode with a confirmed login = %q, want %q", paid, core.ModePremium)
	}
}

// TestModeSaysNothingForAHostJDDoesNotKnow: the label must not spread to every
// link merely because JD is the catch-all. "Free mode" is a claim about a
// hoster, and claiming it for an unknown host would be a guess on screen.
func TestModeSaysNothingForAHostJDDoesNotKnow(t *testing.T) {
	a := newOrderApp(t)
	task := &core.Task{ID: "x", URL: "https://some-random-site.example/dl/9"}

	a.mu.Lock()
	got := a.modeForLocked(task, "jd")
	a.mu.Unlock()

	if got != core.ModeUnknown {
		t.Errorf("mode for a host JD does not know = %q, want %q", got, core.ModeUnknown)
	}
}
