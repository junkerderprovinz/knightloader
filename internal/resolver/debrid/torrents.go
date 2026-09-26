package debrid

// Torrents through a debrid service, the way rdt-client does it: the service
// fetches the torrent onto its own servers, and the files come here over HTTP
// through the engine like any unlocked hoster link. A torrent the service has
// cached is ready at once; one it has not can take hours, and the task shows
// the service's progress meanwhile.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// TorrentService is a debrid service that also takes torrents.
type TorrentService interface {
	ID() string
	Label() string
	// AddTorrent hands the service a torrent and returns the job's id. held
	// reports a job the account already had for the torrent, which is not this
	// client's to delete.
	AddTorrent(ctx context.Context, src TorrentSource) (id string, held bool, err error)
	// TorrentStatus reads one job.
	TorrentStatus(ctx context.Context, id string) (TorrentJob, error)
	// FileURL turns one file of a finished job into a direct link.
	FileURL(ctx context.Context, id string, f TorrentFile) (Direct, error)
	// DeleteTorrent removes the job and its files from the account.
	DeleteTorrent(ctx context.Context, id string) error
}

// FileSelector is implemented by a service that can be told which files of a
// torrent to fetch. It is asked once the job reports TorrentChoosing.
type FileSelector interface {
	SelectFiles(ctx context.Context, id string, files []TorrentFile) error
}

// TorrentSource is a torrent as a service takes it: a magnet link, or the
// bytes of an uploaded .torrent.
type TorrentSource struct {
	Magnet string
	File   []byte
	// InfoHash names the torrent either way, for a service that can only be
	// asked by hash.
	InfoHash string
	// Choose asks the service to hold the torrent until SelectFiles, where it
	// can do that at all. Real-Debrid always holds it.
	Choose bool
}

// TorrentState is where a service's job stands.
type TorrentState int

const (
	// TorrentFetching is the service still working: reading the magnet,
	// waiting in its queue, downloading or moving the files.
	TorrentFetching TorrentState = iota
	// TorrentChoosing is the service waiting for SelectFiles.
	TorrentChoosing
	// TorrentReady is every file on the service, ready to hand out.
	TorrentReady
	// TorrentFailed is the service giving up on the torrent; Reason says why.
	TorrentFailed
)

// TorrentJob is a service's view of one torrent.
type TorrentJob struct {
	Name  string
	Size  int64
	State TorrentState
	// Progress is how much of the torrent the service has, from 0 to 1.
	Progress float64
	Speed    int64
	Seeds    int
	// Reason is the service's own words for a failed torrent.
	Reason string
	// Files is every file of the torrent the service knows of, selected or not.
	Files []TorrentFile
}

// TorrentFile is one file of a service's job.
type TorrentFile struct {
	// ID is the service's own handle on the file, which FileURL takes back.
	ID string
	// Path is where the file sits inside the torrent, slash-separated, as the
	// service states it. Empty when only the unlock learns the name.
	Path string
	Size int64
	// Held says the service has the file and can hand it out.
	Held bool
}

// Refusal is a service declining a torrent outright: a private tracker it does
// not serve, a torrent too big for the plan, a quota spent, a job it gave up
// on. The task moves on to the next backend rather than retrying here.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

// Part is one file of a torrent on its way to the engine.
type Part struct {
	TaskID string
	// ID is the engine's id for this file, unique within the task.
	ID  string
	URL string
	// Path is where the file goes inside the task's folder, slash-separated.
	Path  string
	Size  int64
	Conns int
	// Relink asks the service for a fresh link to the same file.
	Relink func(context.Context) (string, error)
	// Leftover is where an attempt before a restart was writing this file.
	// What is still that attempt's goes before the file is fetched again.
	Leftover string
	// Progress receives the engine's byte count and speed for this file, and
	// where it is writing it.
	Progress func(loaded, speed int64, file string)
}

// PartDownloader is the engine as the files of a torrent reach it, one file at
// a time.
type PartDownloader interface {
	// FetchPart downloads one file and returns once it is written, once the
	// engine gives up on it, or once ctx ends. It returns where the file was
	// written.
	FetchPart(ctx context.Context, p Part) (string, error)
	Pause(id string)
	Resume(id string)
	Remove(id string, deleteFiles bool)
}

