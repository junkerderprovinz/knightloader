// Package jd talks to a headless JDownloader through its local "Deprecated API"
// (plain HTTP JSON on :3128, no cloud, no crypto). KnightLoader uses JD as an
// arm's-length resolver/backend: JD crawls and fetches from its ~1000 hoster
// plugins, KnightLoader mirrors the progress into its own UI.
package jd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Client is a minimal JD Deprecated-API client.
type Client struct {
	base string
	hc   *http.Client
}

// generalSettings is JD's global settings interface for config/set.
const generalSettings = "org.jdownloader.settings.GeneralSettings"

// SetSpeedLimit on the backend forwards the app's global limit to JD.
func (b *Backend) SetSpeedLimit(bytesPerSec int64) error { return b.c.SetSpeedLimit(bytesPerSec) }

// SetSpeedLimit applies a global JD download limit in bytes/s; 0 disables the
// limit (the stored value is kept, only the enable flag is cleared).
func (c *Client) SetSpeedLimit(bytesPerSec int64) error {
	if bytesPerSec > 0 {
		if _, err := c.call("/config/set", generalSettings, nil, "DownloadSpeedLimit", bytesPerSec); err != nil {
			return err
		}
		_, err := c.call("/config/set", generalSettings, nil, "DownloadSpeedLimitEnabled", true)
		return err
	}
	_, err := c.call("/config/set", generalSettings, nil, "DownloadSpeedLimitEnabled", false)
	return err
}

func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), hc: httpx.New(httpx.Options{Timeout: 15 * time.Second})}
}

// call invokes a method: GET /namespace/method?<enc(param0)>&<enc(param1)>...
// Each parameter is URL-encoded JSON; the response envelope is {"data": <result>}.
func (c *Client) call(path string, params ...any) (json.RawMessage, error) {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		b, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		parts = append(parts, url.QueryEscape(string(b)))
	}
	u := c.base + path
	if len(parts) > 0 {
		u += "?" + strings.Join(parts, "&")
	}
	resp, err := c.hc.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// JD's Deprecated API can emit non-UTF-8 bytes (Latin-1) inside string values
	// (e.g. odd filenames), which breaks encoding/json. Scrub to valid UTF-8;
	// JSON structure is ASCII, so only affected string chars become U+FFFD.
	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), "�"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jd %s: HTTP %d: %s", path, resp.StatusCode, trunc(body))
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("jd %s: bad json: %w", path, err)
	}
	return env.Data, nil
}

// Ping checks the API is reachable (the self-describing /help page).
func (c *Client) Ping() error {
	resp, err := c.hc.Get(c.base + "/help")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jd /help: HTTP %d", resp.StatusCode)
	}
	return nil
}

