package tsnetsrv

import (
	"net/http"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	return New(t.TempDir(), func() http.Handler { return http.NotFoundHandler() })
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