// Transfers is what a backend does with one task.
type Transfers interface {
	Download(taskID, link string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// torrentPoll is how often a job the service is still fetching is read again.
// Real-Debrid allows 250 calls a minute per key, shared with every other task.
const torrentPoll = 5 * time.Second

// statusMisses is how many reads of a job in a row may fail before the task
// does. A torrent the service has not cached can take hours, and one dropped
// call in that time is no reason to give up on it.
const statusMisses = 5

// Runs is every torrent one account's backend is fetching, by task. It
// outlives the backend: a rewire builds a new backend for the account, which
// carries on with the runs the old one started, so a pause or a removal still
// reaches them.
type Runs struct {
	slot   string
	mu     sync.Mutex
	byTask map[string]*torrentRun
}

// NewRuns starts the runs of the account in slot (resolver.SlotID).
func NewRuns(slot string) *Runs {
	return &Runs{slot: slot, byTask: map[string]*torrentRun{}}
}

// Restore takes up the job a task held before a restart. The task's next
// Download carries on with it from the first file that is not here yet.
func (rs *Runs) Restore(taskID, link string, j core.ServiceJob) {
	_, _, imported := ParseJobLink(link)
	r := &torrentRun{link: link, job: j.ID, imported: imported, held: !imported && !j.Owned, next: j.Done, partial: j.Partial}
	if !imported {
		if _, name, _, err := sourceOf(link); err == nil {
			r.name = name
		}
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.byTask[taskID] = r
}

// TorrentBackend fetches magnet links and uploaded .torrent files through a
// service's torrent API and passes every other link to the backend beside it.
type TorrentBackend struct {
	svc   TorrentService
	links Transfers
	parts PartDownloader
	runs  *Runs

	onUpdate func(taskID string, u core.Update)

	// Files returns the task's file selection; nil or empty fetches every file.
	Files func(taskID string) []core.TorrentFile
	// Rules returns the file rules for a task nobody chose files for, as a
	// test of a path inside the torrent and a size, or nil when it has none.
	// A selection made by hand wins over them.
	Rules func(taskID string) (keep func(path string, size int64) bool, err error)
	// Keep reports whether a finished torrent stays on the service rather than
	// being deleted there once its files are here.
	Keep func() bool
	// Added is told the id of every job this backend adds to the account, so
	// the import from the account knows the job for one of its own.
	Added func(job string)

	poll time.Duration
}

// torrentRun is one task's torrent. Every field is guarded by the mu of the
// Runs holding it.
type torrentRun struct {
	link  string
	conns int
	// name is what the torrent calls itself, or its info hash, for a service
	// that names it nothing.
	name string
	// cancel ends the attempt under way; nil when none is.
	cancel context.CancelFunc
	// over is closed once the goroutine of the latest attempt has returned.
	over chan struct{}
	// job is the service's id for the torrent, "" before it is added and once
	// the task has no more use for it.
	job string
	// held says the account had the job before this task asked for it, so it
	// is never deleted here.
	held bool
	// imported is a download added to the account outside this instance,
	// whose JobLink names the job. Nothing else holds it, so it is never added
	// again, and whether it is deleted when the task goes is the app's call.
	imported bool
	chosen   bool
	// next is the first file still to fetch, so a resume skips what is here.
	next int
	// partial is where the engine is writing file next, once it has said.
	partial string
	// parts are the engine ids handed out so far, part the one in flight.
	parts []string
	part  string
	done  bool
}

func NewTorrentBackend(svc TorrentService, links Transfers, parts PartDownloader, runs *Runs, onUpdate func(taskID string, u core.Update)) *TorrentBackend {
	return &TorrentBackend{
		svc: svc, links: links, parts: parts, runs: runs, onUpdate: onUpdate,
		poll: torrentPoll,
	}
}

// Service is the service this backend fetches torrents through.
func (b *TorrentBackend) Service() TorrentService { return b.svc }

// Download starts a torrent, or carries on with the one the task already
// holds here: after a failure, or with the job a restart restored.
func (b *TorrentBackend) Download(taskID, link string, headers map[string]string, conns int) {
	_, job, imported := ParseJobLink(link)
	if !imported && !torrent.IsURI(link) {
		b.links.Download(taskID, link, headers, conns)
		return
	}
	b.runs.mu.Lock()
	r := b.runs.byTask[taskID]
	if r == nil || r.done || r.link != link {
		r = &torrentRun{link: link, job: job, imported: imported}
		b.runs.byTask[taskID] = r
	}
	r.conns = conns
	b.runs.mu.Unlock()
	b.start(taskID, r)
}

// start ends the attempt under way, if there is one, and begins another once
// its goroutine has returned, so two never work on one run.
func (b *TorrentBackend) start(taskID string, r *torrentRun) {
	ctx, cancel := context.WithCancel(context.Background())
	over := make(chan struct{})
	b.runs.mu.Lock()
	r.settle()
	r.cancel = cancel
	prev := r.over
	r.over = over
	b.runs.mu.Unlock()
	go func() {
		defer close(over)
		if prev != nil {
			select {
			case <-prev:
			case <-ctx.Done():
				return
			}
		}
		b.run(ctx, taskID, r)
	}()
}

func (b *TorrentBackend) Pause(taskID string) {
	b.runs.mu.Lock()
	r := b.runs.byTask[taskID]
	if r == nil {
		b.runs.mu.Unlock()
		b.links.Pause(taskID)
		return
	}
	part := r.part
	if part == "" {
		r.settle()
	}
	b.runs.mu.Unlock()
	if part != "" {
		b.parts.Pause(part)
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

func (b *TorrentBackend) Resume(taskID string) {
	b.runs.mu.Lock()
	r := b.runs.byTask[taskID]
	if r == nil {
		b.runs.mu.Unlock()
		b.links.Resume(taskID)
		return
	}
	part, idle := r.part, r.cancel == nil && !r.done
	b.runs.mu.Unlock()
	switch {
	case part != "":
		b.parts.Resume(part)
	case idle:
		b.start(taskID, r)
	}
}

func (b *TorrentBackend) Remove(taskID string, deleteFiles bool) {
	b.runs.mu.Lock()
	r := b.runs.byTask[taskID]
	if r == nil {
		b.runs.mu.Unlock()
		b.links.Remove(taskID, deleteFiles)
		return
	}
	delete(b.runs.byTask, taskID)
	r.settle()
	parts, job, ours := r.parts, r.job, r.ours()
	r.job = ""
	b.runs.mu.Unlock()
	for _, p := range parts {
		b.parts.Remove(p, deleteFiles)
	}
	if job != "" && ours {
		b.deleteJob(job)
	}
}

// Holds reports whether the task has a job on the service here to carry on
// with. A retry then goes through Download again, which keeps the job and the
// files already here, rather than through Remove, which gives up both.
func (b *TorrentBackend) Holds(taskID string) bool {
	b.runs.mu.Lock()
	defer b.runs.mu.Unlock()
	r := b.runs.byTask[taskID]
	return r != nil && !r.done && r.job != ""
}

// deleteJob drops a job from the account, best effort and off the caller's
// goroutine.
func (b *TorrentBackend) deleteJob(job string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.svc.DeleteTorrent(ctx, job); err != nil {
			log.Printf("%s: could not delete torrent %s from the account: %v", b.svc.Label(), job, err)
		}
	}()
}

// live reports whether r is still the task's run and its attempt has not been
// called off. Caller holds b.runs.mu.
func (b *TorrentBackend) live(ctx context.Context, taskID string, r *torrentRun) bool {
	return ctx.Err() == nil && b.runs.byTask[taskID] == r
}

// jobLocked is the job the task holds, as the app keeps it across a restart.
// Caller holds b.runs.mu.
func (b *TorrentBackend) jobLocked(r *torrentRun) *core.ServiceJob {
	if r.job == "" {
		return &core.ServiceJob{}
	}
	return &core.ServiceJob{Slot: b.runs.slot, ID: r.job, Owned: r.ours(), Done: r.next, Partial: r.partial}
}

// settle ends the attempt under way, which has run its course. Caller holds
// the Runs' mu.
func (r *torrentRun) settle() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// ours reports whether this instance added the job, so it deletes the job
// when the task gives it up. An imported job is deleted once its files are
// here, as a finished torrent is, but a removal leaves it to the app.
func (r *torrentRun) ours() bool {
	return !r.held && !r.imported
}

func (b *TorrentBackend) run(ctx context.Context, taskID string, r *torrentRun) {
	b.runs.mu.Lock()
	job, next := r.job, r.next
	b.runs.mu.Unlock()
	if job == "" {
		var ok bool
		if job, ok = b.add(ctx, taskID, r); !ok {
			return
		}
	}
	tj, ok := b.await(ctx, taskID, r, job)
	if !ok {
		return
	}
	b.runs.mu.Lock()
	name := cmp.Or(tj.Name, r.name)
	b.runs.mu.Unlock()
	multi := len(tj.Files) > 1
	want, err := b.wanted(taskID, name, tj.Files)
	if err == nil {
		want = slices.DeleteFunc(want, func(f TorrentFile) bool { return !f.Held })
		if len(want) == 0 {
			err = errors.New("the service holds none of the files to fetch")
		}
	}
	if err != nil {
		b.refuse(ctx, taskID, r, err.Error())
		return
	}
	var total, loaded int64
	for i, f := range want {
		total += f.Size
		if i < next {
			loaded += f.Size
		}
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: total, Loaded: loaded})
	var file string
	for i := next; i < len(want); i++ {
		f := want[i]
		written, err := b.fetch(ctx, taskID, r, job, i, name, multi, f, loaded)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.fail(ctx, taskID, err)
			return
		}
		loaded += f.Size
		file = written
		b.runs.mu.Lock()
		r.next, r.partial = i+1, ""
		held := b.jobLocked(r)
		b.runs.mu.Unlock()
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Loaded: loaded, Job: held})
	}
	b.finish(ctx, taskID, r, job, loaded, multi, file)
}

