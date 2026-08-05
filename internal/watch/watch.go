// Package watch picks up links dropped into a folder. Dropping a file into a
// share is the intake method that costs a self-hoster nothing: no browser
// extension, no API client, no port to open. It is also the format
// JDownloader's folder watch uses (*.crawljob), so the tooling people already
// have keeps working when it is pointed at KnightLoader instead.
package watch

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// defaultInterval is the poll period when Options.Interval is zero. A drop
// folder is a human-speed input, and the target is usually a network share
// where a listing is not free, so a few seconds of latency buys a lot of quiet.
const defaultInterval = 5 * time.Second

// maxIntakeSize caps what we are willing to read into memory. An intake file is
// a link list; anything larger is somebody's ISO that happened to be named
// .txt, and reading it would be a self-inflicted denial of service.
const maxIntakeSize = 8 << 20

// Job is one intake file's contents.
type Job struct {
	URLs      []string
	Package   string
	Dir       string // destination override, may be empty
	Password  string // archive password, may be empty
	AutoStart bool   // whether to start immediately rather than stage
}

// schemeURL matches anything carrying a scheme. The intake stays deliberately
// permissive: the resolvers know what they can take, this package does not, and
// silently dropping a link the user explicitly handed us is the worse failure.
var schemeURL = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://\S+$`)

// Parse reads one intake file. Two formats are accepted:
//
//	*.crawljob - JDownloader's key=value format (text=, packageName=,
//	             downloadFolder=, autoStart=, extractPasswords=)
//	*.txt      - one URL per line
//
// The package name defaults to the file's base name, so dropping "Season 3.txt"
// produces a package called "Season 3" without the user configuring anything.
func Parse(name string, r io.Reader) (Job, error) {
	base := filepath.Base(name)
	var (
		job Job
		err error
	)
	switch strings.ToLower(filepath.Ext(base)) {
	case ".crawljob":
		job, err = parseCrawljob(r)
	case ".txt":
		job, err = parseText(r)
	default:
		return Job{}, fmt.Errorf("watch: %s: not an intake file (want .crawljob or .txt)", base)
	}
	if err != nil {
		return Job{}, fmt.Errorf("watch: %s: %w", base, err)
	}
	if len(job.URLs) == 0 {
		return Job{}, fmt.Errorf("watch: %s: no links found", base)
	}
	if job.Package == "" {
		job.Package = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return job, nil
}

// parseCrawljob reads JD's key=value format. JD allows several entries in one
// file separated by blank lines; we fold them into a single job, because the
// only thing downstream cares about is the link list and the last entry's
// settings are as good a choice as any for a hand-dropped file.
func parseCrawljob(r io.Reader) (Job, error) {
	var job Job
	err := eachLine(r, func(line string) {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "text":
			job.URLs = append(job.URLs, splitLinks(value)...)
		case "packagename":
			job.Package = value
		case "downloadfolder":
			job.Dir = value
		case "autostart":
			job.AutoStart = truthy(value)
		case "extractpasswords":
			job.Password = firstPassword(value)
		}
		// Unknown keys (chunks, priority, deepAnalyseEnabled, ...) are ignored
		// rather than rejected: JD writes plenty of them and a file we cannot
		// fully understand is still a file whose links we can take.
	})
	return job, err
}

// parseText reads a plain list, one URL per line.
func parseText(r io.Reader) (Job, error) {
	var job Job
	err := eachLine(r, func(line string) {
		// Split on whitespace rather than taking the whole line: a list pasted
		// onto one line is the normal shape of a copied link block, and the
		// crawljob parser already treats its value that way. Two parsers in one
		// package disagreeing about what a list looks like is how a drop file
		// ends up permanently "unusable" with nothing to explain it.
		job.URLs = append(job.URLs, splitLinks(line)...)
	})
	return job, err
}

// eachLine feeds non-empty, non-comment lines to fn.
// bom is what Windows editors put at the front of a UTF-8 file. Left in place
// it becomes part of the first line, so the first link silently fails to parse
// while the rest succeed — and the file is then retired as consumed, so nothing
// ever reports the loss.
const bom = "\ufeff"

func eachLine(r io.Reader, fn func(string)) error {
	first := true
	sc := bufio.NewScanner(io.LimitReader(r, maxIntakeSize))
	// A single crawljob text= value can hold hundreds of links on one line,
	// which blows straight past Scanner's 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), maxIntakeSize)
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, bom)
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fn(line)
	}
	return sc.Err()
}

// splitLinks pulls the URLs out of one crawljob text= value. JD writes multiple
// links into that single line as the literal two-character sequence \n, so the
// escape has to be undone before splitting on real whitespace.
func splitLinks(s string) []string {
	s = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\r`, "\n").Replace(s)
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t'
	}) {
		if looksLikeURL(f) {
			out = append(out, f)
		}
	}
	return out
}

func looksLikeURL(s string) bool {
	// Magnet links carry no "//" after the colon, so they need their own case.
	return schemeURL.MatchString(s) || strings.HasPrefix(strings.ToLower(s), "magnet:?")
}

// truthy reads JD's tri-state booleans, which are TRUE / FALSE / UNSET.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// firstPassword takes one password out of extractPasswords, which JD writes
// either bare or as a JSON-ish list. We keep the first: the download model
// carries a single password.
func firstPassword(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") {
		v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
		if i := strings.Index(v, ","); i >= 0 {
			v = v[:i]
		}
	}
	return strings.Trim(strings.TrimSpace(v), `"'`)
}

// Options configures a Watcher.
type Options struct {
	Dir      string
	Interval time.Duration // zero means a sane default
	// OnJob receives each parsed job. It runs on the polling goroutine, so it
	// must not block for long or the next poll is delayed behind it.
	OnJob func(Job)
	// Delete removes a consumed file instead of the default, which is to rename
	// it with a ".done" suffix so the same links are never added twice.
	Delete bool
}

