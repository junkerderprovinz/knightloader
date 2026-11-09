package debrid

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Deepbrid's torrent API, which only a premium account may use. A torrent is
// added from a magnet link or a .torrent and read by id until its status is
// downloaded, when it lists one short link per file. Those links name no file
// and expire after a few hours, so a job keeps only their order, and each one
// is read afresh when its file is fetched. Deepbrid gives no info hash and
// lets no file be chosen.

// deepbridMaxHops bounds the redirects a short link may take to its file.
const deepbridMaxHops = 10

// deepbridFinal are the statuses after which a torrent does not move any
// more, in words for the task.
var deepbridFinal = map[string]string{
	"error":        "Deepbrid could not download the torrent",
	"dead":         "the torrent is dead, Deepbrid finds no sources for it",
	"magnet_error": "Deepbrid could not resolve the magnet link",
	"virus":        "Deepbrid found a virus in the torrent",
}

// deepbridTorrent is one torrent in /torrents/info.
type deepbridTorrent struct {
	Message  string          `json:"message"`
	Filename string          `json:"filename"`
	Progress json.RawMessage `json:"progress"`
	Seeders  json.RawMessage `json:"seeders"`
	Speed    string          `json:"speed"`
	Links    []string        `json:"links"`
	Status   string          `json:"status"`
}

// AddTorrent lists the account's torrents before the add. Deepbrid does not
// say what it answers to a torrent added twice, and an id that was on the
// list already is the account's own torrent, which is never deleted here.
func (d *Deepbrid) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	const path = "/torrents/add"
	had, err := d.torrentIDs(ctx)
	if err != nil {
		return "", false, err
	}
	var body bytes.Buffer
	contentType := "application/x-www-form-urlencoded"
	if src.Magnet != "" {
		body.WriteString(url.Values{"magnet": {src.Magnet}}.Encode())
	} else {
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("torrent_file", torrentFileName(src))
		if err != nil {
			return "", false, err
		}
		if _, err := fw.Write(src.File); err != nil {
			return "", false, err
		}
		if err := mw.Close(); err != nil {
			return "", false, err
		}
		contentType = mw.FormDataContentType()
	}
	raw, err := d.send(ctx, http.MethodPost, path, &body, contentType)
	if err != nil {
		return "", false, deepbridDeclined(err)
	}
	// The docs print no answer to an add. Most clients read id, some fall
	// back to torrent.id.
	var added struct {
		ID      json.RawMessage `json:"id"`
		Torrent json.RawMessage `json:"torrent"`
	}
	if json.Unmarshal(raw, &added) != nil {
		return "", false, fmt.Errorf("deepbrid %s: unreadable answer", path)
	}
	id := looseID(added.ID)
	if id == "" {
		var nested struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(added.Torrent, &nested) == nil {
			id = looseID(nested.ID)
		}
	}
	if id == "" || id == "0" {
		return "", false, fmt.Errorf("deepbrid %s: the torrent was taken but no id came back", path)
	}
	return id, had[id], nil
}

// torrentIDs lists the ids of every torrent on the account.
func (d *Deepbrid) torrentIDs(ctx context.Context) (map[string]bool, error) {
	raw, err := d.call(ctx, http.MethodGet, "/torrents/info", nil)
	switch {
	case deepbridCode(err) == 1:
		// No data: the account has no torrent.
		return nil, nil
	case err != nil:
		return nil, deepbridTorrentErr(err)
	}
	var list map[string]json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil, errors.New("deepbrid /torrents/info: unreadable list of torrents")
	}
	ids := map[string]bool{}
	for _, entry := range list {
		var t struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(entry, &t) == nil {
			if id := looseID(t.ID); id != "" {
				ids[id] = true
			}
		}
	}
	return ids, nil
}

// TorrentStatus reads one torrent. The links come with the status downloaded,
// and the files are numbered in their order.
func (d *Deepbrid) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	t, err := d.torrent(ctx, id)
	if err != nil {
		return TorrentJob{}, err
	}
	job := TorrentJob{
		Name:     t.Filename,
		Progress: loosePercent(t.Progress) / 100,
		Speed:    deepbridBytes(strings.TrimSuffix(strings.TrimSpace(t.Speed), "/s")),
		Seeds:    int(looseInt(t.Seeders)),
	}
	if reason, ok := deepbridFinal[t.Status]; ok {
		if msg := deepbridPlain(t.Message); msg != "" && msg != "OK" {
			reason += ": " + msg
		}
		job.State, job.Reason = TorrentFailed, reason
		return job, nil
	}
	if t.Status != "downloaded" {
		return job, nil
	}
	if len(t.Links) == 0 {
		// Counts as a failed read, so the next one asks again.
		return TorrentJob{}, fmt.Errorf("deepbrid: torrent %s is downloaded but lists no link yet", id)
	}
	job.State, job.Progress = TorrentReady, 1
	for i := range t.Links {
		job.Files = append(job.Files, TorrentFile{ID: strconv.Itoa(i), Held: true})
	}
	return job, nil
}