// add hands the torrent to the service. It reports false when the task has
// been dealt with: refused, failed, paused or removed.
func (b *TorrentBackend) add(ctx context.Context, taskID string, r *torrentRun) (string, bool) {
	src, name, private, err := sourceOf(r.link)
	if err != nil {
		b.fail(ctx, taskID, err)
		return "", false
	}
	b.runs.mu.Lock()
	r.name = name
	b.runs.mu.Unlock()
	if private {
		// A private tracker's passkey would go to the service with the
		// torrent, and most private trackers ban an account for that.
		b.refuse(ctx, taskID, r, "a torrent from a private tracker stays with the built-in torrent client")
		return "", false
	}
	sel := b.selection(taskID)
	keep, err := b.rules(taskID)
	src.Choose = selectsSome(sel) || len(sel) == 0 && (keep != nil || err != nil)
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Remote: &core.RemoteFetch{}})
	// A pause or a removal does not cut the add short: the service may have
	// made the job already, and only its id lets this instance claim the job
	// and then keep it or delete it. The client's own timeout still applies.
	job, held, err := b.svc.AddTorrent(context.WithoutCancel(ctx), src)
	if err != nil {
		var no *Refusal
		if errors.As(err, &no) {
			b.refuse(ctx, taskID, r, no.Reason)
		} else {
			b.fail(ctx, taskID, err)
		}
		return "", false
	}
	if b.Added != nil {
		b.Added(job)
	}
	b.runs.mu.Lock()
	kept := b.runs.byTask[taskID] == r
	if kept {
		r.job, r.held = job, held
	}
	u := core.Update{Job: b.jobLocked(r)}
	b.runs.mu.Unlock()
	if !kept {
		// Removed while the service took the torrent in.
		if !held {
			b.deleteJob(job)
		}
		return "", false
	}
	// A paused task keeps the job, and its resume carries on with it.
	if ctx.Err() != nil {
		b.onUpdate(taskID, u)
		return "", false
	}
	u.Status, u.Remote = core.StatusRunning, &core.RemoteFetch{}
	b.onUpdate(taskID, u)
	return job, true
}

