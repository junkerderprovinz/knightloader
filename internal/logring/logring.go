// Package logring keeps a bounded tail of the process's own log output in
// memory, hands it out with a cursor so a follow view can ask only for what is
// new, and - when the operator switches it on - tees the same lines into a
// capped, rotating file on disk. Nothing here decides what gets logged; only
// what happens to the lines after something else already decided to log them.
//
// The file is off until somebody switches it on, and an update does not switch
// it on. See internal/settings.LogFile and file.go for the sink itself. In
// memory the ring is what the diagnostics bundle reads, and it costs no disk.
//
// It taps the standard log package's default output rather than asking the
// couple hundred call sites across the tree to log through an injected writer:
// every one of them calls the package-level log.Printf against the shared
// default logger, and log.SetOutput redirects that logger process-wide.
//
// The tap installs from this package's own init rather than from
// cmd/knightloader, desktop or internal/api's route registration. Go finishes
// initialising every package main imports, this one included, before main's
// body runs, which is before app.New logs its first line about the store, the
// engine, a failed netproxy start or a failed JD provision. A call made while
// route handlers are registered would run after all of those, and a
// diagnostics bundle meant to explain a bad boot would be missing the boot.
//
// The file cannot install that early: the data directory is not known until
// main has read its environment, and whether a file is wanted at all is not
// known until settings.Load has run inside app.New. OpenFile therefore writes
// the ring's current contents into the file before the first live line reaches
// it. log.SetOutput is called exactly once, from init below; a second call
// from main would take the tap off the ring.
package logring

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Capacity is how many lines the default ring keeps. A diagnostics bundle is
// pulled after something has gone wrong, and the interesting line is often a
// few hundred back from what the user noticed.
const Capacity = 500

// Entry is one kept line and the number that identifies it.
//
// Seq counts from 1 and never repeats within one process, so a follow view can
// ask for "everything after 412" instead of re-fetching five hundred lines and
// diffing them, which cannot tell a repeated line from a new one or notice
// that the ring wrapped in between. It resets on restart; see Since for what a
// cursor from a process that is gone does.
type Entry struct {
	Seq  uint64 `json:"seq"`
	Line string `json:"line"`
}

// Ring is a fixed-capacity tail of lines, oldest dropped first. The zero
// value is not usable; construct one with New.
type Ring struct {
	capacity int

	mu      sync.Mutex
	entries []Entry
	seq     uint64

	// sink is the optional file the lines are also written to. Guarded by the
	// same mutex as the lines themselves, so that the order lines reach the
	// file is the order they were logged in: a sink swapped in behind a
	// separate lock could take a line that was already in the ring and miss
	// the one after it.
	sink *fileSink

	// replayed is the highest Seq already written into a file by OpenFile's
	// replay. It lives on the ring and not on the sink because a second arm
	// in the same process opens the same path in append mode, and a watermark
	// that started again with each sink would write the whole ring into the
	// file twice.
	replayed uint64
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
//
// It always answers (len(p), nil). An error reported here would travel back
// out through log.Logger.Output to a caller that has no business handling it,
// and the one failure that can happen, the file sink refusing a write, is
// recorded on the sink instead. See file.go for why it is not logged.
func (r *Ring) Write(p []byte) (int, error) {
	n := len(p)
	s := strings.TrimRight(string(p), "\n")
	if s == "" {
		return n, nil
	}
	r.mu.Lock()
	for _, line := range strings.Split(s, "\n") {
		r.seq++
		r.entries = append(r.entries, Entry{Seq: r.seq, Line: line})
	}
	if over := len(r.entries) - r.capacity; over > 0 {
		// Cleared before the reslice: reslicing alone leaves the dropped
		// strings in the backing array until a later append overwrites the
		// slot, so a bounded ring could hold close to double its capacity
		// in lines it has forgotten.
		for i := 0; i < over; i++ {
			r.entries[i] = Entry{}
		}
		r.entries = r.entries[over:]
	}
	// Inside the lock, so the file's order is the ring's order. The sink never
	// returns an error and never logs; a write it could not make switches it
	// off and is reported through FileStatus.
	if r.sink != nil {
		r.sink.write(p)
	}
	r.mu.Unlock()
	return n, nil
}

// Lines returns the lines currently kept, oldest first. The result is a copy:
// a caller JSON-encoding it straight into an HTTP response must not race the
// next line landing.
//
// Plain strings rather than entries: the diagnostics bundle
// (internal/api/routes_diagnostics.go) and its tests read this, and nobody
// reading a bug report has a use for the sequence numbers.
func (r *Ring) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Line
	}
	return out
}

// Since returns the kept entries newer than after, how many the ring threw
// away that the caller had not seen yet, and the newest sequence number in
// existence.
//
// A busy instance can log more than Capacity lines between two polls two
// seconds apart. Without the dropped count the follow view would join the two
// halves and show a continuous log with a hole in the middle; with it, the
// page can say a gap happened and how big it was.
//
// limit takes the oldest matching entries, not the newest: taking the newest
// would discard the lines in between, while taking the oldest advances the
// cursor and the next poll picks up the rest. A limit of zero or less means
// the ring's own capacity.
//
// An `after` larger than newest means the cursor came from a process that has
// since restarted. Everything kept is returned and dropped is zero, because
// nothing this process had was lost.
func (r *Ring) Since(after uint64, limit int) (out []Entry, dropped int, newest uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newest = r.seq
	if limit <= 0 || limit > r.capacity {
		limit = r.capacity
	}
	if len(r.entries) == 0 {
		return []Entry{}, 0, newest
	}
	if after > newest {
		after = 0
	}
	oldest := r.entries[0].Seq
	if after > 0 && oldest > after+1 {
		dropped = int(oldest - after - 1)
	}
	for _, e := range r.entries {
		if e.Seq <= after {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	if out == nil {
		// Never nil, because this is JSON-encoded straight into a response
		// and a null there is a .map() that throws on a page with nothing
		// to draw.
		out = []Entry{}
	}
	return out, dropped, newest
}

// std is the ring the running process logs into. Everything below forwards to
// it, the shape the log package uses for its own default Logger.
var std = New(Capacity)

func init() {
	log.SetOutput(io.MultiWriter(os.Stderr, std))
}

// Lines returns the process's recent log lines, oldest first.
func Lines() []string { return std.Lines() }

// Since is the process's recent log lines with a cursor - see (*Ring).Since.
func Since(after uint64, limit int) ([]Entry, int, uint64) { return std.Since(after, limit) }
