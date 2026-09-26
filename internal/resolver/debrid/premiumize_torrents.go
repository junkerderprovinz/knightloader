package debrid

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Premiumize.me's transfer API: https://www.premiumize.me/api. A finished
// transfer leaves its files in a folder of the account's cloud, or as a single
// file there, and every file in the cloud carries its own download link.

func (p *Premiumize) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	var data struct {
		pmStatus
		ID string `json:"id"`
	}
	if src.Magnet != "" {
		if err := p.post(ctx, "/transfer/create", url.Values{"src": {src.Magnet}}, &data); err != nil {
			return "", false, err
		}
	} else {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("src", torrentFileName(src))
		if err != nil {
			return "", false, err
		}
		if _, err := fw.Write(src.File); err != nil {
			return "", false, err
		}
		if err := mw.Close(); err != nil {
			return "", false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/transfer/create", &body)
		if err != nil {
			return "", false, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if err := p.send(req, "/transfer/create", &data); err != nil {
			return "", false, err
		}
	}
	if data.Status != "success" {
		if pmLoginTrouble(data.Message) {
			return "", false, data.err("/transfer/create")
		}
		return "", false, &Refusal{Reason: pmReason(data.pmStatus)}
	}
	if data.ID == "" {
		return "", false, fmt.Errorf("premiumize /transfer/create: the transfer was taken but no id came back")
	}
	return data.ID, false, nil
}

// pmLoginTrouble reports whether Premiumize failed a call over the key rather
// than the torrent. It says so only in words.
func pmLoginTrouble(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "logged in") || strings.Contains(m, "log in") || strings.Contains(m, "apikey")
}

func pmReason(s pmStatus) string {
	if s.Message != "" {
		return s.Message
	}
	if s.Code != "" {
		return s.Code
	}
	return "Premiumize declined the transfer and named no reason"
}

