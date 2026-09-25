// Package engine wraps the embedded Gopeed download library and reports task
// updates back to the app. Gopeed fetches bytes; the app owns state and UI.
package engine

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
	"github.com/GopeedLab/gopeed/pkg/util"
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
	// reconnecting is every Reconnect under way, by gopeed task id, and
	// pausing the number of Pause calls under way.
	reconnecting map[string]*reconnect
	pausing      map[string]int
	// files is where each gopeed task writes, keyed by gopeed id because the
	// start event that carries it can arrive before Start has mapped the id.
	files map[string]string
	// jobs is each task's job as it was started, and mends the finished
	// transfers whose missing ranges are being fetched again (see mend.go).
	jobs    map[string]Job
	mends   map[string]*mend
	layouts *layoutStore

	onUpdate func(taskID string, u core.Update)

	// ctx ends when Close begins, which stops every mend under way.
	ctx  context.Context
	stop context.CancelFunc

	// done is closed by Close, and wg counts every goroutine this engine
	// started, so Close can wait for them before tearing the library down.
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	pollOnce  sync.Once
	// closed is set under mu by Close before wg.Wait runs; see Start.
	closed bool

	// metadataTimeout overrides how long a magnet may wait for its file list.
	// Zero means defaultMetadataTimeout.
	metadataTimeout time.Duration

	// cfgMu serialises updateConfig, so two setters cannot lose each other's
	// change.
	cfgMu sync.Mutex
}

// updateConfig hands gopeed a changed copy of its config, so a resolve reading
// the one in use never has its maps written under it. change reports whether
// it changed anything; when it did not, nothing is stored, since gopeed swaps
// the config pointer without a lock.
func (e *Engine) updateConfig(change func(cfg *base.DownloaderStoreConfig) bool) error {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()
	cur, err := e.d.GetConfig()
	if err != nil {
		return err
	}
	next := *cur
	next.ProtocolConfig = maps.Clone(cur.ProtocolConfig)
	if !change(&next) {
		return nil
	}
	return e.d.PutConfig(&next)
}

// SetMetadataTimeout caps how long a magnet may wait for the swarm to send
// its file list. Zero restores the default.
func (e *Engine) SetMetadataTimeout(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metadataTimeout = d
}

// newDownloaderMu is held around download.NewDownloader, which sets a zerolog
// package variable on every call, so two engines built at once would race.
var newDownloaderMu sync.Mutex

// New boots an embedded Gopeed downloader that saves into dir and reports
// per-task changes through onUpdate.
func New(dir string, onUpdate func(taskID string, u core.Update)) (*Engine, error) {
	layouts := &layoutStore{Storage: download.NewMemStorage(), want: map[string][]byte{}}
	cfg := (&download.DownloaderConfig{
		RefreshInterval: 500, // ms between progress events
		Storage:         layouts,
		DownloaderStoreConfig: &base.DownloaderStoreConfig{
			DownloadDir: dir,
			MaxRunning:  5,
		},
	}).Init()
	newDownloaderMu.Lock()
	d := download.NewDownloader(cfg)
	newDownloaderMu.Unlock()
	if err := d.Setup(); err != nil {
		return nil, err
	}
	// Setup reloads the stored config, overriding the values above. Raise
	// Gopeed's own concurrency cap afterwards so the app's scheduler alone
	// decides what runs.
	if sc, err := d.GetConfig(); err == nil && sc.MaxRunning < 64 {
		sc.MaxRunning = 64
		_ = d.PutConfig(sc)
	}
	e := &Engine{
		d:            d,
		dir:          dir,
		toKL:         map[string]string{},
		toGopeed:     map[string]string{},
		torrents:     map[string]bool{},
		reconnecting: map[string]*reconnect{},
		pausing:      map[string]int{},
		files:        map[string]string{},
		jobs:         map[string]Job{},
		mends:        map[string]*mend{},
		layouts:      layouts,
		onUpdate:     onUpdate,
		done:         make(chan struct{}),
	}
	e.ctx, e.stop = context.WithCancel(context.Background())
	d.Listener(e.onEvent)
	return e, nil
}

