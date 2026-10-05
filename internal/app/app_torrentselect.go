package app

// The files of a torrent task as the list shows them under its row, and
// changing which of them it fetches once it is staged or running.

import (
	"errors"
	"slices"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

var (
	// ErrNoTorrent is an id that names no torrent task.
	ErrNoTorrent = errors.New("no such torrent")
	// ErrTorrentFilesUnknown is a magnet whose swarm has not sent its file
	// list yet.
	ErrTorrentFilesUnknown = errors.New("the torrent's file list is not known yet")
	// ErrNoFileSelected is a selection that leaves out every file, which the
	// torrent client would read as all of them.
	ErrNoFileSelected = errors.New("at least one file of the torrent has to stay selected")
)

// torrentPickState is embedded in App so its lock stays in this file. It
// keeps two changes to one torrent's files from overtaking each other between
// the engine and the task.
type torrentPickState struct {
	torrentPickMu sync.Mutex
}

// TorrentFileView is one file of a torrent task and how much of it is here.
type TorrentFileView struct {
	core.TorrentFile
	// Done is the bytes of the file that are here, or nil when nothing can
	// say: a file the running torrent leaves out, or a torrent no client runs
	// at the moment.
	Done *int64 `json:"done,omitempty"`
}

// countTorrentFiles sets how many files t's torrent has: from its selection,
// or for an uploaded .torrent nobody has chosen files of, from the link. A
// magnet's count comes with the list the swarm sends.
func countTorrentFiles(t *core.Task) {
	if len(t.TorrentFiles) > 0 {
		t.TorrentFileCount = len(t.TorrentFiles)
		t.TorrentMedia = core.TorrentMedia(t.TorrentFiles)
		return
	}
	if !torrent.IsURI(t.URL) {
		return
	}
	if md, err := (torrent.Resolver{}).Describe(t.URL); err == nil {
		t.TorrentFileCount = len(md.Files)
	}
}

// torrentSnapshot is what TorrentFiles and SelectTorrentFiles read of a task
// under a.mu.
type torrentSnapshot struct {
	url, category, resolver string
	status                  core.Status
	seeding                 bool
	loaded                  int64
	files                   []core.TorrentFile
}

func (a *App) torrentSnapshot(id string) (torrentSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	if t == nil || !torrent.IsURI(t.URL) {
		return torrentSnapshot{}, ErrNoTorrent
	}
	return torrentSnapshot{
		url: t.URL, category: t.Category, resolver: t.Resolver,
		status: t.Status, seeding: t.Seeding, loaded: t.Loaded,
		files: slices.Clone(t.TorrentFiles),
	}, nil
}

// torrentListing is the torrent's files. An uploaded .torrent nobody has
// chosen files of shows what the file rules of its category would fetch,
// which is what it fetches if it starts untouched.
func (a *App) torrentListing(s torrentSnapshot) []core.TorrentFile {
	if len(s.files) > 0 || torrent.IsMagnet(s.url) {
		return s.files
	}
	md, err := (torrent.Resolver{}).Describe(s.url)
	if err != nil {
		return nil
	}
	picked, err := fileRules(a.Settings.Get().TorrentFileRulesFor(s.category)).Pick(md.Files)
	if err != nil {
		// The start fails on the same rule and says why.
		return md.Files
	}
	return picked
}

// runningOn is the engine when it is the task's client, else nil.
func (a *App) runningOn(resolver string) *engine.Engine {
	if e, ok := a.backendFor(resolver).(*engine.Engine); ok && e != nil {
		return e
	}
	return nil
}

// fileProgress is how much of each file is here, by index, or nil where
// nothing can say.
func (a *App) fileProgress(id string, s torrentSnapshot, files []core.TorrentFile) []*int64 {
	out := make([]*int64, len(files))
	whole := func(i int) {
		if files[i].Selected {
			n := files[i].Size
			out[i] = &n
		}
	}
	nothing := func(i int) {
		if files[i].Selected {
			out[i] = new(int64)
		}
	}
	switch {
	case s.status == core.StatusDone || s.status == core.StatusExtracting:
		for i := range files {
			whole(i)
		}
	case s.status == core.StatusCollected || s.loaded == 0 && s.status == core.StatusQueued:
		for i := range files {
			nothing(i)
		}
	default:
		e := a.runningOn(s.resolver)
		if e == nil {
			return out
		}
		done, ok := e.TorrentFileProgress(id)
		if !ok || len(done) != len(files) {
			return out
		}
		for i, n := range done {
			if n >= 0 {
				out[i] = &n
			}
		}
	}
	return out
}

// TorrentFiles is every file of torrent task id, with its selection and how
// much of it is here. A magnet whose swarm has not sent the list yet has none.
func (a *App) TorrentFiles(id string) ([]TorrentFileView, error) {
	s, err := a.torrentSnapshot(id)
	if err != nil {
		return nil, err
	}
	files := a.torrentListing(s)
	done := a.fileProgress(id, s, files)
	out := make([]TorrentFileView, len(files))
	for i, f := range files {
		out[i] = TorrentFileView{TorrentFile: f, Done: done[i]}
	}
	return out, nil
}

// SelectTorrentFiles makes the files at paths the ones torrent task id
// fetches and returns its files as TorrentFiles does. Paths are matched
// against the torrent's own list, so one it does not have selects nothing.
//
// A running torrent on the built-in client carries on with the new selection
// without starting over (see engine.SelectTorrentFiles). Any other client
// takes it at its next start. A finished torrent keeps what it has.
func (a *App) SelectTorrentFiles(id string, paths []string) ([]TorrentFileView, error) {
	a.torrentPickMu.Lock()
	defer a.torrentPickMu.Unlock()
	s, err := a.torrentSnapshot(id)
	if err != nil {
		return nil, err
	}
	if s.status == core.StatusDone || s.status == core.StatusExtracting || s.seeding {
		return nil, engine.ErrTorrentFinished
	}
	files := a.torrentListing(s)
	if len(files) == 0 {
		return nil, ErrTorrentFilesUnknown
	}
	before := a.fileProgress(id, s, files)
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	next := slices.Clone(files)
	var size, loaded int64
	counted := false
	for i := range next {
		next[i].Selected = want[next[i].Path]
		if !next[i].Selected {
			continue
		}
		size += next[i].Size
		if before[i] != nil {
			loaded += *before[i]
			counted = true
		}
	}
	if !slices.ContainsFunc(next, func(f core.TorrentFile) bool { return f.Selected }) {
		return nil, ErrNoFileSelected
	}
	if e := a.runningOn(s.resolver); e != nil {
		if err := e.SelectTorrentFiles(id, core.SelectedTorrentIndices(next)); err != nil {
			return nil, err
		}
	}

	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return nil, ErrNoTorrent
	}
	t.TorrentFiles = next
	t.TorrentFileCount = len(next)
	t.TorrentMedia = core.TorrentMedia(next)
	t.Size = size
	// What is here of the files still wanted, as far as the client said; its
	// next reading replaces it.
	if counted {
		t.Loaded = loaded
	}
	t.Loaded = min(t.Loaded, size)
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
	return a.TorrentFiles(id)
}
