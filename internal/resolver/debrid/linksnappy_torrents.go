package debrid

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Linksnappy's torrent API, which it does not document. The calls and their
// answers follow the two clients that use it, pyLoad's LinksnappyComTorrent
// and ResolveURL's linksnappy.py. A torrent is added and started, and once
// Linksnappy has finished it, FILES lists a download link for each file. That
// link wants the session cookie, so FileURL follows it to where it leads.

const (
	// lsHeld is the add's answer for a torrent the account already has. It
	// still names the torrent's id.
	lsHeld = "This torrent already exists in your account"
	// lsStarting is START's answer while Linksnappy still reads a magnet link.
	lsStarting = "Magnet URI processing in progress. Please wait."
	// lsStarted is START's answer for a torrent that is under way.
	lsStarted = "Already started."
)

// lsSessionAge is how long one login serves the torrent calls. The cookie
// lasts far longer, but what Linksnappy answers once it drops a session is
// not known, so a session is not trusted for hours on end.
const lsSessionAge = 10 * time.Minute

// login logs in unless a login within lsSessionAge still stands.
func (l *Linksnappy) login(ctx context.Context) error {
	l.mu.Lock()
	fresh := !l.loggedIn.IsZero() && time.Since(l.loggedIn) < lsSessionAge
	l.mu.Unlock()
	if fresh {
		return nil
	}
	return l.Authenticate(ctx)
}

// logout makes the next torrent call log in first. A refused call may be the
// session gone stale.
func (l *Linksnappy) logout() {
	l.mu.Lock()
	l.loggedIn = time.Time{}
	l.mu.Unlock()
}

// torrentCall is envelope on a session that is logged in.
func (l *Linksnappy) torrentCall(ctx context.Context, path string, q url.Values, out any) error {
	if err := l.login(ctx); err != nil {
		return err
	}
	err := l.envelope(ctx, path, q, out)
	if err != nil {
		l.logout()
	}
	return err
}

// lsAdded is one torrent in the answer to an add. Status comes with a magnet
// link only; error is false or empty when the torrent went in.
type lsAdded struct {
	Status    string          `json:"status"`
	Error     json.RawMessage `json:"error"`
	TorrentID json.RawMessage `json:"torrentid"`
}

func (a lsAdded) job(path string) (string, bool, error) {
	msg := lsSentence(a.Error)
	held := msg == lsHeld
	if !held && (msg != "" || a.Status != "" && a.Status != "OK") {
		return "", false, &Refusal{Reason: cmp.Or(msg, "Linksnappy declined the torrent and named no reason")}
	}
	id := looseID(a.TorrentID)
	if id == "" {
		return "", false, fmt.Errorf("linksnappy %s: the torrent was taken but no id came back", path)
	}
	return id, held, nil
}

func (l *Linksnappy) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	if src.Magnet == "" {
		return l.upload(ctx, src)
	}
	const path = "/torrents/ADDMAGNET"
	var added []lsAdded
	if err := l.torrentCall(ctx, path, url.Values{"magnetlinks": {src.Magnet}}, &added); err != nil {
		return "", false, err
	}
	if len(added) == 0 {
		return "", false, fmt.Errorf("linksnappy %s: the answer names no torrent", path)
	}
	return added[0].job(path)
}

// upload sends a .torrent file the way Linksnappy's own site does, to the
// upload script beside the API. The answer is keyed by the file's name.
func (l *Linksnappy) upload(ctx context.Context, src TorrentSource) (string, bool, error) {
	const path = "/includes/ajaxupload.php"
	if err := l.login(ctx); err != nil {
		return "", false, err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("torrents[]", torrentFileName(src))
	if err != nil {
		return "", false, err
	}
	if _, err := fw.Write(src.File); err != nil {
		return "", false, err
	}
	if err := mw.Close(); err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(l.base, "/api")+path, &body)
	if err != nil {
		return "", false, fmt.Errorf("linksnappy %s: %w", path, httpx.StripURL(err))
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	raw, err := l.send(req, path)
	if err != nil {
		return "", false, err
	}
	var answer map[string]lsAdded
	if json.Unmarshal(raw, &answer) != nil || len(answer) != 1 {
		l.logout()
		return "", false, fmt.Errorf("linksnappy %s: unreadable answer", path)
	}
	var added lsAdded
	for _, a := range answer {
		added = a
	}
	return added.job(path)
}

// lsStatus is the return of STATUS. percentDone runs from 0 to 100. pyLoad
// reads getSize as text such as "1.4 GB", and ResolveURL only prints
// downloadRate, so both are read as a number or as such text.
type lsStatus struct {
	Status       string          `json:"status"`
	Error        json.RawMessage `json:"error"`
	Name         string          `json:"name"`
	PercentDone  json.RawMessage `json:"percentDone"`
	GetSize      json.RawMessage `json:"getSize"`
	DownloadRate json.RawMessage `json:"downloadRate"`
}

// TorrentStatus reads STATUS, starts a torrent that waits for it, and lists
// the files of a finished one.
func (l *Linksnappy) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	const path = "/torrents/STATUS"
	var st lsStatus
	if err := l.torrentCall(ctx, path, url.Values{"tid": {id}}, &st); err != nil {
		return TorrentJob{}, err
	}
	if st.Status == "" {
		return TorrentJob{}, fmt.Errorf("linksnappy %s: the answer names no status", path)
	}
	job := TorrentJob{
		Name:     st.Name,
		Size:     textSize(st.GetSize),
		Progress: loosePercent(st.PercentDone) / 100,
		Speed:    lsRate(st.DownloadRate),
	}
	switch st.Status {
	case "ERROR":
		l.forgetStart(id)
		job.State = TorrentFailed
		job.Reason = cmp.Or(lsSentence(st.Error), "Linksnappy reported an error on the torrent")
		return job, nil
	case "FINISHED":
		l.forgetStart(id)
		files, err := l.torrentFiles(ctx, id)
		if err != nil {
			return TorrentJob{}, err
		}
		job.State, job.Progress, job.Files = TorrentReady, 1, files
		return job, nil
	}
	return job, l.start(ctx, id)
}

