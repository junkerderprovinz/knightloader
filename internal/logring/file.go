package logring

// The optional file sink: the same lines the ring keeps in memory, appended to
// a capped file on disk that is renamed and started again when it fills up.
//
// WHY THIS EXISTS AT ALL, given that the ring already answers the diagnostics
// bundle: the ring holds five hundred lines and dies with the process. The
// question people actually arrive with is "it fell over the night before last,
// what did it say", and a buffer that a restart empties cannot answer it. In a
// container the same lines also reach docker logs, which is where most people
// look first; the file is what is still there once that has rolled over, and it
// is the only answer at all on the desktop build.
//
// THREE RULES THIS FILE IS BUILT AROUND, each of which has one way to get it
// badly wrong:
//
//  1. NOTHING IN HERE MAY LOG. This sink sits downstream of
//     log.SetOutput(io.MultiWriter(os.Stderr, std)), and it is called from
//     inside (*Ring).Write with the ring's mutex already held. A log.Printf
//     from a failure path here would re-enter that same Write on the same
//     goroutine and deadlock on a mutex it is already holding - and if it did
//     not, it would fail again, log again, and die on a full stack. So a write
//     that cannot be made switches the sink OFF and records a sentence in
//     Problem, which FileStatus hands to the page. It is never reported
//     through log, and nothing in the write path calls into a package that
//     might: measuring free disk space, which is the one thing here that
//     reaches for another package, happens in FileStatus on an HTTP goroutine
//     with no lock of the ring held, and never on the write path.
//
//  2. CLOSE BEFORE RENAME. Go opens files without FILE_SHARE_DELETE, so on
//     Windows os.Rename against a handle this process still holds is refused
//     with access denied - internal/backup/backup.go already writes this exact
//     platform fact down for a different file. The desktop build is mostly
//     Windows, so a rotation tested only on Linux passes and then breaks the
//     desktop log on the very first roll.
//
//  3. REPLAY ON ARM. The sink cannot be attached at init time, where the ring
//     itself is: the data directory is not known until main has read its
//     environment and whether a file is wanted is not known until
//     settings.Load has run inside app.New. Everything that explains a bad boot
//     - the staged restore, the data dir, the JD provisioning, a store that
//     would not open - is therefore already logged by the time OpenFile is
//     called. So OpenFile writes the ring's current contents into the file
//     before the first live line, and the file starts where the process did
//     rather than in the middle of it.

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
// directory. Fixed, so that a typo in a path can never be the reason a log is
// not being written - see internal/settings.LogFile for the whole of that
// argument and for KL_LOG_DIR, which is the escape hatch it leaves open.
const Name = "knightloader.log"

// FileOptions is what OpenFile needs: where, how big, and how many.
type FileOptions struct {
	// Dir is the directory the file lives in. Created if it is not there.
	Dir string
	// MaxBytes is the size at which the file is renamed and a new one started.
	MaxBytes int64
	// Keep is how many renamed files stay beside it. Zero keeps only the file
	// being written, which is a real answer - see settings.LogFile.Keep.
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
	// Enabled is whether lines are reaching a file RIGHT NOW. It is not the
	// settings switch: a sink that was armed and then failed reports false
	// here with a sentence in Problem, which is the distinction the card is
	// built on.
	Enabled bool `json:"enabled"`

	// Path is the file being written. Empty when nothing is armed.
	//
	// It leaves this package freely and is deliberately NOT in the diagnostics
	// bundle - see Redacted below.
	Path string `json:"path"`

	Bytes    int64 `json:"bytes"`
	MaxBytes int64 `json:"maxBytes"`
	Keep     int   `json:"keep"`

	// Generations is the files on disk, newest first, index 0 leading.
	//
	// NEVER NIL. A nil slice encodes as JSON null, and a fresh install that has
	// never armed the sink would then answer "generations": null and throw on
	// the page's own .map - the same class of bug routes_features.go already
	// documents for archivePasswords.
	Generations []Generation `json:"generations"`

	// Problem is empty while the file is being written. When it is not, it says
	// what failed, in the words the operating system used, and the card turns
	// it into advice with the path and the free space beside it.
	Problem string `json:"problem,omitempty"`

	// FreeBytes is how much room is left on that volume, and FreeKnown is
	// whether this build could find out at all. Two answers and not one number
	// with a sad value: internal/diskspace's own doc comment requires a caller
	// to treat "cannot measure" as no opinion rather than as nought bytes free,
	// and a readout that drew an empty disk on a kernel nobody compiled a
	// branch for would send somebody hunting for a problem they do not have.
	//
	// Only measured while Problem is set, because that is the only moment it
	// answers anything, and because measuring it costs a syscall that must
	// never happen on the write path (see rule 1 at the top of this file).
	FreeBytes int64 `json:"freeBytes,omitempty"`
	FreeKnown bool  `json:"freeKnown"`
}

