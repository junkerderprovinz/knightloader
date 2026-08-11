// Package engine wraps the embedded Gopeed download library and reports task
// updates back to the app. Gopeed fetches bytes; the app owns state and UI.
package engine

import (
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

type Engine struct {
	d   *download.Downloader
	dir string

	mu       sync.Mutex
	toKL     map[string]string // gopeed task id -> KL task id
	toGopeed map[string]string // KL task id -> gopeed task id
	torrents map[string]bool   // KL task id -> this one is a torrent

	onUpdate func(taskID string, u core.Update)

	// done is closed by Close, and wg counts every goroutine this engine
	// started. Together they are what makes Close mean it: before they existed
	// Start's own goroutine could still be mid-resolve while the downloader
	// underneath was being torn down, and the torrent stats poller would have
	// kept reading a closed library forever.
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	pollOnce  sync.Once

	// metadataTimeout overrides how long a magnet may wait for its file list.
	// Zero means defaultMetadataTimeout; a test sets it short.
	metadataTimeout time.Duration
}

// SetMetadataTimeout caps how long a magnet may spend waiting for the swarm to
// send its file list. Zero restores the built-in default.
func (e *Engine) SetMetadataTimeout(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metadataTimeout = d
}

// New boots an embedded Gopeed downloader that saves into dir and reports
// per-task changes through onUpdate.
func New(dir string, onUpdate func(taskID string, u core.Update)) (*Engine, error) {
	cfg := (&download.DownloaderConfig{
		RefreshInterval: 500, // ms between progress events
		DownloaderStoreConfig: &base.DownloaderStoreConfig{
			DownloadDir: dir,
			MaxRunning:  5,
		},
	}).Init()
	d := download.NewDownloader(cfg)
	if err := d.Setup(); err != nil {
		return nil, err
	}
	// Setup reloads the stored config, overriding the values above. Raise
	// Gopeed's internal concurrency cap afterwards so the app-level scheduler
	// (global + per-host slots) is the only authority on what runs.
	if sc, err := d.GetConfig(); err == nil && sc.MaxRunning < 64 {
		sc.MaxRunning = 64
		_ = d.PutConfig(sc)
	}
	e := &Engine{
		d:        d,
		dir:      dir,
		toKL:     map[string]string{},
		toGopeed: map[string]string{},
		torrents: map[string]bool{},
		onUpdate: onUpdate,
		done:     make(chan struct{}),
	}
	d.Listener(e.onEvent)
	return e, nil
}

// UseProxy routes every engine download through a proxy. KnightLoader points
// this at its own loopback proxy, which is where the speed limit is applied —
// the download library itself offers no rate-limit hook.
func (e *Engine) UseProxy(hostPort string) error {
	cfg, err := e.d.GetConfig()
	if err != nil {
		return err
	}
	if hostPort == "" {
		cfg.Proxy = &base.DownloaderProxyConfig{}
	} else {
		host, _, splitErr := net.SplitHostPort(hostPort)
		if splitErr != nil || host == "" {
			return fmt.Errorf("engine: bad proxy address %q", hostPort)
		}
		cfg.Proxy = &base.DownloaderProxyConfig{
			Enable: true,
			Scheme: "http",
			Host:   hostPort,
		}
	}
	return e.d.PutConfig(cfg)
}

// Close stops every goroutine this engine started and then shuts the download
// library down, in that order.
//
// THE ORDER IS THE POINT. Waiting first means nothing of ours is still calling
// into the library when it is torn down. The one goroutine that cannot be
// waited for is the bare resolve inside resolveTorrent, which is parked in the
// torrent client with no way to be interrupted - what releases that one is the
// d.Close() below, and its own caller has already stopped listening for it.
// It is idempotent because the app shuts down from more than one place - a
// second call must not close an already-closed channel or hand the download
// library a second Close.
//
// THE WAIT IS BOUNDED, and the bound is not laziness. Every goroutine started
// here watches e.done and leaves promptly, except the one thing that cannot:
// a resolve already inside the download library, which takes no context. A
// shutdown that hung on one slow host would be a worse failure than the race
// this wait exists to close, so after closeGrace the library is shut down
// anyway - which is also what releases the resolve.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		close(e.done)
		waited := make(chan struct{})
		go func() {
			e.wg.Wait()
			close(waited)
		}()
		select {
		case <-waited:
		case <-time.After(closeGrace):
		}
		e.closeErr = e.d.Close()
	})
	return e.closeErr
}

// closeGrace is how long Close waits for its own goroutines before shutting the
// download library down underneath whatever is left.
const closeGrace = 10 * time.Second

