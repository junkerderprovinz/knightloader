package local

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

	mu      sync.Mutex
	runs    map[string]*runState
	link    map[string]string
	part    map[string]string
	stopped bool
}

// NewBackend builds a backend. client is asked at the start of each file.
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

// Remove stops the task and deletes its part file and map, with or without
// deleteFiles, or a later attempt at the same link would take them up. The
// finished file is left to the app, which has it from Update.File: its name
// may be a counted one, and the name the link gives can be another task's.
func (b *Backend) Remove(taskID string, _ bool) {
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
	if part != "" {
		RemovePart(part)
	}
}

// Stop ends every transfer and returns once each has made its last report,
// the done of a file that just finished included. Nothing starts after it.
func (b *Backend) Stop() {
	b.mu.Lock()
	b.stopped = true
	runs := slices.Collect(maps.Values(b.runs))
	b.mu.Unlock()
	for _, r := range runs {
		r.cancel()
	}
	for _, r := range runs {
		<-r.ended
	}
}

// PartFile is where the file behind link is written in dir until it is whole,
// or "" for a link that is not one of these.
func PartFile(dir, link string) string {
	ref, err := parseLink(link)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, collide.SafeName(ref.name)+reclaim.PartSuffix)
}

// RemovePart deletes a part file and the segment map beside it.
func RemovePart(part string) {
	_ = os.Remove(part)
	removeMap(part + mapSuffix)
}

func (b *Backend) launch(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, ended: make(chan struct{})}
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		cancel()
		return
	}
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
	part := PartFile(dir, link)
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
		verb := "are"
		if missing == 1 {
			verb = "is"
		}
		fail(core.Update{
			Err:    fmt.Sprintf("%d of the %d articles of this file %s on none of your Usenet servers", missing, len(file.Segments), verb),
			Reason: core.ReasonGone,
		})
		return
	}

	final, err := d.finish(filepath.Join(dir, name))
	if err != nil {
		fail(core.Update{Err: err.Error()})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Name: filepath.Base(final), Size: d.size(), Loaded: d.size(), File: final})
}

// fetch downloads the articles the map lacks, on as many workers as the
// client has connections, and reports how many no server had. An error is a
// server that stayed unreachable. A server given more connections meanwhile
// gets more workers at the next progress report; one given fewer holds the
// extra workers back in the client.
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
		mu      sync.Mutex
		missing int
		fatal   error
		// workers is how many were started, running how many have not
		// returned yet. Only this goroutine touches either.
		workers, running int
		exited           = make(chan struct{})
	)
	work := func() {
		defer func() { exited <- struct{}{} }()
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
	}
	grow := func() {
		for ; workers < max(min(client.Connections(), len(todo)), 1); workers++ {
			running++
			go work()
		}
	}
	grow()

	tick := time.NewTicker(progressEvery)
	defer tick.Stop()
	last, lastBytes, lastSave := time.Now(), d.loaded(), time.Now()
	for {
		select {
		case <-exited:
			if running--; running == 0 {
				return missing, fatal
			}
		case now := <-tick.C:
			grow()
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