// await polls the job until the service has every file, showing its progress
// on the task. It reports false when the task has been dealt with.
func (b *TorrentBackend) await(ctx context.Context, taskID string, r *torrentRun, job string) (TorrentJob, bool) {
	misses := 0
	for {
		tj, err := b.svc.TorrentStatus(ctx, job)
		if ctx.Err() != nil {
			return TorrentJob{}, false
		}
		var no *Refusal
		switch {
		case errors.As(err, &no):
			b.refuse(ctx, taskID, r, no.Reason)
			return TorrentJob{}, false
		case err != nil:
			misses++
			if misses >= statusMisses {
				b.fail(ctx, taskID, err)
				return TorrentJob{}, false
			}
		case tj.State == TorrentReady:
			return tj, true
		case tj.State == TorrentFailed:
			b.refuse(ctx, taskID, r, cmp.Or(tj.Reason, "the service gave up on this torrent"))
			return TorrentJob{}, false
		case tj.State == TorrentChoosing:
			misses = 0
			if !b.choose(ctx, taskID, r, job, tj) {
				return TorrentJob{}, false
			}
		default:
			misses = 0
			b.onUpdate(taskID, core.Update{
				Status: core.StatusRunning,
				Name:   tj.Name,
				Size:   b.remoteSize(taskID, tj),
				Remote: &core.RemoteFetch{Progress: clamp01(tj.Progress), Speed: tj.Speed, Seeds: tj.Seeds},
			})
		}
		select {
		case <-ctx.Done():
			return TorrentJob{}, false
		case <-time.After(b.poll):
		}
	}
}

