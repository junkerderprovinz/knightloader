package api

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// watchTask is the part of a task the phone's background watch compares
// between two looks. Progress is left out, so the list stays the same while
// downloads only move bytes, and the failure fields come only with a failed
// task, which is the one place a notification words them.
type watchTask struct {
	ID     string      `json:"id"`
	Status core.Status `json:"status"`
	// Name is the link while the task has no name yet.
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Enabled bool   `json:"enabled"`
	// Retrying is a failure with an automatic retry still to come, which is
	// not the end of the download yet.
	Retrying bool `json:"retrying,omitempty"`
	// Stalled and Remote mark a running task that moves no bytes here: one that
	// stood still past the stall timeout, and a torrent a debrid service is
	// still fetching onto its own servers.
	Stalled bool `json:"stalled,omitempty"`
	Remote  bool `json:"remote,omitempty"`

	Error        string            `json:"error,omitempty"`
	ErrorCode    core.ErrorCode    `json:"errorCode,omitempty"`
	ErrorParams  map[string]string `json:"errorParams,omitempty"`
	Reason       core.Reason       `json:"reason,omitempty"`
	RejectCode   string            `json:"rejectCode,omitempty"`
	RejectParams map[string]string `json:"rejectParams,omitempty"`
	Dir          string            `json:"dir,omitempty"`
	Resolver     string            `json:"resolver,omitempty"`
}

func watchTaskOf(t *core.Task) watchTask {
	w := watchTask{
		ID:       t.ID,
		Status:   t.Status,
		Name:     t.Name,
		Package:  t.Package,
		Enabled:  t.Enabled,
		Retrying: t.Status == core.StatusError && !t.NextTry.IsZero(),
		Stalled:  t.Status == core.StatusRunning && !t.StalledSince.IsZero(),
		Remote:   t.Status == core.StatusRunning && t.Remote != nil,
	}
	if w.Name == "" {
		w.Name = t.URL
	}
	if t.Status == core.StatusError {
		w.Error, w.ErrorCode, w.ErrorParams, w.Reason = t.Error, t.ErrorCode, t.ErrorParams, t.Reason
		w.RejectCode, w.RejectParams = t.RejectCode, t.RejectParams
		w.Dir, w.Resolver = t.Dir, t.Resolver
	}
	return w
}

// watchList answers a look. Same says the tasks are as they were under the tag
// the caller sent, and then they are left out.
type watchList struct {
	Tag   string      `json:"tag"`
	Same  bool        `json:"same,omitempty"`
	Tasks []watchTask `json:"tasks,omitzero"`
}

// watchTag names one state of the list. It is a query argument rather than an
// ETag because the relay carries no request headers.
func watchTag(tasks []watchTask) string {
	b, _ := json.Marshal(tasks)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// serveTaskWatch answers GET /api/tasks/watch from the list tasks returns.
func serveTaskWatch(tasks func() []*core.Task) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := tasks()
		list := watchList{Tasks: make([]watchTask, 0, len(all))}
		for _, t := range all {
			list.Tasks = append(list.Tasks, watchTaskOf(t))
		}
		list.Tag = watchTag(list.Tasks)
		if r.URL.Query().Get("tag") == list.Tag {
			list.Same, list.Tasks = true, nil
		}
		// A phone asks every few seconds for as long as it watches, and a
		// list of finished downloads shrinks to a tenth.
		if !acceptsGzip(r) {
			writeJSON(w, list)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		z := gzip.NewWriter(w)
		_ = json.NewEncoder(z).Encode(list)
		_ = z.Close()
	}
}

// acceptsGzip reads Accept-Encoding, where "gzip;q=0" is a refusal.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q=")
		if !ok {
			return true
		}
		v, err := strconv.ParseFloat(q, 64)
		return err == nil && v > 0
	}
	return false
}
