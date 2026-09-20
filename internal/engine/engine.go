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

	onUpdate func(taskID string, u core.Update)

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
}

// SetMetadataTimeout caps how long a magnet may wait for the swarm to send
// its file list. Zero restores the default.
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
	// Gopeed's own concurrency cap afterwards so the app's scheduler alone
	// decides what runs.
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
// it at its own loopback proxy, which applies the speed limit, since the
// library has no rate-limit hook.
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
	cfg, err := e.d.GetConfig()
	if err != nil {
		return err
	}
	var bt btProtocolConfig
	if err := util.MapToStruct(cfg.ProtocolConfig["bt"], &bt); err != nil {
		return err
	}
	bt.ListenPort = port
	bt.SeedRatio = seedRatio
	bt.SeedTime = int64(seedDurationSeconds)
	cfg.ProtocolConfig["bt"] = bt
	return e.d.PutConfig(cfg)
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

// Download resolves the direct URL (to learn name and size), then starts a
// task. It runs async so the caller never blocks on the network. It takes no
// collision policy, since other backends name the file in another process.
func (e *Engine) Download(taskID, url string, headers map[string]string, conns int) {
	e.DownloadTo(taskID, url, headers, conns, "")
}

// DownloadTo is Download with an explicit destination; an empty dir falls
// back to the engine's default. It goes out over the loopback proxy.
func (e *Engine) DownloadTo(taskID, url string, headers map[string]string, conns int, dir string) {
	e.DownloadVia(taskID, url, headers, conns, dir, proxycfg.Route{})
}

// DownloadVia is DownloadTo over one named outbound connection. Gopeed
// prefers a request's own proxy over the global one, so the route is one
// field on the request.
//
// A request with its own proxy bypasses the loopback proxy, which is the only
// place bytes are metered, so a routed download is not throttled.
func (e *Engine) DownloadVia(taskID, url string, headers map[string]string, conns int, dir string, route proxycfg.Route) {
	e.Start(Job{TaskID: taskID, URL: url, Headers: headers, Conns: conns, Dir: dir, Route: route})
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
	Route   proxycfg.Route

	// TorrentSelect names which files of a multi-file torrent to fetch, by
	// index in the resolved file list. Nil fetches all of them.
	TorrentSelect []int
	// Trackers are extra announce URLs for a torrent. The library ignores
	// them for a private torrent.
	Trackers []string

	// Collision is what to do when the resolved name is taken. Empty means no
	// policy at all, unlike collide, where empty means Rename; the older entry
	// points set none and must not get a silent rename.
	Collision collide.Policy
	// MaxCollisionAttempts caps how many counted names a rename tries. Zero
	// means collide's own cap.
	MaxCollisionAttempts int
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

func (e *Engine) Pause(taskID string)  { e.filterOp(taskID, e.d.Pause) }
func (e *Engine) Resume(taskID string) { e.filterOp(taskID, e.d.Continue) }

// Remove drops the task. deleteFiles also erases what was already written,
// which a restart needs; tidying the list does not.
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
		// A finished torrent keeps uploading, and the library's speed loop
		// sends progress for it twice a second. Treating that as running would
		// pull the task out of done, and the idle action would never fire. The
		// stats poller reports the seeding phase instead.
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
