package engine

// Carrying a paused HTTP transfer on after a restart. The library keeps its
// tasks in memory, so a download paused before a restart would start over and
// write its file again from the first byte. Every pause of a single-file
// transfer fetched in ranges is written to a file of its own in the engine's
// state folder: the library's task and the connection layout it saves, which
// is what the library's own restore reads. At boot they go back into the
// library, which holds them as paused tasks, and the next Start of the same
// task carries on with the bytes on disk once the server is seen to still send
// that file in ranges.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// taskLabel is the request label naming the task a library task belongs to,
// which is how a record finds its task again after the restart.
const taskLabel = "knightloader.task"

// savedTaskBucket is the library's name for the bucket it keeps its tasks in.
const savedTaskBucket = "task"

// keptTransfers writes paused transfers to dir and reads them back at boot.
type keptTransfers struct {
	dir string

	mu     sync.Mutex
	closed bool
	// saves is the connection state the library last saved for each of its
	// tasks, owner the task each record on disk belongs to, both by library
	// id, and byTask the library id of every task's record.
	saves  map[string]any
	owner  map[string]string
	byTask map[string]string
	// restoring is the connection state of each transfer handed back at
	// boot, until the library reads it.
	restoring map[string]json.RawMessage
	// removed holds the library id of every task the library deleted. Its
	// pause saves the task on a goroutine of its own, which can land after
	// the delete and would write the record again.
	removed map[string]bool
}

// keptRecord is one paused transfer as it lies on disk.
type keptRecord struct {
	Task *download.Task  `json:"task"`
	Save json.RawMessage `json:"save"`
}

func newKeptTransfers(dir string) *keptTransfers {
	return &keptTransfers{
		dir:       dir,
		saves:     map[string]any{},
		owner:     map[string]string{},
		byTask:    map[string]string{},
		restoring: map[string]json.RawMessage{},
		removed:   map[string]bool{},
	}
}

