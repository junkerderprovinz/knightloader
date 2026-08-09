package watch

// One watched directory: the listing, the rule that decides a file has finished
// arriving, and the handover. Everything here is about a single folder; which
// folders exist at all is watcher.go's problem.

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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

// poller watches one directory and hands each new file to a sink.
type poller struct {
	dir      string // resolved, see resolveDir: it is also the key the set is deduped on
	interval time.Duration
	onJob    func(Job)
	del      bool

	// pending is touched only by the polling goroutine, so it needs no lock.
	pending map[string]fileState

	// started says whether loop is running, and therefore whether close has a
	// goroutine to wait for. It is written and read under the Watcher's lock and
	// nowhere else, which is why it carries no lock of its own.
	started   bool
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
}

func newPoller(dir string, del bool, interval time.Duration, onJob func(Job)) *poller {
	return &poller{
		dir:      dir,
		interval: interval,
		onJob:    onJob,
		del:      del,
		pending:  make(map[string]fileState),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (p *poller) start() {
	if p.started {
		return
	}
	p.started = true
	go p.loop()
}

// close stops the polling loop and waits for the running poll to finish, so no
// OnJob call is still in flight once it returns. That wait is what lets a folder
// be dropped and taken up again without two goroutines ever looking at one
// directory.
func (p *poller) close() {
	p.closeOnce.Do(func() { close(p.stop) })
	if p.started {
		<-p.done
	}
}

func (p *poller) loop() {
	defer close(p.done)
	// Poll straight away so files already sitting in the folder at start-up are
	// not held back by a full interval.
	select {
	case <-p.stop:
		return
	default:
		p.poll()
	}
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.poll()
		}
	}
}

// poll lists the folder once and consumes every file that has settled.
//
// This is deliberately polling rather than fsnotify. The drop folder normally
// lives on a network share on an Unraid box, and inotify only reports changes
// made by the local kernel: a file written over SMB or NFS from another host
// never fires an event. Polling is the only thing that sees those writes.
func (p *poller) poll() {
	entries, err := os.ReadDir(p.dir)
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
		prev, known := p.pending[name]

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
		if err := p.consume(filepath.Join(p.dir, name)); err != nil {
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
	p.pending = seen
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

// consume parses one file and retires it. The file is retired before the jobs
// are handed over on purpose: if the sink panics or the process dies during the
// handoff we would rather lose a single file than re-add the same links on every
// poll from here to eternity.
func (p *poller) consume(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	jobs, err := Parse(filepath.Base(path), f)
	f.Close()
	if err != nil {
		return err
	}
	if p.del {
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if err := os.Rename(path, path+".done"); err != nil {
		return err
	}
	for _, j := range jobs {
		p.onJob(j)
	}
	return nil
}