// UseProxy routes every engine download through a proxy. KnightLoader points
// it at its own loopback proxy, which applies the speed limit, since the
// library has no rate-limit hook.
func (e *Engine) UseProxy(hostPort string) error {
	proxy := &base.DownloaderProxyConfig{}
	if hostPort != "" {
		host, _, splitErr := net.SplitHostPort(hostPort)
		if splitErr != nil || host == "" {
			return fmt.Errorf("engine: bad proxy address %q", hostPort)
		}
		proxy = &base.DownloaderProxyConfig{Enable: true, Scheme: "http", Host: hostPort}
	}
	return e.updateConfig(func(cfg *base.DownloaderStoreConfig) bool {
		if cfg.Proxy != nil && *cfg.Proxy == *proxy {
			return false
		}
		cfg.Proxy = proxy
		return true
	})
}

// btProtocolConfig mirrors gopeed's unexported bt config with identical json
// tags. ProtocolConfig["bt"] round-trips through JSON (util.MapToStruct), so
// the mirror reads and writes it correctly.
type btProtocolConfig struct {
	ListenPort int      `json:"listenPort"`
	Trackers   []string `json:"trackers"`
	SeedKeep   bool     `json:"seedKeep"`
	SeedRatio  float64  `json:"seedRatio"`
	SeedTime   int64    `json:"seedTime"`
}

// SetTorrentConfig writes the listen port and seeding targets into gopeed's
// bt protocol config; Trackers and SeedKeep are passed through unchanged.
// Call it at boot and on every settings save.
//
// The seeding targets reach every torrent started afterwards, since gopeed
// reads the config per task. The port does not: gopeed builds its shared
// torrent client once, on the first torrent of the process, so a new port
// only applies if no torrent has started yet.
func (e *Engine) SetTorrentConfig(port int, seedRatio float64, seedDurationSeconds int) error {
	var decodeErr error
	err := e.updateConfig(func(cfg *base.DownloaderStoreConfig) bool {
		var bt btProtocolConfig
		if decodeErr = util.MapToStruct(cfg.ProtocolConfig["bt"], &bt); decodeErr != nil {
			return false
		}
		if bt.ListenPort == port && bt.SeedRatio == seedRatio && bt.SeedTime == int64(seedDurationSeconds) {
			return false
		}
		bt.ListenPort = port
		bt.SeedRatio = seedRatio
		bt.SeedTime = int64(seedDurationSeconds)
		if cfg.ProtocolConfig == nil {
			cfg.ProtocolConfig = map[string]any{}
		}
		cfg.ProtocolConfig["bt"] = bt
		return true
	})
	return cmp.Or(decodeErr, err)
}

// Close stops every goroutine this engine started and then shuts the download
// library down, in that order, so nothing of ours is calling into it while it
// is torn down. It is idempotent, since the app shuts down from more than one
// place.
//
// The wait is capped at closeGrace. A resolve already inside the library
// takes no context and cannot be interrupted; closing the library is what
// releases it, and a shutdown that hung on one slow host would be worse.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.mu.Unlock()
		e.stop()
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

// closeGrace is how long Close waits for its own goroutines before shutting
// the download library down anyway.
const closeGrace = 10 * time.Second

// Download starts url into the engine's own folder, with no collision policy.
// It is what the app's backend interface asks of every backend; the app
// starts its own engine jobs with Start.
func (e *Engine) Download(taskID, url string, headers map[string]string, conns int) {
	e.Start(Job{TaskID: taskID, URL: url, Headers: headers, Conns: conns})
}