// fileState is what a poll remembers about a candidate file. Only size and mod
// are compared for stability; bad is carried alongside so a file we already
// failed to parse is not parsed again until its bytes actually change.
type fileState struct {
	size int64
	mod  time.Time
	bad  bool
}

func (s fileState) sameBytes(o fileState) bool {
	return s.size == o.size && s.mod.Equal(o.mod)
}

// Watcher polls a directory and hands each new file to a sink.
type Watcher struct {
	dir      string
	interval time.Duration
	onJob    func(Job)
	del      bool

	// pending is touched only by the polling goroutine, so it needs no lock.
	pending map[string]fileState

	startOnce sync.Once
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}

	mu      sync.Mutex
	started bool
}

// New builds a Watcher for o.Dir, creating the directory if it does not exist
// yet so a fresh install has somewhere to drop files.
// writable reports whether the process can retire a consumed file. It is
// checked once at startup because the failure is a permission problem that will
// never resolve on its own, and the alternative is a folder that silently
// accepts nothing forever.
func writable(dir string) error {
	probe := filepath.Join(dir, ".knightloader-watch-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(probe)
}

func New(o Options) (*Watcher, error) {
	if strings.TrimSpace(o.Dir) == "" {
		return nil, errors.New("watch: no directory configured")
	}
	if o.OnJob == nil {
		// A watcher without a sink would consume files and drop the links on
		// the floor, which looks exactly like data loss from the outside.
		return nil, errors.New("watch: OnJob is required")
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("watch: %s: %w", o.Dir, err)
	}
	// A folder the process cannot write is useless: consuming a file means
	// retiring it, and a file that cannot be retired is never taken at all.
	// Saying so at startup beats a watcher that appears to run and does nothing.
	if err := writable(o.Dir); err != nil {
		return nil, fmt.Errorf("watch: %s is not writable: %w", o.Dir, err)
	}
	interval := o.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Watcher{
		dir:      o.Dir,
		interval: interval,
		onJob:    o.OnJob,
		del:      o.Delete,
		pending:  make(map[string]fileState),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}, nil
}

// Start begins polling in the background. Calling it twice is a no-op.
func (w *Watcher) Start() {
	w.startOnce.Do(func() {
		w.mu.Lock()
		w.started = true
		w.mu.Unlock()
		go w.loop()
	})
}

// Close stops the polling loop and waits for the running poll to finish, so no
// OnJob call is still in flight once it returns.
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() { close(w.stop) })
	w.mu.Lock()
	started := w.started
	w.mu.Unlock()
	if started {
		<-w.done
	}
	return nil
}

func (w *Watcher) loop() {
	defer close(w.done)
	// Poll straight away so files already sitting in the folder at start-up are
	// not held back by a full interval.
	select {
	case <-w.stop:
		return
	default:
		w.poll()
	}
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.poll()
		}
	}
}

// poll lists the folder once and consumes every file that has settled.
//
// This is deliberately polling rather than fsnotify. The drop folder normally
// lives on a network share on an Unraid box, and inotify only reports changes
// made by the local kernel: a file written over SMB or NFS from another host
// never fires an event. Polling is the only thing that sees those writes.
func (w *Watcher) poll() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		// The share can be briefly unreachable; the next tick tries again.
		return
	}
	seen := make(map[string]fileState, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isIntakeName(name) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		cur := fileState{size: fi.Size(), mod: fi.ModTime()}
		prev, known := w.pending[name]

		// The bug this guards against: a file copied onto the share appears in
		// the listing the moment it is created, long before the last byte
		// lands, and consuming it then yields half a link list that is then
		// renamed away and lost. So a file is only taken once its size and
		// modification time are identical to what the previous poll saw.
		if !known || !prev.sameBytes(cur) {
			seen[name] = cur
			continue
		}
		if prev.bad || cur.size > maxIntakeSize {
			// Already known to be unusable at these exact bytes. Keep the
			// verdict so we do not re-read it on every single poll, and leave
			// the file alone so the user can see and fix it.
			cur.bad = true
			seen[name] = cur
			continue
		}
		if err := w.consume(filepath.Join(w.dir, name)); err != nil {
			// Said once per file, not once per poll: a folder the process
			// cannot write is a permanent misconfiguration, and a drop file
			// that is quietly ignored forever is indistinguishable from a
			// watcher that is not running at all.
			log.Printf("intake file %s was not taken: %v", name, err)
			cur.bad = true
			seen[name] = cur
			continue
		}
		// Consumed: the file is gone under this name, so it drops out of the
		// map entirely. A new file with the same name starts from scratch.
	}
	// Rebuilding the map from the listing prunes files that have disappeared,
	// which keeps a long-running watcher from accumulating dead entries.
	w.pending = seen
}

// isIntakeName reports whether a directory entry is a file we should read.
func isIntakeName(name string) bool {
	// Leading dots are how rsync, SMB clients and macOS name their in-progress
	// copies, so those are never candidates no matter what they end in.
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".crawljob", ".txt":
		return true
	}
	// Anything else, ".done" included, is not ours.
	return false
}

// consume parses one file and retires it. The file is retired before the job is
// handed over on purpose: if the sink panics or the process dies during the
// handoff we would rather lose a single job than re-add the same links on every
// poll from here to eternity.
func (w *Watcher) consume(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	job, err := Parse(filepath.Base(path), f)
	f.Close()
	if err != nil {
		return err
	}
	if w.del {
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if err := os.Rename(path, path+".done"); err != nil {
		return err
	}
	w.onJob(job)
	return nil
}
