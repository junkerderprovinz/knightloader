package usenet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// TorBoxAPI is the address of TorBox's API (https://api-docs.torbox.app).
const TorBoxAPI = "https://api.torbox.app/v1"

// torboxSubmitsPerHour is TorBox's documented limit on createusenetdownload,
// per API token.
const torboxSubmitsPerHour = 60

// apiTimeout bounds one call. The upload of a large NZB is the slow one.
const apiTimeout = 3 * time.Minute

// maxAnswer caps what is read of one answer. The full job list of an account
// that keeps its finished downloads runs to a few megabytes.
const maxAnswer = 32 << 20

// torboxBusy are the error codes TorBox answers with when the account may not
// start another job for a while. The NZB is kept and offered again.
var torboxBusy = map[string]bool{
	"ACTIVE_LIMIT":   true,
	"COOLDOWN_LIMIT": true,
	"MONTHLY_LIMIT":  true,
}

// queuedPrefix marks the id of a download TorBox queued until a slot is free.
// The id carries the queue entry and the NZB's hash, by which the download is
// found once it starts under an id of its own.
const queuedPrefix = "queued:"

func queuedID(entry, hash string) string { return queuedPrefix + entry + ":" + hash }

func parseQueued(id string) (entry, hash string, ok bool) {
	rest, ok := strings.CutPrefix(id, queuedPrefix)
	if !ok {
		return "", "", false
	}
	entry, hash, _ = strings.Cut(rest, ":")
	return entry, strings.ToLower(hash), entry != ""
}

// TorBox is one TorBox account's Usenet downloads.
type TorBox struct {
	base string
	slot string
	key  string
	hc   *http.Client
}

// NewTorBox builds the client for the account in slot, speaking to the API at
// base, which is TorBoxAPI outside tests.
func NewTorBox(base, slot, key string) *TorBox {
	return &TorBox{base: base, slot: slot, key: key, hc: httpx.New(httpx.Options{Timeout: apiTimeout})}
}

func (t *TorBox) Slot() string      { return t.slot }
func (*TorBox) Label() string       { return "TorBox" }
func (*TorBox) SubmitsPerHour() int { return torboxSubmitsPerHour }

// torboxEnvelope is the wrapper around every TorBox answer.
type torboxEnvelope struct {
	Success bool            `json:"success"`
	Error   any             `json:"error"`
	Detail  string          `json:"detail"`
	Data    json.RawMessage `json:"data"`
}

// torboxID is an id TorBox sends as a number, or as a string where its own
// SDK types it so.
type torboxID string

func (id *torboxID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*id = torboxID(s)
	return nil
}

// send makes one call. call names it in errors without its query, which for
// requestdl carries the API key.
func (t *TorBox) send(req *http.Request, call string, out any) error {
	req.Header.Set("Authorization", "Bearer "+t.key)
	resp, err := t.hc.Do(req)
	if err != nil {
		// Do's error quotes the whole URL.
		return unreachable{fmt.Errorf("torbox %s: %w", call, errors.Unwrap(err))}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("torbox %s: %w", call, ErrBusy)
	case resp.StatusCode >= 500:
		return unreachable{fmt.Errorf("torbox %s: %s", call, resp.Status)}
	}
	var env torboxEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("torbox %s: %s", call, resp.Status)
	}
	if !env.Success {
		code := ""
		if env.Error != nil {
			code = fmt.Sprint(env.Error)
		}
		msg := strings.TrimSpace(code + " " + env.Detail)
		switch {
		case torboxBusy[code]:
			return fmt.Errorf("torbox %s: %s: %w", call, msg, ErrBusy)
		case code == "PLAN_RESTRICTED_FEATURE":
			return fmt.Errorf("torbox %s: %s: %w", call, msg, ErrNoUsenet)
		case code == "ITEM_NOT_FOUND":
			return fmt.Errorf("torbox %s: %s: %w", call, msg, ErrGone)
		}
		return fmt.Errorf("torbox %s: %s", call, msg)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (t *TorBox) get(ctx context.Context, path string, q url.Values, call string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return t.send(req, call, out)
}

func (t *TorBox) postJSON(ctx context.Context, path string, body any, call string) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return t.send(req, call, nil)
}