// Job is one download as the engine takes it.
type Job struct {
	TaskID  string
	URL     string
	Headers map[string]string
	Conns   int
	// Dir is where the file lands; empty means the engine's own folder.
	Dir string
	// WorkDir is where the bytes are written while they arrive, when the
	// caller keeps that apart from Dir; empty writes straight into Dir.
	//
	// With a working folder no .part file appears at the destination, and the
	// collision policy is not applied here: the working folder is the wrong
	// place to decide a name, and whoever moves the file into Dir applies it
	// (see internal/workdir). Moving the file is the caller's job, since only
	// it knows whether a checksum or an extraction still needs the file where
	// it is.
	WorkDir string
	// Route is the outbound connection the download goes over. A route with a
	// proxy of its own bypasses the loopback proxy, which is the only place
	// bytes are metered, so a routed download is not throttled.
	Route proxycfg.Route

	// TorrentSelect names which files of a multi-file torrent to fetch, by
	// index in the resolved file list. Nil fetches all of them, or what
	// FileRules choose.
	TorrentSelect []int
	// FileRules choose the files of a torrent whose TorrentSelect is nil: an
	// upload's from its own file list, a magnet's once the swarm has sent it.
	FileRules torrent.FileRules
	// Trackers are extra announce URLs for a torrent. The library leaves them
	// out for a private .torrent file, but adds them to a magnet before its
	// metadata can say it is private, so for a magnet the caller decides.
	Trackers []string

	// Collision is what to do when the resolved name is taken. Empty means no
	// policy at all, unlike collide, where empty means Rename; the older entry
	// points set none and must not get a silent rename.
	Collision collide.Policy
	// MaxCollisionAttempts caps how many counted names a rename tries. Zero
	// means collide's own cap.
	MaxCollisionAttempts int

	// Relink asks whoever handed over URL for a fresh link to the same file,
	// for the ranges a link that stopped working part way left missing (see
	// mend.go). Nil asks URL again.
	Relink func(ctx context.Context) (string, error)
}

// writeDir is the folder this job's bytes are written into.
func (j Job) writeDir() string {
	if j.WorkDir != "" {
		return j.WorkDir
	}
	return j.Dir
}

// placed reports whether this job's collision policy is decided here; see
// Job.WorkDir.
func (j Job) placed() bool { return j.Collision != "" && j.WorkDir == "" }

// Start resolves the URL to learn the name, size and kind of resource,
// settles where the file lands, and starts the task.
func (e *Engine) Start(j Job) {
	// Checking closed and calling wg.Add under one lock keeps Close's Wait
	// from starting between the two, which panics with "WaitGroup misuse".
	// The window was hit under the race detector with a live torrent swarm.
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: "shutting down"})
		return
	}
	e.wg.Add(1)
	e.mu.Unlock()

	if j.Dir == "" {
		j.Dir = e.dir
	}
	// The scheme decides the protocol, as it does inside the library, so the
	// caller does not state it a second time.
	if torrent.IsURI(j.URL) {
		e.startTorrent(j)
		return
	}
	go func() {
		defer e.wg.Done()
		req := &base.Request{
			URL:   j.URL,
			Extra: &fhttp.ReqExtra{Method: "GET", Header: j.Headers},
			Proxy: requestProxy(j.Route),
		}
		opts := &base.Options{Path: j.writeDir(), Extra: &fhttp.OptsExtra{Connections: j.Conns}}
		rr, err := e.d.Resolve(req, opts)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		_, size := metaOf(rr.Res)
		// Settled before Create, which starts the transfer.
		name, err := place(j, rr.Res, opts)
		if err != nil {
			// Report the name with the failure, so the app can pre-empt the
			// collision on a retry.
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
		e.jobs[j.TaskID] = j
		e.mu.Unlock()
	}()
}

