package crossseed

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// DaemonKey is the cross-seed daemon's name in the history.
const DaemonKey = "daemon"

// IndexerKey is a Torznab indexer's name in the history, apart from the
// sites' names.
func IndexerKey(name string) string { return "torznab:" + name }

// Account is the user's account on a Gazelle site: the client that asks the
// site, and the announce address a twin found there announces to.
type Account struct {
	Client   *Gazelle
	Announce string
}

// Config is what a runner searches with. With a Daemon, the daemon searches
// and the rest goes unused.
type Config struct {
	Gazelle []Account
	Torznab []*Torznab
	Daemon  *Daemon
}

// Source is a finished torrent to find twins of. Torrent is its .torrent,
// nil for a magnet, whose Files then say what it holds; Dir is the folder it
// landed in.
type Source struct {
	Hash    string
	Name    string
	Torrent []byte
	Files   []FileEntry
	Dir     string
}

// Found is a twin on another tracker: the same files under another info hash.
type Found struct {
	Tracker string
	Hash    string
	Torrent []byte
	Files   []FileEntry
}

// Options are what a runner needs. Inject puts a twin into the torrent
// client, or returns why it did not.
type Options struct {
	Config  func() Config
	History *History
	Log     *Log
	Inject  func(ctx context.Context, src Source, f Found) error
}

// Runner searches for the twins of the torrents it is offered, one at a time
// in the background, and asks each tracker about each torrent once.
type Runner struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	done   chan struct{}

	mu     sync.Mutex
	queue  []Source
	queued map[string]bool
	busy   bool
}

// Start runs a runner until Close.
func Start(o Options) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{o: o, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), queued: map[string]bool{}}
	go r.run()
	return r
}

