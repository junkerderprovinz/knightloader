// Package logring keeps a bounded tail of the process's own log output in
// memory, hands it out with a cursor so a follow view can ask only for what is
// new, and - when the operator switches it on - tees the same lines into a
// capped, rotating file on disk. Nothing here decides what gets logged; only
// what happens to the lines after something else already decided to log them.
//
// THE FILE IS OFF UNTIL SOMEBODY SWITCHES IT ON, and an update never switches
// it on for anybody. See internal/settings.LogFile and file.go for the sink
// itself. In memory this package still behaves exactly as it always did: the
// ring is the thing the diagnostics bundle reads, and it costs no disk at all.
//
// It taps the standard "log" package's default output rather than asking the
// couple hundred call sites across the tree to log through an injected writer
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
//
// THAT ARGUMENT IS ALSO WHAT MAKES THE FILE HARD, and file.go's replay is the
// answer: the data directory is not known until main has read its environment,
// and whether a file is wanted at all is not known until settings.Load has run
// inside app.New. So the sink can only ever be attached AFTER the lines that
// explain a bad boot have already been logged. OpenFile therefore writes the
// ring's current contents into the file before the first live line reaches it.
// log.SetOutput is still called exactly once, from init below, and a second
// call from main would take the tap off the ring and undo all of the above.
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

// Entry is one kept line and the number that identifies it.
//
// Seq counts from 1 and never repeats WITHIN one process. It exists so that a
// follow view can say "everything after 412" instead of re-fetching all five
// hundred lines every two seconds and diffing them, which cannot tell a
// repeated line from a new one and cannot notice that the ring wrapped in
// between. It resets on restart, because it is an index into a buffer that
// does not survive one - Since below says what it does about a cursor from a
// process that is gone.
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

	// replayed is the highest Seq that has already been written into a file by
	// OpenFile's replay. It lives on the RING and not on the sink because a
	// second arm in the same process opens the same path in append mode: a
	// watermark that started again with each sink would write the whole ring
	// into the file a second time, and the operator would read the same
	// morning twice.
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
// It always answers (len(p), nil). A short write or an error reported here
// would travel back out through log.Logger.Output to a caller that has no
// business handling it, and the one failure that can actually happen - the
// file sink refusing a write - is recorded on the sink instead. See file.go
// for why that failure must never be logged.
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
		// Cleared before the reslice, not just dropped from view: reslicing
		// alone leaves the dropped strings sitting in the backing array,
		// reachable through it until some later append happens to overwrite
		// that same slot or growth replaces the array outright - a "bounded"
		// ring that can quietly hold close to double its own capacity in
		// lines it has already forgotten about.
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
// Deliberately unchanged when the ring grew sequence numbers underneath it.
// The diagnostics bundle (internal/api/routes_diagnostics.go) and its tests
// read this, and a bundle whose log section suddenly became an array of
// objects would break every reader that already knows the old shape for a
// number nobody reading a bug report has any use for.
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
// DROPPED IS THE HALF THAT MAKES FOLLOWING HONEST. A busy instance can log
// more than Capacity lines between two polls two seconds apart, and without
// this count the follow view would join the two halves silently and show a
// continuous log with a hole in the middle. With it, the page can say a gap
// happened and how big it was, which is also the one moment where "switch the
// log file on" is advice rather than an advert.
//
// limit takes the OLDEST matching entries and not the newest. Taking the
// newest would silently discard the lines in between; taking the oldest means
// the cursor advances, and the very next poll picks up the rest. A limit of
// zero or less means the ring's own capacity.
//
// An `after` LARGER than newest means the cursor came from a process that has
// since restarted - the counter began again at 1 and the caller is holding a
// number from a buffer that no longer exists. Everything kept is returned and
// dropped is zero, because nothing was lost that this process ever had.
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
		// Never nil, because this is JSON-encoded straight into a response and
		// a null there is a .map() that throws on a page that had nothing to
		// draw. Same class of bug routes_features.go documents for
		// archivePasswords.
		out = []Entry{}
	}
	return out, dropped, newest
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

// Since is the process's recent log lines with a cursor - see (*Ring).Since.
func Since(after uint64, limit int) ([]Entry, int, uint64) { return std.Since(after, limit) }
