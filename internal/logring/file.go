package logring

// The optional file sink: the same lines the ring keeps in memory, appended to
// a capped file on disk that is renamed and started again when it fills up.
//
// The ring holds five hundred lines and dies with the process, so it cannot
// answer "it fell over the night before last, what did it say". In a container
// the same lines also reach docker logs; the file is what is still there once
// those have rolled over, and the only answer at all on the desktop build.
//
// Three rules this file is built around:
//
//  1. Nothing in here may log. The sink sits downstream of
//     log.SetOutput(io.MultiWriter(os.Stderr, std)) and is called from inside
//     (*Ring).Write with the ring's mutex held, so a log.Printf from a failure
//     path would re-enter that Write on the same goroutine and deadlock on a
//     mutex it already holds. A write that cannot be made switches the sink
//     off and records a sentence in Problem, which FileStatus hands to the
//     page. Measuring free disk space, the one call here that reaches into
//     another package, happens in FileStatus on an HTTP goroutine with no ring
//     lock held.
//
//  2. Close before rename. Go opens files without FILE_SHARE_DELETE, so on
//     Windows os.Rename against a handle this process still holds is refused
//     with access denied. The desktop build is mostly Windows, so a rotation
//     tested only on Linux passes and then breaks on the first roll.
//
//  3. Replay on arm. The sink cannot be attached at init time, where the ring
//     is: the data directory is not known until main has read its environment,
//     and whether a file is wanted is not known until settings.Load has run
//     inside app.New. Everything that explains a bad boot is already logged by
//     then, so OpenFile writes the ring's current contents into the file
//     before the first live line.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/diskspace"
)

// Name is the file the current log is written to, inside the configured
// directory. Fixed, so a typo in a path cannot be the reason a log is not
// being written. See internal/settings.LogFile and KL_LOG_DIR.
const Name = "knightloader.log"

// FileOptions is what OpenFile needs: where, how big, and how many.
type FileOptions struct {
	// Dir is the directory the file lives in. Created if it is not there.
	Dir string
	// MaxBytes is the size at which the file is renamed and a new one started.
	MaxBytes int64
	// Keep is how many renamed files stay beside it. Zero keeps only the file
	// being written, see settings.LogFile.Keep.
	Keep int
}