// Submit uploads the NZB to /api/usenet/createusenetdownload. TorBox repairs,
// unpacks and removes the archives by default, so the files it hands back are
// the release itself. With every slot of the account taken, TorBox queues the
// download rather than refusing it, and answers with a queue entry instead of
// a download id.
func (t *TorBox) Submit(ctx context.Context, name string, nzb []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="` + quoteEscaper.Replace(nzbFilename(name)) + `"`},
		// TorBox refuses the upload under application/octet-stream.
		"Content-Type": {"application/x-nzb"},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write(nzb); err != nil {
		return "", err
	}
	if err := mw.WriteField("name", name); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/api/usenet/createusenetdownload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var created struct {
		ID     torboxID `json:"usenetdownload_id"`
		Queued torboxID `json:"queued_id"`
		Hash   string   `json:"hash"`
	}
	if err := t.send(req, "createusenetdownload", &created); err != nil {
		return "", err
	}
	switch {
	case created.ID != "" && created.ID != "0":
		return string(created.ID), nil
	case created.Queued != "" && created.Queued != "0":
		return queuedID(string(created.Queued), created.Hash), nil
	}
	return "", errors.New("torbox createusenetdownload: the answer carries no job id")
}

// torboxJob is one entry of /api/usenet/mylist.
type torboxJob struct {
	ID              int64   `json:"id"`
	Hash            string  `json:"hash"`
	Name            string  `json:"name"`
	Size            int64   `json:"size"`
	Progress        float64 `json:"progress"`
	DownloadSpeed   int64   `json:"download_speed"`
	DownloadState   string  `json:"download_state"`
	DownloadPresent bool    `json:"download_present"`
	Files           []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"files"`
}

func (j torboxJob) status() Status {
	st := Status{
		Phase:    PhaseFetching,
		Name:     j.Name,
		Size:     j.Size,
		Progress: j.Progress,
		Speed:    j.DownloadSpeed,
	}
	state := strings.ToLower(j.DownloadState)
	switch {
	case j.DownloadPresent && len(j.Files) > 0:
		st.Phase, st.Progress, st.Speed = PhaseReady, 1, 0
		for _, f := range j.Files {
			// name starts with the job's own folder, which the package's
			// folder stands in for.
			dir, name := splitPath(f.Name)
			if _, inner, found := strings.Cut(dir, "/"); found {
				dir = inner
			} else {
				dir = ""
			}
			st.Files = append(st.Files, File{ID: strconv.FormatInt(f.ID, 10), Name: name, Dir: dir, Size: f.Size})
		}
	case strings.Contains(state, "fail") || strings.Contains(state, "error"):
		st.Phase, st.Reason = PhaseFailed, "TorBox: "+j.DownloadState
	}
	return st
}

// list reads every Usenet download of the account in one call. TorBox may
// answer an account without any with ITEM_NOT_FOUND, which is read as none, so
// the jobs asked about count as gone instead of the call failing.
func (t *TorBox) list(ctx context.Context) ([]torboxJob, error) {
	var jobs []torboxJob
	err := t.get(ctx, "/api/usenet/mylist", url.Values{"bypass_cache": {"true"}}, "usenet/mylist", &jobs)
	if errors.Is(err, ErrGone) {
		return nil, nil
	}
	return jobs, err
}

// job reads one Usenet download. TorBox documents the answer for an id as the
// download itself rather than a list; a list is taken as well.
func (t *TorBox) job(ctx context.Context, id string) (torboxJob, bool, error) {
	var raw json.RawMessage
	q := url.Values{"bypass_cache": {"true"}, "id": {id}}
	err := t.get(ctx, "/api/usenet/mylist", q, "usenet/mylist", &raw)
	if errors.Is(err, ErrGone) || err == nil && len(raw) == 0 {
		return torboxJob{}, false, nil
	}
	if err != nil {
		return torboxJob{}, false, err
	}
	var one torboxJob
	if json.Unmarshal(raw, &one) == nil {
		return one, one.ID != 0, nil
	}
	var list []torboxJob
	if err := json.Unmarshal(raw, &list); err != nil {
		return torboxJob{}, false, fmt.Errorf("torbox usenet/mylist: %w", err)
	}
	for _, j := range list {
		if strconv.FormatInt(j.ID, 10) == id {
			return j, true, nil
		}
	}
	return torboxJob{}, false, nil
}

// queue reads the Usenet downloads waiting in the account's queue. An empty
// queue may come as ITEM_NOT_FOUND too, as in list.
func (t *TorBox) queue(ctx context.Context) (map[string]bool, error) {
	var entries []struct {
		ID torboxID `json:"id"`
	}
	q := url.Values{"bypass_cache": {"true"}, "type": {"usenet"}}
	err := t.get(ctx, "/api/queued/getqueued", q, "queued/getqueued", &entries)
	if err != nil && !errors.Is(err, ErrGone) {
		return nil, err
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[string(e.ID)] = true
	}
	return out, nil
}