// choose tells a service that waits for it which files to fetch. It reports
// false when the task has been dealt with.
func (b *TorrentBackend) choose(ctx context.Context, taskID string, r *torrentRun, job string, tj TorrentJob) bool {
	b.runs.mu.Lock()
	chosen, name := r.chosen, cmp.Or(tj.Name, r.name)
	b.runs.mu.Unlock()
	if chosen {
		return true
	}
	fs, ok := b.svc.(FileSelector)
	if !ok {
		b.fail(ctx, taskID, fmt.Errorf("%s waits for a file selection this client cannot send", b.svc.Label()))
		return false
	}
	want, err := b.wanted(taskID, name, tj.Files)
	if err != nil {
		b.refuse(ctx, taskID, r, err.Error())
		return false
	}
	if err := fs.SelectFiles(ctx, job, want); err != nil {
		var no *Refusal
		if errors.As(err, &no) {
			b.refuse(ctx, taskID, r, no.Reason)
		} else {
			b.fail(ctx, taskID, err)
		}
		return false
	}
	b.runs.mu.Lock()
	r.chosen = true
	b.runs.mu.Unlock()
	return true
}

// fetch hands one file to the engine and waits for it.
func (b *TorrentBackend) fetch(ctx context.Context, taskID string, r *torrentRun, job string, i int, name string, multi bool, f TorrentFile, before int64) (string, error) {
	d, err := b.svc.FileURL(ctx, job, f)
	if err != nil {
		return "", err
	}
	rel, err := localPath(name, multi, f.Path, d.Name)
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%s/%d", taskID, i)
	b.runs.mu.Lock()
	again := slices.Contains(r.parts, id)
	b.runs.mu.Unlock()
	if again {
		// What a failed attempt wrote of this file goes first, or the engine
		// would write this one beside it under another name.
		b.parts.Remove(id, true)
	}
	b.runs.mu.Lock()
	if !b.live(ctx, taskID, r) {
		b.runs.mu.Unlock()
		return "", ctx.Err()
	}
	r.part = id
	if !again {
		r.parts = append(r.parts, id)
	}
	conns, leftover := r.conns, r.partial
	b.runs.mu.Unlock()
	defer func() {
		b.runs.mu.Lock()
		if r.part == id {
			r.part = ""
		}
		b.runs.mu.Unlock()
	}()
	return b.parts.FetchPart(ctx, Part{
		TaskID: taskID,
		ID:     id,
		URL:    d.URL,
		Path:   rel,
		Size:   f.Size,
		Conns:  conns,
		Relink: func(ctx context.Context) (string, error) {
			d, err := b.svc.FileURL(ctx, job, f)
			return d.URL, err
		},
		Leftover: leftover,
		Progress: func(loaded, speed int64, file string) {
			u := core.Update{Status: core.StatusRunning, Loaded: before + loaded, Speed: speed}
			b.runs.mu.Lock()
			if file != "" && file != r.partial && r.part == id {
				r.partial = file
				u.Job = b.jobLocked(r)
			}
			b.runs.mu.Unlock()
			b.onUpdate(taskID, u)
		},
	})
}