// load hands every record in the folder to the library's store, before the
// library reads it, and returns the library id of each task's transfer. A file
// that is no record, such as a write cut off by a crash, is deleted.
func (k *keptTransfers) load(into download.Storage) (map[string]string, error) {
	if err := os.MkdirAll(k.dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(k.dir)
	if err != nil {
		return nil, err
	}
	if err := into.Setup([]string{savedTaskBucket}); err != nil {
		return nil, err
	}
	for _, ent := range entries {
		path := filepath.Join(k.dir, ent.Name())
		gid, isRecord := strings.CutSuffix(ent.Name(), ".json")
		var rec keptRecord
		if isRecord {
			raw, err := os.ReadFile(path)
			isRecord = err == nil && json.Unmarshal(raw, &rec) == nil && rec.Task != nil && taskOf(rec.Task) != ""
		}
		if !isRecord {
			_ = os.Remove(path)
			continue
		}
		if err := into.Put(savedTaskBucket, gid, rec.Task); err != nil {
			return nil, err
		}
		id := taskOf(rec.Task)
		k.restoring[gid] = rec.Save
		k.owner[gid] = id
		k.byTask[id] = gid
	}
	return maps.Clone(k.byTask), nil
}

func (k *keptTransfers) put(bucket, gid string, v any) {
	switch bucket {
	case savedLayoutBucket:
		k.mu.Lock()
		if !k.removed[gid] {
			k.saves[gid] = v
		}
		k.mu.Unlock()
	case savedTaskBucket:
		if t, ok := v.(*download.Task); ok {
			k.note(gid, t)
		}
	}
}

// note brings the record of library task gid in step with t as the library
// saves it. The library saves under the task's lock, so t holds still. A pause
// is written; a finished transfer, or one that cannot carry on, loses its
// record. Anything else keeps the record of its last pause, which claims no
// byte that is not on disk.
func (k *keptTransfers) note(gid string, t *download.Task) {
	id := taskOf(t)
	if id == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed || k.removed[gid] {
		return
	}
	if old := k.byTask[id]; old != "" && old != gid {
		// A new transfer of the task, which writes its file from the start.
		k.dropLocked(old)
	}
	switch {
	case !resumable(t) || t.Status == base.DownloadStatusDone:
		k.dropLocked(gid)
	case t.Status == base.DownloadStatusPause:
		if err := k.writeLocked(gid, id, t); err != nil {
			log.Printf("could not keep the paused download for after a restart: %v (task %s)", err, id)
		}
	}
}

// writeLocked writes t with its last saved connection layout. The headers, the
// proxy and the further sources stay out of it: they can hold a login, or work
// for anybody who has them, and the start that carries the transfer on sets
// the headers and the proxy again. Caller holds k.mu.
func (k *keptTransfers) writeLocked(gid, id string, t *download.Task) error {
	save, err := json.Marshal(k.saves[gid])
	if err != nil {
		return err
	}
	bare := *t
	meta := *t.Meta
	meta.Req = &base.Request{URL: t.Meta.Req.URL, Labels: t.Meta.Req.Labels}
	meta.Opts = ownShare(t.Meta.Req, t.Meta.Opts)
	bare.Meta = &meta
	raw, err := json.Marshal(keptRecord{Task: &bare, Save: save})
	if err != nil {
		return err
	}
	if err := k.storeLocked(gid, raw); err != nil {
		return err
	}
	k.owner[gid] = id
	k.byTask[id] = gid
	return nil
}

// storeLocked replaces the record of library task gid with raw, in one rename
// so a crash leaves the old record or the new one. Caller holds k.mu.
func (k *keptTransfers) storeLocked(gid string, raw []byte) error {
	f, err := os.CreateTemp(k.dir, gid+"-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(k.dir, gid+".json"))
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// moved points the record of library task gid, if it has one, at path, the
// folder its file was moved to. The library saves a paused task only when it
// pauses, so the record would otherwise send the next boot to the old folder.
func (k *keptTransfers) moved(gid, path string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(k.dir, gid+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var rec keptRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return err
	}
	rec.Task.Meta.Opts.Path = path
	if raw, err = json.Marshal(rec); err != nil {
		return err
	}
	return k.storeLocked(gid, raw)
}

// ownShare is opts with the connections of req's own link alone. Start
// multiplies the count by the sources, and a transfer carried on without them
// would open all of those connections to its own link.
func ownShare(req *base.Request, opts *base.Options) *base.Options {
	rx, ok := req.Extra.(*fhttp.ReqExtra)
	if !ok || len(rx.Mirrors) == 0 {
		return opts
	}
	ox, ok := opts.Extra.(*fhttp.OptsExtra)
	if !ok {
		return opts
	}
	n := 1 + len(rx.Mirrors)
	own, extra := *opts, *ox
	extra.Connections = max(1, (ox.Connections+n-1)/n)
	own.Extra = &extra
	return &own
}

// dropLocked deletes the record of library task gid. Caller holds k.mu.
func (k *keptTransfers) dropLocked(gid string) {
	id, ok := k.owner[gid]
	if !ok {
		return
	}
	delete(k.owner, gid)
	if k.byTask[id] == gid {
		delete(k.byTask, id)
	}
	if err := os.Remove(filepath.Join(k.dir, gid+".json")); err != nil && !os.IsNotExist(err) {
		log.Printf("could not delete the record of a paused download: %v (task %s)", err, id)
	}
}

func (k *keptTransfers) delete(bucket, gid string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	switch bucket {
	case savedLayoutBucket:
		delete(k.saves, gid)
		delete(k.restoring, gid)
	case savedTaskBucket:
		if !k.closed {
			k.removed[gid] = true
			k.dropLocked(gid)
		}
	}
}

// pop reads the connection layout of a transfer handed back at boot into v,
// and reports whether gid is such a transfer.
func (k *keptTransfers) pop(gid string, v any) (bool, error) {
	k.mu.Lock()
	raw, ok := k.restoring[gid]
	delete(k.restoring, gid)
	k.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

// close stops all writing. The library still saves the pauses of its shutdown
// after that, and a write then would race the process's exit.
func (k *keptTransfers) close() {
	k.mu.Lock()
	k.closed = true
	k.mu.Unlock()
}

func (s *layoutStore) Pop(bucket, key string, v any) error {
	if s.kept != nil && bucket == savedLayoutBucket {
		if ok, err := s.kept.pop(key, v); ok {
			return err
		}
	}
	return s.Storage.Pop(bucket, key, v)
}

func (s *layoutStore) Delete(bucket, key string) error {
	if s.kept != nil {
		s.kept.delete(bucket, key)
	}
	return s.Storage.Delete(bucket, key)
}

func (s *layoutStore) Close() error {
	if s.kept != nil {
		s.kept.close()
	}
	return s.Storage.Close()
}

// taskOf is the task library task t belongs to, empty when it carries none.
func taskOf(t *download.Task) string {
	if t.Meta == nil || t.Meta.Req == nil {
		return ""
	}
	return t.Meta.Req.Labels[taskLabel]
}

// resumable reports whether t is a transfer the library can carry on from its
// saved layout: one file of a known size, fetched in ranges.
func resumable(t *download.Task) bool {
	if t.Protocol != "http" || t.Meta == nil {
		return false
	}
	res := t.Meta.Res
	return res != nil && res.Name == "" && len(res.Files) == 1 && res.Range && res.Size > 0
}

// heldFile is the file of a transfer handed back at boot, while it is still
// the whole preallocated file the transfer was writing.
func heldFile(t *download.Task) (string, bool) {
	if t == nil || t.Status != base.DownloadStatusPause || !resumable(t) {
		return "", false
	}
	file := settledFile(t)
	fi, err := os.Lstat(file)
	return file, err == nil && fi.Mode().IsRegular() && fi.Size() == t.Meta.Res.Size
}

// Resumes reports whether the next Start of the task carries on writing file,
// with a transfer of it from before a restart. The caller leaves the file where
// it is then; if the server does not send it in ranges, the start deletes
// it before it begins again.
func (e *Engine) Resumes(taskID, file string) bool {
	e.mu.Lock()
	gid := e.restored[taskID]
	e.mu.Unlock()
	if gid == "" || file == "" {
		return false
	}
	held, ok := heldFile(e.d.GetTask(gid))
	return ok && filepath.Clean(held) == filepath.Clean(file)
}

// Holds reports whether the task has a transfer from before a restart that its
// next Start carries on with. A retry then goes through Start again, rather
// than through Remove, which deletes the bytes.
func (e *Engine) Holds(taskID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.restored[taskID] != ""
}

// PruneRestored lets go of every transfer handed back at boot whose task known
// does not report, leaving its file where it is.
func (e *Engine) PruneRestored(known func(taskID string) bool) {
	var gone []string
	e.mu.Lock()
	for id, gid := range e.restored {
		if !known(id) {
			gone = append(gone, gid)
			delete(e.restored, id)
		}
	}
	e.mu.Unlock()
	if len(gone) > 0 {
		_ = e.d.Delete(&download.TaskFilter{IDs: gone}, false)
	}
}

// takeUp carries on with the transfer of j's task from before the restart, and
// reports whether the start is over: carried on, failed with the transfer kept
// for the next start, or ended by a pause or a removal meanwhile. Otherwise the
// old transfer is gone, with its file if that was still the transfer's, and the
// start begins afresh.
func (e *Engine) takeUp(j Job, s *start) bool {
	e.mu.Lock()
	gid := e.restored[j.TaskID]
	e.mu.Unlock()
	if gid == "" {
		return false
	}
	t := e.d.GetTask(gid)
	file, ours := heldFile(t)
	if ours && filepath.Clean(t.Meta.Opts.Path) == filepath.Clean(j.writeDir()) {
		err := e.servesRest(&j, t)
		var other otherFile
		switch {
		case err == nil:
			if !e.proceed(s, j) {
				return true
			}
			e.mu.Lock()
			delete(e.restored, j.TaskID)
			e.toKL[gid] = j.TaskID
			e.toGopeed[j.TaskID] = gid
			e.jobs[j.TaskID] = j
			e.mu.Unlock()
			e.carryOn(j, gid, t, file)
			return true
		case !errors.As(err, &other):
			if e.proceed(s, j) {
				e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: e.failure(j, err.Error()), File: file})
			}
			return true
		}
		log.Printf("%s starts over: %v (task %s)", file, err, j.TaskID)
	}
	e.mu.Lock()
	delete(e.restored, j.TaskID)
	e.mu.Unlock()
	_ = e.d.Delete(&download.TaskFilter{IDs: []string{gid}}, ours)
	return false
}

// carryOn points the library task gid at j's link and lets it go on.
func (e *Engine) carryOn(j Job, gid string, t *download.Task, file string) {
	proxy := requestProxy(j.Route)
	if proxy == nil {
		proxy = &base.RequestProxy{Mode: base.RequestProxyModeFollow}
	}
	req := &base.Request{URL: j.URL, Extra: &fhttp.ReqExtra{Method: http.MethodGet, Header: j.Headers}, Proxy: proxy}
	var loaded int64
	if t.Progress != nil {
		loaded = t.Progress.Downloaded
	}
	e.emit(j.TaskID, core.Update{Status: core.StatusRunning, Name: filepath.Base(file), Size: t.Meta.Res.Size, Loaded: loaded, File: file})
	err := e.d.Patch(gid, req, nil)
	if err == nil {
		err = e.d.Continue(&download.TaskFilter{IDs: []string{gid}})
	}
	if err != nil {
		e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error(), File: file})
	}
}