// Generation is one file on disk as the page lists it.
type Generation struct {
	// Index 0 is the file being written; 1 is the newest renamed one.
	Index      int       `json:"index"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// FileState is the whole answer to "is the log being written, where, how much
// of it is there, and what should I do if it is not".
type FileState struct {
	// Enabled is whether lines are reaching a file now, not what the settings
	// switch says: a sink that was armed and then failed reports false here
	// with a sentence in Problem.
	Enabled bool `json:"enabled"`

	// Path is the file being written. Empty when nothing is armed, and left
	// out of the diagnostics bundle, see Redacted.
	Path string `json:"path"`

	Bytes    int64 `json:"bytes"`
	MaxBytes int64 `json:"maxBytes"`
	Keep     int   `json:"keep"`

	// Generations is the files on disk, newest first, index 0 leading. Never
	// nil: a nil slice encodes as JSON null, and a fresh install that never
	// armed the sink would throw on the page's own .map.
	Generations []Generation `json:"generations"`

	// Problem is empty while the file is being written. When it is not, it says
	// what failed, in the words the operating system used, and the card turns
	// it into advice with the path and the free space beside it.
	Problem string `json:"problem,omitempty"`

	// FreeBytes is how much room is left on that volume, and FreeKnown is
	// whether this build could find out. Two answers rather than one number,
	// because internal/diskspace requires a caller to treat "cannot measure"
	// as no opinion rather than as nothing free, and a readout that drew an
	// empty disk would send somebody hunting for a problem they do not have.
	//
	// Only measured while Problem is set: it answers nothing otherwise, and
	// the syscall must not reach the write path (see rule 1 above).
	FreeBytes int64 `json:"freeBytes,omitempty"`
	FreeKnown bool  `json:"freeKnown"`
}

// Redacted is this state as the diagnostics bundle may carry it: everything
// except the path.
//
// The bundle is a file people attach to public bug reports, and a desktop data
// directory reads C:\Users\<a real name>\AppData\..., the same argument
// internal/api/routes_diagnostics.go makes for the store and settings paths.
// Whether a file exists, how big it is and whether it is failing are the facts
// a report needs; where it lives stays on the session-guarded route.
func (s FileState) Redacted() FileState {
	s.Path = ""
	if s.Generations == nil {
		s.Generations = []Generation{}
	}
	return s
}

// fileSink is the writer behind FileState. Its own mutex guards everything in
// it and is always taken under the ring's when both are held, the order
// (*Ring).Write takes them in.
type fileSink struct {
	dir      string
	path     string
	maxBytes int64
	keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
	// problem is the sentence FileStatus reports. Non-empty means f is nil and
	// this sink is not writing any more.
	problem string
}

// OpenFile arms the file sink on the process's own ring: the same lines the
// diagnostics bundle reads are appended to <Dir>/knightloader.log from here on.
//
// Calling it while a sink is already armed replaces it, which is what a saved
// settings page does when the size or the generation count changed.
func OpenFile(o FileOptions) error { return std.OpenFile(o) }

// CloseFile disarms the sink and closes the handle. Safe to call when nothing
// is armed, which is what both mains' shutdown path relies on.
//
// Writes are unbuffered per record, so a crash without this loses nothing; the
// close is tidiness and a released handle on Windows, not durability.
func CloseFile() error { return std.CloseFile() }

// FileStatus is what the log-file card and the diagnostics bundle read.
func FileStatus() FileState { return std.FileStatus() }

// OpenFile arms this ring's file sink. See the package-level OpenFile.
func (r *Ring) OpenFile(o FileOptions) error {
	if o.MaxBytes < 1 {
		// A cap of zero would rotate on every line. A floor rather than an
		// error because settings.LogFile.Sanitized has already clamped
		// anything a person could type, so this only catches a caller inside
		// the tree.
		o.MaxBytes = 1 << 20
	}
	if o.Keep < 0 {
		o.Keep = 0
	}
	s := &fileSink{
		dir:      o.Dir,
		path:     filepath.Join(o.Dir, Name),
		maxBytes: o.MaxBytes,
		keep:     o.Keep,
	}
	err := s.open()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sink != nil {
		r.sink.close()
	}
	// Attached even when opening failed: a sink holding a Problem is how the
	// card says why nothing is being written, while a nil sink would look the
	// same as the switch being off.
	r.sink = s
	if err == nil {
		// Under the ring's lock, so no live line slips in between the
		// snapshot and the sink being attached and lands in the file before
		// the older ones.
		r.replayed = s.replay(r.entries, r.replayed)
	}
	return err
}

// CloseFile disarms this ring's file sink. See the package-level CloseFile.
func (r *Ring) CloseFile() error {
	r.mu.Lock()
	s := r.sink
	r.sink = nil
	r.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.close()
}

// FileStatus reports this ring's sink. See the package-level FileStatus.
//
// The ring's lock is released before the sink is asked, because answering
// involves a stat per generation and, when something is wrong, a free-space
// syscall - none of which may sit in front of every log call in the process.
func (r *Ring) FileStatus() FileState {
	r.mu.Lock()
	s := r.sink
	r.mu.Unlock()
	if s == nil {
		return FileState{Generations: []Generation{}}
	}
	return s.status()
}

// open creates the directory if it is not there and opens the file for
// appending, taking the size it already has as the starting point.
//
// The size is tracked in memory from here on rather than stat-ed per line,
// because a syscall per log record on a spinning array volume is the
// difference between a feature that is off by default and one nobody could
// leave on.
//
// 0o700 on the directory and 0o600 on the file, because a log line can carry a
// feed URL with an indexer's API key in its query string: internal/feed's
// poller logs subscription addresses verbatim, and so does internal/crawler
// for the pages it walks.
func (s *fileSink) open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openLocked()
}

func (s *fileSink) openLocked() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		s.problem = err.Error()
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.problem = err.Error()
		return err
	}
	size := int64(0)
	if st, serr := f.Stat(); serr == nil {
		size = st.Size()
	}
	s.f = f
	s.size = size
	s.problem = ""
	return nil
}

// write appends one already-formatted record. It never returns anything,
// because there is no caller in a position to do something about a failure -
// see rule 1 at the top of this file.
func (s *fileSink) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked(p)
}

func (s *fileSink) writeLocked(p []byte) {
	if s.f == nil {
		return
	}
	// s.size > 0 guards the case where rotating would loop: a single record
	// longer than the whole cap. Rotating an empty file for it would rename
	// nothing, write the record anyway and do it again for the next line,
	// throwing away every generation on disk within a second. An oversized
	// record goes whole into the file it lands in, and the cap holds again
	// from the next line.
	if s.size > 0 && s.size+int64(len(p)) > s.maxBytes {
		if err := s.rotateLocked(); err != nil {
			s.failLocked("rotating the log file: " + err.Error())
			return
		}
	}
	n, err := s.f.Write(p)
	s.size += int64(n)
	if err != nil {
		s.failLocked(err.Error())
	}
}

// failLocked switches the sink off and records why. It does not log, retry or
// measure anything, see rule 1.
//
// Off rather than degraded: a sink kept alive would fail on every line for as
// long as the volume stays full, each failure costing a syscall in front of
// every log call in the process. The lines are still in the ring, on stderr
// and in the diagnostics bundle, which is what the card says so that "the log
// file stopped" is not read as "logging stopped".
func (s *fileSink) failLocked(problem string) {
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	s.problem = problem
}

// rotateLocked renames the current file down the generations and starts a
// fresh one. The handle is closed first, see rule 2 at the top of this file.
func (s *fileSink) rotateLocked() error {
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			s.f = nil
			return err
		}
		s.f = nil
	}
	if s.keep <= 0 {
		// "Keep only the file being written" means the full file goes and a
		// new one starts. Not truncating in place, because the file may be
		// open in somebody's editor and a truncate leaves them reading a
		// hole.
		if err := removeIfPresent(s.path); err != nil {
			return err
		}
		return s.openLocked()
	}
	// The overflow first, then each survivor one place down, bottom-up so that
	// nothing is renamed onto a file that has not moved yet.
	if err := removeIfPresent(s.generation(s.keep)); err != nil {
		return err
	}
	for i := s.keep - 1; i >= 1; i-- {
		if err := renameIfPresent(s.generation(i), s.generation(i+1)); err != nil {
			return err
		}
	}
	if err := renameIfPresent(s.path, s.generation(1)); err != nil {
		return err
	}
	return s.openLocked()
}

// replay writes the ring's current lines into a freshly opened file, so a file
// armed halfway through the boot still starts at the boot, see rule 3.
//
// after is the highest sequence number a previous arm wrote, and the return
// value is the new watermark. Without it, switching the file off and on again
// in one process would write the same lines twice.
//
// It writes through the ordinary write path, so an oversized ring rotates
// exactly as live lines do, and a replay that cannot be written switches the
// sink off with the same Problem any other failure would.
func (s *fileSink) replay(entries []Entry, after uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return after
	}
	pending := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Seq > after {
			pending = append(pending, e)
		}
	}
	if len(pending) == 0 {
		return after
	}
	// One marker, so nobody reading the file takes the boot for something
	// that happened when the switch was flipped. English and unlocalised,
	// like every other line here.
	s.writeLocked([]byte(fmt.Sprintf("--- %d line(s) logged before this file was opened ---\n", len(pending))))
	for _, e := range pending {
		s.writeLocked([]byte(e.Line + "\n"))
	}
	return pending[len(pending)-1].Seq
}

func (s *fileSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// status is FileState for this sink, generations and free space included.
func (s *fileSink) status() FileState {
	s.mu.Lock()
	st := FileState{
		Enabled:     s.f != nil,
		Path:        s.path,
		Bytes:       s.size,
		MaxBytes:    s.maxBytes,
		Keep:        s.keep,
		Generations: []Generation{},
		Problem:     s.problem,
	}
	paths := []string{s.path}
	for i := 1; i <= s.keep; i++ {
		paths = append(paths, s.generation(i))
	}
	dir := s.dir
	s.mu.Unlock()

	for i, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		st.Generations = append(st.Generations, Generation{
			Index:      i,
			Bytes:      info.Size(),
			ModifiedAt: info.ModTime().UTC(),
		})
	}
	if st.Problem != "" {
		// Measured here and never on the write path: this is an HTTP goroutine
		// holding no lock of the ring, so a syscall that blocks costs one
		// answer rather than every log call in the process.
		if free, ok := diskspace.Free(dir); ok {
			st.FreeKnown = true
			if free <= 1<<62 {
				st.FreeBytes = int64(free)
			}
		}
	}
	return st
}

// generation is the name of the i-th renamed file. 1 is the newest.
func (s *fileSink) generation(i int) string {
	return s.path + "." + strconv.Itoa(i)
}

// GenerationPath is the file behind an index the page asked to download: 0 is
// the file being written, 1 the newest renamed one. It answers false for an
// index this sink does not keep, so a number arriving from a URL cannot become
// part of a path and no caller joins one itself.
func (r *Ring) GenerationPath(index int) (string, bool) {
	r.mu.Lock()
	s := r.sink
	r.mu.Unlock()
	if s == nil || index < 0 || index > s.keep {
		return "", false
	}
	if index == 0 {
		return s.path, true
	}
	return s.generation(index), true
}

// GenerationPath is the process's own - see (*Ring).GenerationPath.
func GenerationPath(index int) (string, bool) { return std.GenerationPath(index) }

// removeIfPresent and renameIfPresent treat "it was not there" as done rather
// than as a failure. A generation that does not exist yet is the normal state
// for every rotation before the keep-th one, and on Windows an antivirus
// scanner holding a handle open is a real, transient reason for a rename to be
// refused - which is a genuine failure and is reported as one.
func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func renameIfPresent(from, to string) error {
	if err := os.Rename(from, to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
