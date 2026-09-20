package app

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Each input is a value or sentence this build really produces (Gopeed, JD,
// yt-dlp, Go's transport errors), so a backend that changes its wording breaks
// this test instead of silently classifying everything as unknown.
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   failure
		want core.Reason
	}{
		{"a dead link", failure{status: 404}, core.ReasonGone},
		{"a link the host has withdrawn", failure{status: 410}, core.ReasonGone},
		{"the engine's own words for a 404", failure{text: "http request fail, code:404"}, core.ReasonGone},

		{"credentials refused", failure{status: 403}, core.ReasonAuth},
		{"a proxy wanting credentials", failure{status: 407}, core.ReasonAuth},
		{"JD reporting a 401", failure{text: "jd /downloads/add: HTTP 401: no"}, core.ReasonAuth},

		{"throttled", failure{status: 429}, core.ReasonLimit},
		{"the hoster bandwidth code", failure{status: 509}, core.ReasonLimit},
		{"an allowance spent", failure{text: "alldebrid: quota exceeded for today"}, core.ReasonLimit},

		{"the host is down for now", failure{status: 503}, core.ReasonUnavailable},
		{"a gateway in between gave up", failure{status: 502}, core.ReasonUnavailable},
		{"a hoster saying so in words", failure{text: "service unavailable, try later"}, core.ReasonUnavailable},

		{"a name that does not resolve", failure{err: &net.DNSError{Name: "host.example"}}, core.ReasonNetwork},
		{"nothing listening", failure{err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, core.ReasonNetwork},
		{"the request ran out of time", failure{err: context.DeadlineExceeded}, core.ReasonNetwork},
		{"a per-connection timeout in words", failure{text: "connection 2 failed: i/o timeout"}, core.ReasonNetwork},

		{"a full disk as an errno", failure{err: fmt.Errorf("write chunk: %w", syscall.ENOSPC)}, core.ReasonDiskFull},
		{"a full disk in Go's words", failure{text: "write /data/x.part: no space left on device"}, core.ReasonDiskFull},
		{"a full disk in Windows' words", failure{text: "There is not enough space on the disk."}, core.ReasonDiskFull},

		{"nothing claims the link", failure{text: "yt-dlp: Unsupported URL: https://x.example/p"}, core.ReasonUnsupported},

		{"a human is being asked", failure{text: "jd: waiting for captcha input"}, core.ReasonCaptcha},

		{"called off from this side", failure{err: context.Canceled}, core.ReasonCancelled},

		// A sentence nothing matches stays unknown; a guess would send somebody
		// to fix the wrong problem.
		{"an error nothing recognises", failure{text: "rapidgator: error code 7731"}, core.ReasonUnknown},
		{"a status with no specific meaning", failure{status: 418}, core.ReasonUnknown},
		{"no error at all", failure{}, core.ReasonUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.in); got != tc.want {
				t.Errorf("classify(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Go's Windows ENOSPC is a synthetic value no call returns, so errno 112 is
// matched instead, on Windows only, since elsewhere it is EHOSTDOWN.
func TestClassifyWindowsDiskFull(t *testing.T) {
	err := fmt.Errorf("write x.part: %w", syscall.Errno(112))
	got := classify(failure{err: err})
	if runtime.GOOS == "windows" {
		if got != core.ReasonDiskFull {
			t.Errorf("ERROR_DISK_FULL classified as %q, want %q", got, core.ReasonDiskFull)
		}
		return
	}
	if got == core.ReasonDiskFull {
		t.Errorf("errno 112 was read as a full disk on %s, where it is EHOSTDOWN", runtime.GOOS)
	}
}

// The typed error outranks the words, which Windows localises.
func TestClassifyPrefersTheErrorValue(t *testing.T) {
	// A cancelled context whose sentence also contains a phrase from the table.
	err := fmt.Errorf("connection reset: %w", context.Canceled)
	if got := classify(failure{err: err}); got != core.ReasonCancelled {
		t.Errorf("classify = %q, want %q: the error value must win over the wording", got, core.ReasonCancelled)
	}
}

// A status the caller passes wins over the sentence around it.
func TestClassifyCallerStatusWins(t *testing.T) {
	if got := classify(failure{text: "offline", status: 404}); got != core.ReasonGone {
		t.Errorf("classify = %q, want %q", got, core.ReasonGone)
	}
}

func TestStatusIn(t *testing.T) {
	cases := map[string]int{
		"jd /downloads: HTTP 403":                    403,
		"http request fail, code:404":                404,
		"connection 0 failed: retries=3, status=503": 503,
		"offline (HTTP 429)":                         429,
		// Digits in a quoted URL are not a status.
		"could not fetch https://host.example/a/file.zip": 0,
		"rapidgator: error code 7731":                     0,
		"":                                                0,
	}
	for in, want := range cases {
		if got := statusIn(in); got != want {
			t.Errorf("statusIn(%q) = %d, want %d", in, got, want)
		}
	}
}

// A task failing with a sentence carries the typed reason too.
func TestFailedTaskCarriesReason(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "http request fail, code:404"})
	a.mu.Lock()
	reason := task.Reason
	a.mu.Unlock()
	if reason != core.ReasonGone {
		t.Errorf("reason = %q, want %q", reason, core.ReasonGone)
	}

	// A restart clears the reason with the sentence.
	a.RestartTasks([]string{task.ID})
	a.mu.Lock()
	reason = task.Reason
	a.mu.Unlock()
	if reason != core.ReasonUnknown {
		t.Errorf("after a restart the reason is %q, want it cleared", reason)
	}
}

// A full disk is not retried: retrying frees no space.
func TestDiskFullIsNotRetried(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{
		Status: core.StatusError,
		Err:    "write /data/f.bin.part: no space left on device",
	})

	a.mu.Lock()
	reason, retries, next := task.Reason, task.Retries, task.NextTry
	a.mu.Unlock()
	if reason != core.ReasonDiskFull {
		t.Fatalf("reason = %q, want %q", reason, core.ReasonDiskFull)
	}
	if retries != 0 {
		t.Errorf("retries = %d, want the attempt not to be repeated", retries)
	}
	if !next.IsZero() {
		t.Errorf("a retry is armed for %v; a full disk does not empty itself", next)
	}
}

// An ordinary failure keeps its backoff.
func TestUnknownFailureStillRetries(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "rapidgator: error code 7731"})

	a.mu.Lock()
	reason, retries := task.Reason, task.Retries
	a.mu.Unlock()
	if reason != core.ReasonUnknown {
		t.Errorf("reason = %q, want an unrecognised failure to stay unknown", reason)
	}
	if retries != 1 {
		t.Errorf("retries = %d, want 1: an ordinary failure is still worth another go", retries)
	}
}

