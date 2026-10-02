package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/nntp"
	"github.com/junkerderprovinz/knightloader/internal/nzb"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
)

const (
	// progressEvery is how often a running file reports itself, and saveEvery
	// how often its segment map is written.
	progressEvery = 500 * time.Millisecond
	saveEvery     = 2 * time.Second
	// unreachableTries is how often an article is asked for again while a
	// server that may have it cannot be reached, the wait doubling from
	// Backend.Wait, before the file fails and the retry policy takes over.
	unreachableTries = 5
	defaultWait      = 5 * time.Second
)

// Files gives the backend a job's files. *Service is one.
type Files interface {
	File(job string, index int) (nzb.File, error)
}

// Backend downloads the files of the jobs the own servers took: every article
// is fetched on one of the client's connections and written at its offset in
// a part file of the file's full size. A map beside the part file records the
// articles already written, so a pause or a restart carries on with the rest.
type Backend struct {
	files    Files
	client   func() *nntp.Client
	dir      string
	onUpdate func(taskID string, u core.Update)

	// Dir returns the destination for one task, the backend's own directory
	// when nil or empty.
	Dir func(taskID string) string
	// Incomplete hears of a file some of whose articles no server has. It
	// reports whether the job went to another account, in which case the
	// task is not failed here: the app removes it with the rest of the job.
	Incomplete func(job string, missing int) bool
	// Wait is the first pause before an article is asked for again while a
	// server is unreachable.
	Wait time.Duration

	mu   sync.Mutex
	runs map[string]*runState
	link map[string]string
	part map[string]string
}

// NewBackend builds a backend. client is asked at the start of each file, so
// a change to the servers applies to the next file.
func NewBackend(files Files, client func() *nntp.Client, dir string, onUpdate func(string, core.Update)) *Backend {
	return &Backend{
		files: files, client: client, dir: dir, onUpdate: onUpdate, Wait: defaultWait,
		runs: map[string]*runState{}, link: map[string]string{}, part: map[string]string{},
	}
}

type runState struct {
	cancel context.CancelFunc
	ended  chan struct{}
}

func (b *Backend) Download(taskID, link string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.mu.Unlock()
	b.launch(taskID, link)
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	link := b.link[taskID]
	b.mu.Unlock()
	if link != "" {
		b.launch(taskID, link)
	}
}

// Halt stops the task's transfer and returns once its files are closed, so
// their folder can be moved. It reports whether one was running.
func (b *Backend) Halt(taskID string) bool {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r == nil {
		return false
	}
	r.cancel()
	<-r.ended
	return true
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	r := b.runs[taskID]
	part := b.part[taskID]
	delete(b.link, taskID)
	delete(b.part, taskID)
	b.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.ended
	}
	// The part file and its map always go, or a later attempt at the same
	// link would take them up; the finished file only with deleteFiles.
	if part != "" {
		_ = os.Remove(part)
		removeMap(part + mapSuffix)
		if deleteFiles {
			_ = os.Remove(part[:len(part)-len(reclaim.PartSuffix)])
		}
	}
}

func (b *Backend) launch(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, ended: make(chan struct{})}
	b.mu.Lock()
	prev := b.runs[taskID]
	b.runs[taskID] = r
	b.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			b.mu.Lock()
			if b.runs[taskID] == r {
				delete(b.runs, taskID)
			}
			b.mu.Unlock()
			close(r.ended)
		}()
		// A resume right after a pause waits for the paused run to let go
		// of the part file.
		if prev != nil {
			prev.cancel()
			<-prev.ended
		}
		b.run(ctx, taskID, link)
	}()
}

