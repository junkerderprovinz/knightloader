package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// The download mode: whether a hoster link goes out on an account or in free
// mode.

// A file on a plain web server is neither free nor premium.
func TestModeIsUnknownForAnOrdinaryFile(t *testing.T) {
	a := newOrderApp(t)
	task := &core.Task{ID: "x", URL: "https://example.com/holiday.zip"}

	a.mu.Lock()
	got := a.modeForLocked(task, "direct")
	a.mu.Unlock()

	if got != core.ModeUnknown {
		t.Errorf("mode for a plain file = %q, want %q; only a hoster link gets a label", got, core.ModeUnknown)
	}
}

// A debrid resolver is the account, so anything routed to one is premium.
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

// The same URL and resolver differ only in whether JD has confirmed a login.
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

// JD is the catch-all, but "free mode" is only claimed for hosts it knows.
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