// The reason can veto a reconnect, but anything unclassified still allows one.
func TestAddressMayHelp(t *testing.T) {
	cannotHelp := []core.Reason{
		core.ReasonGone, core.ReasonAuth, core.ReasonDiskFull,
		core.ReasonUnsupported, core.ReasonCaptcha, core.ReasonCancelled,
	}
	for _, r := range cannotHelp {
		if addressMayHelp(r) {
			t.Errorf("%q would reboot the router, and a new address cannot mend it", r)
		}
	}
	canHelp := []core.Reason{
		core.ReasonLimit, core.ReasonNetwork, core.ReasonUnavailable, core.ReasonUnknown,
	}
	for _, r := range canHelp {
		if !addressMayHelp(r) {
			t.Errorf("%q blocks a reconnect that should still be allowed to run", r)
		}
	}
}

// When every matching backend has handed a link on, it is unsupported.
func TestExhaustedChainIsUnsupported(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// "http" is the last resolver in the chain, so there is nothing behind it.
	task := &core.Task{ID: "1", URL: "https://host.example/watch/x", Resolver: "http", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "nope", Unsupported: true})

	a.mu.Lock()
	reason := task.Reason
	a.mu.Unlock()
	if reason != core.ReasonUnsupported {
		t.Errorf("reason = %q, want %q", reason, core.ReasonUnsupported)
	}
}
