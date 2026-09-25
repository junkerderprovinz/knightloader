package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Real-Debrid's torrent API: https://api.real-debrid.com/#torrents. A torrent
// waits for a file selection before it starts, and once downloaded it lists
// one restricted link per selected file, which /unrestrict/link turns into a
// download like any hoster link.

// rdFailureCodes are Real-Debrid's error codes about the account, the address
// or the service itself rather than the torrent. Any other code is Real-Debrid
// declining the torrent. https://api.real-debrid.com/#api_error_codes
var rdFailureCodes = map[int]bool{
	-1: true, // Internal error
	5:  true, // Slow down
	6:  true, // Ressource unreachable
	8:  true, // Bad token
	9:  true, // Permission denied
	10: true, // Two-Factor authentication needed
	11: true, // Two-Factor authentication pending
	12: true, // Invalid login
	13: true, // Invalid password
	14: true, // Account locked
	15: true, // Account not activated
	22: true, // IP address not allowed
	25: true, // Service unavailable
	34: true, // Too many requests
}

// torrentCall is do for the torrent endpoints, which answer 201 and 204 and
// take a raw .torrent by PUT. A declined torrent comes back as a Refusal.
func (r *RealDebrid) torrentCall(ctx context.Context, method, path string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return err
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		var e rdError
		if json.Unmarshal(raw, &e) != nil || e.Error == "" {
			return fmt.Errorf("realdebrid %s: HTTP %d", path, resp.StatusCode)
		}
		// The code decides: Real-Debrid answers a spent limit with a 503 just
		// as it does its own outage.
		if rdFailureCodes[e.Code] || (e.Code == 0 && resp.StatusCode >= 500) {
			return fmt.Errorf("realdebrid %s: %s", path, e.Error)
		}
		return &Refusal{Reason: strings.ReplaceAll(e.Error, "_", " ")}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (r *RealDebrid) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	var out struct {
		ID string `json:"id"`
	}
	var err error
	if src.Magnet != "" {
		form := url.Values{"magnet": {src.Magnet}}
		err = r.torrentCall(ctx, http.MethodPost, "/torrents/addMagnet", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &out)
	} else {
		err = r.torrentCall(ctx, http.MethodPut, "/torrents/addTorrent", bytes.NewReader(src.File), "application/x-bittorrent", &out)
	}
	if err != nil {
		return "", false, err
	}
	if out.ID == "" {
		return "", false, fmt.Errorf("realdebrid: the torrent was taken but no id came back")
	}
	return out.ID, false, nil
}

// rdTorrent is /torrents/info. Progress runs from 0 to 100.
type rdTorrent struct {
	Filename string  `json:"filename"`
	Bytes    int64   `json:"bytes"`
	Progress float64 `json:"progress"`
	Status   string  `json:"status"`
	Speed    int64   `json:"speed"`
	Seeders  int     `json:"seeders"`
	Files    []struct {
		ID       int    `json:"id"`
		Path     string `json:"path"`
		Bytes    int64  `json:"bytes"`
		Selected int    `json:"selected"`
	} `json:"files"`
	Links []string `json:"links"`
}

// TorrentStatus reads /torrents/info. A file's ID is Real-Debrid's number for
// it while the torrent waits for a selection, and its restricted link once the
// torrent is downloaded, which is what FileURL unlocks.
func (r *RealDebrid) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	var t rdTorrent
	if err := r.torrentCall(ctx, http.MethodGet, "/torrents/info/"+url.PathEscape(id), nil, "", &t); err != nil {
		return TorrentJob{}, err
	}
	job := TorrentJob{
		Name:     t.Filename,
		Size:     t.Bytes,
		Progress: t.Progress / 100,
		Speed:    t.Speed,
		Seeds:    t.Seeders,
	}
	switch t.Status {
	case "downloaded":
		job.State = TorrentReady
	case "waiting_files_selection":
		job.State = TorrentChoosing
	case "magnet_error", "error", "virus", "dead":
		job.State = TorrentFailed
		job.Reason = "Real-Debrid marked the torrent " + strings.ReplaceAll(t.Status, "_", " ")
	}
	var selected []int
	for i, f := range t.Files {
		job.Files = append(job.Files, TorrentFile{ID: strconv.Itoa(f.ID), Path: f.Path, Size: f.Bytes})
		if f.Selected == 1 {
			selected = append(selected, i)
		}
	}
	if job.State != TorrentReady {
		return job, nil
	}
	if len(selected) == len(t.Links) {
		for k, i := range selected {
			job.Files[i].ID, job.Files[i].Held = t.Links[k], true
		}
		return job, nil
	}
	// Real-Debrid packed the selection into fewer archives, so its links no
	// longer line up with the files and each is fetched under the name the
	// unlock gives it.
	for _, l := range t.Links {
		job.Files = append(job.Files, TorrentFile{ID: l, Held: true})
	}
	return job, nil
}

func (r *RealDebrid) SelectFiles(ctx context.Context, id string, files []TorrentFile) error {
	ids := make([]string, len(files))
	for i, f := range files {
		ids[i] = f.ID
	}
	form := url.Values{"files": {strings.Join(ids, ",")}}
	return r.torrentCall(ctx, http.MethodPost, "/torrents/selectFiles/"+url.PathEscape(id), strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil)
}

func (r *RealDebrid) FileURL(ctx context.Context, _ string, f TorrentFile) (Direct, error) {
	return r.Unlock(ctx, f.ID)
}

func (r *RealDebrid) DeleteTorrent(ctx context.Context, id string) error {
	return r.torrentCall(ctx, http.MethodDelete, "/torrents/delete/"+url.PathEscape(id), nil, "", nil)
}

// rdListPage is how many torrents List asks for. Real-Debrid lists the newest
// first, so one page shows what was added since the last look.
const rdListPage = 100

// List reads the newest page of /torrents.
func (r *RealDebrid) List(ctx context.Context) ([]Listed, bool, error) {
	var page []struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Hash     string `json:"hash"`
		Bytes    int64  `json:"bytes"`
		Added    string `json:"added"`
	}
	q := url.Values{"limit": {strconv.Itoa(rdListPage)}}
	if err := r.torrentCall(ctx, http.MethodGet, "/torrents?"+q.Encode(), nil, "", &page); err != nil {
		return nil, false, err
	}
	out := make([]Listed, 0, len(page))
	for _, t := range page {
		out = append(out, Listed{ID: t.ID, Name: t.Filename, Size: t.Bytes, Hash: strings.ToLower(t.Hash), Added: parseStamp(t.Added)})
	}
	return out, len(page) < rdListPage, nil
}
