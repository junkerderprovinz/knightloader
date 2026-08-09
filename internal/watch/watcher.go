package watch

// The set of drop folders. A box that mounts three shares wants all three
// watched, and the app used to hold exactly one poller that was thrown away and
// rebuilt whenever the configured folder changed.

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// defaultInterval is the poll period when Options.Interval is zero. A drop
// folder is a human-speed input, and the target is usually a network share
// where a listing is not free, so a few seconds of latency buys a lot of quiet.
const defaultInterval = 5 * time.Second

// Folder is one drop folder as the user configured it. It is the key the live
// set is held under, so it has to stay comparable: an unchanged row is
// recognised by its value, which costs no filesystem call at all.
type Folder struct {
	// Dir is the directory to watch. It is created if it is not there yet, so a
	// fresh install has somewhere to drop files.
	Dir string
	// Delete removes a consumed file instead of the default, which is to rename
	// it with a ".done" suffix so the same links are never added twice.
	Delete bool
}

// Options configures a Watcher.
type Options struct {
	// Folders are the drop folders to watch. Two entries that name one directory
	// collapse into a single poller, see Apply.
	Folders []Folder
	// Dir and Delete are the one-folder spelling of Folders and are folded into
	// it by New. They are what a caller holding a single configured folder
	// passes, which is what the settings still hold.
	Dir    string
	Delete bool

	Interval time.Duration // zero means a sane default
	// OnJob receives each parsed job. It runs on a folder's polling goroutine, so
	// it must not block for long or that folder's next poll is delayed behind it.
	// Folders poll independently, so a slow sink holds up only its own.
	OnJob func(Job)
}

// Watcher polls a set of directories and hands each new file to a sink.
type Watcher struct {
	interval time.Duration
	onJob    func(Job)

	// mu guards live, started and closed. It is held across a poller's close on
	// purpose - see Apply.
	mu      sync.Mutex
	live    map[Folder]*poller
	started bool
	closed  bool
}

// New builds a Watcher over the configured folders and creates any that are not
// there yet. Nothing polls until Start.
//
// It fails only when not one of the folders can be watched. One share being
// unreachable must not turn the whole intake off, so a folder that fails
// alongside a folder that works is logged here rather than returned: the caller
// has a usable watcher, and a caller that read a partial failure as fatal would
// drop it on the floor with its goroutines still inside.
func New(o Options) (*Watcher, error) {
	if o.OnJob == nil {
		// A watcher without a sink would consume files and drop the links on
		// the floor, which looks exactly like data loss from the outside.
		return nil, errors.New("watch: OnJob is required")
	}
	// Built into a fresh slice rather than appended onto o.Folders: appending to
	// a caller's slice writes into their backing array whenever it has the room,
	// and the caller here is holding the configured list.
	folders := make([]Folder, 0, len(o.Folders)+1)
	for _, f := range o.Folders {
		if strings.TrimSpace(f.Dir) != "" {
			folders = append(folders, f)
		}
	}
	if strings.TrimSpace(o.Dir) != "" {
		folders = append(folders, Folder{Dir: o.Dir, Delete: o.Delete})
	}
	if len(folders) == 0 {
		return nil, errors.New("watch: no directory configured")
	}
	interval := o.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	w := &Watcher{
		interval: interval,
		onJob:    o.OnJob,
		live:     make(map[Folder]*poller, len(folders)),
	}
	errs := w.Apply(folders)
	if len(w.Dirs()) == 0 {
		if err := errors.Join(errs...); err != nil {
			return nil, err
		}
		return nil, errors.New("watch: no directory configured")
	}
	for _, err := range errs {
		log.Printf("drop folder is not being watched: %v", err)
	}
	return w, nil
}