// torrent reads /torrents/info for one id. The status counts over the error
// field: a failed torrent may come with error 0, and one Deepbrid could not
// read with error 1 and the status error.
func (d *Deepbrid) torrent(ctx context.Context, id string) (deepbridTorrent, error) {
	path := "/torrents/info?" + url.Values{"id": {id}}.Encode()
	raw, err := d.send(ctx, http.MethodGet, path, nil, "")
	var t deepbridTorrent
	if raw != nil && json.Unmarshal(raw, &t) == nil && t.Status != "" {
		return t, nil
	}
	switch {
	case deepbridCode(err) == 1:
		return deepbridTorrent{}, fmt.Errorf("deepbrid: torrent %s: %w", id, ErrJobGone)
	case err != nil:
		return deepbridTorrent{}, deepbridTorrentErr(err)
	}
	return deepbridTorrent{}, fmt.Errorf("deepbrid %s: the answer names no status", path)
}

// deepbridTorrentErr hands the torrent on when the account may not use
// torrents at all. Any other failure stays an error, and the call is tried
// again.
func deepbridTorrentErr(err error) error {
	var de *deepbridError
	if !errors.As(err, &de) {
		return err
	}
	switch de.code {
	case 2:
		return &Refusal{Reason: "Deepbrid takes torrents only from a premium account"}
	case http.StatusForbidden:
		return &Refusal{Reason: cmp.Or(de.msg, "Deepbrid refused the account")}
	}
	return err
}

// deepbridDeclined is deepbridTorrentErr for an add, which Deepbrid may also
// decline with a code in its answer. Handing it the same torrent again would
// not change that, where an HTTP error may pass.
func deepbridDeclined(err error) error {
	var de *deepbridError
	if errors.As(err, &de) && de.code != 2 && de.code < 100 {
		return &Refusal{Reason: cmp.Or(de.msg, fmt.Sprintf("Deepbrid declined the torrent with code %d", de.code))}
	}
	return deepbridTorrentErr(err)
}

// FileURL reads the torrent's links afresh, since an old one may have
// expired, and follows the file's to where it leads.
func (d *Deepbrid) FileURL(ctx context.Context, job string, f TorrentFile) (Direct, error) {
	i, err := strconv.Atoi(f.ID)
	if err != nil {
		return Direct{}, fmt.Errorf("deepbrid: %q is not a file of a torrent", f.ID)
	}
	t, err := d.torrent(ctx, job)
	if err != nil {
		return Direct{}, err
	}
	if i >= len(t.Links) {
		return Direct{}, fmt.Errorf("deepbrid: torrent %s no longer lists file %d", job, i+1)
	}
	return d.follow(ctx, t.Links[i], f.Size)
}

// follow walks a short link's redirects to the download server and hands on
// where they end, with the name the file is served under. The key goes only
// to Deepbrid's own host: the engine fetches the file without it, and a
// download server elsewhere has no use for it.
func (d *Deepbrid) follow(ctx context.Context, link string, size int64) (Direct, error) {
	const what = "deepbrid torrent link"
	home, err := url.Parse(d.base)
	if err != nil {
		return Direct{}, err
	}
	at, err := url.Parse(link)
	if err != nil {
		return Direct{}, fmt.Errorf("%s: %w", what, err)
	}
	for range deepbridMaxHops {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, at.String(), nil)
		if err != nil {
			return Direct{}, fmt.Errorf("%s: %w", what, httpx.StripURL(err))
		}
		own := at.Host == home.Host
		if own {
			req.Header.Set("Authorization", "Bearer "+d.key)
		}
		resp, err := d.hc.Do(req)
		if err != nil {
			return Direct{}, fmt.Errorf("%s: %w", what, httpx.StripURL(err))
		}
		// Only the headers count, and a file served at once stays unread.
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			next, err := at.Parse(resp.Header.Get("Location"))
			if err != nil {
				return Direct{}, fmt.Errorf("%s: unreadable redirect", what)
			}
			if next.Host == home.Host && strings.HasSuffix(strings.TrimRight(next.Path, "/"), "/login") {
				return Direct{}, fmt.Errorf("%s: Deepbrid did not take the API key for it", what)
			}
			at = next
			continue
		case resp.StatusCode >= 400:
			return Direct{}, fmt.Errorf("%s: %s", what, resp.Status)
		case own:
			return Direct{}, fmt.Errorf("%s: Deepbrid serves the file only with the API key, which the download engine does not send", what)
		}
		return Direct{URL: at.String(), Name: servedName(resp), Size: size}, nil
	}
	return Direct{}, fmt.Errorf("%s: more than %d redirects", what, deepbridMaxHops)
}

// DeleteTorrent deletes the torrent. Deepbrid answers code 1 when nothing
// matched, which leaves the account as a delete would.
func (d *Deepbrid) DeleteTorrent(ctx context.Context, id string) error {
	_, err := d.call(ctx, http.MethodDelete, "/torrents/delete/"+url.PathEscape(id), nil)
	if deepbridCode(err) == 1 {
		return nil
	}
	return err
}
