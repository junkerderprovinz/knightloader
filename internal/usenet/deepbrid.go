package usenet

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DeepbridAPI is the address of Deepbrid's API
// (https://www.deepbrid.com/api-docs).
const DeepbridAPI = "https://www.deepbrid.com/api/v1"

// Two kinds of job id stand apart from Deepbrid's plain upload id. keptPrefix
// marks an upload the account had before the NZB went to it: Deepbrid answers
// an NZB uploaded twice with the upload it has, which is the user's and is
// never deleted here. unsavedPrefix marks an upload Deepbrid processed without
// keeping it, whose files are known only from the answer to the add.
const (
	keptPrefix    = "kept:"
	unsavedPrefix = "unsaved:"
)

// Deepbrid is one Deepbrid account's Usenet uploads. Deepbrid reads an NZB the
// moment it is uploaded and serves the files in it straight out of the
// archive, so a job is ready as soon as it is taken. A file it cannot serve
// that way, from an archive missing volumes or one packed with compression,
// comes without a link and is left out.
type Deepbrid struct {
	base string
	slot string
	key  string
	hc   *http.Client

	mu sync.Mutex
	// unsaved holds the status of each upload Deepbrid did not keep, until
	// the job is deleted.
	unsaved map[string]Status
}

// NewDeepbrid builds the client for the account in slot, speaking to the API
// at base, which is DeepbridAPI outside tests.
func NewDeepbrid(base, slot, key string) *Deepbrid {
	return &Deepbrid{
		base: base, slot: slot, key: key,
		hc:      httpx.New(httpx.Options{Timeout: apiTimeout, Class: httpx.ClassUsenet}),
		unsaved: map[string]Status{},
	}
}

func (d *Deepbrid) Slot() string      { return d.slot }
func (*Deepbrid) Label() string       { return "Deepbrid" }
func (*Deepbrid) SubmitsPerHour() int { return 0 }

// dbAnswer is the status part of every answer: error is 0 on success, a code
// of Deepbrid's own or the HTTP status otherwise.
type dbAnswer struct {
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
}

func (a dbAnswer) code() int {
	switch v := strings.Trim(string(a.Error), `" `); v {
	case "", "0", "null", "false":
		return 0
	default:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		return -1
	}
}

