package tsnetsrv

import (
	"net/http"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	return New(t.TempDir(), func() http.Handler { return http.NotFoundHandler() })
}

// newUnreachableManager builds a Manager whose Start drives a REAL
// *tsnet.Server through a real Up() call, but pointed at a control URL
// nothing answers on (port 1 is reserved and refuses connections
// immediately, no DNS lookup needed) - so Up() runs its genuine local state
// machine without reaching Tailscale's actual control plane or needing real
// credentials. This is what lets the concurrency test below exercise the
// genuine Start/Stop/run race the code review flagged, not a mock of it.
//
// A fresh node with no AuthKey starts in local state "NeedsLogin" and Up()
// then blocks on an interactive login that can never complete against a
// fake control URL - by design (tsnet's local state machine decides this
// before ever attempting a network call), not something a bad ControlURL or
// AuthKey value can turn into a fast synchronous error without a real
// account or a protocol-compatible fake control server, neither of which is
// worth building for this test. That is exactly the "still connecting"
// window Stop() has to be able to interrupt, so it is also exactly the
// window this suite tests against.
func newUnreachableManager(t *testing.T) *Manager {
	m := newTestManager(t)
	m.controlURL = "http://127.0.0.1:1"
	return m
}

// waitForStatus polls Info() until it matches want or the timeout elapses,
// failing the test rather than hanging forever if it never does - the
// pattern this whole file uses instead of a fixed sleep, since exactly how
// long a real Up() takes to fail against an unreachable control URL is not
// something this test should have to pin down precisely.
func waitForStatus(t *testing.T, m *Manager, want Status, timeout time.Duration) Info {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		info := m.Info()
		if info.Status == want {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("Status never reached %q within %s, last Info() = %+v", want, timeout, info)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerInitialStatusOff(t *testing.T) {
	m := newTestManager(t)
	info := m.Info()
	if info.Status != StatusOff {
		t.Fatalf("Status = %q, want %q", info.Status, StatusOff)
	}
	if info.AuthURL != "" || info.FunnelURL != "" || info.Hostname != "" || info.Error != "" {
		t.Fatalf("Info() on a fresh Manager should be all-empty, got %+v", info)
	}
}

// Stop before Start is what a page load calls if it renders the "off" state
// and the user immediately presses a still-off Trennen button, or what a
// double-click on Trennen produces once the first call already cleared
// m.srv - either way it must not panic or report an error.
func TestManagerStopBeforeStartIsNoop(t *testing.T) {
	m := newTestManager(t)
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() on a never-started Manager returned %v, want nil", err)
	}
	if info := m.Info(); info.Status != StatusOff {
		t.Fatalf("Status after a no-op Stop = %q, want %q", info.Status, StatusOff)
	}
}

// authURLPattern is how the interactive login link escapes tsnet's UserLogf
// stream (see Start's own doc comment) - the one piece of parsing logic in
// this package that has no network dependency and is worth pinning down on
// its own.
func TestAuthURLPatternExtractsLoginLink(t *testing.T) {
	line := "To authenticate, visit:\n\n\thttps://login.tailscale.com/a/0123456789abcdef\n\n"
	got := authURLPattern.FindString(line)
	want := "https://login.tailscale.com/a/0123456789abcdef"
	if got != want {
		t.Fatalf("authURLPattern.FindString(%q) = %q, want %q", line, got, want)
	}
}

func TestAuthURLPatternNoMatch(t *testing.T) {
	if got := authURLPattern.FindString("Connected."); got != "" {
		t.Fatalf("authURLPattern.FindString on an unrelated log line = %q, want \"\"", got)
	}
}

// A failed connection attempt must not permanently wedge the Manager: the
// bug the review found was Start's idempotency guard checking only
// `m.srv != nil`, which run's error paths never cleared - every later
// Start() after any failure silently no-op'd forever, with no way for the
// UI's own "press Connect again" hint to ever actually retry.
//
// startTimeout is what turns "blocked forever in local NeedsLogin state"
// (see newUnreachableManager's own comment) into a real, fast error from
// Up() - a context deadline - so this exercises run()'s actual error
// branch, not a stand-in for it.
func TestManagerFailedStartAllowsRetry(t *testing.T) {
	m := newUnreachableManager(t)
	m.startTimeout = 500 * time.Millisecond

	if err := m.Start(""); err != nil {
		t.Fatalf("first Start() returned %v, want nil", err)
	}
	first := waitForStatus(t, m, StatusError, 20*time.Second)
	if first.Error == "" {
		t.Fatalf("Info() after a failed Up() has no Error, got %+v", first)
	}

	// The real assertion: Start() must actually begin a NEW attempt here,
	// not silently no-op because m.srv was left non-nil by the failure
	// above.
	if err := m.Start(""); err != nil {
		t.Fatalf("retry Start() returned %v, want nil", err)
	}
	if info := m.Info(); info.Status != StatusConnecting {
		t.Fatalf("Status right after a retry Start() = %q, want %q (a wedged Manager silently stays %q)", info.Status, StatusConnecting, StatusError)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("cleanup Stop() returned %v, want nil", err)
	}
}

// Stop() must return promptly even while run() is still blocked inside
// Up() - the review's second-most-severe finding was that calling
// srv.Close() concurrently with an in-flight Up() violates tsnet's own
// documented contract ("must not be called before or concurrently with
// Start"). The fix cancels Up()'s context and waits for run() to actually
// return before calling Close(); this test's whole point is proving that
// wait terminates instead of deadlocking.
func TestManagerStopDuringConnectingDoesNotHang(t *testing.T) {
	m := newUnreachableManager(t)
	if err := m.Start(""); err != nil {
		t.Fatalf("Start() returned %v, want nil", err)
	}
	// No wait here on purpose: Stop() is called while run() is presumably
	// still inside (or just about to enter) srv.Up() - the exact window the
	// race lived in.

	stopped := make(chan error, 1)
	go func() { stopped <- m.Stop() }()

	select {
	case err := <-stopped:
		if err != nil {
			// srv.Close() on a server whose Up() never finished starting can
			// legitimately return an error - only a hang is the bug this
			// test guards against, not a non-nil return.
			t.Logf("Stop() returned %v (non-nil is fine here, a hang is not)", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Stop() did not return within 20s while Start() was still connecting - it deadlocked")
	}

	if info := m.Info(); info.Status != StatusOff {
		t.Fatalf("Status after Stop() = %q, want %q", info.Status, StatusOff)
	}

	// A Manager that just stopped mid-connect must accept a fresh Start
	// immediately, the same guarantee TestManagerFailedStartAllowsRetry
	// checks for the error path.
	if err := m.Start(""); err != nil {
		t.Fatalf("Start() after Stop() returned %v, want nil", err)
	}
	if info := m.Info(); info.Status != StatusConnecting {
		t.Fatalf("Status after Start() following a mid-connect Stop() = %q, want %q", info.Status, StatusConnecting)
	}
	_ = m.Stop()
}
