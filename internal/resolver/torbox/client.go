// Package torbox is the debrid resolver: it turns a supported file-hoster link
// into a direct CDN URL via TorBox's Web Downloads / Debrid API, which the
// embedded engine then downloads. https://api-docs.torbox.app
package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.torbox.app/v1"

type Client struct {
	key  string
	base string
	hc   *http.Client
}

func NewClient(key string) *Client {
	return &Client{key: key, base: apiBase, hc: &http.Client{Timeout: 30 * time.Second}}
}

// envelope is TorBox's uniform response wrapper.
type envelope struct {
	Success bool            `json:"success"`
	Error   any             `json:"error"`
	Detail  string          `json:"detail"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if method == http.MethodPost && form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("torbox %s: %s: %w", path, resp.Status, err)
	}
	if !env.Success {
		return fmt.Errorf("torbox %s: %v %s", path, env.Error, env.Detail)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// Hoster describes one supported file host. TorBox returns either a single
// `domain` or a `domains` list depending on the host.
type Hoster struct {
	Name    string   `json:"name"`
	Domain  string   `json:"domain"`
	Domains []string `json:"domains"`
	Status  bool     `json:"status"`
}

// Hosters returns the supported file hosts. Auth is optional here.
func (c *Client) Hosters(ctx context.Context) ([]Hoster, error) {
	var hs []Hoster
	if err := c.do(ctx, http.MethodGet, "/api/webdl/hosters", nil, &hs); err != nil {
		return nil, err
	}
	return hs, nil
}

// WebFile is one downloadable file inside a web download.
type WebFile struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimetype"`
}

// WebDownload is a debrid job: TorBox fetching a hoster link onto its CDN.
type WebDownload struct {
	ID               int64     `json:"id"`
	Hash             string    `json:"hash"`
	Name             string    `json:"name"`
	Size             int64     `json:"size"`
	Active           bool      `json:"active"`
	DownloadPresent  bool      `json:"download_present"`
	DownloadFinished bool      `json:"download_finished"`
	DownloadState    string    `json:"download_state"`
	Progress         float64   `json:"progress"`
	DownloadSpeed    int64     `json:"download_speed"`
	Files            []WebFile `json:"files"`
}

// createResp tolerates the two field names TorBox has used for the new job id.
type createResp struct {
	ID    int64  `json:"webdownload_id"`
	IDAlt int64  `json:"id"`
	Hash  string `json:"hash"`
}

// CreateWebDownload queues a hoster link and returns the new job id.
func (c *Client) CreateWebDownload(ctx context.Context, link string) (int64, error) {
	var cr createResp
	if err := c.do(ctx, http.MethodPost, "/api/webdl/createwebdownload", url.Values{"link": {link}}, &cr); err != nil {
		return 0, err
	}
	if cr.ID != 0 {
		return cr.ID, nil
	}
	return cr.IDAlt, nil
}

// Get returns the current state of one web download by id.
func (c *Client) Get(ctx context.Context, id int64) (*WebDownload, error) {
	q := url.Values{"bypass_cache": {"true"}, "id": {fmt.Sprint(id)}}
	// With an id, TorBox returns a single object; without, a list. Decode either.
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/webdl/mylist?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	var one WebDownload
	if err := json.Unmarshal(raw, &one); err == nil && one.ID != 0 {
		return &one, nil
	}
	var list []WebDownload
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("torbox: web download %d not found", id)
}

// RequestDL resolves the direct, downloadable CDN URL for one file.
func (c *Client) RequestDL(ctx context.Context, webID, fileID int64) (string, error) {
	q := url.Values{
		"token":   {c.key},
		"web_id":  {fmt.Sprint(webID)},
		"file_id": {fmt.Sprint(fileID)},
	}
	var link string
	if err := c.do(ctx, http.MethodGet, "/api/webdl/requestdl?"+q.Encode(), nil, &link); err != nil {
		return "", err
	}
	return link, nil
}