func (d *Deepbrid) send(req *http.Request, call string, out any) error {
	req.Header.Set("Authorization", "Bearer "+d.key)
	// Without it a refused request is redirected to the login page instead of
	// getting the JSON answer.
	req.Header.Set("Accept", "application/json")
	resp, err := d.hc.Do(req)
	if err != nil {
		return unreachable{fmt.Errorf("deepbrid %s: %w", call, errors.Unwrap(err))}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("deepbrid %s: %w", call, ErrBusy)
	case resp.StatusCode >= 500:
		// 503 is maintenance, which Deepbrid asks to wait out.
		return unreachable{fmt.Errorf("deepbrid %s: %s", call, resp.Status)}
	}
	var a dbAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("deepbrid %s: %s", call, resp.Status)
	}
	code := a.code()
	if code == 0 && resp.StatusCode >= 400 {
		code = resp.StatusCode
	}
	if code != 0 {
		return dbRefusal(call, code, strings.TrimSpace(a.Message))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// dbRefusal words a code the Usenet calls answer with. 1 is the docs' answer
// to an id that matched nothing.
func dbRefusal(call string, code int, msg string) error {
	if msg == "" {
		msg = fmt.Sprintf("refused with code %d", code)
	}
	switch code {
	case 1, http.StatusNotFound:
		return fmt.Errorf("deepbrid %s: %s: %w", call, msg, ErrGone)
	case 2, http.StatusForbidden:
		return fmt.Errorf("deepbrid %s: %s: %w", call, msg, ErrNoUsenet)
	case 11:
		return fmt.Errorf("deepbrid %s: %s: %w", call, msg, ErrBusy)
	case 15:
		return fmt.Errorf("deepbrid %s: Deepbrid took this server for a proxy or VPN and serves it no Usenet; its support can allow the account", call)
	case http.StatusUnauthorized:
		return fmt.Errorf("deepbrid %s: the API key was refused", call)
	}
	return fmt.Errorf("deepbrid %s: %s", call, msg)
}

func (d *Deepbrid) get(ctx context.Context, path string, q url.Values, out any) error {
	u := d.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return d.send(req, path, out)
}

// dbAdded is one file in the answer to an add.
type dbAdded struct {
	Filename    string `json:"filename"`
	Filesize    int64  `json:"filesize"`
	DownloadURL string `json:"download_url"`
}

// Submit uploads the NZB. The account's uploads are listed first, so that an
// upload Deepbrid answers with and already had is told apart from a new one.
func (d *Deepbrid) Submit(ctx context.Context, name string, nzb []byte) (string, error) {
	had, err := d.uploadIDs(ctx)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="nzb_file"; filename="` + quoteEscaper.Replace(nzbFilename(name)) + `"`},
		"Content-Type":        {"application/x-nzb"},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write(nzb); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/usenet/add", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var added struct {
		ID    int64     `json:"id"`
		Files []dbAdded `json:"files"`
	}
	if err := d.send(req, "/usenet/add", &added); err != nil {
		return "", err
	}
	if added.ID == 0 {
		return d.keepUnsaved(added.Files)
	}
	id := strconv.FormatInt(added.ID, 10)
	if had[added.ID] {
		return keptPrefix + id, nil
	}
	return id, nil
}

// keepUnsaved holds the files of an upload Deepbrid processed but did not
// keep. Their links cannot be asked for again, so they are staged as they are.
func (d *Deepbrid) keepUnsaved(added []dbAdded) (string, error) {
	var st Status
	var left []string
	for _, a := range added {
		if a.DownloadURL == "" {
			left = append(left, a.Filename)
			continue
		}
		dir, name := splitPath(a.Filename)
		st.Files = append(st.Files, File{ID: strconv.Itoa(len(st.Files)), Name: name, Dir: dir, Size: a.Filesize, Link: a.DownloadURL})
		st.Size += a.Filesize
	}
	if len(st.Files) == 0 {
		return "", errors.New("deepbrid /usenet/add: Deepbrid lists no file of this NZB it can serve")
	}
	if len(left) > 0 {
		st.Note = "left out what Deepbrid cannot serve: " + strings.Join(left, ", ")
	}
	st.Phase, st.Progress = PhaseReady, 1
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := unsavedPrefix + hex.EncodeToString(b[:])
	d.mu.Lock()
	d.unsaved[id] = st
	d.mu.Unlock()
	return id, nil
}

// uploadIDs lists the ids of every upload on the account.
func (d *Deepbrid) uploadIDs(ctx context.Context) (map[int64]bool, error) {
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := d.get(ctx, "/usenet/uploads", nil, &list); err != nil {
		return nil, err
	}
	ids := make(map[int64]bool, len(list.Items))
	for _, it := range list.Items {
		ids[it.ID] = true
	}
	return ids, nil
}

// dbFile is one file in /usenet/uploads/info. Unreachable is set, and the
// link empty, for a file Deepbrid cannot serve. Its words are free-form, and
// only whether they are there counts.
type dbFile struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Link        string `json:"link"`
	Unreachable string `json:"unreachable"`
}

func (f dbFile) servable() bool { return f.Unreachable == "" && f.Link != "" }

func (d *Deepbrid) upload(ctx context.Context, id string) ([]dbFile, error) {
	var info struct {
		Files []dbFile `json:"files"`
	}
	if err := d.get(ctx, "/usenet/uploads/info", url.Values{"id": {strings.TrimPrefix(id, keptPrefix)}}, &info); err != nil {
		return nil, err
	}
	return info.Files, nil
}

// Status reads each upload's files, one call per upload. That is affordable
// because an upload is ready the first time it is read, after which the job
// is staged and not read again. An upload Deepbrid no longer has is left out
// of the answer.
func (d *Deepbrid) Status(ctx context.Context, ids []string) (map[string]Status, error) {
	out := make(map[string]Status, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, unsavedPrefix) {
			d.mu.Lock()
			st, ok := d.unsaved[id]
			d.mu.Unlock()
			if ok {
				out[id] = st
			}
			continue
		}
		files, err := d.upload(ctx, id)
		switch {
		case errors.Is(err, ErrGone):
			continue
		case errors.Is(err, ErrBusy), temporary(err):
			return nil, err
		case err != nil:
			out[id] = Status{Phase: PhaseFailed, Reason: err.Error()}
			continue
		}
		out[id] = dbStatus(files)
	}
	return out, nil
}

// dbStatus is a ready upload's files. A file's id is its place in the
// upload's list rather than its name, which may hold a slash, where the file
// link carries the id as one segment of its path.
func dbStatus(files []dbFile) Status {
	st := Status{Phase: PhaseReady, Progress: 1}
	var left []string
	for i, f := range files {
		if !f.servable() {
			why := f.Name
			if f.Unreachable != "" {
				why += " (" + f.Unreachable + ")"
			}
			left = append(left, why)
			continue
		}
		dir, name := splitPath(f.Name)
		st.Files = append(st.Files, File{ID: strconv.Itoa(i), Name: name, Dir: dir, Size: f.Size})
		st.Size += f.Size
	}
	if len(left) > 0 {
		st.Note = "left out what Deepbrid cannot serve: " + strings.Join(left, ", ")
	}
	if len(st.Files) == 0 {
		return Status{Phase: PhaseFailed, Reason: "Deepbrid can serve no file of this upload: " + strings.Join(left, ", ")}
	}
	return st
}

// Link reads the file's address from the upload again, since Deepbrid does
// not say how long one lasts.
func (d *Deepbrid) Link(ctx context.Context, id, fileID string) (string, error) {
	files, err := d.upload(ctx, id)
	if err != nil {
		return "", err
	}
	i, err := strconv.Atoi(fileID)
	if err != nil || i >= len(files) {
		return "", fmt.Errorf("deepbrid: upload %s lists no file %s", id, fileID)
	}
	if f := files[i]; !f.servable() {
		return "", fmt.Errorf("deepbrid: %s cannot be served: %s", f.Name, f.Unreachable)
	}
	return files[i].Link, nil
}

// Delete removes an upload this client made. One the account had stays, and
// one Deepbrid did not keep is only forgotten here.
func (d *Deepbrid) Delete(ctx context.Context, id string) error {
	switch {
	case strings.HasPrefix(id, keptPrefix):
		return nil
	case strings.HasPrefix(id, unsavedPrefix):
		d.mu.Lock()
		delete(d.unsaved, id)
		d.mu.Unlock()
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, d.base+"/usenet/uploads/delete/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	return d.send(req, "/usenet/uploads/delete", nil)
}