// finish reports the task done and, unless the torrent is to stay on the
// service or was the account's before, deletes it there.
func (b *TorrentBackend) finish(ctx context.Context, taskID string, r *torrentRun, job string, loaded int64, multi bool, file string) {
	b.runs.mu.Lock()
	if !b.live(ctx, taskID, r) {
		b.runs.mu.Unlock()
		return
	}
	r.done = true
	r.settle()
	r.job = ""
	held := r.held
	b.runs.mu.Unlock()
	if !held && (b.Keep == nil || !b.Keep()) {
		b.deleteJob(job)
	}
	u := core.Update{Status: core.StatusDone, Loaded: loaded, Job: &core.ServiceJob{}}
	if !multi {
		u.File = file
	}
	b.onUpdate(taskID, u)
}

// refuse hands the task to the next backend: the service will not fetch this
// torrent, so waiting and asking again would not help. The job, if there is
// one, is of no further use. An imported download has no next backend, so it
// fails where it is and stays on the account for the user to look at.
func (b *TorrentBackend) refuse(ctx context.Context, taskID string, r *torrentRun, reason string) {
	b.runs.mu.Lock()
	if !b.live(ctx, taskID, r) {
		b.runs.mu.Unlock()
		return
	}
	if r.imported {
		b.runs.mu.Unlock()
		b.fail(ctx, taskID, errors.New(reason))
		return
	}
	job, ours := r.job, r.ours()
	r.job = ""
	r.settle()
	b.runs.mu.Unlock()
	if job != "" && ours {
		b.deleteJob(job)
	}
	log.Printf("task %s: %s will not fetch this torrent: %s", taskID, b.svc.Label(), reason)
	b.onUpdate(taskID, core.Update{
		Status:      core.StatusError,
		Err:         b.svc.ID() + ": " + reason,
		Reason:      core.ReasonUnsupported,
		Unsupported: true,
		Job:         &core.ServiceJob{},
	})
}

// fail reports err on the task, unless the attempt was called off: Pause and
// Remove set the task's state themselves. The run keeps its job and the files
// already here, so a resume or a retry carries on from where it failed.
func (b *TorrentBackend) fail(ctx context.Context, taskID string, err error) {
	b.runs.mu.Lock()
	if ctx.Err() != nil {
		b.runs.mu.Unlock()
		return
	}
	if r := b.runs.byTask[taskID]; r != nil {
		r.settle()
	}
	b.runs.mu.Unlock()
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: b.svc.ID() + ": " + err.Error()})
}

func (b *TorrentBackend) selection(taskID string) []core.TorrentFile {
	if b.Files == nil {
		return nil
	}
	return b.Files(taskID)
}

func (b *TorrentBackend) rules(taskID string) (func(string, int64) bool, error) {
	if b.Rules == nil {
		return nil, nil
	}
	return b.Rules(taskID)
}

