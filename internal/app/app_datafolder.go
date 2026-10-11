package app

import (
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// dataFolderOf refuses the first of dirs that lies in the data folder or holds
// it, and returns nil when none does. Nothing a download, an extraction, a
// rename or a move writes may land there, beside the password hash, the API
// tokens, the settings and the tools KnightLoader runs (see
// settings.OverlapsData).
func (a *App) dataFolderOf(dirs ...string) *dataFolderError {
	for _, d := range dirs {
		switch {
		case d == "":
		case settings.InsideData(a.DataDir, d):
			return &dataFolderError{dir: d}
		case settings.HoldsData(a.DataDir, d):
			return &dataFolderError{dir: d, holds: true}
		}
	}
	return nil
}

// folderLookupWait bounds how long the dispatcher, which holds a.mu, waits
// for the answer about one folder. Finding it out walks the folder's path on
// disk, and a network mount that hangs would otherwise stall the whole app;
// the task waits for a later pass instead.
const folderLookupWait = 2 * time.Second

// folderVerdictAge is how long an answer about a folder stands. A link put in
// a folder's place later is caught at the next look, and by the check every
// file of an extraction or a move passes.
const folderVerdictAge = 30 * time.Second

// maxFolderVerdicts bounds the answers kept; past it the stale ones go.
const maxFolderVerdicts = 256

// folderVerdicts keeps the dispatcher's answers about folders, looked up on
// goroutines of their own.
type folderVerdicts struct {
	mu     sync.Mutex
	known  map[string]folderVerdict
	asking map[string]chan struct{}
	// lookup stands in for dataFolderOf in tests.
	lookup func(dir string) *dataFolderError
}

type folderVerdict struct {
	refusal *dataFolderError
	at      time.Time
}

// dataFolderOfLocked is dataFolderOf for a caller holding a.mu. Each folder is
// looked up away from the lock; ok is false when an answer did not come
// within folderLookupWait, and the dispatcher runs again once it has.
func (a *App) dataFolderOfLocked(dirs ...string) (refusal *dataFolderError, ok bool) {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		r, ok := a.folderVerdict(d)
		if !ok || r != nil {
			return r, ok
		}
	}
	return nil, true
}

func (a *App) folderVerdict(dir string) (*dataFolderError, bool) {
	v := &a.folders
	now := time.Now()
	v.mu.Lock()
	if k, ok := v.known[dir]; ok && now.Sub(k.at) < folderVerdictAge {
		v.mu.Unlock()
		return k.refusal, true
	}
	done, asking := v.asking[dir]
	started := !asking
	if started {
		if v.asking == nil {
			v.asking, v.known = map[string]chan struct{}{}, map[string]folderVerdict{}
		}
		done = make(chan struct{})
		v.asking[dir] = done
		lookup := v.lookup
		if lookup == nil {
			lookup = func(dir string) *dataFolderError { return a.dataFolderOf(dir) }
		}
		go func() {
			r := lookup(dir)
			v.mu.Lock()
			if len(v.known) >= maxFolderVerdicts {
				for d, k := range v.known {
					if time.Since(k.at) >= folderVerdictAge {
						delete(v.known, d)
					}
				}
			}
			v.known[dir] = folderVerdict{refusal: r, at: time.Now()}
			delete(v.asking, dir)
			v.mu.Unlock()
			close(done)
		}()
	}
	v.mu.Unlock()
	select {
	case <-done:
	case <-time.After(folderLookupWait):
		if !started {
			return nil, false
		}
		go func() {
			<-done
			a.spawn(func() {
				a.mu.Lock()
				a.dispatchLocked()
				a.mu.Unlock()
			})
		}()
		return nil, false
	}
	v.mu.Lock()
	k := v.known[dir]
	v.mu.Unlock()
	return k.refusal, true
}

// barFolder is dataFolderOf for one folder, in the shape a check that takes an
// error wants.
func (a *App) barFolder(dir string) error {
	if refusal := a.dataFolderOf(dir); refusal != nil {
		return refusal
	}
	return nil
}

// barFile refuses a file or folder about to be written at path when path
// lands in the data folder, through whatever folders and links lead there.
func (a *App) barFile(path string) error {
	if settings.InsideData(a.DataDir, path) {
		return &dataFolderError{dir: path}
	}
	return nil
}

// barFiles is barFile for the many files one extraction or move writes, with
// a settings.DataGuard that remembers the folders it looked at.
func (a *App) barFiles() func(path string) error {
	guard := settings.NewDataGuard(a.DataDir)
	return func(path string) error {
		if guard.Inside(path) {
			return &dataFolderError{dir: path}
		}
		return nil
	}
}

// dataFolderError is a write refused because dir lies in the data folder, or
// with holds set, because dir holds it. It reads as core.CodeDataFolder or
// core.CodeHoldsDataFolder (see diskCode).
type dataFolderError struct {
	dir   string
	holds bool
}

func (e *dataFolderError) Error() string {
	if e.holds {
		return e.dir + " holds KnightLoader's data folder; choose a folder that does not"
	}
	return e.dir + " is inside KnightLoader's data folder; choose a folder outside it"
}