// otherFile is an answer that settles that the bytes on disk are of no use:
// the server sends the file only whole, or it is not the file the transfer
// began with. Any other failure of the probe may pass, and leaves the bytes.
type otherFile string

func (o otherFile) Error() string { return string(o) }

// servesRest checks that j's link still sends t's file in ranges, asking as
// KnightLoader when the server hangs up on the agent j names, as resolve does.
// j then keeps that agent for the transfer's own requests.
func (e *Engine) servesRest(j *Job, t *download.Task) error {
	err := e.probeRest(*j, t)
	if err == nil || !httpx.HungUp(err) || hasUserAgent(j.Headers) {
		return err
	}
	ours := asKnightLoader(*j)
	err = e.probeRest(ours, t)
	switch {
	case err == nil:
		*j = ours
	case httpx.HungUp(err):
		err = hungUp(err)
	}
	return err
}

// probeRest asks j's link for the first byte of t's file.
func (e *Engine) probeRest(j Job, t *download.Task) error {
	ctx, cancel := context.WithTimeout(e.ctx, mendIdle)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return err
	}
	e.setHeaders(req, j.Headers)
	req.Header.Set("Range", "bytes=0-0")
	client := e.mendClient(j)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	size := t.Meta.Res.Size
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		return otherFile("the server sends this file only from the start")
	case http.StatusRequestedRangeNotSatisfiable:
		return otherFile(fmt.Sprintf("the file on the server is not the %s this download began with", mib(size)))
	default:
		return fmt.Errorf("the server answered a request for part of the file with HTTP %d", resp.StatusCode)
	}
	if start, total, ok := contentRange(resp.Header.Get("Content-Range")); !ok || start != 0 || total != size {
		return otherFile(fmt.Sprintf("the file on the server is not the %s this download began with", mib(size)))
	}
	// Another link may come from another server with its own dates, as in
	// fetch.
	if j.URL == t.Meta.Req.URL {
		if ct := t.Meta.Res.Files[0].Ctime; ct != nil && !ct.IsZero() {
			if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil && !lm.Equal(*ct) {
				return otherFile("the file on the server has changed since this download began")
			}
		}
	}
	return nil
}
