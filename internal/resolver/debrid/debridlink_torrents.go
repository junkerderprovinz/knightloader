package debrid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Debrid-Link's seedbox API: https://debrid-link.com/api_doc/v2/seedbox. Every
// file of a torrent carries its own download link once it is complete, and a
// torrent added with wait set holds until /seedbox/{id}/config names the files
// to leave out.

// dlError is a call Debrid-Link answered with success false.
type dlError struct{ path, code string }

func (e *dlError) Error() string {
	return fmt.Sprintf("debrid-link %s: %s", e.path, errorText(e.code))
}

// dlDeclines are the error codes that decline the torrent itself, in words a
// person can act on.
var dlDeclines = map[string]string{
	"notAddTorrent": "Debrid-Link could not add the torrent",
	"torrentTooBig": "the torrent is too big for Debrid-Link or has too many files",
	"maxTorrent":    "the daily torrent limit for this account is used up",
	"maxTransfer":   "this account already has as many torrents in transfer as it may",
	"badId":         "the torrent is gone from Debrid-Link",
}

// seedboxErr turns a declining answer into a Refusal and leaves any other
// error as it is.
func seedboxErr(err error) error {
	var de *dlError
	if errors.As(err, &de) {
		if why, ok := dlDeclines[de.code]; ok {
			return &Refusal{Reason: why}
		}
	}
	return err
}

// dlTorrent is a torrent as the seedbox endpoints describe it. Both percents
// run from 0 to 100.
type dlTorrent struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Wait            bool    `json:"wait"`
	Status          int     `json:"status"`
	TotalSize       int64   `json:"totalSize"`
	DownloadPercent float64 `json:"downloadPercent"`
	DownloadSpeed   int64   `json:"downloadSpeed"`
	IsZip           bool    `json:"isZip"`
	Files           []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Size            int64   `json:"size"`
		DownloadURL     string  `json:"downloadUrl"`
		DownloadPercent float64 `json:"downloadPercent"`
	} `json:"files"`
}

// dlFinished is the status of a torrent that is complete on the seedbox.
const dlFinished = 100

func (d *DebridLink) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	var t dlTorrent
	var err error
	if src.Magnet != "" {
		err = d.postJSON(ctx, "/seedbox/add", map[string]any{"url": src.Magnet, "wait": src.Choose}, &t)
	} else {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, ferr := mw.CreateFormFile("file", torrentFileName(src))
		if ferr != nil {
			return "", false, ferr
		}
		if _, ferr := fw.Write(src.File); ferr != nil {
			return "", false, ferr
		}
		if ferr := mw.WriteField("wait", strconv.FormatBool(src.Choose)); ferr != nil {
			return "", false, ferr
		}
		if ferr := mw.Close(); ferr != nil {
			return "", false, ferr
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/seedbox/add", &body)
		if rerr != nil {
			return "", false, rerr
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		err = d.send(req, "/seedbox/add", &t)
	}
	if err != nil {
		return "", false, seedboxErr(err)
	}
	if t.ID == "" {
		return "", false, errors.New("debrid-link /seedbox/add: the torrent was taken but no id came back")
	}
	return t.ID, false, nil
}

// torrent reads one torrent with every file it has. For a torrent of many
// files the list shows a single ZIP in place of them, and the docs name ?id=
// as the form that lists them all. A torrent that stays one ZIP there too is
// refused, since its files cannot be fetched one by one.
func (d *DebridLink) torrent(ctx context.Context, id string) (dlTorrent, error) {
	t, err := d.listed(ctx, "ids", id)
	if err != nil || !t.IsZip {
		return t, err
	}
	if t, err = d.listed(ctx, "id", id); err != nil {
		return dlTorrent{}, err
	}
	if t.IsZip {
		return dlTorrent{}, &Refusal{Reason: "Debrid-Link offers this torrent only as one ZIP of all its files"}
	}
	return t, nil
}

// listed reads the torrent id from /seedbox/list, asking by the query key.
func (d *DebridLink) listed(ctx context.Context, key, id string) (dlTorrent, error) {
	var list []dlTorrent
	if err := d.get(ctx, "/seedbox/list?"+key+"="+url.QueryEscape(id), &list); err != nil {
		return dlTorrent{}, seedboxErr(err)
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return dlTorrent{}, &Refusal{Reason: dlDeclines["badId"]}
}

func (d *DebridLink) TorrentStatus(ctx context.Context, id string) (TorrentJob, error) {
	t, err := d.torrent(ctx, id)
	if err != nil {
		return TorrentJob{}, err
	}
	job := TorrentJob{
		Name:     t.Name,
		Size:     t.TotalSize,
		Progress: t.DownloadPercent / 100,
		Speed:    t.DownloadSpeed,
	}
	done := t.Status == dlFinished || t.DownloadPercent >= 100
	for _, f := range t.Files {
		job.Files = append(job.Files, TorrentFile{ID: f.ID, Path: f.Name, Size: f.Size, Held: f.DownloadPercent >= 100 && f.DownloadURL != ""})
	}
	switch {
	case t.Wait && len(t.Files) > 0:
		job.State = TorrentChoosing
	case done && len(job.Files) > 0:
		job.State = TorrentReady
	}
	return job, nil
}

// SelectFiles names the files to leave out, which is how Debrid-Link takes a
// selection.
func (d *DebridLink) SelectFiles(ctx context.Context, id string, files []TorrentFile) error {
	t, err := d.torrent(ctx, id)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, f := range files {
		keep[f.ID] = true
	}
	unwanted := []string{}
	for _, f := range t.Files {
		if !keep[f.ID] {
			unwanted = append(unwanted, f.ID)
		}
	}
	return seedboxErr(d.postJSON(ctx, "/seedbox/"+url.PathEscape(id)+"/config", map[string]any{"files-unwanted": unwanted}, nil))
}

// FileURL reads the torrent again for the file's link, which is fixed once the
// file is complete.
func (d *DebridLink) FileURL(ctx context.Context, id string, f TorrentFile) (Direct, error) {
	t, err := d.torrent(ctx, id)
	if err != nil {
		return Direct{}, err
	}
	for _, tf := range t.Files {
		if tf.ID == f.ID && tf.DownloadURL != "" {
			return Direct{URL: tf.DownloadURL, Size: tf.Size}, nil
		}
	}
	return Direct{}, fmt.Errorf("debrid-link: file %s of torrent %s has no link", f.ID, id)
}

// dlListPage is the most torrents /seedbox/list answers with at once.
const dlListPage = 50

// List reads the first page of /seedbox/list, which holds the newest.
func (d *DebridLink) List(ctx context.Context) ([]Listed, bool, error) {
	var page []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		HashString string `json:"hashString"`
		TotalSize  int64  `json:"totalSize"`
		Created    int64  `json:"created"`
	}
	if err := d.get(ctx, "/seedbox/list?perPage="+strconv.Itoa(dlListPage), &page); err != nil {
		return nil, false, err
	}
	out := make([]Listed, 0, len(page))
	for _, t := range page {
		l := Listed{ID: t.ID, Name: t.Name, Size: t.TotalSize, Hash: strings.ToLower(t.HashString)}
		if t.Created > 0 {
			l.Added = time.Unix(t.Created, 0)
		}
		out = append(out, l)
	}
	return out, len(page) < dlListPage, nil
}

func (d *DebridLink) DeleteTorrent(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, d.base+"/seedbox/"+url.PathEscape(id)+"/remove", nil)
	if err != nil {
		return err
	}
	return d.send(req, "/seedbox/remove", nil)
}