// place applies the collision policy to the resolved resource, writes the
// chosen name into opts and returns the name to show. It runs here because
// the resolve is the first moment the real name exists, and not at all for a
// job with a working folder.
//
// opts is the struct the download runs with: Resolve keeps the pointer and
// Create takes no options, so a name set here is what the fetcher uses. Its
// path placeholders have been expanded by then, so the reservation happens in
// the folder that is actually written to.
func place(j Job, res *base.Resource, opts *base.Options) (string, error) {
	name, _ := metaOf(res)
	if !j.placed() || res == nil {
		return name, nil
	}
	if res.Name != "" {
		// A resource with its own name is a folder, and Options.Name then
		// names the folder. A file name there would produce a directory
		// called "movie (2).mkv".
		return name, placeFolder(j, res, opts)
	}
	if len(res.Files) == 0 || res.Files[0].Name == "" {
		// Nothing named, so nothing to reserve; the library picks the name.
		return name, nil
	}
	// The library joins the file's relative path onto opts.Path.
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

// placeFolder is place for a multi-file resource. The task keeps the first
// file's name.
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

// requestProxy is the route as gopeed reads it. A route without a proxy
// returns nil, which gopeed reads as "use the global config", the metered
// loopback proxy.
//
// RequestProxyModeNone looks right for a direct route but removes the proxy
// handler altogether, which would silently drop the speed limit. socks4 needs
// no check here, since proxycfg refuses it before a Route exists.
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

// Pause waits for a Reconnect of the same task to finish first, or the resume
// half of it would undo this pause. No Reconnect starts while it runs: one that
// began after the look would pause the task first, leave this pause nothing to
// do, and then resume it.
func (e *Engine) Pause(taskID string) {
	if e.pauseMend(taskID) {
		return
	}
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	if gid == "" {
		e.mu.Unlock()
		return
	}
	r := e.reconnecting[gid]
	e.pausing[gid]++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.pausing[gid]--
		if e.pausing[gid] == 0 {
			delete(e.pausing, gid)
		}
		e.mu.Unlock()
	}()
	if r != nil {
		<-r.done
	}
	_ = e.d.Pause(&download.TaskFilter{IDs: []string{gid}})
}

func (e *Engine) Resume(taskID string) {
	if e.resumeMend(taskID) {
		return
	}
	e.filterOp(taskID, e.d.Continue)
}

// reconnect is one Reconnect under way. paused and started are closed when
// the library reports that step done, done when Reconnect returns.
type reconnect struct {
	paused, started, done chan struct{}
}

// closeOnce closes ch unless it is closed already, and reports whether it did.
// The caller holds e.mu, which makes the check and the close one step.
func closeOnce(ch chan struct{}) bool {
	select {
	case <-ch:
		return false
	default:
		close(ch)
		return true
	}
}

// reconnectWait bounds each of Reconnect's two waits. Either step only waits
// for the transfer's own goroutines, so this is for a library that has lost
// track of one, not a delay anybody should see.
const reconnectWait = 30 * time.Second

// Reconnectable reports whether the engine is fetching the task over HTTP,
// which is what Reconnect can pick up again.
func (e *Engine) Reconnectable(taskID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.toGopeed[taskID] != "" && !e.torrents[taskID]
}

// Reconnect drops the task's connections and opens new ones that ask for the
// rest of the file, keeping every byte already written unless the server can
// only send the whole file. It is a pause and a resume, and the app hears of
// neither. It reports false when there is nothing to reconnect: the engine is
// not fetching the task over HTTP, or the task has stopped or is being paused.
//
// The library pauses and resumes on goroutines of its own, and whichever of
// two gets there second wins. A resume overtaken by its own pause leaves a
// transfer that never moves again while the library calls it running, so
// every step waits for the one before it to land.
func (e *Engine) Reconnect(taskID string) bool {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	if gid == "" || e.torrents[taskID] || e.closed || e.reconnecting[gid] != nil || e.pausing[gid] > 0 {
		e.mu.Unlock()
		return false
	}
	r := &reconnect{paused: make(chan struct{}), started: make(chan struct{}), done: make(chan struct{})}
	e.reconnecting[gid] = r
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.reconnecting, gid)
		e.mu.Unlock()
		close(r.done)
	}()

	landed := func(step chan struct{}) bool {
		select {
		case <-step:
			return true
		case <-time.After(reconnectWait):
			return true
		case <-e.done:
			return false
		}
	}
	if e.d.Pause(&download.TaskFilter{IDs: []string{gid}}) != nil || !landed(r.paused) {
		return false
	}
	if e.d.Continue(&download.TaskFilter{IDs: []string{gid}}) != nil {
		return false
	}
	return landed(r.started)
}