// AddLinks pushes links into JD. autostart=true makes JD crawl, move to the
// download list and start automatically. Returns the collecting job id.
func (c *Client) AddLinks(links, packageName string, autostart bool) (int64, error) {
	data, err := c.call("/linkgrabberv2/addLinks", map[string]any{
		"links":       links,
		"packageName": packageName,
		"autostart":   autostart,
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(data, &res)
	return res.ID, nil
}

// DownloadLink is one entry in JD's download list.
type DownloadLink struct {
	UUID        int64  `json:"uuid"`
	Name        string `json:"name"`
	PackageUUID int64  `json:"packageUUID"`
	BytesLoaded int64  `json:"bytesLoaded"`
	BytesTotal  int64  `json:"bytesTotal"`
	Speed       int64  `json:"speed"`
	Finished    bool   `json:"finished"`
	Status      string `json:"status"`
}

// QueryDownloads returns the live download links for one package. Scoping the
// query to a single package keeps the response small and, crucially, avoids
// unrelated links whose odd filenames can make JD emit malformed JSON.
func (c *Client) QueryDownloads(packageUUID int64) ([]DownloadLink, error) {
	data, err := c.call("/downloadsV2/queryLinks", map[string]any{
		"bytesLoaded":  true,
		"bytesTotal":   true,
		"speed":        true,
		"status":       true,
		"finished":     true,
		"name":         true,
		"packageUUIDs": []int64{packageUUID},
	})
	if err != nil {
		return nil, err
	}
	var out []DownloadLink
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// downloadPackage is one entry in JD's download package list.
type downloadPackage struct {
	UUID int64  `json:"uuid"`
	Name string `json:"name"`
}

// PackageUUID returns the download-list package whose name matches, or 0.
func (c *Client) PackageUUID(name string) (int64, error) {
	data, err := c.call("/downloadsV2/queryPackages", map[string]any{"packageUUIDs": []int64{}})
	if err != nil {
		return 0, err
	}
	var pkgs []downloadPackage
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return 0, err
	}
	for _, p := range pkgs {
		if p.Name == name {
			return p.UUID, nil
		}
	}
	return 0, nil
}

// CrawledLink is one entry in JD's link grabber — the staging list a container
// is decrypted into, which is a different list from the downloads.
type CrawledLink struct {
	UUID int64  `json:"uuid"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// AddContainerLinks hands JD a container and pins the package it lands in.
//
// overwritePackagizerRules is the load-bearing part. Without it JD names the
// package after what it found *inside* the container — a DLC of a film arrives
// as the film's own release name — and there is then no way to tell our links
// from the ones the user added through JD's own window. With it, the name we
// pass wins, which is how the crawl is identified afterwards.
//
// Identifying it by the job id this returns does not work, however reasonable
// it looks: queryLinks accepts a jobUUIDs filter and it never matches, staying
// empty while the unfiltered query shows every link. Measured against a live JD,
// not assumed.
func (c *Client) AddContainerLinks(url, packageName string) (int64, error) {
	data, err := c.call("/linkgrabberv2/addLinks", map[string]any{
		"links":                    url,
		"packageName":              packageName,
		"autostart":                false,
		"overwritePackagizerRules": true,
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(data, &res)
	return res.ID, nil
}

// CrawledPackageUUID finds a link-grabber package by the name we gave it, or 0.
func (c *Client) CrawledPackageUUID(name string) (int64, error) {
	data, err := c.call("/linkgrabberv2/queryPackages", map[string]any{"name": true})
	if err != nil {
		return 0, err
	}
	var pkgs []downloadPackage
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return 0, err
	}
	for _, p := range pkgs {
		if p.Name == name {
			return p.UUID, nil
		}
	}
	return 0, nil
}

// Collecting reports whether the link grabber is still crawling. Asked before
// reading the results: a container that has produced three of its eleven links
// looks exactly like a finished one to a query, and harvesting there loses the
// other eight without any error to notice.
func (c *Client) Collecting() (bool, error) {
	data, err := c.call("/linkgrabberv2/isCollecting")
	if err != nil {
		return false, err
	}
	var busy bool
	if err := json.Unmarshal(data, &busy); err != nil {
		return false, err
	}
	return busy, nil
}

// CrawledLinks returns the links in one link-grabber package. Scoped to the
// package rather than reading the whole grabber, because anything the user put
// there through JD's own window is theirs and must not be swept up with ours.
func (c *Client) CrawledLinks(packageUUID int64) ([]CrawledLink, error) {
	data, err := c.call("/linkgrabberv2/queryLinks", map[string]any{
		"url":          true,
		"name":         true,
		"host":         true,
		"packageUUIDs": []int64{packageUUID},
	})
	if err != nil {
		return nil, err
	}
	var out []CrawledLink
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveCrawledPackage clears our package out of the link grabber. Called once
// its links have been read: JD's staging list is not our storage, and leaving
// every container we ever opened in it turns the user's own grabber into a bin.
func (c *Client) RemoveCrawledPackage(packageUUID int64) error {
	if packageUUID == 0 {
		return nil
	}
	_, err := c.call("/linkgrabberv2/removeLinks", []int64{}, []int64{packageUUID})
	return err
}

// RemoveLinks removes links (and/or whole packages) from the download list.
func (c *Client) RemoveLinks(linkIDs, packageIDs []int64) error {
	if linkIDs == nil {
		linkIDs = []int64{}
	}
	if packageIDs == nil {
		packageIDs = []int64{}
	}
	_, err := c.call("/downloadsV2/removeLinks", linkIDs, packageIDs)
	return err
}

// SetEnabled pauses (false) or resumes (true) links in the download list.
func (c *Client) SetEnabled(enabled bool, linkIDs []int64) error {
	_, err := c.call("/downloadsV2/setEnabled", enabled, linkIDs, []int64{})
	return err
}

func trunc(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