// Apply reconciles the live set with folders and returns one error per folder
// that could not be watched; every other folder in the list is running.
//
// A row that has not changed keeps its poller, and with it what that poller
// already knows about a file still being copied into the folder. Rebuilding the
// whole set on every settings save would restart that clock and re-probe every
// share, including the ones the save had nothing to do with.
//
// THE TWO TRAPS, both closed by holding mu across the whole call.
//
// A folder removed and added back in one call must not end up with two pollers:
// the second would take a file the first had already taken, and the same links
// would be staged twice. So the ones that are going are closed - which waits for
// their goroutine - before any new one is opened, and no other Apply can
// interleave between the two halves.
//
// Two rows can also spell one directory. They are collapsed on the resolved
// path rather than on the string the user typed, because "/mnt/user/watch" and
// "/mnt/user/watch/" are one folder and a dropped file in it can only be
// consumed once.
func (w *Watcher) Apply(folders []Folder) []error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		// Silently accepting the list would leave a watcher that reports folders
		// it is not polling, which is the failure this whole package exists to
		// make impossible.
		return []error{errors.New("watch: the watcher is closed")}
	}

	var want []Folder
	for _, f := range folders {
		f.Dir = strings.TrimSpace(f.Dir)
		if f.Dir != "" {
			want = append(want, f)
		}
	}

	next := make(map[Folder]*poller, len(want))
	dirs := make(map[string]bool, len(want))
	// Carried over by the configured value and not by the path it resolves to,
	// so that an unchanged row costs no filesystem call at all: a share that has
	// gone unreachable must not be re-probed by a save that never mentioned it.
	for _, f := range want {
		if p := w.live[f]; p != nil {
			next[f] = p
			dirs[p.dir] = true
		}
	}
	// Before anything new is opened, and still under the same lock.
	for f, p := range w.live {
		if next[f] == nil {
			p.close()
		}
	}

	var errs []error
	for _, f := range want {
		if next[f] != nil {
			continue
		}
		dir, err := resolveDir(f.Dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if dirs[dir] {
			// Another row already resolved to this directory. Collapsed rather
			// than reported: naming one folder twice is something a person can
			// reasonably do, and the answer to it is one poller, not an error
			// they would have no idea what to do with.
			continue
		}
		// A folder the process cannot write is useless: consuming a file means
		// retiring it, and a file that cannot be retired is never taken at all.
		// Saying so now beats a folder that appears to be watched and does nothing.
		if err := writable(dir); err != nil {
			errs = append(errs, fmt.Errorf("watch: %s is not writable: %w", dir, err))
			continue
		}
		p := newPoller(dir, f.Delete, w.interval, w.onJob)
		dirs[dir] = true
		next[f] = p
		if w.started {
			p.start()
		}
	}
	w.live = next
	return errs
}

// Start begins polling every folder in the background, and every folder added
// after it. Calling it twice is a no-op.
func (w *Watcher) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.closed {
		return
	}
	w.started = true
	for _, p := range w.live {
		p.start()
	}
}

// Dirs returns the directories actually being watched, resolved and sorted, so
// that what is logged is what is polled rather than what was asked for.
func (w *Watcher) Dirs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.live))
	for _, p := range w.live {
		out = append(out, p.dir)
	}
	sort.Strings(out)
	return out
}

// Close stops every polling loop and waits for the running polls to finish, so
// no OnJob call is still in flight once it returns.
func (w *Watcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for f, p := range w.live {
		p.close()
		delete(w.live, f)
	}
	w.closed = true
	return nil
}

// resolveDir turns a configured folder into the path everything else keys on. It
// creates the directory first, because a fresh install has to have somewhere to
// drop files and because a path cannot be resolved before it exists.
func resolveDir(dir string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return "", fmt.Errorf("watch: %s: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("watch: %s: %w", abs, err)
	}
	// Resolving is what makes a symlink and its target one folder, which matters
	// because a container is usually handed the same share under two names. A
	// path that will not resolve is still a path we can poll, so the failure only
	// costs the comparison, not the folder.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// writable reports whether the process can retire a consumed file. It is checked
// when a folder is taken up rather than on every poll, because the failure is a
// permission problem that will never resolve on its own.
func writable(dir string) error {
	probe := filepath.Join(dir, ".knightloader-watch-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(probe)
}
