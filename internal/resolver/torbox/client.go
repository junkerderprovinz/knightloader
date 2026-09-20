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

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

const apiBase = "https://api.torbox.app/v1"

// apiTimeout bounds one API call. Unlocking a link is the slow one: TorBox may
// be fetching from the hoster while we wait.
const apiTimeout = 30 * time.Second

type Client struct {
	key  string
	base string
	hc   *http.Client
}

func NewClient(key string) *Client {
	// httpx drops the bearer token if a redirect ever leaves the API host.
	return &Client{key: key, base: apiBase, hc: httpx.New(httpx.Options{Timeout: apiTimeout})}
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

// AccountInfo is one account's plan, premium expiry and lifetime downloaded
// bytes. It mirrors debrid.AccountInfo without importing that package.
type AccountInfo struct {
	Tier      string
	Traffic   TrafficInfo
	ExpiresAt time.Time
}

// TrafficInfo mirrors debrid.TrafficInfo; the app folds both into the same
// app.TrafficState.
type TrafficInfo struct {
	UsedBytes  int64
	LimitBytes int64
	Unlimited  bool
}

// planNames maps the numeric plan to TorBox's names, per its UserService
// documentation. An unknown id reads "unknown".
var planNames = map[int]string{0: "free", 1: "essential", 2: "pro", 3: "standard"}

// Account reads /api/user/me for plan, premium expiry and lifetime downloaded
// bytes; field names follow TorBox's Go SDK.
//
// TorBox exposes no byte cap for any plan. Unlimited follows is_subscribed
// rather than the plan number, so a lapsed subscription stops reading as
// unlimited, and a free account keeps zero traffic.
func (c *Client) Account(ctx context.Context) (AccountInfo, error) {
	var data struct {
		Plan             int     `json:"plan"`
		IsSubscribed     bool    `json:"is_subscribed"`
		PremiumExpiresAt string  `json:"premium_expires_at"`
		TotalDownloaded  float64 `json:"total_downloaded"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/user/me", nil, &data); err != nil {
		return AccountInfo{}, err
	}
	tier, ok := planNames[data.Plan]
	if !ok {
		tier = "unknown"
	}
	info := AccountInfo{
		Tier:    tier,
		Traffic: TrafficInfo{UsedBytes: int64(data.TotalDownloaded), Unlimited: data.IsSubscribed},
	}
	if data.PremiumExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, data.PremiumExpiresAt); err == nil {
			info.ExpiresAt = t
		}
	}
	return info, nil
}

// Hoster describes one supported file host. TorBox returns either a single
// domain or a domains list depending on the host.
//
// Type is "hoster" for a file-hosting service or "stream" for a media site
// TorBox scrapes (YouTube, Twitch, ...), which yt-dlp serves directly. Any
// other value counts as not a plain hoster, so it stays with yt-dlp.
type Hoster struct {
	Name    string   `json:"name"`
	Domain  string   `json:"domain"`
	Domains []string `json:"domains"`
	Status  bool     `json:"status"`
	Type    string   `json:"type"`
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

// Delete removes a web download from the TorBox account.
func (c *Client) Delete(ctx context.Context, id int64) error {
	body, _ := json.Marshal(map[string]any{"webdl_id": id, "operation": "delete"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/webdl/controlwebdownload", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("torbox delete: %s: %w", resp.Status, err)
	}
	if !env.Success {
		return fmt.Errorf("torbox delete: %v %s", env.Error, env.Detail)
	}
	return nil
}
