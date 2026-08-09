package extract

// The worker layer. The half of this package above it answers "what is inside
// this file"; this half answers "what is this job doing, how far has it got,
// and what is left on disk when somebody stops it".
//
// It reaches into the reader layer through exactly one line: writeFile copies
// through copyWatched rather than io.Copy. Every byte every format writes goes
// through that one call, so a tap there is both the progress counter and the
// only place a cancelled job can stop part-way through a forty-gigabyte volume.
// Threading a context down through nine reader signatures buys the same thing
// and edits every format in the package to get it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultDepth is how many archives deep a job follows when the caller does not
// say. Deep extraction is the norm in the wild - a release is a multi-volume rar
// holding a zip holding the files - but an archive that unpacks to a copy of
// itself is a disk filled in silence, so there is always a floor.
const DefaultDepth = 4

// copyChunk is how much of a file is written between two cancellation checks.
// Small enough that an abort is felt at once on any disk worth extracting to,
// large enough that the check is not what the copy is spending its time on.
const copyChunk = 256 << 10

// reportEvery bounds how often a job says where it has got to. A progress
// callback fires on the extracting goroutine and every listener behind it takes
// a lock, so an unthrottled tap would spend a fast extraction's time inside the
// broadcast rather than on the archive.
const reportEvery = 250 * time.Millisecond

// recordedFiles caps how many written paths one job remembers. The record is
// what lets an abort take back exactly this job's work; past the cap the
// clean-up falls back to the directories the job created, which is what holds a
// large extraction anyway. The one case the list is indispensable for - a single
// compressed stream whose payload lands BESIDE the archive, in a folder full of
// other people's files - is one file long.
const recordedFiles = 4096

// slot is the one extraction this package runs at a time.
//
// The byte tap is a single binding, so two jobs at once would each be told the
// other's progress and either could cancel the other's copy. A semaphore rather
// than a mutex because waiting for it has to be interruptible: a job called off
// while it is still queued behind another must come back as cancelled, not sit
// on a lock until the archive in front of it has finished.
//
// It is not the user-visible queue - that belongs to the caller, which is what
// puts a waiting extraction in the list with a name and a stop button. This is
// only the guarantee the tap needs, and it holds however many callers there are.
// Two archives unpacking against the same disk make each other slower anyway.
var slot = make(chan struct{}, 1)

// Progress is a snapshot of a job in flight, in the terms the list shows it in.
type Progress struct {
	// Archive is the base name of the file currently open, which for a deep
	// extraction is not the one the job was started on.
	Archive string `json:"archive"`
	// Depth is how far inside the outermost archive that file was found; 0 is
	// the archive the job started with.
	Depth int `json:"depth"`
	// Files and Bytes count everything this job has written, at every depth.
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Request is one unpacking as the worker takes it on.
type Request struct {
	// Path is the archive to open: the first volume of a set, or the first part
	// of a plain split file.
	Path string
	// Options is where it writes, what it does about a destination that is
	// already there, and which passwords it tries.
	//
	// Dest and Subfolder apply to the archive the job was started on and to
	// nothing found inside it. A collected extraction folder is a place to put
	// downloads, not a place to flatten a release into: three nested archives
	// all named "Setup.zip" would unpack over each other there, while beside
	// their own parents they stay three separate folders.
	Options Options
	// Depth is how many archives deep to follow. Zero means DefaultDepth; one
	// means "unpack what I named and nothing found inside it".
	Depth int
	// OnProgress is called on the goroutine running the job, no more often than
	// reportEvery. It must not block: everything waiting on the extraction is
	// waiting behind it.
	OnProgress func(Progress)
}

// Outcome is what a finished job produced.
type Outcome struct {
	// Dir is where the archive the job was started on unpacked to.
	Dir string
	// Dirs is every directory the job wrote into, the nested ones included.
	Dirs []string
	// Files and Bytes are the whole job's output.
	Files int
	Bytes int64
	// Volumes is every archive file consumed at every depth, and the parts of
	// every split file joined. It is what "delete the archive afterwards" acts
	// on, so a nested archive left sitting inside the output is not something
	// the caller has to go looking for.
	Volumes []string
	// Joined names the files put back together from a split set.
	Joined []string
	// Nested counts the archives found inside the output and unpacked in turn.
	Nested int
}

// bound is the job the byte tap reports to, or nil when nothing is running.
var bound atomic.Pointer[sink]

// bufPool holds the copy buffers. One per file would be one allocation per
// entry, and an archive of small files is mostly entries.
var bufPool = sync.Pool{New: func() any { b := make([]byte, copyChunk); return &b }}

// copyWatched is the seam. With no job bound it is io.Copy and costs a nil
// check; with one it is the same copy, counted and interruptible.
func copyWatched(f *os.File, r io.Reader) (int64, error) {
	if s := bound.Load(); s != nil {
		return s.copy(f, r)
	}
	return io.Copy(f, r)
}

// Run unpacks the archive at req.Path, follows the archives inside what it
// unpacked, and reports what it did.
//
// A job that does not finish - a failure, or a caller cancelling the context -
// takes its own work back off the disk before it returns. That is the whole
// reason this exists as a job rather than a call: a half-written extraction
// folder is indistinguishable from a finished one. Nothing on disk says which,
// the person looking at it calls the download good, and the next deep pass walks
// into it.
func Run(ctx context.Context, req Request) (*Outcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-slot }()

	s := &sink{ctx: ctx, report: req.OnProgress}
	bound.Store(s)
	defer bound.Store(nil)

	out, err := s.run(ctx, req)
	if err != nil {
		s.undo()
		return nil, err
	}
	s.finish(out)
	s.emit(true)
	return out, nil
}

