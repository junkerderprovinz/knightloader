// Package jd talks to a headless JDownloader through its local "Deprecated API"
// (plain HTTP JSON on :3128, no cloud, no crypto). KnightLoader uses JD as an
// arm's-length resolver/backend: JD crawls and fetches from its ~1000 hoster
// plugins, KnightLoader mirrors the progress into its own UI.
package jd

import (
	"encoding/base64"
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

// Version asks JD for its build, the revision number that JDAPIImpl.version()
// returns. JD reports itself by that increasing integer, not by a semantic
// version.
func (c *Client) Version() (int64, error) {
	data, err := c.call("/jd/version")
	if err != nil {
		return 0, err
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return 0, fmt.Errorf("jd /jd/version: %w", err)
	}
	return v, nil
}

// SetDownloadFolder points JD's default download directory at path.
//
// JD's own default resolves against the JVM home ("/root/Downloads" or
// "/Downloads"), which the container's uid 99 cannot write, so every package
// fails with "Invalid download directory" and nothing reports it. It is set on
// every start because instances provisioned earlier already have the wrong
// value in their config.
func (c *Client) SetDownloadFolder(path string) error {
	_, err := c.call("/config/set", generalSettings, nil, "DefaultDownloadFolder", path)
	return err
}

// SetPackageDirectory moves one or more download-list packages to dir. Unlike
// addLinks' destinationFolder, to which JD appends the package name, this sets
// the folder verbatim, so JD's files land where every other backend puts them.
func (c *Client) SetPackageDirectory(dir string, pkgUUIDs []int64) error {
	if dir == "" || len(pkgUUIDs) == 0 {
		return nil
	}
	_, err := c.call("/downloadsV2/setDownloadDirectory", dir, pkgUUIDs)
	return err
}

// AddLinks pushes links into JD. autostart=true makes JD crawl, move to the
// download list and start automatically. destination, when set, is the folder
// JD files the package under - see SetPackageDirectory for why the final folder
// is corrected afterwards rather than relied on here. Returns the collecting
// job id.
func (c *Client) AddLinks(links, packageName, destination string, autostart bool) (int64, error) {
	q := map[string]any{
		"links":       links,
		"packageName": packageName,
		"autostart":   autostart,
	}
	if destination != "" {
		q["destinationFolder"] = destination
	}
	data, err := c.call("/linkgrabberv2/addLinks", q)
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
// query keeps the response small and away from unrelated links whose odd
// filenames can make JD emit malformed JSON.
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

// downloadPackage is one entry in JD's download package list. Status matters
// because JD reports an unwritable package only there, never as a link error
// (see fatalPackageStatus).
type downloadPackage struct {
	UUID   int64  `json:"uuid"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// PackageUUID returns the download-list package whose name matches, or 0.
func (c *Client) PackageUUID(name string) (int64, error) {
	p, err := c.Package(name)
	if err != nil || p == nil {
		return 0, err
	}
	return p.UUID, nil
}

// Package returns the download-list package whose name matches, or nil.
func (c *Client) Package(name string) (*downloadPackage, error) {
	data, err := c.call("/downloadsV2/queryPackages", map[string]any{
		"packageUUIDs": []int64{},
		"status":       true,
	})
	if err != nil {
		return nil, err
	}
	var pkgs []downloadPackage
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return nil, err
	}
	for i := range pkgs {
		if pkgs[i].Name == name {
			return &pkgs[i], nil
		}
	}
	return nil, nil
}

// CrawledLink is one entry in JD's link grabber, the staging list a container
// is decrypted into, separate from the download list.
type CrawledLink struct {
	UUID int64  `json:"uuid"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Host string `json:"host"`
	// Size is what the crawl already knows about the file, so the caller
	// need not wait for a second crawl at download time.
	Size int64 `json:"bytesTotal"`
	// PackageUUID is the grabber package the link ended up in, the only way
	// to follow a container that opens into several packages.
	PackageUUID int64 `json:"packageUUID"`
	// Availability is the hoster plugin's verdict: "ONLINE", "OFFLINE", or
	// anything else when the plugin has no opinion. Plugins answer it without
	// a premium account.
	Availability string `json:"availability"`
}

// AddContainerLinks hands JD a container and asks for the package it lands in
// to be named packageName. It returns the crawl job's id.
//
// Name and job id are two independent anchors for finding the result, and
// neither is enough alone. overwritePackagizerRules makes the given name win
// over the container's own, but a container that declares several packages
// can still open into those. The jobUUIDs filter of queryLinks is ignored by
// JD revision 48637 and answers with the whole grabber, so
// Backend.awaitContainerLinks takes the union of both and
// Backend.jobFilterProbe checks whether the filter works.
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

// AddPlainLinks stages plain links under one marker package without starting
// them, for Backend.CheckLinks. Like AddContainerLinks it pins the name
// against the user's packagizer rules and returns the job id as a second
// anchor.
func (c *Client) AddPlainLinks(links, packageName string) (int64, error) {
	data, err := c.call("/linkgrabberv2/addLinks", map[string]any{
		"links":                    links,
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

// AddContainerData hands JD an encrypted container as inline content and is
// followed back the same way as AddContainerLinks.
//
// It serves Click'n'Load's addcrypted, whose payload exists only as a POST
// field with no URL to hand JD. JD decodes a dataURLs entry to a temp file
// named by ext and crawls it like a fetched URL; ext is "dlc" for addcrypted
// because JD's own CnL listener treats the field the same way.
func (c *Client) AddContainerData(ext string, data []byte, packageName string) (int64, error) {
	dataURL := "data:application/" + ext + ";base64," + base64.StdEncoding.EncodeToString(data)
	res, err := c.call("/linkgrabberv2/addLinks", map[string]any{
		"dataURLs":                 []string{dataURL},
		"packageName":              packageName,
		"autostart":                false,
		"overwritePackagizerRules": true,
	})
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(res, &out)
	return out.ID, nil
}

// CrawledPackages lists every package in JD's link grabber, not only ours:
// callers need all packages carrying a marker, and whether the grabber holds
// anything at all for the jobUUIDs probe.
func (c *Client) CrawledPackages() ([]downloadPackage, error) {
	data, err := c.call("/linkgrabberv2/queryPackages", map[string]any{"name": true})
	if err != nil {
		return nil, err
	}
	var pkgs []downloadPackage
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

// Collecting reports whether the link grabber is crawling anything at all. It
// is only a hint: the flag is global to the instance and also drops between
// sub-crawls, so a crawl counts as finished when its link count stops
// changing (see Backend.awaitContainerLinks).
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

// crawledLinkFields is the set of per-link facts every grabber query asks for.
// A field left out comes back zeroed without an error.
func crawledLinkFields() map[string]any {
	return map[string]any{
		"url":          true,
		"name":         true,
		"host":         true,
		"bytesTotal":   true,
		"availability": true,
		"packageUUID":  true,
	}
}

func (c *Client) queryCrawledLinks(q map[string]any) ([]CrawledLink, error) {
	data, err := c.call("/linkgrabberv2/queryLinks", q)
	if err != nil {
		return nil, err
	}
	var out []CrawledLink
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CrawledLinks returns the links in the given link-grabber packages. With no
// package it returns nothing, since an unfiltered query would include links
// the user added through JD's own window.
func (c *Client) CrawledLinks(packageUUIDs ...int64) ([]CrawledLink, error) {
	if len(packageUUIDs) == 0 {
		return nil, nil
	}
	q := crawledLinkFields()
	q["packageUUIDs"] = packageUUIDs
	return c.queryCrawledLinks(q)
}

// CrawledLinksForJob returns the links one addLinks job produced. It has come
// back empty on a live JD while the links were there, so an empty answer means
// no news, not an empty container.
func (c *Client) CrawledLinksForJob(jobUUID int64) ([]CrawledLink, error) {
	if jobUUID == 0 {
		return nil, nil
	}
	q := crawledLinkFields()
	q["jobUUIDs"] = []int64{jobUUID}
	return c.queryCrawledLinks(q)
}

// RemoveCrawled clears our crawl out of the link grabber once it has been
// read, by link id and by package. A leftover link poisons the grabber: JD's
// duplicate manager silently drops any newly crawled link it already holds,
// so that link would vanish from every later container (see
// Backend.sweepGrabber).
func (c *Client) RemoveCrawled(linkUUIDs, packageUUIDs []int64) error {
	linkUUIDs = nonZero(linkUUIDs)
	packageUUIDs = nonZero(packageUUIDs)
	if len(linkUUIDs) == 0 && len(packageUUIDs) == 0 {
		return nil
	}
	_, err := c.call("/linkgrabberv2/removeLinks", linkUUIDs, packageUUIDs)
	return err
}

// nonZero drops unset ids, since what removeLinks does with a zero is unknown.
func nonZero(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
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

// StartDownloads starts JD's download controller. It does nothing when the
// controller already runs.
func (c *Client) StartDownloads() error {
	_, err := c.call("/downloadcontroller/start")
	return err
}

func trunc(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
