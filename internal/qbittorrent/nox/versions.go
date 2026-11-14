// Package nox fetches, configures and runs the qBittorrent KnightLoader drives
// in the background: qbittorrent-nox, the build without a window, on
// 127.0.0.1 with a WebUI login only KnightLoader knows.
//
// Private trackers admit only the clients on their whitelist, so the version
// is pinned to one they accept and never follows the newest release. The pins
// are data, in versions.json, so a new one is a change to the table.
package nox

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed versions.json
var versionsJSON []byte

// Table is the versions KnightLoader knows and the one it runs unless told
// otherwise.
type Table struct {
	Default  string    `json:"default"`
	Versions []Version `json:"versions"`
}

// Version is one qBittorrent release and its builds, by "goos-goarch".
// Whitelists names the trackers whose public client list admits it.
type Version struct {
	Version    string           `json:"version"`
	Libtorrent string           `json:"libtorrent"`
	Whitelists []string         `json:"whitelists"`
	Note       string           `json:"note"`
	Assets     map[string]Asset `json:"assets"`
}

// Asset is one build of a version. A build without Repo comes from the release
// location (ReleaseLocation), where this project's own builds are published.
type Asset struct {
	Repo string `json:"repo,omitempty"`
	Tag  string `json:"tag"`
	Name string `json:"name"`
	// Bin is the program's path inside a zipped build, when it is not
	// qbittorrent-nox at the top, such as inside a macOS app bundle.
	Bin    string `json:"bin,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

func (a Asset) from() string {
	if a.Repo != "" {
		return a.Repo
	}
	return ReleaseLocation()
}

// The repository the Windows and macOS builds are published in. A fork or a
// separate build repository is set with KL_QBITTORRENT_RELEASES.
const (
	releasesEnv     = "KL_QBITTORRENT_RELEASES"
	defaultReleases = "junkerderprovinz/knightloader"
)

// ReleaseLocation is the owner/repo this project's own qbittorrent-nox builds
// are fetched from.
func ReleaseLocation() string {
	if v := strings.TrimSpace(os.Getenv(releasesEnv)); v != "" {
		return v
	}
	return defaultReleases
}

// Versions is the table built into this binary.
func Versions() Table {
	var t Table
	if err := json.Unmarshal(versionsJSON, &t); err != nil {
		panic("nox: versions.json: " + err.Error())
	}
	return t
}

// Pick is the version named, or the default for "".
func (t Table) Pick(version string) (Version, error) {
	if version == "" {
		version = t.Default
	}
	for _, v := range t.Versions {
		if v.Version == version {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("qBittorrent %s is not a version this KnightLoader knows", version)
}

// AssetFor is the build for one platform.
func (v Version) AssetFor(goos, goarch string) (Asset, bool) {
	a, ok := v.Assets[goos+"-"+goarch]
	return a, ok
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