// Startable reports whether a job can be started on this file: an archive the
// readers can open, or the first part of a plain split file, which becomes one
// once it has been put back together.
func Startable(name string) bool { return Supported(name) || SplitStart(name) }

// step is one archive on the job's worklist, and how deep inside the outermost
// one it was found.
type step struct {
	path  string
	depth int
}

// sink is one job's view of the bytes going past: the counters the list shows,
// the context that stops them, and the record of what was written - which is
// what lets an abort take back this job's work and leave alone what was in the
// folder before it started.
type sink struct {
	ctx    context.Context
	report func(Progress)

	mu        sync.Mutex
	archive   string
	depth     int
	files     int
	bytes     int64
	written   []string
	truncated bool
	owned     []string
	lastAt    time.Time
}

func (s *sink) run(ctx context.Context, req Request) (*Outcome, error) {
	depth := req.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}
	out := &Outcome{}
	// Keyed by cleaned absolute path: an archive that unpacks to a copy of
	// itself would otherwise be a job that never ends, and the depth cap alone
	// would only make it end late.
	seen := map[string]bool{}
	queue := []step{{path: req.Path}}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cur := queue[0]
		queue = queue[1:]
		key := cleanKey(cur.path)
		if seen[key] {
			continue
		}
		seen[key] = true

		if SplitStart(filepath.Base(cur.path)) {
			joined, parts, err := s.join(cur.path)
			if err != nil {
				return nil, err
			}
			out.Joined = append(out.Joined, joined)
			out.Volumes = append(out.Volumes, parts...)
			// Joining is not a level: a set cut into pieces is one file, and
			// charging it a level would cost the archive inside it its own.
			if Supported(filepath.Base(joined)) {
				queue = append(queue, step{path: joined, depth: cur.depth})
			}
			continue
		}

		if cur.depth > 0 {
			out.Nested++
		}
		o := req.Options
		if cur.depth > 0 {
			o.Dest, o.Subfolder = "", false
		}
		// The two halves of Options.Extract, taken apart for one reason: the
		// destination has to be known BEFORE anything is written. MkdirAll cannot
		// say afterwards whether it made the folder or found it, and an abort
		// that removes a folder the user already had is a worse failure than the
		// half-written one this is here to prevent.
		dest, err := o.destination(cur.path)
		if err != nil {
			// A destination already there under the skip policy is a decision the
			// user made, not a broken archive. At depth it is not even about the
			// file they pointed at, so the job carries on with the rest.
			if cur.depth > 0 && errors.Is(err, ErrDestinationTaken) {
				continue
			}
			return nil, err
		}
		fresh := missing(dest)
		s.open(cur.path, cur.depth)
		mark := s.recorded()

		res, err := extractInto(cur.path, dest, o.Passwords)
		if err != nil {
			if cur.depth == 0 {
				return nil, err
			}
			// Named at depth, because the job is no longer about the file the
			// user pointed at: "no password fits" on its own sends them back to
			// the outer archive, which opened perfectly well.
			return nil, fmt.Errorf("%s: %w", filepath.Base(cur.path), err)
		}
		if fresh {
			s.own(dest)
		}
		out.Volumes = append(out.Volumes, res.Volumes...)
		if out.Dir == "" {
			out.Dir = res.Dir
		}
		out.Dirs = appendUnique(out.Dirs, res.Dir)

		if cur.depth+1 >= depth {
			continue
		}
		for _, p := range s.produced(res, cur.path, mark) {
			queue = append(queue, step{path: p, depth: cur.depth + 1})
		}
	}
	return out, nil
}

