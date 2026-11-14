package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Info is one torrent as torrents/info lists it.
type Info struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	// Size counts the files picked, TotalSize every file.
	Size        int64   `json:"size"`
	TotalSize   int64   `json:"total_size"`
	Downloaded  int64   `json:"downloaded"`
	Uploaded    int64   `json:"uploaded"`
	Ratio       float64 `json:"ratio"`
	DLSpeed     int64   `json:"dlspeed"`
	UPSpeed     int64   `json:"upspeed"`
	Seeds       int     `json:"num_seeds"`
	Leechs      int     `json:"num_leechs"`
	SeedingTime int64   `json:"seeding_time"`
	AmountLeft  int64   `json:"amount_left"`
	SavePath    string  `json:"save_path"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"`
}

// Filter narrows Torrents; the zero Filter lists every torrent.
type Filter struct {
	Tag      string
	Category string
	Hashes   []string
}

// File is one file of a torrent. Priority 0 leaves it out.
type File struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// Properties is the part of torrents/properties KnightLoader reads.
type Properties struct {
	PieceSize int64  `json:"piece_size"`
	Pieces    int    `json:"pieces_num"`
	SavePath  string `json:"save_path"`
}

// Torrents lists the torrents f names.
func (c *Client) Torrents(ctx context.Context, f Filter) ([]Info, error) {
	q := url.Values{}
	if f.Tag != "" {
		q.Set("tag", f.Tag)
	}
	if f.Category != "" {
		q.Set("category", f.Category)
	}
	if len(f.Hashes) > 0 {
		q.Set("hashes", strings.Join(f.Hashes, "|"))
	}
	var out []Info
	return out, c.getJSON(ctx, "torrents/info", q, "", &out)
}

// Files lists the files of the torrent with hash, in the torrent's order.
func (c *Client) Files(ctx context.Context, hash string) ([]File, error) {
	var out []File
	return out, c.getJSON(ctx, "torrents/files", url.Values{"hash": {hash}}, hash, &out)
}

// PieceStates is each piece of the torrent: 0 missing, 1 on its way, 2 here.
func (c *Client) PieceStates(ctx context.Context, hash string) ([]int, error) {
	var out []int
	return out, c.getJSON(ctx, "torrents/pieceStates", url.Values{"hash": {hash}}, hash, &out)
}

// Properties reads the torrent's piece geometry and save path.
func (c *Client) Properties(ctx context.Context, hash string) (Properties, error) {
	var out Properties
	return out, c.getJSON(ctx, "torrents/properties", url.Values{"hash": {hash}}, hash, &out)
}

// SetFilePriority gives the files of the torrent at ids the priority, 0 to
// leave them out and 1 to fetch them.
func (c *Client) SetFilePriority(ctx context.Context, hash string, ids []int, priority int) error {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return c.post(ctx, "torrents/filePrio", url.Values{
		"hash":     {hash},
		"id":       {strings.Join(parts, "|")},
		"priority": {strconv.Itoa(priority)},
	}, hash)
}

// Stop stops the torrents. 5.0 calls this torrents/stop and 4.x
// torrents/pause, and each answers 404 to the other's name.
func (c *Client) Stop(ctx context.Context, hashes ...string) error {
	return c.either(ctx, "torrents/stop", "torrents/pause", hashes)
}

// Start starts the torrents, through torrents/start or torrents/resume.
func (c *Client) Start(ctx context.Context, hashes ...string) error {
	return c.either(ctx, "torrents/start", "torrents/resume", hashes)
}

// Delete removes the torrents, and their files with deleteFiles.
func (c *Client) Delete(ctx context.Context, deleteFiles bool, hashes ...string) error {
	return c.post(ctx, "torrents/delete", url.Values{
		"hashes":      {strings.Join(hashes, "|")},
		"deleteFiles": {strconv.FormatBool(deleteFiles)},
	}, "")
}

// SetLocation moves the torrents' files to dir.
func (c *Client) SetLocation(ctx context.Context, dir string, hashes ...string) error {
	return c.post(ctx, "torrents/setLocation", url.Values{
		"hashes":   {strings.Join(hashes, "|")},
		"location": {dir},
	}, "")
}

func (c *Client) either(ctx context.Context, modern, older string, hashes []string) error {
	form := []byte(url.Values{"hashes": {strings.Join(hashes, "|")}}.Encode())
	a, err := c.call(ctx, http.MethodPost, modern, formType, form)
	if err == nil && a.status == http.StatusNotFound {
		a, err = c.call(ctx, http.MethodPost, older, formType, form)
	}
	if err != nil {
		return err
	}
	if a.status != http.StatusOK {
		return refused(older, a)
	}
	return nil
}

func (c *Client) post(ctx context.Context, endpoint string, form url.Values, hash string) error {
	a, err := c.call(ctx, http.MethodPost, endpoint, formType, []byte(form.Encode()))
	if err != nil {
		return err
	}
	return answered(endpoint, a, hash)
}

func (c *Client) getJSON(ctx context.Context, endpoint string, q url.Values, hash string, v any) error {
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	a, err := c.call(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return err
	}
	if err := answered(endpoint, a, hash); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(a.body), v); err != nil {
		return &Error{Reason: "qBittorrent's answer could not be read", Err: err}
	}
	return nil
}

// answered turns a status other than 200 into an Error. qBittorrent answers
// 404 for a hash it does not hold.
func answered(what string, a answer, hash string) error {
	switch {
	case a.status == http.StatusOK:
		return nil
	case a.status == http.StatusNotFound && hash != "":
		return &Error{Reason: "qBittorrent has no torrent " + hash}
	}
	return refused(what, a)
}
