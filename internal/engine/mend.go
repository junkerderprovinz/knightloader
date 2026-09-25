package engine

// Mending a transfer the library calls finished while part of the file never
// arrived.
//
// The library gives up on a connection that is answered 403, taking it for a
// server that allows no more connections, and when the rest have finished it
// leaves that connection's range out of the count and reports the task done.
// A debrid link that expires part way through ends like that, with a hole in
// the file that surfaces much later as a damaged archive. So a finished HTTP
// transfer is held against its size, and a short one is not reported done:
// the ranges nobody fetched are asked for again, from a fresh link where the
// backend that gave the URL can get one.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
	"github.com/GopeedLab/gopeed/pkg/util"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// mendTries is how often the missing ranges are asked for before the task
// fails: from the URL the transfer had, then from fresh ones.
const mendTries = 3

// mendIdle is how long a request for a missing range may send nothing before
// it is given up.
const mendIdle = 30 * time.Second

// mendWait is the pause before the same URL is asked again when the backend
// has no fresh one to give. A var so the tests do not sit it out.
var mendWait = 2 * time.Second

// span is a byte range of a file, both ends included.
type span struct{ from, to int64 }

func (s span) len() int64 { return s.to - s.from + 1 }

// mend is one finished transfer whose missing ranges are being fetched again.
type mend struct {
	job  Job
	url  string
	file string
	size int64
	// gaps is what is still missing, in file order. It shrinks under e.mu as
	// bytes land, so a pause keeps what was fetched.
	gaps []span
	// cancel stops the run under way and is nil when none is; ended is closed
	// when that run has returned.
	cancel context.CancelFunc
	ended  chan struct{}
}

func (m *mend) missing() int64 {
	var n int64
	for _, g := range m.gaps {
		n += g.len()
	}
	return n
}

// layoutStore is the library's task store that can also hand out the
// connection layout the library saves for an HTTP task: which range each
// connection was given and how much of it arrived. That is the only place the
// library says where a file's gaps are.
type layoutStore struct {
	download.Storage

	mu   sync.Mutex
	want map[string][]byte
}

// savedLayoutBucket is the library's name for the bucket it saves a task's
// connection state in.
const savedLayoutBucket = "save"

func (s *layoutStore) Put(bucket, key string, v any) error {
	if bucket == savedLayoutBucket {
		s.mu.Lock()
		if _, asked := s.want[key]; asked {
			s.want[key], _ = json.Marshal(v)
		}
		s.mu.Unlock()
	}
	return s.Storage.Put(bucket, key, v)
}

// layout has save make the library store task gid's layout and returns it as
// stored. The library reads the connections without a lock while it stores
// them, so the transfer must have ended.
func (s *layoutStore) layout(gid string, save func() error) ([]byte, bool) {
	s.mu.Lock()
	s.want[gid] = nil
	s.mu.Unlock()
	err := save()
	s.mu.Lock()
	raw := s.want[gid]
	delete(s.want, gid)
	s.mu.Unlock()
	return raw, err == nil && raw != nil
}

// savedLayout mirrors the part of the library's saved connection state read
// here. The library's type is internal; these are exported fields it saves as
// they are.
type savedLayout struct {
	Connections []struct {
		Chunk *struct {
			Begin, End, Downloaded int64
		}
		Downloaded int64
	}
}

// gapsIn is what a saved layout leaves unwritten. A ranged transfer splits the
// file between its connections, and only ever hands on the part of a range
// that has not arrived, so what each connection had not fetched of its range,
// from Begin+Downloaded to End, is together all that is missing. A transfer
// without ranges has one connection that wrote from the start.
func gapsIn(raw []byte, size int64, ranged bool) []span {
	var l savedLayout
	if json.Unmarshal(raw, &l) != nil {
		return nil
	}
	var gaps []span
	for _, c := range l.Connections {
		switch {
		case !ranged:
			if c.Downloaded < size {
				gaps = append(gaps, span{c.Downloaded, size - 1})
			}
		case c.Chunk != nil:
			if from, to := c.Chunk.Begin+c.Chunk.Downloaded, min(c.Chunk.End, size-1); from <= to {
				gaps = append(gaps, span{from, to})
			}
		}
	}
	slices.SortFunc(gaps, func(x, y span) int { return cmp.Compare(x.from, y.from) })
	var out []span
	for _, g := range gaps {
		if n := len(out); n > 0 && g.from <= out[n-1].to+1 {
			out[n-1].to = max(out[n-1].to, g.to)
			continue
		}
		out = append(out, g)
	}
	return out
}