func (b *Backend) run(ctx context.Context, taskID, link string) {
	fail := func(u core.Update) {
		if ctx.Err() == nil {
			u.Status = core.StatusError
			b.onUpdate(taskID, u)
		}
	}
	ref, err := parseLink(link)
	if err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	file, err := b.files.File(ref.job, ref.index)
	if err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	dir := b.dir
	if b.Dir != nil {
		if d := b.Dir(taskID); d != "" {
			dir = d
		}
	}
	if err := os.MkdirAll(dir, filemode.Dir); err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	name := collide.SafeName(ref.name)
	part := filepath.Join(dir, name+reclaim.PartSuffix)
	b.mu.Lock()
	b.part[taskID] = part
	b.mu.Unlock()

	d, err := openDownload(part, file)
	if err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: d.size(), Loaded: d.loaded()})

	missing, err := b.fetch(ctx, taskID, d)
	closeErr := d.close()
	switch {
	case ctx.Err() != nil:
		return
	case errors.Is(err, nntp.ErrUnavailable):
		fail(core.Update{Err: "a Usenet server could not be reached: " + err.Error(), Reason: core.ReasonNetwork})
		return
	case err != nil:
		fail(core.Update{Err: err.Error()})
		return
	case closeErr != nil:
		fail(core.Update{Err: closeErr.Error()})
		return
	case missing > 0:
		if b.Incomplete != nil && b.Incomplete(ref.job, missing) {
			return
		}
		fail(core.Update{
			Err:    fmt.Sprintf("%d of the %d articles of this file are on none of your Usenet servers", missing, len(file.Segments)),
			Reason: core.ReasonGone,
		})
		return
	}

	final, err := d.finish(filepath.Join(dir, name))
	if err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Name: filepath.Base(final), Size: d.size(), Loaded: d.size()})
}

// fetch downloads the articles the map lacks, on as many workers as the
// client has connections, and reports how many no server had. An error is a
// server that stayed unreachable.
func (b *Backend) fetch(ctx context.Context, taskID string, d *download) (int, error) {
	client := b.client()
	todo := d.pending()
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	queue := make(chan int)
	go func() {
		defer close(queue)
		for _, i := range todo {
			select {
			case queue <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	var (
		mu       sync.Mutex
		missing  int
		fatal    error
		finished = make(chan struct{})
		wg       sync.WaitGroup
	)
	for range max(min(client.Connections(), len(todo)), 1) {
		wg.Go(func() {
			for i := range queue {
				switch err := b.fetchOne(ctx, client, d, i); {
				case err == nil, ctx.Err() != nil:
				case errors.Is(err, nntp.ErrMissing), errors.Is(err, nntp.ErrDamaged):
					mu.Lock()
					missing++
					mu.Unlock()
				default:
					mu.Lock()
					if fatal == nil {
						fatal = err
					}
					mu.Unlock()
					stop()
				}
			}
		})
	}
	go func() {
		wg.Wait()
		close(finished)
	}()

	tick := time.NewTicker(progressEvery)
	defer tick.Stop()
	last, lastBytes, lastSave := time.Now(), d.loaded(), time.Now()
	for {
		select {
		case <-finished:
			return missing, fatal
		case now := <-tick.C:
			loaded := d.loaded()
			b.onUpdate(taskID, core.Update{
				Status: core.StatusRunning, Size: d.size(), Loaded: loaded,
				Speed: int64(float64(loaded-lastBytes) / now.Sub(last).Seconds()),
			})
			last, lastBytes = now, loaded
			if now.Sub(lastSave) >= saveEvery {
				d.save()
				lastSave = now
			}
		}
	}
}

// fetchOne fetches one article and writes it, asking again a few times while
// a server that may have it is unreachable.
func (b *Backend) fetchOne(ctx context.Context, client *nntp.Client, d *download, i int) error {
	seg := d.file.Segments[i]
	wait := b.Wait
	for try := 1; ; try++ {
		p, err := client.Fetch(ctx, seg.ID, d.file.Posted)
		if err == nil {
			return d.write(i, p)
		}
		if !errors.Is(err, nntp.ErrUnavailable) || try == unreachableTries {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
}
