package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AllDebrid's magnet API: https://docs.alldebrid.com/#magnet. A finished magnet
// lists its files as a tree of AllDebrid links, which /link/unlock turns into
// downloads like any hoster link. The status call lives under v4.1.

// adFailure is an AllDebrid answer about the account, the address or the
// service rather than the torrent. Any MAGNET_ code but these declines the
// torrent.
func adFailure(code string) bool {
	switch code {
	case "MAGNET_NO_SERVER", "MAGNET_PROCESSING":
		return true
	}
	return !strings.HasPrefix(code, "MAGNET_")
}

type adError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e adError) err(path string) error {
	if adFailure(e.Code) {
		return fmt.Errorf("alldebrid %s: %s (%s)", path, e.Message, e.Code)
	}
	return &Refusal{Reason: fmt.Sprintf("%s (%s)", e.Message, e.Code)}
}

// magnetCall sends a torrent call with the key as Bearer token and unwraps the
// envelope, keeping the error code so a declined torrent reads as a Refusal.
func (a *AllDebrid) magnetCall(ctx context.Context, base, path string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	if a.key != "" {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Error  *adError        `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("alldebrid %s: %s: %w", path, resp.Status, err)
	}
	if env.Status != "success" {
		if env.Error != nil {
			return env.Error.err(path)
		}
		return fmt.Errorf("alldebrid %s: %s", path, resp.Status)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (a *AllDebrid) formCall(ctx context.Context, base, path string, form url.Values, out any) error {
	return a.magnetCall(ctx, base, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out)
}

// adMagnet is one entry of an upload answer, which reports a refused magnet
// in place rather than failing the call.
type adMagnet struct {
	ID    int64    `json:"id"`
	Error *adError `json:"error"`
}

func (a *AllDebrid) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	var data struct {
		Magnets []adMagnet `json:"magnets"`
		Files   []adMagnet `json:"files"`
	}
	path := "/magnet/upload"
	if src.Magnet != "" {
		if err := a.formCall(ctx, a.base, path, url.Values{"magnets[]": {src.Magnet}}, &data); err != nil {
			return "", false, err
		}
	} else {
		path = "/magnet/upload/file"
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("files[]", torrentFileName(src))
		if err != nil {
			return "", false, err
		}
		if _, err := fw.Write(src.File); err != nil {
			return "", false, err
		}
		if err := mw.Close(); err != nil {
			return "", false, err
		}
		if err := a.magnetCall(ctx, a.base, path, &body, mw.FormDataContentType(), &data); err != nil {
			return "", false, err
		}
	}
	got := append(data.Magnets, data.Files...)
	if len(got) == 0 {
		return "", false, fmt.Errorf("alldebrid %s: the answer names no magnet", path)
	}
	if got[0].Error != nil {
		return "", false, got[0].Error.err(path)
	}
	return strconv.FormatInt(got[0].ID, 10), false, nil
}

// adStatus is one magnet as /magnet/status describes it.
type adStatus struct {
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	Status        string `json:"status"`
	StatusCode    int    `json:"statusCode"`
	Downloaded    int64  `json:"downloaded"`
	DownloadSpeed int64  `json:"downloadSpeed"`
	Seeders       int    `json:"seeders"`
}

// v41 is the base for the calls AllDebrid only offers under v4.1.
func (a *AllDebrid) v41() string {
	return strings.TrimSuffix(a.base, "/v4") + "/v4.1"
}

func (a *AllDebrid) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	var data struct {
		Magnets json.RawMessage `json:"magnets"`
	}
	if err := a.formCall(ctx, a.v41(), "/magnet/status", url.Values{"id": {id}}, &data); err != nil {
		return TorrentJob{}, err
	}
	// Asked for one id, AllDebrid's own examples show a list and its client
	// libraries read an object, so either is taken.
	var st adStatus
	if err := json.Unmarshal(data.Magnets, &st); err != nil {
		var list []adStatus
		if err := json.Unmarshal(data.Magnets, &list); err != nil || len(list) == 0 {
			return TorrentJob{}, fmt.Errorf("alldebrid /magnet/status: magnet %s is not in the answer", id)
		}
		st = list[0]
	}
	job := TorrentJob{Name: st.Filename, Size: st.Size, Speed: st.DownloadSpeed, Seeds: st.Seeders}
	if st.Size > 0 {
		job.Progress = float64(st.Downloaded) / float64(st.Size)
	}
	switch {
	case st.StatusCode == 4:
		job.State = TorrentReady
	case st.StatusCode > 4:
		job.State = TorrentFailed
		job.Reason = st.Status
		return job, nil
	default:
		return job, nil
	}
	files, err := a.magnetFiles(ctx, id)
	if err != nil {
		return TorrentJob{}, err
	}
	job.Files = files
	return job, nil
}

// adEntry is one node of /magnet/files: a file with a size and a link, or a
// folder with entries.
type adEntry struct {
	Name    string    `json:"n"`
	Size    int64     `json:"s"`
	Link    string    `json:"l"`
	Entries []adEntry `json:"e"`
}

func (a *AllDebrid) magnetFiles(ctx context.Context, id string) ([]TorrentFile, error) {
	var data struct {
		Magnets []struct {
			Files []adEntry `json:"files"`
			Error *adError  `json:"error"`
		} `json:"magnets"`
	}
	if err := a.formCall(ctx, a.base, "/magnet/files", url.Values{"id[]": {id}}, &data); err != nil {
		return nil, err
	}
	if len(data.Magnets) == 0 {
		return nil, fmt.Errorf("alldebrid /magnet/files: magnet %s is not in the answer", id)
	}
	if e := data.Magnets[0].Error; e != nil {
		return nil, e.err("/magnet/files")
	}
	var out []TorrentFile
	var walk func(prefix string, es []adEntry)
	walk = func(prefix string, es []adEntry) {
		for _, e := range es {
			p := prefix + e.Name
			if len(e.Entries) > 0 {
				walk(p+"/", e.Entries)
				continue
			}
			out = append(out, TorrentFile{ID: e.Link, Path: p, Size: e.Size, Held: e.Link != ""})
		}
	}
	walk("", data.Magnets[0].Files)
	return out, nil
}

func (a *AllDebrid) FileURL(ctx context.Context, _ string, f TorrentFile) (Direct, error) {
	return a.Unlock(ctx, f.ID)
}

func (a *AllDebrid) DeleteTorrent(ctx context.Context, id string) error {
	return a.formCall(ctx, a.base, "/magnet/delete", url.Values{"id": {id}}, nil)
}

// List reads every magnet on the account from /magnet/status, which without
// an id answers them all.
func (a *AllDebrid) List(ctx context.Context) ([]Listed, bool, error) {
	var data struct {
		Magnets []struct {
			ID         int64  `json:"id"`
			Filename   string `json:"filename"`
			Size       int64  `json:"size"`
			Hash       string `json:"hash"`
			UploadDate int64  `json:"uploadDate"`
		} `json:"magnets"`
	}
	if err := a.formCall(ctx, a.v41(), "/magnet/status", url.Values{}, &data); err != nil {
		return nil, false, err
	}
	out := make([]Listed, 0, len(data.Magnets))
	for _, m := range data.Magnets {
		l := Listed{ID: strconv.FormatInt(m.ID, 10), Name: m.Filename, Size: m.Size, Hash: strings.ToLower(m.Hash)}
		if m.UploadDate > 0 {
			l.Added = time.Unix(m.UploadDate, 0)
		}
		out = append(out, l)
	}
	return out, true, nil
}

// torrentFileName is what an uploaded .torrent is called in a multipart body.
func torrentFileName(src TorrentSource) string {
	if src.InfoHash != "" {
		return src.InfoHash + ".torrent"
	}
	return "upload.torrent"
}