// missing reports the ranges of a finished single-file HTTP transfer that
// were never written, and whether any bytes are missing at all. The library's
// progress is no help, since it is set to the size on done; what its
// connections fetched is.
func (e *Engine) missing(t *download.Task) ([]span, bool) {
	res := t.Meta.Res
	if t.Protocol != "http" || res == nil || res.Name != "" || res.Size <= 0 {
		return nil, false
	}
	st, err := e.d.Stats(t.ID)
	stats, ok := st.(*fhttp.Stats)
	// No connections at all is a file that arrived whole while it was being
	// resolved.
	if err != nil || !ok || len(stats.Connections) == 0 {
		return nil, false
	}
	var fetched int64
	for _, c := range stats.Connections {
		fetched += c.Downloaded
	}
	if fetched >= res.Size {
		return nil, false
	}
	// A patch that changes nothing still makes the library save the layout.
	raw, ok := e.layouts.layout(t.ID, func() error { return e.d.Patch(t.ID, &base.Request{}, nil) })
	if !ok {
		return nil, true
	}
	return gapsIn(raw, res.Size, res.Range), true
}

// startMend takes over a transfer the library finished short.
func (e *Engine) startMend(taskID string, t *download.Task, file string, gaps []span) {
	size := t.Meta.Res.Size
	if len(gaps) == 0 {
		e.emit(taskID, core.Update{Status: core.StatusError, File: file, Err: fmt.Sprintf(
			"the download stopped short of its full %s, and the download library did not say which part is missing", mib(size))})
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	j := e.jobs[taskID]
	m := &mend{job: j, url: j.URL, file: file, size: size, gaps: gaps}
	e.mends[taskID] = m
	log.Printf("%s of %s never arrived (%d ranges); asking for it again (task %s)", mib(m.missing()), file, len(gaps), taskID)
	e.runMendLocked(taskID, m)
}

// runMendLocked starts a run of m. Caller holds e.mu.
func (e *Engine) runMendLocked(taskID string, m *mend) {
	ctx, cancel := context.WithCancel(e.ctx)
	m.cancel = cancel
	m.ended = make(chan struct{})
	e.wg.Add(1)
	go e.mendRun(ctx, taskID, m, m.ended)
}

func (e *Engine) mendRun(ctx context.Context, taskID string, m *mend, ended chan struct{}) {
	defer e.wg.Done()
	defer close(ended)
	e.mu.Lock()
	loaded := m.size - m.missing()
	e.mu.Unlock()
	e.emit(taskID, core.Update{Status: core.StatusRunning, Loaded: loaded, File: m.file})

	err := e.fill(ctx, taskID, m)

	e.mu.Lock()
	stopped := ctx.Err() != nil
	m.cancel = nil
	if !stopped && e.mends[taskID] == m {
		delete(e.mends, taskID)
	}
	short := m.missing()
	e.mu.Unlock()
	switch {
	case stopped:
		// Pause, Remove and Close stopped it, and report the task themselves.
	case err != nil:
		e.emit(taskID, core.Update{Status: core.StatusError, File: m.file, Err: fmt.Sprintf(
			"the server stopped sending part of the file: %s never arrived, and asking for it again failed: %v", mib(short), err)})
	default:
		e.emit(taskID, core.Update{Status: core.StatusDone, Loaded: m.size, File: m.file})
	}
}

// fill fetches m's gaps, from the URL the transfer had and then from fresh
// ones the job's Relink hands out.
func (e *Engine) fill(ctx context.Context, taskID string, m *mend) error {
	f, err := os.OpenFile(m.file, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	client := e.mendClient(m.job)
	defer client.CloseIdleConnections()
	var last error
	for try := range mendTries {
		if try > 0 {
			url, err := e.nextURL(ctx, m)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				last = err
				continue
			}
			e.mu.Lock()
			m.url = url
			e.mu.Unlock()
		}
		last = e.fetchGaps(ctx, client, f, taskID, m)
		if last == nil || ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return last
}

// fetchGaps asks m's current URL for every gap still open, and stops at the
// first that fails.
func (e *Engine) fetchGaps(ctx context.Context, client *http.Client, f *os.File, taskID string, m *mend) error {
	e.mu.Lock()
	url, gaps := m.url, slices.Clone(m.gaps)
	e.mu.Unlock()
	for _, g := range gaps {
		if err := e.fetch(ctx, client, f, url, taskID, m, g); err != nil {
			return err
		}
	}
	return nil
}

// nextURL is a fresh link from the job's backend, or the same URL again after
// a moment when the backend has none to give: a 403 that meant too many
// connections is gone once the others have finished.
func (e *Engine) nextURL(ctx context.Context, m *mend) (string, error) {
	if m.job.Relink != nil {
		return m.job.Relink(ctx)
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(mendWait):
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return m.url, nil
}

// fetch asks url for one gap and writes what arrives into f, taking the bytes
// off m's gaps as they land and reporting the progress.
func (e *Engine) fetch(ctx context.Context, client *http.Client, f *os.File, url, taskID string, m *mend, g span) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range m.job.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		if ua := e.userAgent(); ua != "" {
			req.Header.Set("User-Agent", ua)
		}
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", g.from, g.to))
	// Given up when nothing arrives for a while; a server can take a request
	// and then say nothing.
	idle := time.AfterFunc(mendIdle, cancel)
	defer idle.Stop()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent:
		if start, ok := rangeStart(resp.Header.Get("Content-Range")); !ok || start != g.from {
			return fmt.Errorf("the server answered with a different part of the file (%s)", resp.Header.Get("Content-Range"))
		}
	case resp.StatusCode == http.StatusOK && g.from == 0:
	case resp.StatusCode == http.StatusOK:
		return errors.New("the server can only send the whole file")
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 32<<10)
	body := io.LimitReader(resp.Body, g.len())
	off := g.from
	e.mu.Lock()
	lastLoaded := m.size - m.missing()
	e.mu.Unlock()
	lastReport := time.Now()
	for off <= g.to {
		n, err := body.Read(buf)
		if n > 0 {
			idle.Reset(mendIdle)
			if _, werr := f.WriteAt(buf[:n], off); werr != nil {
				return werr
			}
			off += int64(n)
			e.mu.Lock()
			m.gaps = landed(m.gaps, span{off - int64(n), off - 1})
			loaded := m.size - m.missing()
			e.mu.Unlock()
			if since := time.Since(lastReport); since >= 500*time.Millisecond {
				speed := int64(float64(loaded-lastLoaded) / since.Seconds())
				e.emit(taskID, core.Update{Status: core.StatusRunning, Loaded: loaded, Speed: speed, File: m.file})
				lastReport, lastLoaded = time.Now(), loaded
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if off <= g.to {
		return fmt.Errorf("the server's answer ended %s early", mib(g.to-off+1))
	}
	return nil
}

// landed takes a written range off gaps.
func landed(gaps []span, w span) []span {
	var out []span
	for _, g := range gaps {
		if w.to < g.from || w.from > g.to {
			out = append(out, g)
			continue
		}
		if g.from < w.from {
			out = append(out, span{g.from, w.from - 1})
		}
		if g.to > w.to {
			out = append(out, span{w.to + 1, g.to})
		}
	}
	return out
}

// rangeStart reads where a "bytes a-b/c" Content-Range starts.
func rangeStart(v string) (int64, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(v), "bytes ")
	if !ok {
		return 0, false
	}
	from, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(from, 10, 64)
	return n, err == nil
}

// mendClient goes out the way the library would for this job: over the job's
// own proxy, else the global one, which is the loopback proxy that meters.
func (e *Engine) mendClient(j Job) *http.Client {
	proxy := requestProxy(j.Route).ToHandler()
	if proxy == nil {
		if cfg, err := e.d.GetConfig(); err == nil {
			proxy = cfg.Proxy.ToHandler()
		}
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:               proxy,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second,
	}}
}

// userAgent is the one the library sends, from its HTTP protocol config.
func (e *Engine) userAgent() string {
	cfg, err := e.d.GetConfig()
	if err != nil {
		return ""
	}
	var c struct {
		UserAgent string `json:"userAgent"`
	}
	if util.MapToStruct(cfg.ProtocolConfig["http"], &c) != nil {
		return ""
	}
	return c.UserAgent
}

// pauseMend stops a mend under way and reports whether taskID had one. What
// was fetched stays, and resumeMend carries on from there.
func (e *Engine) pauseMend(taskID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.mends[taskID]
	if m == nil {
		return false
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	return true
}

// resumeMend starts a paused mend again, once its last run has wound down, and
// reports whether taskID had one.
func (e *Engine) resumeMend(taskID string) bool {
	e.mu.Lock()
	m := e.mends[taskID]
	if m == nil || m.cancel != nil {
		e.mu.Unlock()
		return m != nil
	}
	ended := m.ended
	e.mu.Unlock()
	<-ended
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mends[taskID] == m && m.cancel == nil && !e.closed {
		e.runMendLocked(taskID, m)
	}
	return true
}

// dropMend stops and forgets taskID's mend, and waits for its run to stop
// writing, so the file can go.
func (e *Engine) dropMend(taskID string) {
	e.mu.Lock()
	m := e.mends[taskID]
	delete(e.mends, taskID)
	if m != nil && m.cancel != nil {
		m.cancel()
	}
	e.mu.Unlock()
	if m != nil {
		<-m.ended
	}
}

// mib is n in mebibytes for a sentence.
func mib(n int64) string {
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}
