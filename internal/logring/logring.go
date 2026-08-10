// Package logring keeps a bounded tail of the process's own log output in
// memory, for the diagnostics bundle and nothing else: it is not a log sink
// anybody configures, it never touches disk, and nothing here decides what
// gets logged - only what happens to the lines after something else already
// decided to log them.
//
// It taps the standard "log" package's default output rather than asking the
// couple dozen call sites across the tree to log through an injected writer
// instead: every one of them already calls the package-level log.Printf (and
// friends) against the shared default logger, and log.SetOutput is what
// redirects that logger process-wide, for every caller, without touching any
// of them.
//
// The tap installs from this package's own init, not from a call inside
// cmd/knightloader, desktop, or internal/api's route registration. Go finishes
// initialising every package main imports - transitively, which includes this
// one, because internal/api imports it for Lines - before main's own function
// body runs, and that is before app.New() logs its first line about the
// store, the engine, a failed netproxy start or a failed JD provision. A call
// made from inside a route handler's registration would run only once the
// HTTP server is already being built, which is after every one of those, and
// a diagnostics bundle whose whole point is explaining a bad boot would then
// be missing the boot.
package logring

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Capacity is how many lines the default ring keeps. Generous on purpose: a
// diagnostics bundle is pulled after something has already gone wrong, and
// the interesting line is often a few hundred back from whatever the user
// noticed.
const Capacity = 500

// Ring is a fixed-capacity tail of lines, oldest dropped first. The zero
// value is not usable; construct one with New.
type Ring struct {
	capacity int

	mu    sync.Mutex
	lines []string
}

// New returns a Ring that keeps at most capacity lines.
func New(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{capacity: capacity}
}

// Write implements io.Writer. log.Logger.Output hands this exactly one
// completed record per call, always newline-terminated, but nothing enforces
// that on whoever else might write here - so this splits on '\n' rather than
// trusting p to be a single line, and keeps every non-empty line it finds
// rather than only the first.
func (r *Ring) Write(p []byte) (int, error) {
	n := len(p)
	s := strings.TrimRight(string(p), "\n")
	if s == "" {
		return n, nil
	}
	r.mu.Lock()
	for _, line := range strings.Split(s, "\n") {
		r.lines = append(r.lines, line)
	}
	if over := len(r.lines) - r.capacity; over > 0 {
		// Cleared before the reslice, not just dropped from view: reslicing
		// alone leaves the dropped strings sitting in the backing array,
		// reachable through it until some later append happens to overwrite
		// that same slot or growth replaces the array outright - a "bounded"
		// ring that can quietly hold close to double its own capacity in
		// lines it has already forgotten about.
		for i := 0; i < over; i++ {
			r.lines[i] = ""
		}
		r.lines = r.lines[over:]
	}
	r.mu.Unlock()
	return n, nil
}

// Lines returns the lines currently kept, oldest first. The result is a copy:
// a caller JSON-encoding it straight into an HTTP response must not race the
// next line landing.
func (r *Ring) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// std is the ring the running process actually logs into. Everything below is
// package-level convenience over it, the same shape the "log" package itself
// uses for its own default Logger plus the top-level functions that forward
// to it.
var std = New(Capacity)

func init() {
	log.SetOutput(io.MultiWriter(os.Stderr, std))
}

// Lines returns the process's recent log lines, oldest first.
func Lines() []string { return std.Lines() }