// Offer queues torrents to search for. One queued or being searched for
// already is not queued again.
func (r *Runner) Offer(srcs ...Source) {
	r.mu.Lock()
	for _, s := range srcs {
		if !r.queued[s.Hash] {
			r.queued[s.Hash] = true
			r.queue = append(r.queue, s)
		}
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close cuts the search under way short and waits for the runner to stop.
// What was cut short is not remembered, so the next start asks again.
func (r *Runner) Close() {
	r.cancel()
	<-r.done
}

// Wait blocks until nothing is queued or searched for.
func (r *Runner) Wait(ctx context.Context) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		r.mu.Lock()
		idle := len(r.queue) == 0 && !r.busy
		r.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (r *Runner) run() {
	defer close(r.done)
	for {
		src, ok := r.next()
		if !ok {
			select {
			case <-r.ctx.Done():
				return
			case <-r.wake:
				continue
			}
		}
		r.search(src)
		r.mu.Lock()
		delete(r.queued, src.Hash)
		r.busy = false
		r.mu.Unlock()
		if err := r.o.History.Save(); err != nil {
			log.Printf("cross-seed: the answers could not be written: %v", err)
		}
		if r.ctx.Err() != nil {
			return
		}
	}
}

func (r *Runner) next() (Source, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 || r.ctx.Err() != nil {
		return Source{}, false
	}
	src := r.queue[0]
	r.queue = r.queue[1:]
	r.busy = true
	return src, true
}

func (r *Runner) search(src Source) {
	c := r.o.Config()
	if c.Daemon != nil {
		r.handToDaemon(c.Daemon, src)
		return
	}
	if src.Torrent != nil {
		for _, acc := range c.Gazelle {
			r.ask(acc.Client.Site().Name, acc.Client.Site().Name, src, func(ctx context.Context) (*Found, error) {
				return findOnGazelle(ctx, acc, src)
			})
		}
	}
	files := src.Files
	if len(c.Torznab) == 0 {
		return
	}
	if files == nil {
		var err error
		if _, files, err = FilesOf(src.Torrent); err != nil {
			log.Printf("cross-seed: the files of %s could not be read: %v", src.Name, err)
			return
		}
	}
	for _, ix := range c.Torznab {
		r.ask(ix.name, IndexerKey(ix.name), src, func(ctx context.Context) (*Found, error) {
			return findOnIndexer(ctx, ix, src, files)
		})
	}
}

func findOnGazelle(ctx context.Context, acc Account, src Source) (*Found, error) {
	m, err := acc.Client.Find(ctx, src.Torrent)
	if err != nil || m == nil {
		return nil, err
	}
	b, err := Build(m.Candidate, acc.Announce)
	if err != nil {
		return nil, err
	}
	_, files, err := FilesOf(b)
	if err != nil {
		return nil, err
	}
	return &Found{Tracker: acc.Client.Site().Name, Hash: m.Candidate.InfoHash, Torrent: b, Files: files}, nil
}

func findOnIndexer(ctx context.Context, ix *Torznab, src Source, files []FileEntry) (*Found, error) {
	m, err := ix.Find(ctx, src.Name, files, func(hash string) bool { return strings.EqualFold(hash, src.Hash) })
	if err != nil || m == nil {
		return nil, err
	}
	_, theirs, err := FilesOf(m.Torrent)
	if err != nil {
		return nil, err
	}
	return &Found{Tracker: ix.name, Hash: m.InfoHash, Torrent: m.Torrent, Files: theirs}, nil
}

// ask asks one tracker about src unless it has answered before, injects a
// twin it has, and remembers and logs what came of it.
func (r *Runner) ask(tracker, key string, src Source, find func(ctx context.Context) (*Found, error)) {
	if r.o.History.Asked(key, src.Hash) {
		return
	}
	f, err := find(r.ctx)
	switch {
	case r.ctx.Err() != nil, errors.Is(err, ErrNotGazelle), errors.Is(err, ErrSameSite):
		return
	case err != nil:
		r.o.History.Note(key, src.Hash, Answer{Result: ResultError, Reason: err.Error()})
		r.o.Log.Add(Entry{Tracker: tracker, Name: src.Name, InfoHash: src.Hash, Result: ResultError, Reason: err.Error()})
		return
	case f == nil:
		r.o.History.Note(key, src.Hash, Answer{Result: ResultNone})
		return
	}
	if err := r.o.Inject(r.ctx, src, *f); err != nil {
		if r.ctx.Err() != nil {
			return
		}
		r.o.History.Note(key, src.Hash, Answer{Result: ResultRefused, InfoHash: f.Hash, Reason: err.Error()})
		r.o.Log.Add(Entry{Tracker: tracker, Name: src.Name, InfoHash: f.Hash, Result: ResultRefused, Reason: err.Error()})
		log.Printf("cross-seed: the twin of %s on %s was not injected: %v", src.Name, tracker, err)
		return
	}
	r.o.History.Note(key, src.Hash, Answer{Result: ResultInjected, InfoHash: f.Hash})
	r.o.Log.Add(Entry{Tracker: tracker, Name: src.Name, InfoHash: f.Hash, Result: ResultInjected})
	log.Printf("cross-seed: %s seeds on %s as well", src.Name, tracker)
}

func (r *Runner) handToDaemon(d *Daemon, src Source) {
	if r.o.History.Asked(DaemonKey, src.Hash) {
		return
	}
	err := d.Announce(r.ctx, src.Hash)
	switch {
	case r.ctx.Err() != nil:
		return
	case err != nil:
		r.o.History.Note(DaemonKey, src.Hash, Answer{Result: ResultError, Reason: err.Error()})
		r.o.Log.Add(Entry{Tracker: DaemonKey, Name: src.Name, InfoHash: src.Hash, Result: ResultError, Reason: err.Error()})
		return
	}
	r.o.History.Note(DaemonKey, src.Hash, Answer{Result: ResultHanded})
	r.o.Log.Add(Entry{Tracker: DaemonKey, Name: src.Name, InfoHash: src.Hash, Result: ResultHanded})
}
