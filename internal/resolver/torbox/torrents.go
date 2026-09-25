package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// Torrent is one torrent job on the TorBox account. Progress runs from 0 to 1,
// and a file's name starts with the torrent's own folder.
type Torrent struct {
	ID              int64     `json:"id"`
	Hash            string    `json:"hash"`
	Name            string    `json:"name"`
	Size            int64     `json:"size"`
	DownloadState   string    `json:"download_state"`
	DownloadPresent bool      `json:"download_present"`
	Progress        float64   `json:"progress"`
	DownloadSpeed   int64     `json:"download_speed"`
	Seeds           int       `json:"seeds"`
	Files           []WebFile `json:"files"`
}

// CreateTorrent adds a magnet link or the bytes of a .torrent and returns the
// new job's id. TorBox would otherwise offer a finished torrent of several
// files as one zip, which the files here are not fetched as.
func (c *Client) CreateTorrent(ctx context.Context, magnet string, file []byte, fileName string) (int64, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if magnet != "" {
		if err := mw.WriteField("magnet", magnet); err != nil {
			return 0, err
		}
	} else {
		fw, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			return 0, err
		}
		if _, err := fw.Write(file); err != nil {
			return 0, err
		}
	}
	if err := mw.WriteField("allow_zip", "false"); err != nil {
		return 0, err
	}
	if err := mw.Close(); err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"torrent_id"`
	}
	if err := c.send(ctx, http.MethodPost, "/api/torrents/createtorrent", &body, mw.FormDataContentType(), &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, errors.New("torbox /api/torrents/createtorrent: the torrent was taken but no id came back")
	}
	return out.ID, nil
}

// TorrentInfo returns one torrent job by id.
func (c *Client) TorrentInfo(ctx context.Context, id int64) (*Torrent, error) {
	q := url.Values{"bypass_cache": {"true"}, "id": {strconv.FormatInt(id, 10)}}
	var t Torrent
	if err := c.do(ctx, http.MethodGet, "/api/torrents/mylist?"+q.Encode(), nil, &t); err != nil {
		return nil, err
	}
	if t.ID == 0 {
		return nil, &APIError{Path: "/api/torrents/mylist", Code: "ITEM_NOT_FOUND", Detail: fmt.Sprintf("torrent %d is not on the account", id)}
	}
	return &t, nil
}

// TorrentByHash finds the job for an info hash, for a torrent the account
// already holds.
func (c *Client) TorrentByHash(ctx context.Context, hash string) (*Torrent, error) {
	var list []Torrent
	if err := c.do(ctx, http.MethodGet, "/api/torrents/mylist?bypass_cache=true", nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].Hash, hash) {
			return &list[i], nil
		}
	}
	return nil, nil
}

// TorrentLink resolves the direct download URL for one file of a torrent.
func (c *Client) TorrentLink(ctx context.Context, torrentID, fileID int64) (string, error) {
	q := url.Values{
		"token":      {c.key},
		"torrent_id": {strconv.FormatInt(torrentID, 10)},
		"file_id":    {strconv.FormatInt(fileID, 10)},
	}
	var link string
	if err := c.do(ctx, http.MethodGet, "/api/torrents/requestdl?"+q.Encode(), nil, &link); err != nil {
		return "", err
	}
	return link, nil
}

// DeleteTorrent removes a torrent job and its files from the account.
func (c *Client) DeleteTorrent(ctx context.Context, id int64) error {
	body, err := json.Marshal(map[string]any{"torrent_id": id, "operation": "delete"})
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, "/api/torrents/controltorrent", bytes.NewReader(body), "application/json", nil)
}

// failureCodes are the TorBox error codes about the key or TorBox itself
// rather than the torrent. Any other code declines the torrent.
var failureCodes = map[string]bool{
	"NO_AUTH":                    true,
	"BAD_TOKEN":                  true,
	"AUTH_ERROR":                 true,
	"RATE_LIMIT":                 true,
	"INTERNAL_ERROR":             true,
	"DATABASE_ERROR":             true,
	"NO_SERVERS_AVAILABLE_ERROR": true,
	"DOWNLOAD_SERVER_ERROR":      true,
	"ENDPOINT_NOT_FOUND":         true,
}

// Torrents is the account's torrent API as debrid.TorrentBackend drives it.
type Torrents struct{ c *Client }

func NewTorrents(c *Client) Torrents { return Torrents{c: c} }

func (Torrents) ID() string    { return "torbox" }
func (Torrents) Label() string { return "TorBox" }

// declined turns a TorBox answer that declines the torrent into a
// debrid.Refusal and leaves any other error as it is.
func declined(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code == "" || failureCodes[apiErr.Code] {
		return err
	}
	return &debrid.Refusal{Reason: strings.TrimSpace(apiErr.Detail + " (" + apiErr.Code + ")")}
}

func (t Torrents) AddTorrent(ctx context.Context, src debrid.TorrentSource) (string, bool, error) {
	id, err := t.c.CreateTorrent(ctx, src.Magnet, src.File, src.InfoHash+".torrent")
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == "DUPLICATE_ITEM" && src.InfoHash != "" {
		// The account holds this torrent already, the user's own or another
		// app's. Its files are as good as a new job's, but it is not ours to
		// delete.
		held, lerr := t.c.TorrentByHash(ctx, src.InfoHash)
		if lerr != nil {
			return "", false, lerr
		}
		if held != nil {
			return strconv.FormatInt(held.ID, 10), true, nil
		}
	}
	if err != nil {
		return "", false, declined(err)
	}
	return strconv.FormatInt(id, 10), false, nil
}

func (t Torrents) TorrentStatus(ctx context.Context, id string) (debrid.TorrentJob, error) {
	kind, n, err := jobKind(id)
	if err != nil {
		return debrid.TorrentJob{}, err
	}
	switch kind {
	case webPrefix:
		return downloadStatus(t.c.Get(ctx, n))
	case usenetPrefix:
		return downloadStatus(t.c.Usenet(ctx, n))
	}
	tr, err := t.c.TorrentInfo(ctx, n)
	if err != nil {
		return debrid.TorrentJob{}, declined(err)
	}
	job := debrid.TorrentJob{
		Name:     tr.Name,
		Size:     tr.Size,
		Progress: tr.Progress,
		Speed:    tr.DownloadSpeed,
		Seeds:    tr.Seeds,
	}
	state := strings.ToLower(tr.DownloadState)
	switch {
	case tr.DownloadPresent:
		job.State = debrid.TorrentReady
	case state == "error" || strings.HasPrefix(state, "failed"):
		job.State = debrid.TorrentFailed
		job.Reason = "TorBox reports the torrent as " + tr.DownloadState
	}
	for _, f := range tr.Files {
		job.Files = append(job.Files, debrid.TorrentFile{
			ID:   strconv.FormatInt(f.ID, 10),
			Path: f.Name,
			Size: f.Size,
			Held: tr.DownloadPresent,
		})
	}
	return job, nil
}

func (t Torrents) FileURL(ctx context.Context, id string, f debrid.TorrentFile) (debrid.Direct, error) {
	kind, job, err := jobKind(id)
	if err != nil {
		return debrid.Direct{}, err
	}
	file, err := strconv.ParseInt(f.ID, 10, 64)
	if err != nil {
		return debrid.Direct{}, err
	}
	link := t.c.TorrentLink
	switch kind {
	case webPrefix:
		link = t.c.RequestDL
	case usenetPrefix:
		link = t.c.UsenetLink
	}
	u, err := link(ctx, job, file)
	if err != nil {
		return debrid.Direct{}, err
	}
	return debrid.Direct{URL: u, Size: f.Size}, nil
}

func (t Torrents) DeleteTorrent(ctx context.Context, id string) error {
	kind, n, err := jobKind(id)
	if err != nil {
		return err
	}
	switch kind {
	case webPrefix:
		return t.c.Delete(ctx, n)
	case usenetPrefix:
		return t.c.DeleteUsenet(ctx, n)
	}
	return t.c.DeleteTorrent(ctx, n)
}
