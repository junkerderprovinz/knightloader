package mediafire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where Mediafire's pages and API answer.
const DefaultSite = "https://www.mediafire.com"

// maxAnswer caps how much of an answer or a page is read. A file page is
// about 330 KB.
const maxAnswer = 4 << 20

// maxConns is the most connections one Mediafire download opens, as
// JDownloader's plugin allows a free download.
const maxConns = 15

// Client asks Mediafire's public API about files and folders and reads the
// file pages for their download links.
type Client struct {
	hc   *http.Client
	site string
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// fileInfo is the part of the API's file description the client reads. The
// API writes every number as a string.
type fileInfo struct {
	QuickKey   string `json:"quickkey"`
	Name       string `json:"filename"`
	Size       string `json:"size"`
	DeleteDate string `json:"delete_date"`
	Links      struct {
		NormalDownload string `json:"normal_download"`
	} `json:"links"`
}

func (f fileInfo) size() int64 {
	n, _ := strconv.ParseInt(f.Size, 10, 64)
	return n
}

type folderInfo struct {
	FolderKey string `json:"folderkey"`
	Name      string `json:"name"`
}

// apiError is the API turning a request down, with Mediafire's error number.
type apiError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return "mediafire: " + e.Message }

// The API's numbers for a key it does not know: a file's, a folder's, and a
// folder that is not shared.
const (
	errUnknownFile   = 110
	errUnknownFolder = 112
	errNoAccess      = 114
)

// call asks the API's action with q and decodes its response into out.
func (c *Client) call(ctx context.Context, action string, q url.Values, out any) error {
	q.Set("response_format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/api/1.5/"+action+".php?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return err
	}
	var a struct {
		Response json.RawMessage `json:"response"`
	}
	var head struct {
		Result string `json:"result"`
		apiError
	}
	if json.Unmarshal(body, &a) != nil || json.Unmarshal(a.Response, &head) != nil {
		return fmt.Errorf("mediafire: the API answered %d in a shape it does not use", resp.StatusCode)
	}
	if head.Result == "Error" {
		return &head.apiError
	}
	if err := json.Unmarshal(a.Response, out); err != nil {
		return fmt.Errorf("mediafire: the API answered in a shape it does not use: %w", err)
	}
	return nil
}

// infos asks for the files of keys in one request. A key Mediafire does not
// know is left out of the answer.
func (c *Client) infos(ctx context.Context, keys []string) (map[string]fileInfo, error) {
	// One key is answered under file_info, several under file_infos.
	var r struct {
		One  *fileInfo  `json:"file_info"`
		Many []fileInfo `json:"file_infos"`
	}
	err := c.call(ctx, "file/get_info", url.Values{"quick_key": {strings.Join(keys, ",")}}, &r)
	var e *apiError
	if errors.As(err, &e) && e.Code == errUnknownFile {
		return map[string]fileInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	if r.One != nil {
		r.Many = append(r.Many, *r.One)
	}
	out := make(map[string]fileInfo, len(r.Many))
	for _, f := range r.Many {
		out[f.QuickKey] = f
	}
	return out, nil
}

// folderError turns the API's answer for a folder it will not open into a
// refusal.
func folderError(err error) error {
	var e *apiError
	switch {
	case errors.As(err, &e) && e.Code == errUnknownFolder:
		return hoster.Gone("mediafire")
	case errors.As(err, &e) && e.Code == errNoAccess:
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "mediafire: the owner keeps this folder private"}
	}
	return err
}

func (c *Client) folderName(ctx context.Context, key string) (string, error) {
	var r struct {
		Info folderInfo `json:"folder_info"`
	}
	if err := c.call(ctx, "folder/get_info", url.Values{"folder_key": {key}}, &r); err != nil {
		return "", folderError(err)
	}
	return r.Info.Name, nil
}

// maxChunks caps the pages of one folder's content, a hundred entries each.
const maxChunks = 100

// content lists a folder's files or its subfolders, kind "files" or
// "folders", over every page. The list of the other kind stays empty.
func (c *Client) content(ctx context.Context, key, kind string) ([]fileInfo, []folderInfo, error) {
	var files []fileInfo
	var folders []folderInfo
	for chunk := 1; chunk <= maxChunks; chunk++ {
		var r struct {
			Content struct {
				Files      []fileInfo   `json:"files"`
				Folders    []folderInfo `json:"folders"`
				MoreChunks string       `json:"more_chunks"`
			} `json:"folder_content"`
		}
		q := url.Values{"folder_key": {key}, "content_type": {kind}, "chunk": {strconv.Itoa(chunk)}}
		if err := c.call(ctx, "folder/get_content", q, &r); err != nil {
			return nil, nil, folderError(err)
		}
		files = append(files, r.Content.Files...)
		folders = append(folders, r.Content.Folders...)
		if r.Content.MoreChunks != "yes" {
			break
		}
	}
	return files, folders, nil
}
