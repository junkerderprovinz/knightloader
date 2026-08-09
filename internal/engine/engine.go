// Package engine wraps the embedded Gopeed download library and reports task
// updates back to the app. Gopeed fetches bytes; the app owns state and UI.
package engine

import (
	"fmt"
	"net"
	"sync"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

type Engine struct {
	d   *download.Downloader
	dir string

	mu       sync.Mutex
	toKL     map[string]string // gopeed task id -> KL task id
	toGopeed map[string]string // KL task id -> gopeed task id

	onUpdate func(taskID string, u core.Update)
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
		onUpdate: onUpdate,
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

func (e *Engine) Close() error { return e.d.Close() }

// Download resolves the direct URL (to learn name+size), then starts a task.
// It runs async so the caller (an HTTP handler) never blocks on the network.
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
	if dir == "" {
		dir = e.dir
	}
	go func() {
		req := &base.Request{
			URL:   url,
			Extra: &fhttp.ReqExtra{Method: "GET", Header: headers},
			Proxy: requestProxy(route),
		}
		opts := &base.Options{Path: dir, Extra: &fhttp.OptsExtra{Connections: conns}}
		rr, err := e.d.Resolve(req, opts)
		if err != nil {
			e.emit(taskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		name, size := metaOf(rr.Res)
		e.emit(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: size})
		gid, err := e.d.Create(rr.ID)
		if err != nil {
			e.emit(taskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		e.mu.Lock()
		e.toKL[gid] = taskID
		e.toGopeed[taskID] = gid
		e.mu.Unlock()
	}()
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