// Download resolves the direct URL (to learn name+size), then starts a task.
// It runs async so the caller (an HTTP handler) never blocks on the network.
//
// This is the shape every download backend shares, and it deliberately carries
// no collision policy. Only this engine can be told the name it must write; a
// task handed to headless JD or TorBox is fetched in another process that names
// the file itself. Widening the shared shape would put a decision on every
// backend that all but one of them would drop on the floor.
func (e *Engine) Download(taskID, url string, headers map[string]string, conns int) {
	e.DownloadTo(taskID, url, headers, conns, "")
}

// DownloadTo is Download with an explicit destination; an empty dir falls back
// to the engine's default. It routes nothing: the download goes out the way
// every download went out before connections could be named, which is over the
// loopback proxy UseProxy configured.
func (e *Engine) DownloadTo(taskID, url string, headers map[string]string, conns int, dir string) {
	e.DownloadVia(taskID, url, headers, conns, dir, proxycfg.Route{})
}

// DownloadVia is DownloadTo carried on one named outbound connection.
//
// This is the whole of per-download routing, and it is one field on the request
// because that is all gopeed needs: setupFetcher resolves the request's own
// proxy ahead of the global one ("task request proxy config has higher
// priority"), so a per-download connection is stated where the download is
// stated. No dialer, no second proxy, no chain - and the small version is the
// right one to build, because the large one was sized against a belief about
// this library that turned out to be wrong.
//
// It has a cost, and the cost is real rather than theoretical: a request that
// names its own proxy no longer passes through the loopback proxy, and the
// loopback proxy is the only place this build can meter bytes. A routed download
// is therefore an unthrottled download until metering moves to a listener per
// task, which is the arrangement the bandwidth budget is designed around. Until
// then, routing wins over the speed limit for the downloads that are routed.
func (e *Engine) DownloadVia(taskID, url string, headers map[string]string, conns int, dir string, route proxycfg.Route) {
	e.Start(Job{TaskID: taskID, URL: url, Headers: headers, Conns: conns, Dir: dir, Route: route})
}

// Job is one download as the engine takes it.
//
// A struct rather than a longer parameter list, because the list had reached six
// and the two this wave adds are a policy and a cap - the kind that compile
// perfectly well in the wrong order.
type Job struct {
	TaskID  string
	URL     string
	Headers map[string]string
	Conns   int
	// Dir is where the file lands; empty means the engine's own folder.
	Dir   string
	Route proxycfg.Route

	// TorrentSelect names which files of a multi-file torrent to fetch, by
	// index in the resolved file list. Nil fetches all of them, which is what
	// the download library reads an empty selection as. Ignored for every job
	// whose URL is not a torrent.
	TorrentSelect []int
	// Trackers are extra announce URLs to add to a torrent. Ignored for a
	// private torrent by the library itself, which is the correct behaviour and
	// not something this side has to remember.
	Trackers []string

	// Collision is what to do when the resolved name is already taken. EMPTY
	// MEANS NO POLICY AT ALL, which is not what the collide package reads it as:
	// there an empty policy is its own default, Rename. The difference is
	// deliberate and it is why this is not passed straight through - the older
	// entry points above set no policy, and folding them onto a default would
	// give every caller that never asked for one a silent rename.
	Collision collide.Policy
	// MaxCollisionAttempts caps how many counted names a rename tries. Zero means
	// the collide package's own cap.
	MaxCollisionAttempts int
}

