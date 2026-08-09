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

// TestClassify pins one real failure to each reason. Every input here is a
// value or a sentence this build can actually produce — Gopeed's wording, JD's,
// yt-dlp's, Go's own transport errors — so a backend that changes its phrasing
// breaks this test rather than quietly settling every failure as unknown.
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

		// The rule the whole classifier rests on. A hoster sentence nothing in the
		// table matches must stay unknown: the interface shows the sentence and
		// says nothing more, which is right, where a guess would send somebody to
		// fix a problem they do not have.
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

// TestClassifyWindowsDiskFull covers the errno Windows really returns for a full
// disk. Go's Windows syscall package defines ENOSPC as a synthetic value no call
// ever produces, so the number is the only thing to match on — and it is matched
// on Windows only, because 112 is EHOSTDOWN elsewhere.
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

// TestClassifyPrefersTheErrorValue makes sure the typed error outranks the
// words. A Windows box reports its errors in the language it was installed in,
// so a classifier that read the sentence first would answer differently on a
// German machine for the same failure.
func TestClassifyPrefersTheErrorValue(t *testing.T) {
	// A cancelled context whose sentence also contains a phrase from the table.
	err := fmt.Errorf("connection reset: %w", context.Canceled)
	if got := classify(failure{err: err}); got != core.ReasonCancelled {
		t.Errorf("classify = %q, want %q: the error value must win over the wording", got, core.ReasonCancelled)
	}
}

// TestClassifyCallerStatusWins guards the seam the availability probe uses: it
// holds a real response, and the number in its hand must not be second-guessed
// from the sentence it formatted around it.
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
		// A link in the sentence is not a status. This is the false positive worth
		// guarding: an error that quotes the URL it failed on would otherwise be
		// classified by whatever digits happen to sit in the path.
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

// TestFailedTaskCarriesReason is the wiring test: a backend reports a failure as
// a sentence, and the task that settles from it has to carry the typed cause as
// well as the words.
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

	// And a restart takes the reason away with the sentence: a task that is
	// running again must not still be advising about the failure before it.
	a.RestartTasks([]string{task.ID})
	a.mu.Lock()
	reason = task.Reason
	a.mu.Unlock()
	if reason != core.ReasonUnknown {
		t.Errorf("after a restart the reason is %q, want it cleared", reason)
	}
}

// TestDiskFullIsNotRetried is the point of telling a full disk apart from a
// write error at all. Retrying frees no space, and five more attempts bury the
// one failure the user could have fixed.
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

// TestUnknownFailureStillRetries is the other half of it: only a full disk is
// exempt, and an ordinary failure must keep its backoff.
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

// TestAddressMayHelp pins the veto the reason gives the reconnect. It must
// answer yes for anything unclassified, or the taxonomy would silently switch
// off a reconnect that fires today.
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

// TestExhaustedChainIsUnsupported: when every backend that matched a link has
// handed it on, "no backend handles this" is not a guess, it is the record.
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