// produced is what an archive left behind that is worth opening in turn.
//
// A container archive gets a folder of its own and everything under it is fair
// game. A single compressed stream does not: gunzip leaves its payload BESIDE
// the archive, so res.Dir is the download folder, and walking that would pull
// every unrelated archive sitting next to it into this job - a job the user
// started on one file that quietly unpacks the whole folder. For that case the
// only new file is the one this job just wrote, which is what the written record
// is for.
func (s *sink) produced(res *Result, archive string, mark int) []string {
	if filepath.Clean(res.Dir) != filepath.Clean(filepath.Dir(archive)) {
		return candidatesIn(res.Dir)
	}
	var out []string
	for _, p := range s.recordedFrom(mark) {
		if Startable(filepath.Base(p)) {
			out = append(out, p)
		}
	}
	return out
}

// candidatesIn is every archive under dir that can start a job of its own. A
// corner of the tree that cannot be read is skipped rather than failing the
// job: it did not come out of this archive, so it is not this job's business.
func candidatesIn(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if Startable(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// join concatenates a plain split set back into the file it was cut from, and
// reports the file it made and the parts it consumed.
func (s *sink) join(first string) (string, []string, error) {
	stem, digits, ok := splitFields(filepath.Base(first))
	if !ok {
		return "", nil, fmt.Errorf("extract: %q is not the first part of a split file", filepath.Base(first))
	}
	dir := filepath.Dir(first)
	n, _ := strconv.Atoi(digits)
	// Contiguous from the part we were handed, and no further: a set missing its
	// middle is not a set with a shorter file at the end, it is a download that
	// has not finished, and writing out the first half of a film as if it were
	// the film is the failure worth refusing.
	var parts []string
	for i := n; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s.%0*d", stem, len(digits), i))
		if missing(p) {
			break
		}
		parts = append(parts, p)
	}
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("extract: %s is one part of a split file and the rest are not here", filepath.Base(first))
	}

	target := filepath.Join(dir, stem)
	// O_EXCL, so a file already sitting under the joined name is a refusal and
	// never a silent overwrite. It is either the last run's output or somebody
	// else's file, and neither is ours to replace.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", nil, fmt.Errorf("extract: %s cannot be joined: %w", stem, err)
	}
	s.note(target)
	for _, p := range parts {
		src, err := os.Open(p)
		if err != nil {
			f.Close()
			return "", nil, err
		}
		_, err = s.pipe(f, src)
		src.Close()
		if err != nil {
			f.Close()
			return "", nil, err
		}
	}
	if err := f.Close(); err != nil {
		return "", nil, err
	}
	return target, parts, nil
}

// open records which archive the job is on now and says so at once, rather than
// waiting for the throttle: moving to the next volume is the one progress event
// that carries information even when no bytes have moved yet.
func (s *sink) open(archive string, depth int) {
	s.mu.Lock()
	s.archive, s.depth = filepath.Base(archive), depth
	s.mu.Unlock()
	s.emit(true)
}

// note records a file this job created and counts it.
func (s *sink) note(path string) {
	s.mu.Lock()
	s.files++
	if len(s.written) < recordedFiles {
		s.written = append(s.written, path)
	} else {
		s.truncated = true
	}
	s.mu.Unlock()
}

func (s *sink) own(dir string) {
	s.mu.Lock()
	s.owned = append(s.owned, dir)
	s.mu.Unlock()
}

func (s *sink) recorded() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.truncated {
		return -1
	}
	return len(s.written)
}

// recordedFrom is the files written since mark, and nothing when the record has
// already overflowed - a job of that size is unpacking into a folder of its own,
// where the tree walk answers the same question properly.
func (s *sink) recordedFrom(mark int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mark < 0 || s.truncated || mark > len(s.written) {
		return nil
	}
	return append([]string(nil), s.written[mark:]...)
}