// Start resolves the URL (to learn the name, the size and the shape of what is
// on the other end), settles where the file lands, and then starts the task.
func (e *Engine) Start(j Job) {
	// Closed already. Every path below this ends in e.wg.Add(1) - the torrent
	// branch inside startTorrent, the plain one a few lines down - and Close
	// is, by the time this can even be reached, already past close(e.done) and
	// quite possibly already inside e.wg.Wait(). Add racing a Wait already in
	// progress is not "the task starts a little late", it is documented Go
	// runtime behaviour ("sync: WaitGroup misuse: Add called concurrently with
	// Wait") that can panic the whole process instead of losing one task - the
	// one failure mode here worse than the race it would be replacing. A
	// caller reaching Start after shutdown has begun gets the same terminal
	// update resolveTorrent's own e.done case already gives a magnet cut off
	// mid-resolve, rather than a task that silently never moves again.
	select {
	case <-e.done:
		e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: "shutting down"})
		return
	default:
	}
	if j.Dir == "" {
		j.Dir = e.dir
	}
	// A magnet or an uploaded .torrent goes down the torrent branch, and the
	// test for that is the URL itself rather than a flag on the Job.
	//
	// THE DISPATCHER IS NOT ASKED, on purpose. Which protocol a link belongs to
	// is already decided by its scheme, and the library underneath decides it
	// exactly this way (Downloader.parseFm walks its fetch managers' scheme
	// filters). Making the caller state it as well would be a second copy of
	// that decision, in a place with less information, that can disagree - and
	// the way it would disagree is a magnet dispatched as an HTTP job, which
	// resolves as a torrent anyway and skips every check below.
	if torrent.IsURI(j.URL) {
		e.startTorrent(j)
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		req := &base.Request{
			URL:   j.URL,
			Extra: &fhttp.ReqExtra{Method: "GET", Header: j.Headers},
			Proxy: requestProxy(j.Route),
		}
		opts := &base.Options{Path: j.Dir, Extra: &fhttp.OptsExtra{Connections: j.Conns}}
		rr, err := e.d.Resolve(req, opts)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		_, size := metaOf(rr.Res)
		// Settled before Create, because Create is what starts the transfer.
		name, err := place(j, rr.Res, opts)
		if err != nil {
			// The name goes out with the failure. It costs one field and it stops
			// the retry loop guessing: the app can only pre-empt a collision for a
			// task whose name it knows, and until this resolve it did not.
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Name: name, Err: err.Error()})
			return
		}
		e.emit(j.TaskID, core.Update{Status: core.StatusRunning, Name: name, Size: size})
		gid, err := e.d.Create(rr.ID)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		e.mu.Lock()
		e.toKL[gid] = j.TaskID
		e.toGopeed[j.TaskID] = gid
		e.mu.Unlock()
	}()
}

// place applies the collision policy to what was just resolved, writes the name
// the download must use into opts, and reports the name to show on the task.
//
// IT RUNS HERE AND NOT IN THE DISPATCHER because this is the first moment the
// real name exists. Before the resolve there is only whatever the resolver made
// of the URL, and a collision decided on a guessed name is a decision about a
// different file.
//
// opts is the very struct the download runs with: Downloader.Resolve keeps the
// pointer it was given and Create is called with no options of its own, so a
// name written here is the name the fetcher reads. It has also had the library's
// own path placeholders expanded into it by then, which is the other reason to
// be here - reserving against the folder we asked for rather than the one that
// came back would reserve in a folder nothing is written to.
func place(j Job, res *base.Resource, opts *base.Options) (string, error) {
	name, _ := metaOf(res)
	if j.Collision == "" || res == nil {
		return name, nil
	}
	if res.Name != "" {
		// A resource that carries a name of its own is a FOLDER, and Options.Name
		// then names that folder rather than a file in it. Handing it a file name
		// would apply the policy to the wrong thing entirely, and quietly: the
		// download would succeed, into a directory called "movie (2).mkv".
		return name, placeFolder(j, res, opts)
	}
	if len(res.Files) == 0 || res.Files[0].Name == "" {
		// Nothing was named, so there is nothing to reserve and no honest name to
		// invent. Left to the library, which is where the name is coming from.
		return name, nil
	}
	// Where the single file actually goes, which is not always opts.Path: the
	// library joins the file's own relative path in between.
	dir := filepath.Join(opts.Path, filepath.FromSlash(res.Files[0].Path))
	r, err := collide.Options{MaxAttempts: j.MaxCollisionAttempts}.
		Handover(filepath.Join(dir, res.Files[0].Name), j.Collision)
	if err != nil {
		return name, err
	}
	if r.Action == collide.Skipped {
		return filepath.Base(r.Path), fmt.Errorf("not downloaded: %s already exists", r.Path)
	}
	opts.Name = filepath.Base(r.Path)
	return opts.Name, nil
}

// placeFolder is place for a multi-file resource. The task's own name is left
// alone: it is the first file's, and moving the folder does not move that.
func placeFolder(j Job, res *base.Resource, opts *base.Options) error {
	r, err := collide.Options{MaxAttempts: j.MaxCollisionAttempts}.
		HandoverFolder(filepath.Join(opts.Path, res.Name), j.Collision)
	if err != nil {
		return err
	}
	if r.Action == collide.Skipped {
		return fmt.Errorf("not downloaded: %s already exists", r.Path)
	}
	opts.Name = filepath.Base(r.Path)
	return nil
}

