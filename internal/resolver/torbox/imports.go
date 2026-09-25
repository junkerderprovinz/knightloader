package torbox

// The downloads on a TorBox account for the import from the account: its
// torrents, and its web and usenet downloads, which Torrents fetches like a
// torrent once they are imported. A torrent's job id is TorBox's number as it
// is; the other two carry their kind in front, "web/123" and "usenet/45".

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

const (
	webPrefix    = "web/"
	usenetPrefix = "usenet/"
)

// webJob is the job id of a web download.
func webJob(id int64) string { return webPrefix + strconv.FormatInt(id, 10) }

// UsenetJob is the job id the import lists a usenet download under, given
// TorBox's own id for it.
func UsenetJob(id string) string { return usenetPrefix + id }

// jobKind splits a job id into its kind's prefix, "" for a torrent, and
// TorBox's number.
func jobKind(job string) (string, int64, error) {
	prefix := ""
	for _, p := range []string{webPrefix, usenetPrefix} {
		if strings.HasPrefix(job, p) {
			prefix = p
			break
		}
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(job, prefix), 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("torbox: %q names no download", job)
	}
	return prefix, n, nil
}

// listLimit is how many entries one mylist call answers with at most.
const listLimit = 1000

// listed is what the import reads of a mylist entry, which the three kinds
// share.
type listed struct {
	ID        int64  `json:"id"`
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

// List reads the account's torrents, web downloads and usenet downloads.
func (t Torrents) List(ctx context.Context) ([]debrid.Listed, bool, error) {
	kinds := []struct {
		path, prefix string
		torrent      bool
	}{
		{"/api/torrents/mylist", "", true},
		{"/api/webdl/mylist", webPrefix, false},
		{"/api/usenet/mylist", usenetPrefix, false},
	}
	q := url.Values{"bypass_cache": {"true"}, "limit": {strconv.Itoa(listLimit)}}.Encode()
	var out []debrid.Listed
	complete := true
	for _, k := range kinds {
		var items []listed
		if err := t.c.do(ctx, http.MethodGet, k.path+"?"+q, nil, &items); err != nil {
			return nil, false, err
		}
		complete = complete && len(items) < listLimit
		for _, it := range items {
			l := debrid.Listed{ID: k.prefix + strconv.FormatInt(it.ID, 10), Name: it.Name, Size: it.Size}
			// The hash of a web or usenet download is TorBox's own, not a
			// torrent's.
			if k.torrent {
				l.Hash = strings.ToLower(it.Hash)
			}
			if at, err := time.Parse(time.RFC3339Nano, it.CreatedAt); err == nil {
				l.Added = at
			}
			out = append(out, l)
		}
	}
	return out, complete, nil
}

// Usenet returns one usenet download by id. TorBox describes it with the
// fields of a web download.
func (c *Client) Usenet(ctx context.Context, id int64) (*WebDownload, error) {
	return c.listItem(ctx, "/api/usenet/mylist", id)
}

// UsenetLink resolves the direct download URL for one file of a usenet
// download.
func (c *Client) UsenetLink(ctx context.Context, id, fileID int64) (string, error) {
	q := url.Values{
		"token":     {c.key},
		"usenet_id": {strconv.FormatInt(id, 10)},
		"file_id":   {strconv.FormatInt(fileID, 10)},
	}
	var link string
	if err := c.do(ctx, http.MethodGet, "/api/usenet/requestdl?"+q.Encode(), nil, &link); err != nil {
		return "", err
	}
	return link, nil
}

// DeleteUsenet removes a usenet download from the account.
func (c *Client) DeleteUsenet(ctx context.Context, id int64) error {
	body, err := json.Marshal(map[string]any{"usenet_id": id, "operation": "delete"})
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, "/api/usenet/controlusenetdownload", bytes.NewReader(body), "application/json", nil)
}

// downloadStatus is a web or usenet download as debrid.TorrentBackend reads a
// job, from what reading it returned.
func downloadStatus(d *WebDownload, err error) (debrid.TorrentJob, error) {
	if err != nil {
		return debrid.TorrentJob{}, declined(err)
	}
	job := debrid.TorrentJob{Name: d.Name, Size: d.Size, Progress: d.Progress, Speed: d.DownloadSpeed}
	state := strings.ToLower(d.DownloadState)
	switch {
	case d.DownloadPresent && len(d.Files) > 0:
		job.State = debrid.TorrentReady
	case strings.Contains(state, "error") || strings.Contains(state, "fail"):
		job.State = debrid.TorrentFailed
		job.Reason = "TorBox reports the download as " + d.DownloadState
	}
	for _, f := range d.Files {
		job.Files = append(job.Files, debrid.TorrentFile{
			ID:   strconv.FormatInt(f.ID, 10),
			Path: f.Name,
			Size: f.Size,
			Held: d.DownloadPresent,
		})
	}
	return job, nil
}