func (s *sink) pipe(f *os.File, r io.Reader) (int64, error) {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	var total int64
	for {
		if err := s.ctx.Err(); err != nil {
			return total, err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			w, werr := f.Write(buf[:n])
			total += int64(w)
			s.add(int64(w))
			s.emit(false)
			if werr != nil {
				return total, werr
			}
			if w != n {
				return total, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

func (s *sink) copy(f *os.File, r io.Reader) (int64, error) {
	s.note(f.Name())
	return s.pipe(f, r)
}

func (s *sink) add(n int64) {
	s.mu.Lock()
	s.bytes += n
	s.mu.Unlock()
}

func (s *sink) finish(out *Outcome) {
	s.mu.Lock()
	out.Files, out.Bytes = s.files, s.bytes
	s.mu.Unlock()
}

func (s *sink) emit(force bool) {
	if s.report == nil {
		return
	}
	s.mu.Lock()
	now := time.Now()
	if !force && now.Sub(s.lastAt) < reportEvery {
		s.mu.Unlock()
		return
	}
	s.lastAt = now
	p := Progress{Archive: s.archive, Depth: s.depth, Files: s.files, Bytes: s.bytes}
	s.mu.Unlock()
	// Off the lock: the listener takes locks of its own, and holding this one
	// across a broadcast would put the whole extraction behind the slowest UI.
	s.report(p)
}

// undo removes what this job wrote, and only what this job wrote: the folders
// it created, and the individual files it put into folders that were already
// there. A folder that existed before the job started is never removed whole -
// the payload of a single compressed stream lands next to the archive, which for
// a download folder means next to everything else the user owns.
func (s *sink) undo() {
	s.mu.Lock()
	dirs := append([]string(nil), s.owned...)
	files := append([]string(nil), s.written...)
	s.mu.Unlock()
	// Folders first: the files inside them go with them, and the per-file pass
	// then has nothing left to do for the common case.
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
	for _, f := range files {
		_ = os.Remove(f)
	}
}

// splitPart matches one part of a plain split file: a run of digits behind the
// whole name, including its own extension, the way HJSplit and `split -d` leave
// them. Three digits at least, because a two-digit tail is as likely to be a
// version or a chapter as it is a part.
var splitPart = regexp.MustCompile(`^(.+)\.(\d{3,4})$`)

// splitFields cuts a part name into the file it belongs to and its number, with
// the digits kept as written so the rest of the set can be spelled the same way.
//
// A split 7z is deliberately not one of these: "set.7z.001" is read by the
// sevenzip reader, volumes and all, and gluing the parts together first would
// produce a file that reader cannot open.
func splitFields(name string) (stem, digits string, ok bool) {
	m := splitPart.FindStringSubmatch(filepath.Base(name))
	if m == nil {
		return "", "", false
	}
	if strings.HasSuffix(strings.ToLower(m[1]), ".7z") {
		return "", "", false
	}
	return m[1], m[2], true
}

// SplitPart reports the split set a numbered part belongs to, and which part it
// is. It is false for anything that is not a part, a split 7z included.
func SplitPart(name string) (stem string, part int, ok bool) {
	s, digits, ok := splitFields(name)
	if !ok {
		return "", 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}
	return s, n, true
}

// SplitStart reports whether name is the part a join starts from. Both spellings
// count: HJSplit numbers from 001 and `split -d` from 000.
func SplitStart(name string) bool {
	_, n, ok := SplitPart(name)
	return ok && n <= 1
}

var (
	rarPartN    = regexp.MustCompile(`(?i)\.part(\d+)\.rar$`)
	rarOldN     = regexp.MustCompile(`(?i)\.r(\d\d)$`)
	sevenZipN   = regexp.MustCompile(`(?i)\.7z\.(\d{3})$`)
	zipSpannedN = regexp.MustCompile(`(?i)\.z(\d\d)$`)
)

// VolumeRank orders the parts of one archive set the way the readers consume
// them, which is not the order their names sort in.
//
// Two families disagree with each other, and both disagree with a plain sort:
// a spanned rar starts at "film.rar" and continues into "film.r00", while a
// spanned zip ends at "film.zip" and starts at "film.z01". Sorted by name the
// rar set puts its last part first and the zip set puts its last part in the
// middle, so a list numbering the volumes off the sort would tell the user part
// 1 of 5 is the part the archive finishes with.
func VolumeRank(name string) int {
	base := filepath.Base(name)
	if m := rarPartN.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := rarOldN.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n + 2 // ".rar" is the first part, ".r00" the second
	}
	if m := sevenZipN.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := zipSpannedN.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if _, n, ok := SplitPart(base); ok {
		return n
	}
	l := strings.ToLower(base)
	if strings.HasSuffix(l, ".zip") {
		// The end of a spanned zip, and equally a zip that is alone in its set,
		// where being last and being first are the same thing.
		return math.MaxInt
	}
	return 1
}

// cleanKey is a path in the one spelling the worklist compares by.
func cleanKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// missing reports whether nothing is at path. An unreadable path counts as
// present: the job may not claim it created something it cannot even see.
func missing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