// requestProxy is the route as gopeed reads it.
//
// A route with no proxy returns nil, and nil is the answer that matters here.
// gopeed reads nil as "follow the global config", which is the loopback proxy -
// so a download deliberately sent over the machine's own connection is still
// metered and still unproxied, which is exactly what the direct gateway means.
//
// The mode that looks right for it is the one that must never be used.
// RequestProxyModeNone does not mean "no upstream proxy", it means no proxy
// handler at all: it would take the download off the loopback proxy as well, and
// the speed limit would silently stop applying to every download somebody
// explicitly marked direct. The bug reads as "the limit does nothing", weeks
// later, on the one setting a user was deliberate about.
//
// Nothing here has to defend against socks4: proxycfg.Entry.Route refuses those
// kinds before a Route can exist, because this ends in http.ProxyURL and that
// has never spoken socks4.
func requestProxy(r proxycfg.Route) *base.RequestProxy {
	if !r.Proxied() {
		return nil
	}
	return &base.RequestProxy{
		Mode:   base.RequestProxyModeCustom,
		Scheme: r.Scheme,
		Host:   r.Host,
		Usr:    r.Username,
		Pwd:    r.Password,
	}
}

func (e *Engine) Pause(taskID string)  { e.filterOp(taskID, e.d.Pause) }
func (e *Engine) Resume(taskID string) { e.filterOp(taskID, e.d.Continue) }

// Remove drops the task. deleteFiles also erases what was already written —
// used for a restart (the partial must go), never for tidying the list.
func (e *Engine) Remove(taskID string, deleteFiles bool) {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	delete(e.toGopeed, taskID)
	delete(e.toKL, gid)
	delete(e.torrents, taskID)
	e.mu.Unlock()
	if gid != "" {
		_ = e.d.Delete(&download.TaskFilter{IDs: []string{gid}}, deleteFiles)
	}
}

func (e *Engine) filterOp(taskID string, op func(*download.TaskFilter) error) {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	e.mu.Unlock()
	if gid != "" {
		_ = op(&download.TaskFilter{IDs: []string{gid}})
	}
}

func (e *Engine) onEvent(ev *download.Event) {
	if ev.Task == nil {
		return
	}
	e.mu.Lock()
	taskID, ok := e.toKL[ev.Task.ID]
	e.mu.Unlock()
	if !ok {
		return
	}
	switch ev.Key {
	case download.EventKeyProgress:
		// A PROGRESS EVENT AFTER THE DONE EVENT IS NOT PROGRESS, and until
		// torrents existed there was no such thing so this branch could assume
		// otherwise. The download library's speed loop ticks for every task that
		// is "running OR uploading", and a torrent goes on uploading for hours
		// after it is done - so a finished torrent produces a progress event
		// twice a second, forever, and mapping each of them to StatusRunning
		// dragged the task back out of done immediately after it settled.
		//
		// The symptom was not subtle and it was still invisible without a live
		// run: the task showed as running with a complete file and a full
		// download bar, permanently. Downstream it is worse than cosmetic -
		// Wave 10's end-of-queue idle action would see work owed for as long as
		// anything seeds, which is exactly the outcome decision 4 of the torrent
		// spec exists to prevent.
		//
		// Dropped rather than translated, because the seeding phase already has
		// an owner: the stats poller, which reports peers, ratio and the end of
		// seeding on its own three-second tick.
		if ev.Task.Status == base.DownloadStatusDone {
			return
		}
		if pr := ev.Task.Progress; pr != nil {
			e.emit(taskID, core.Update{Status: core.StatusRunning, Loaded: pr.Downloaded, Speed: pr.Speed})
		}
	case download.EventKeyPause:
		e.emit(taskID, core.Update{Status: core.StatusPaused})
	case download.EventKeyDone:
		u := core.Update{Status: core.StatusDone}
		if pr := ev.Task.Progress; pr != nil {
			u.Loaded = pr.Downloaded
		}
		// The seeding flag travels WITH the done, not three seconds behind it on
		// the next poll. In that gap a torrent would be done and not seeding,
		// which is the one combination that means "finished, nothing owed" - and
		// the idle action fires on exactly that reading.
		if e.isTorrent(taskID) {
			if s, _, ok := e.readTorrentStats(ev.Task.ID); ok {
				u.Torrent = &s
			}
		}
		e.emit(taskID, u)
	case download.EventKeyError:
		// The failure leaves here as text and is given its typed reason in the
		// app. An Update carries no reason field on purpose: one classifier that
		// the engine, JD, yt-dlp and every debrid service pass through is what
		// makes a full disk read as a full disk whichever of them hit it.
		msg := "download error"
		if ev.Err != nil {
			msg = ev.Err.Error()
		}
		e.emit(taskID, core.Update{Status: core.StatusError, Err: msg})
	}
}

func (e *Engine) emit(taskID string, u core.Update) {
	if e.onUpdate != nil {
		e.onUpdate(taskID, u)
	}
}

func metaOf(res *base.Resource) (name string, size int64) {
	if res == nil {
		return "", 0
	}
	size = res.Size
	if len(res.Files) > 0 && res.Files[0].Name != "" {
		return res.Files[0].Name, size
	}
	return res.Name, size
}