// Redacted is this state as the DIAGNOSTICS BUNDLE may carry it: everything
// except the path.
//
// The bundle is a file people attach to public bug reports, and a desktop data
// directory is C:\Users\<a person's real name>\AppData\... - exactly the
// argument internal/api/routes_diagnostics.go already makes for the store and
// settings paths, and exactly what TestDiagnosticsShipsNoPaths pins. Whether a
// file exists, how big it has grown and whether it is failing are the facts
// somebody reading a report wants; where it is is the reader's own business and
// is on the session-guarded route this package's own card reads.
func (s FileState) Redacted() FileState {
	s.Path = ""
	if s.Generations == nil {
		s.Generations = []Generation{}
	}
	return s
}

// fileSink is the writer behind FileState. Its own mutex guards everything in
// it, and it is always taken UNDER the ring's when both are held, because
// (*Ring).Write is the only path that holds both and it takes them in that
// order.
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
		// A cap of zero would rotate on every single line. The floor is a
		// megabyte rather than an error because this is reached from a settings
		// document, and settings.LogFile.Sanitized has already clamped anything
		// a person could type - this only catches a caller inside the tree.
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
	// Attached even when opening failed, and that is the point: a sink holding
	// a Problem is how the card gets to say WHY nothing is being written. A nil
	// sink would be indistinguishable from the switch being off.
	r.sink = s
	if err == nil {
		// Under the ring's lock, so that no live line can slip in between the
		// snapshot and the sink being attached - the file would then have that
		// line before the older ones it is about to be handed.
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
// The size is tracked in memory from here on rather than stat-ed per line: a
// syscall per log record on a spinning array volume is the difference between a
// feature that is off by default and one nobody could leave on.
//
// 0o700 on the directory and 0o600 on the file, because a log line can carry a
// feed URL with an indexer's API key in its query string - internal/feed's
// poller logs subscription addresses verbatim, and so does internal/crawler for
// the pages it walks. That is a leak worth narrowing here even though the
// diagnostics bundle carries the same lines unredacted today; the bundle's
// missing line redaction is its own bug and not one to inherit quietly.
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
	// s.size > 0 guards the one case where rotating would loop: a single record
	// longer than the whole cap. Rotating an empty file for it would rename a
	// nothing, write the record anyway, and do it again for the next line -
	// throwing away every generation on the disk in the space of a second. An
	// oversized record is written whole into the file it lands in, and the cap
	// is honoured on the line after it.
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

// failLocked switches the sink off and records why. It does not log, it does
// not retry, and it does not measure anything - see rule 1.
//
// OFF RATHER THAN DEGRADED, because the alternative is a sink that fails on
// every line for as long as the volume stays full, each failure costing a
// syscall in front of every log call in the process. The lines themselves are
// not lost: they are still in the ring, still on stderr, and still in the
// diagnostics bundle, which is what the card says out loud so that "the log
// file stopped" is never read as "logging stopped".
func (s *fileSink) failLocked(problem string) {
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	s.problem = problem
}

// rotateLocked renames the current file down the generations and starts a fresh
// one. The handle is closed FIRST - see rule 2 at the top of this file.
func (s *fileSink) rotateLocked() error {
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			s.f = nil
			return err
		}
		s.f = nil
	}
	if s.keep <= 0 {
		// "Keep only the file being written" is a real setting, and this is
		// what it means: the full file goes and a new one starts. Not
		// truncating in place, because the file may be open in somebody's
		// editor and a truncate would leave them reading a hole.
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

// replay writes the ring's current lines into a freshly opened file, so that a
// file armed halfway through the boot still starts at the boot - see rule 3.
//
// after is the highest sequence number a previous arm already wrote, and the
// return value is the new watermark. Without it, switching the file off and on
// again in one process would write the same lines a second time and the
// operator would read the same morning twice.
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
	// One marker, so that nobody reading the file mistakes the boot for
	// something that happened at the moment the switch was flipped. English and
	// unlocalised, like every other line in this file - a log is not the
	// interface.
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
// index this sink does not keep, so that a number arriving from a URL can never
// become part of a path - the range check happens here, once, and the caller
// never joins anything itself.
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
