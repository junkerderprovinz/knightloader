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

// Version asks JD for its own build - the "jd" namespace's version() call,
// which JDownloader itself defines as its revision number
// (org.jdownloader.api.jd.JDAPIImpl in JD's own open source: version()
// returns JDUtilities.getRevisionNumber()). It is a plain, monotonically
// increasing integer, not a semantic version string - that is genuinely how
// JD reports itself, in its own UI as well as here, so this is deliberately
// int64 rather than a parsed "vX.Y.Z" this app would have to invent.
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

// SetDownloadFolder points JD's own default download directory at path.
//
// This is not a nicety, and the cost of not having it was five rounds of "es
// lädt nirgends was runter" (jdp, 2026-08-27 to 2026-09-01). A headless JD
// nobody has told otherwise downloads into its own default, which resolves
// against the JVM's home directory: measured on the two live instances, one had
// "/root/Downloads" and the other "/Downloads". The container runs as uid 99 and
// can write to neither, so JD answered every single package with the status
// "Invalid download directory" - fourteen out of fourteen when this was found -
// and downloaded nothing, for ever, without ever reporting a failure to anyone.
//
// KnightLoader provisions that JD itself (internal/provision), so its download
// folder is KnightLoader's to set. Applied at every start rather than only at
// provisioning time, because an instance that has already been provisioned has
// the wrong value written into its config file and would otherwise stay broken
// through any number of updates.
func (c *Client) SetDownloadFolder(path string) error {
	_, err := c.call("/config/set", generalSettings, nil, "DefaultDownloadFolder", path)
	return err
}

// SetPackageDirectory moves one or more download-list packages to dir.
//
// Unlike addLinks' destinationFolder, which JD treats as a PARENT and appends
// the package name to (measured: "/data/download/zielA" with package "KL-probeA"
// became "/data/download/zielA/KL-probeA"), this sets the folder verbatim. That
// is what lets a JD-fetched file land exactly where every other backend puts
// one, instead of inside a folder named after an internal task id.
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
//
// Status is asked for because JD says things there that it says NOWHERE else:
// a package it cannot write is reported as a package status, never as a link
// error, so a poller reading only the links sees a healthy package sitting at
// zero bytes. See Backend.poll's fatalPackageStatus.
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

// CrawledLink is one entry in JD's link grabber — the staging list a container
// is decrypted into, which is a different list from the downloads.
type CrawledLink struct {
	UUID int64  `json:"uuid"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Host string `json:"host"`
	// Size is what the crawl itself already knows about the file, the same
	// number JD's own link-grabber window shows in its Size column before
	// anything downloads. Requested here for the same reason Name is: a
	// container's crawl is the one moment this size is free to ask for, and
	// awaitContainerLinks hands both back rather than making the caller wait
	// for a second crawl, at download time, to learn what this one already
	// knew.
	Size int64 `json:"bytesTotal"`
	// Availability is JD's own hoster-plugin verdict on this one link -
	// "ONLINE", "OFFLINE", or absent/something else when the plugin has no
	// opinion. Measured against a live JD (rev 48637): a real rapidgator.net
	// link came back ONLINE and a fabricated one OFFLINE, both without any
	// premium account configured, because a hoster plugin's job is exactly
	// this - reading that host's own file-info signal, something a generic
	// HTTP probe from outside can't do (see Backend.CheckLinks).
	Availability string `json:"availability"`
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

// AddPlainLinks stages a batch of already-known, plain (non-container) links
// under one marker package - the same overwritePackagizerRules pinning
// AddContainerLinks uses and for the identical reason, so a packagizer rule
// the user has configured in JD cannot rename the package out from under the
// marker this app looks it up by afterwards. autostart is always false: this
// exists for Backend.CheckLinks, which only ever wants JD's crawl-time
// verdict, never a download.
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

// AddContainerData hands JD an encrypted container as inline content instead
// of a URL to fetch, and pins the package it lands in exactly as
// AddContainerLinks does (same two reasons: overwritePackagizerRules so our
// name wins over whatever the container decrypts to, and a fresh marker
// because the returned job id does not filter queryLinks — see
// AddContainerLinks's own doc).
//
// This is Click'n'Load's addcrypted (v1): unlike a .dlc/.ccf/.rsdf a user
// saved and later uploaded, that payload was never a file anywhere — it
// exists only as one POST form field — so there is no URL to hand JD for it.
// dataURLs is the Deprecated API's answer to exactly that gap (verified
// against JDownloader's own LinkCollectorAPIImplV2#addLinks: a dataURLs entry
// is base64-decoded to a temp file named by the declared extension and fed
// into the identical crawl entrance a fetched URL would use). ext is that
// declared extension, "dlc" for addcrypted v1 because that is genuinely what
// JD's own listener does with the same field
// (org.jdownloader.api.cnl2.ExternInterfaceImpl#addcrypted writes it to a
// temp .dlc and hands that in) — reusing it here is the identical treatment,
// not a second, KnightLoader-specific decryption path.
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
		"bytesTotal":   true,
		"availability": true,
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