// wanted is the files of a job to fetch, in the job's order: those the task
// selected, or all of them when it selected nothing in particular. A task
// nobody chose files for goes by its file rules. root is the torrent's own
// folder, which some services put in front of every path.
func (b *TorrentBackend) wanted(taskID, root string, files []TorrentFile) ([]TorrentFile, error) {
	sel := b.selection(taskID)
	if len(sel) == 0 {
		return b.byRules(taskID, root, files)
	}
	if !selectsSome(sel) {
		return files, nil
	}
	var out []TorrentFile
	for _, f := range files {
		if pickedIn(sel, root, f.Path) {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the service lists none of the files selected for this torrent")
	}
	return out, nil
}

// byRules is the files the task's file rules keep, or every file when it has
// none or they would keep none, as the built-in client picks (see
// torrent.Picker.Pick). The rules read a path inside the torrent, so the
// torrent's own folder comes off where the service puts it in front.
func (b *TorrentBackend) byRules(taskID, root string, files []TorrentFile) ([]TorrentFile, error) {
	keep, err := b.rules(taskID)
	if err != nil || keep == nil {
		return files, err
	}
	var out []TorrentFile
	for _, f := range files {
		p := strings.TrimPrefix(strings.ReplaceAll(f.Path, `\`, "/"), "/")
		if rest, ok := strings.CutPrefix(p, root+"/"); ok && root != "" {
			p = rest
		}
		// A file the service could not place in the torrent is fetched, as in
		// pickedIn.
		if p == "" || keep(p, f.Size) {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return files, nil
	}
	return out, nil
}

// selectsSome reports whether a selection leaves any file out. One that
// selects everything, or nothing is known about, fetches the whole torrent.
func selectsSome(sel []core.TorrentFile) bool {
	for _, f := range sel {
		if !f.Selected {
			return true
		}
	}
	return false
}

// pickedIn reports whether the service's path for a file is one the task
// selected. The task's paths start inside the torrent's own folder, root. A
// service states a path with or without a leading slash and with or without
// that folder, so the folder comes off once where the path as it stands names
// no file of the task.
func pickedIn(sel []core.TorrentFile, root, p string) bool {
	p = strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), "/")
	if p == "" {
		// A file the service could not place in the torrent, such as
		// Real-Debrid packing several into one archive, is fetched whatever
		// the selection says.
		return true
	}
	at := func(p string) int {
		return slices.IndexFunc(sel, func(f core.TorrentFile) bool { return f.Path == p })
	}
	i := at(p)
	if rest, ok := strings.CutPrefix(p, root+"/"); i < 0 && ok && root != "" {
		i = at(rest)
	}
	return i >= 0 && sel[i].Selected
}

// remoteSize is the size to show while the service fetches: what the task
// fetches of it, once the service lists the files, else the whole torrent.
func (b *TorrentBackend) remoteSize(taskID string, tj TorrentJob) int64 {
	want, err := b.wanted(taskID, tj.Name, tj.Files)
	if err != nil || len(tj.Files) == 0 || len(want) == len(tj.Files) {
		return tj.Size
	}
	var n int64
	for _, f := range want {
		n += f.Size
	}
	return n
}

// localPath is where one file goes inside the task's folder: a multi-file
// torrent in a folder of its own name, as the built-in client writes it, and a
// single file as it is. The service's path comes from the torrent's author, so
// the caller still checks it stays inside the folder.
func localPath(name string, multi bool, p, fallback string) (string, error) {
	p = strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/")
	if p == "" {
		p = fallback
	}
	if p == "" {
		return "", errors.New("the service named no file")
	}
	if !multi {
		return path.Base(p), nil
	}
	folder := strings.TrimSpace(name)
	if folder == "" || strings.ContainsAny(folder, `/\`) || folder == "." || folder == ".." {
		return "", fmt.Errorf("the torrent's name %q is not a usable folder name", name)
	}
	return folder + "/" + strings.TrimPrefix(p, folder+"/"), nil
}

// sourceOf reads a link the torrent resolver accepted back into what a service
// takes, the torrent's own name, and whether it is private.
func sourceOf(link string) (src TorrentSource, name string, private bool, err error) {
	md, err := (torrent.Resolver{}).Describe(link)
	if err != nil {
		return TorrentSource{}, "", false, err
	}
	name = cmp.Or(md.Name, md.InfoHash)
	private = torrent.Private(link, md)
	if torrent.IsMagnet(link) {
		return TorrentSource{Magnet: link, InfoHash: md.InfoHash}, name, private, nil
	}
	b, err := torrent.DecodeBytes(link)
	if err != nil {
		return TorrentSource{}, "", false, err
	}
	return TorrentSource{File: b, InfoHash: md.InfoHash}, name, private, nil
}

func clamp01(f float64) float64 {
	return min(max(f, 0), 1)
}