// Remove drops the task. deleteFiles also erases what was already written,
// which a restart needs; tidying the list does not.
func (e *Engine) Remove(taskID string, deleteFiles bool) {
	e.dropMend(taskID)
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	delete(e.toGopeed, taskID)
	delete(e.toKL, gid)
	delete(e.torrents, taskID)
	delete(e.files, gid)
	delete(e.jobs, taskID)
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
	if ev.Key == download.EventKeyStart {
		if p := settledFile(ev.Task); p != "" {
			e.mu.Lock()
			e.files[ev.Task.ID] = p
			e.mu.Unlock()
		}
		return
	}
	e.mu.Lock()
	taskID, ok := e.toKL[ev.Task.ID]
	// The pause Reconnect makes is not a pause of the task, so it is not
	// passed on.
	var own bool
	if r := e.reconnecting[ev.Task.ID]; r != nil {
		switch ev.Key {
		case download.EventKeyPause:
			own = closeOnce(r.paused)
		case download.EventKeyStart:
			closeOnce(r.started)
		}
	}
	file := e.files[ev.Task.ID]
	e.mu.Unlock()
	if !ok || own {
		return
	}
	switch ev.Key {
	case download.EventKeyProgress:
		// A finished torrent keeps uploading, and the library's speed loop
		// sends progress for it twice a second. Treating that as running would
		// pull the task out of done, and the idle action would never fire. The
		// stats poller reports the seeding phase instead.
		if ev.Task.Status == base.DownloadStatusDone {
			return
		}
		if pr := ev.Task.Progress; pr != nil {
			e.emit(taskID, core.Update{Status: core.StatusRunning, Loaded: pr.Downloaded, Speed: pr.Speed, File: file})
		}
	case download.EventKeyPause:
		e.emit(taskID, core.Update{Status: core.StatusPaused, File: file})
	case download.EventKeyDone:
		// A short transfer can finish before its start event is out.
		u := core.Update{Status: core.StatusDone, File: cmp.Or(file, settledFile(ev.Task))}
		if gaps, short := e.missing(ev.Task); short {
			e.startMend(taskID, ev.Task, u.File, gaps)
			return
		}
		if pr := ev.Task.Progress; pr != nil {
			u.Loaded = pr.Downloaded
		}
		// The seeding flag goes out with done rather than on the next poll;
		// in between, done-and-not-seeding would read as "nothing owed" to the
		// idle action.
		if e.isTorrent(taskID) {
			if s, _, ok := e.readTorrentStats(ev.Task.ID); ok {
				u.Torrent = &s
			}
		}
		e.emit(taskID, u)
	case download.EventKeyError:
		// The app classifies the message, the same way for every backend.
		msg := "download error"
		if ev.Err != nil {
			msg = ev.Err.Error()
		}
		// With the file, so a restart before the next attempt still knows
		// which leftover is this task's.
		e.emit(taskID, core.Update{Status: core.StatusError, Err: msg, File: file})
	}
}

func (e *Engine) emit(taskID string, u core.Update) {
	if e.onUpdate != nil {
		e.onUpdate(taskID, u)
	}
}

// settledFile is where the library writes a plain HTTP download, with the name
// it settled on. Without a collision policy it renames around any file already
// there, and this is the only place that name can be read, so a task that kept
// the resolved name would otherwise point at somebody else's file. It is empty
// for torrents and folder resources, which are not one file.
//
// It is read from the start event, which the library sends on the goroutine
// that wrote the name, and from the done event, which comes after the transfer
// that started once the name was written. A progress event can come before.
func settledFile(t *download.Task) string {
	m := t.Meta
	if t.Protocol != "http" || m.Res.Name != "" || len(m.Res.Files) == 0 {
		return ""
	}
	f := m.Res.Files[0]
	return filepath.Join(m.Opts.Path, filepath.FromSlash(f.Path), cmp.Or(m.Opts.Name, f.Name))
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