// start sends START once for a torrent, which an added torrent waits for.
// While Linksnappy still reads a magnet link it declines, and the next status
// read asks again.
func (l *Linksnappy) start(ctx context.Context, id string) error {
	l.mu.Lock()
	done := l.started[id]
	l.mu.Unlock()
	if done {
		return nil
	}
	if err := l.login(ctx); err != nil {
		return err
	}
	err := l.envelope(ctx, "/torrents/START", url.Values{"tid": {id}}, nil)
	var said *lsError
	if errors.As(err, &said) {
		switch said.msg {
		case lsStarting:
			return nil
		case lsStarted:
			err = nil
		}
	}
	if err != nil {
		l.logout()
		return err
	}
	l.mu.Lock()
	l.started[id] = true
	l.mu.Unlock()
	return nil
}

func (l *Linksnappy) forgetStart(id string) {
	l.mu.Lock()
	delete(l.started, id)
	l.mu.Unlock()
}

// lsFile is a file in the tree FILES answers. A folder is an object of
// further entries.
type lsFile struct {
	DownloadLink string          `json:"downloadLink"`
	Size         json.RawMessage `json:"size"`
}

// torrentFiles reads FILES, a tree of objects whose files carry a
// downloadLink and a size, the way ResolveURL walks it. Right after a torrent
// finishes the links may not be there yet, and that read counts as a failed
// one, so the next read tries again.
func (l *Linksnappy) torrentFiles(ctx context.Context, id string) ([]TorrentFile, error) {
	const path = "/torrents/FILES"
	var tree json.RawMessage
	if err := l.torrentCall(ctx, path, url.Values{"id": {id}}, &tree); err != nil {
		return nil, err
	}
	var files []TorrentFile
	lsWalk(tree, "", &files)
	if len(files) == 0 {
		return nil, fmt.Errorf("linksnappy %s: the finished torrent lists no download link yet", path)
	}
	return files, nil
}

// lsWalk collects the files under node, in the order of their keys so that
// every read lists them alike. Neither client reads a file's name from the
// tree, so its path is the keys it sits under. A key of digits only is taken
// for one of Linksnappy's file ids, which are numbers, and left out.
func lsWalk(node json.RawMessage, dir string, out *[]TorrentFile) {
	var entries map[string]json.RawMessage
	if json.Unmarshal(node, &entries) == nil {
		var f lsFile
		if json.Unmarshal(node, &f) == nil && f.DownloadLink != "" {
			*out = append(*out, TorrentFile{ID: f.DownloadLink, Path: dir, Size: looseInt(f.Size), Held: true})
			return
		}
		for _, k := range slices.Sorted(maps.Keys(entries)) {
			p := dir
			if name := strings.Trim(k, "/"); strings.Trim(name, "0123456789") != "" {
				p = strings.TrimPrefix(dir+"/"+name, "/")
			}
			lsWalk(entries[k], p, out)
		}
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(node, &list) == nil {
		for _, n := range list {
			lsWalk(n, dir, out)
		}
	}
}

// FileURL follows a file's download link on the session, as ResolveURL does,
// and hands on where it leads. The engine carries no cookie.
func (l *Linksnappy) FileURL(ctx context.Context, _ string, f TorrentFile) (Direct, error) {
	const what = "file link"
	if err := l.login(ctx); err != nil {
		return Direct{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, f.ID, nil)
	if err != nil {
		return Direct{}, fmt.Errorf("linksnappy %s: %w", what, httpx.StripURL(err))
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return Direct{}, fmt.Errorf("linksnappy %s: %w", what, httpx.StripURL(err))
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		l.logout()
		return Direct{}, fmt.Errorf("linksnappy %s: HTTP %d", what, resp.StatusCode)
	}
	return Direct{URL: resp.Request.URL.String(), Name: servedName(resp), Size: f.Size}, nil
}

// servedName is the file name a download answers with, from its
// Content-Disposition or else from the end of its address.
func servedName(resp *http.Response) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return path.Base(params["filename"])
	}
	if name := path.Base(resp.Request.URL.Path); name != "." && name != "/" {
		return name
	}
	return ""
}

func (l *Linksnappy) DeleteTorrent(ctx context.Context, id string) error {
	l.forgetStart(id)
	return l.torrentCall(ctx, "/torrents/DELETETORRENT", url.Values{"tid": {id}, "delFiles": {"1"}}, nil)
}

// Cached asks HASHCHECK, which answers "CACHED" for a torrent Linksnappy has.
func (l *Linksnappy) Cached(ctx context.Context, hash string) (bool, error) {
	var answer string
	if err := l.torrentCall(ctx, "/torrents/HASHCHECK", url.Values{"hash": {hash}}, &answer); err != nil {
		return false, err
	}
	return answer == "CACHED", nil
}

// looseID reads an id sent as a string or as a number.
func looseID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// loosePercent reads a percentage sent as a number or as digits.
func loosePercent(raw json.RawMessage) float64 {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		f, _ = strconv.ParseFloat(strings.TrimSpace(s), 64)
	}
	return f
}

// lsRate reads downloadRate as bytes a second, or as text such as "1.2 MB/s".
func lsRate(raw json.RawMessage) int64 {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw, _ = json.Marshal(strings.TrimSuffix(strings.TrimSpace(s), "/s"))
	}
	return textSize(raw)
}