// singleReads is how many started downloads Status reads one by one. The
// whole list of an account that keeps its finished downloads runs to
// megabytes, but past a handful of downloads a call for each every few
// seconds would take too much of TorBox's 300 calls a minute.
const singleReads = 5

// Status finds a queued download in the queue, and one that has left it by its
// hash in the whole list, under the id it started under. The queue is read
// first, so a download that starts in between is found in the list rather
// than lost. The other downloads are read one by one while they are few.
func (t *TorBox) Status(ctx context.Context, ids []string) (map[string]Status, error) {
	out := make(map[string]Status, len(ids))
	var started, left []string
	var queue map[string]bool
	var queueErr error
	for _, id := range ids {
		entry, _, queued := parseQueued(id)
		if !queued {
			started = append(started, id)
			continue
		}
		if queue == nil && queueErr == nil {
			queue, queueErr = t.queue(ctx)
		}
		if queue[entry] {
			out[id] = Status{Phase: PhaseFetching}
		} else {
			left = append(left, id)
		}
	}

	if len(left) == 0 && len(started) <= singleReads {
		for _, id := range started {
			j, ok, err := t.job(ctx, id)
			if err != nil {
				return nil, err
			}
			if ok {
				out[id] = j.status()
			}
		}
		return out, nil
	}

	jobs, err := t.list(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]torboxJob, len(jobs))
	byHash := make(map[string]torboxJob, len(jobs))
	for _, j := range jobs {
		byID[strconv.FormatInt(j.ID, 10)] = j
		// The newest, should the same NZB be on the account twice.
		if h := strings.ToLower(j.Hash); h != "" && j.ID > byHash[h].ID {
			byHash[h] = j
		}
	}
	for _, id := range started {
		if j, ok := byID[id]; ok {
			out[id] = j.status()
		}
	}
	for _, id := range left {
		_, hash, _ := parseQueued(id)
		switch j := byHash[hash]; {
		case hash != "" && j.ID != 0:
			st := j.status()
			st.ID = strconv.FormatInt(j.ID, 10)
			out[id] = st
		case queueErr != nil:
			// The queue could not be read, so it may still be waiting there.
			out[id] = Status{Phase: PhaseFetching}
		}
	}
	return out, nil
}

// Link asks /api/usenet/requestdl for one file's address, which stays valid
// for three hours unless a download has started on it.
func (t *TorBox) Link(ctx context.Context, id, fileID string) (string, error) {
	q := url.Values{"token": {t.key}, "usenet_id": {id}, "file_id": {fileID}}
	var link string
	if err := t.get(ctx, "/api/usenet/requestdl", q, "usenet/requestdl", &link); err != nil {
		return "", err
	}
	if link == "" {
		return "", errors.New("torbox usenet/requestdl: the answer carries no address")
	}
	return link, nil
}

// Delete removes the job through /api/usenet/controlusenetdownload, or its
// queue entry through /api/queued/controlqueued while it has not started. A
// queued download that started meanwhile is found by its hash.
func (t *TorBox) Delete(ctx context.Context, id string) error {
	entry, hash, queued := parseQueued(id)
	if !queued {
		return t.deleteJob(ctx, id)
	}
	n, err := strconv.ParseInt(entry, 10, 64)
	if err != nil {
		return fmt.Errorf("torbox: %q is not a queue entry", entry)
	}
	err = t.postJSON(ctx, "/api/queued/controlqueued", map[string]any{"queued_id": n, "operation": "delete"}, "queued/controlqueued")
	if !errors.Is(err, ErrGone) || hash == "" {
		return err
	}
	jobs, err := t.list(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if strings.EqualFold(j.Hash, hash) {
			return t.deleteJob(ctx, strconv.FormatInt(j.ID, 10))
		}
	}
	return fmt.Errorf("torbox: queue entry %s: %w", entry, ErrGone)
}

func (t *TorBox) deleteJob(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("torbox: %q is not a usenet job id", id)
	}
	return t.postJSON(ctx, "/api/usenet/controlusenetdownload", map[string]any{"usenet_id": n, "operation": "delete"}, "controlusenetdownload")
}

// quoteEscaper is what mime/multipart applies to a file name in a
// Content-Disposition header.
var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// nzbFilename is the name an upload goes out under, which services read the
// format from.
func nzbFilename(name string) string {
	// A line break would end the part's header early.
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "download"
	}
	if strings.HasSuffix(strings.ToLower(name), ".nzb") {
		return name
	}
	return name + ".nzb"
}