// pmTransfer is one entry of /transfer/list. Progress runs from 0 to 1.
type pmTransfer struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Message  string  `json:"message"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	FolderID string  `json:"folder_id"`
	FileID   string  `json:"file_id"`
}

// pmListFresh is how long one /transfer/list answer serves every caller on the
// account. Premiumize reads transfers only as the whole list, which every
// task's poll would otherwise fetch again for itself.
const pmListFresh = 5 * time.Second

// transfers returns the account's transfers, from an answer at most
// listFresh old unless fresh is set, and whether it was read for this call.
// Callers wait for a read in flight rather than send their own.
func (p *Premiumize) transfers(ctx context.Context, fresh bool) ([]pmTransfer, bool, error) {
	p.listMu.Lock()
	defer p.listMu.Unlock()
	if !fresh && time.Since(p.listAt) < p.listFresh {
		return p.listed, false, nil
	}
	var data struct {
		pmStatus
		Transfers []pmTransfer `json:"transfers"`
	}
	if err := p.get(ctx, "/transfer/list", &data); err != nil {
		return nil, false, err
	}
	if err := data.err("/transfer/list"); err != nil {
		return nil, false, err
	}
	p.listed, p.listAt = data.Transfers, time.Now()
	return data.Transfers, true, nil
}

// transfer finds one transfer in /transfer/list, the only call that reads
// transfers. One that is gone was deleted on the service.
func (p *Premiumize) transfer(ctx context.Context, id string) (pmTransfer, error) {
	is := func(t pmTransfer) bool { return t.ID == id }
	list, read, err := p.transfers(ctx, false)
	if err == nil && !read && !slices.ContainsFunc(list, is) {
		// Added since the shared answer was read.
		list, _, err = p.transfers(ctx, true)
	}
	if err != nil {
		return pmTransfer{}, err
	}
	if i := slices.IndexFunc(list, is); i >= 0 {
		return list[i], nil
	}
	return pmTransfer{}, &Refusal{Reason: "the transfer is gone from Premiumize"}
}

func (p *Premiumize) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	t, err := p.transfer(ctx, id)
	if err != nil {
		return TorrentJob{}, err
	}
	job := TorrentJob{Name: t.Name, Progress: t.Progress}
	switch t.Status {
	case "finished", "seeding":
		job.State = TorrentReady
	case "error", "deleted", "banned", "timeout":
		job.State = TorrentFailed
		job.Reason = pmReason(pmStatus{Message: t.Message, Code: t.Status})
		return job, nil
	default:
		return job, nil
	}
	switch {
	case t.FileID != "":
		f, err := p.item(ctx, t.FileID)
		if err != nil {
			return TorrentJob{}, err
		}
		job.Files = []TorrentFile{f}
	case t.FolderID != "":
		if job.Files, err = p.folderFiles(ctx, t.FolderID, ""); err != nil {
			return TorrentJob{}, err
		}
	default:
		// /folder/list without an id would list the whole cloud.
		job.State = TorrentFailed
		job.Reason = "Premiumize left no file or folder in your cloud for this transfer, which happens when it is routed to an external cloud"
		return job, nil
	}
	for _, f := range job.Files {
		job.Size += f.Size
	}
	return job, nil
}

// pmItem is a file or folder in the cloud.
type pmItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Link string `json:"link"`
}

func (p *Premiumize) item(ctx context.Context, id string) (TorrentFile, error) {
	var data struct {
		pmStatus
		pmItem
	}
	if err := p.get(ctx, "/item/details?id="+url.QueryEscape(id), &data); err != nil {
		return TorrentFile{}, err
	}
	if data.Status != "" && data.Status != "success" {
		return TorrentFile{}, data.err("/item/details")
	}
	return TorrentFile{ID: data.Link, Path: data.Name, Size: data.Size, Held: data.Link != ""}, nil
}

// pmFolderDepth bounds the walk; a torrent's folders do not nest deeper than
// this, and the listing comes from the service.
const pmFolderDepth = 16

func (p *Premiumize) folderFiles(ctx context.Context, id, prefix string) ([]TorrentFile, error) {
	if strings.Count(prefix, "/") > pmFolderDepth {
		return nil, fmt.Errorf("premiumize: folder %s nests deeper than %d levels", id, pmFolderDepth)
	}
	_, content, err := p.folder(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []TorrentFile
	for _, it := range content {
		if it.Type == "folder" {
			sub, err := p.folderFiles(ctx, it.ID, prefix+it.Name+"/")
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, TorrentFile{ID: it.Link, Path: prefix + it.Name, Size: it.Size, Held: it.Link != ""})
	}
	return out, nil
}

// folder reads one cloud folder: its name and what is in it.
func (p *Premiumize) folder(ctx context.Context, id string) (string, []pmItem, error) {
	var data struct {
		pmStatus
		Name    string   `json:"name"`
		Content []pmItem `json:"content"`
	}
	if err := p.get(ctx, "/folder/list?id="+url.QueryEscape(id), &data); err != nil {
		return "", nil, err
	}
	if err := data.err("/folder/list"); err != nil {
		return "", nil, err
	}
	return data.Name, data.Content, nil
}

func (p *Premiumize) FileURL(_ context.Context, _ string, f TorrentFile) (Direct, error) {
	return Direct{URL: f.ID, Size: f.Size}, nil
}

// DeleteTorrent deletes the transfer and what it left in the cloud, which
// would otherwise keep taking up the account's storage. A folder goes only
// when it carries the transfer's own name, so a folder the transfer was merely
// put into is never deleted with everything else in it.
func (p *Premiumize) DeleteTorrent(ctx context.Context, id string) error {
	t, err := p.transfer(ctx, id)
	if err != nil {
		return err
	}
	if err := p.remove(ctx, "/transfer/delete", id); err != nil {
		return err
	}
	if t.FileID != "" {
		return p.remove(ctx, "/item/delete", t.FileID)
	}
	if t.FolderID == "" {
		return nil
	}
	name, _, err := p.folder(ctx, t.FolderID)
	if err != nil || name != t.Name {
		return err
	}
	return p.remove(ctx, "/folder/delete", t.FolderID)
}

// List reads every transfer on the account: torrents, usenet and web
// downloads alike, since TorrentStatus fetches any of them. A transfer names
// its source only through a proxy link, so none carries an info hash, and the
// import waits for a second look before it takes one.
func (p *Premiumize) List(ctx context.Context) ([]Listed, bool, error) {
	list, _, err := p.transfers(ctx, false)
	if err != nil {
		return nil, false, err
	}
	out := make([]Listed, 0, len(list))
	for _, t := range list {
		out = append(out, Listed{ID: t.ID, Name: t.Name})
	}
	return out, true, nil
}

func (p *Premiumize) remove(ctx context.Context, path, id string) error {
	var data pmStatus
	if err := p.post(ctx, path, url.Values{"id": {id}}, &data); err != nil {
		return err
	}
	return data.err(path)
}
