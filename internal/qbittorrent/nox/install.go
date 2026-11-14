package nox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// A static build is about 35 MB; the cap only keeps a broken host from filling
// the data folder.
const (
	maxBuildBytes = 200 << 20
	maxSumsBytes  = 1 << 20
	fetchTimeout  = 15 * time.Minute
	smokeTimeout  = 30 * time.Second
	sumsAsset     = "SHA256SUMS"
	recordFile    = "record.json"
)

// Record is what was fetched for one version.
type Record struct {
	Version    string    `json:"version"`
	Libtorrent string    `json:"libtorrent"`
	Repo       string    `json:"repo"`
	Tag        string    `json:"tag"`
	Asset      string    `json:"asset"`
	SHA256     string    `json:"sha256"`
	FetchedAt  time.Time `json:"fetchedAt"`
}

func toolsDir(dataDir string) string { return filepath.Join(dataDir, "tools", "qbittorrent") }

// Each version has a folder of its own, so going back to one fetched before
// needs no download, and a running build is never written over.
func dirFor(dataDir, version string) string { return filepath.Join(toolsDir(dataDir), version) }

func binaryIn(dir, goos string) string {
	if goos == "windows" {
		return filepath.Join(dir, "qbittorrent-nox.exe")
	}
	return filepath.Join(dir, "qbittorrent-nox")
}

// BinaryPath is where the build of version is put on this machine.
func BinaryPath(dataDir, version string) string {
	return binaryIn(dirFor(dataDir, version), runtime.GOOS)
}

// Installed is the record of version, or nil when it was never fetched or
// its program is gone.
func Installed(dataDir, version string) (*Record, error) {
	dir := dirFor(dataDir, version)
	rec, err := loadRecord(dir)
	if err != nil || rec == nil {
		return rec, err
	}
	if fi, err := os.Stat(binaryIn(dir, runtime.GOOS)); err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	return rec, nil
}

func loadRecord(dir string) (*Record, error) {
	b, err := os.ReadFile(filepath.Join(dir, recordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Install puts the build of v for this machine in place unless it is there
// already, which current reports. progress hears how much of the download has
// arrived of how much, with -1 while the total is unknown.
func Install(ctx context.Context, dataDir string, v Version, progress func(done, total int64)) (rec Record, current bool, err error) {
	in := installer{
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
		lookup: ghrelease.Tagged,
		fetch: func(ctx context.Context, a ghrelease.Asset, dst string) error {
			return download(ctx, a, dst, progress)
		},
		smoke: smokeTest,
	}
	return in.install(ctx, dataDir, v, progress)
}

// installer is Install with GitHub and the run of the program replaceable:
// ghrelease only talks to GitHub's own hosts, so a test cannot stand in for
// them over HTTP.
type installer struct {
	goos, goarch string
	lookup       func(ctx context.Context, repo, tag string) (ghrelease.Release, error)
	fetch        func(ctx context.Context, a ghrelease.Asset, dst string) error
	smoke        func(ctx context.Context, bin string) (string, error)
}

func (in installer) install(ctx context.Context, dataDir string, v Version, progress func(done, total int64)) (Record, bool, error) {
	pin, ok := v.AssetFor(in.goos, in.goarch)
	if !ok {
		return Record{}, false, fmt.Errorf("there is no qBittorrent %s build for %s/%s", v.Version, in.goos, in.goarch)
	}
	dir := dirFor(dataDir, v.Version)
	if rec, err := loadRecord(dir); err == nil && rec != nil && rec.Asset == pin.Name && (pin.SHA256 == "" || rec.SHA256 == pin.SHA256) {
		if fi, err := os.Stat(binaryIn(dir, in.goos)); err == nil && fi.Mode().IsRegular() {
			return *rec, true, nil
		}
	}

	repo := pin.from()
	asset, want, err := in.vouch(ctx, repo, pin)
	if err != nil {
		return Record{}, false, err
	}
	if err := os.MkdirAll(toolsDir(dataDir), 0o755); err != nil {
		return Record{}, false, err
	}
	stage, err := os.MkdirTemp(toolsDir(dataDir), ".stage-")
	if err != nil {
		return Record{}, false, err
	}
	defer os.RemoveAll(stage)

	file := filepath.Join(stage, ".download")
	if err := in.fetch(ctx, asset, file); err != nil {
		return Record{}, false, err
	}
	ok, err = checksum.Verify(file, checksum.Sum{Name: pin.Name, Kind: checksum.SHA256, Hex: want})
	if err != nil {
		return Record{}, false, err
	}
	if !ok {
		return Record{}, false, &checksum.MismatchError{Msg: fmt.Sprintf("%s from %s does not match its SHA-256, so it was thrown away", pin.Name, repo)}
	}
	bin := binaryIn(stage, in.goos)
	if strings.HasSuffix(pin.Name, ".zip") {
		err = unzipInto(file, stage)
		_ = os.Remove(file)
	} else {
		err = os.Rename(file, bin)
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("%s: %w", pin.Name, err)
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return Record{}, false, fmt.Errorf("%s holds no qbittorrent-nox: %w", pin.Name, err)
	}
	got, err := in.smoke(ctx, bin)
	if err != nil {
		return Record{}, false, err
	}
	if got != v.Version {
		return Record{}, false, fmt.Errorf("the build fetched as qBittorrent %s says it is %q", v.Version, got)
	}

	rec := Record{Version: v.Version, Libtorrent: v.Libtorrent, Repo: repo, Tag: pin.Tag, Asset: pin.Name, SHA256: want, FetchedAt: time.Now().UTC()}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Record{}, false, err
	}
	if err := os.WriteFile(filepath.Join(stage, recordFile), append(b, '\n'), 0o644); err != nil {
		return Record{}, false, err
	}
	if err := swapDir(stage, dir); err != nil {
		return Record{}, false, err
	}
	return rec, false, nil
}

// vouch is the asset to fetch and the digest it must have: the one pinned in
// the table, else the one GitHub worked out when it was uploaded, else the
// release's SHA256SUMS. A build nothing vouches for is not fetched.
func (in installer) vouch(ctx context.Context, repo string, pin Asset) (ghrelease.Asset, string, error) {
	if pin.SHA256 != "" {
		return ghrelease.Asset{
			Name: pin.Name,
			URL:  "https://github.com/" + repo + "/releases/download/" + pin.Tag + "/" + pin.Name,
			Size: pin.Size,
		}, pin.SHA256, nil
	}
	rel, err := in.lookup(ctx, repo, pin.Tag)
	if err != nil {
		return ghrelease.Asset{}, "", err
	}
	asset, ok := rel.Find(pin.Name)
	if !ok {
		return ghrelease.Asset{}, "", fmt.Errorf("release %s of %s has no %s", pin.Tag, repo, pin.Name)
	}
	if d := asset.SHA256(); d != "" {
		return asset, d, nil
	}
	sums, ok := rel.Find(sumsAsset)
	if !ok {
		return ghrelease.Asset{}, "", fmt.Errorf("release %s of %s gives no SHA-256 for %s, so it cannot be verified", pin.Tag, repo, pin.Name)
	}
	f, err := os.CreateTemp("", "kl-sums-")
	if err != nil {
		return ghrelease.Asset{}, "", err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	if err := in.fetch(ctx, sums, name); err != nil {
		return ghrelease.Asset{}, "", err
	}
	raw, err := os.ReadFile(name)
	if err != nil || len(raw) > maxSumsBytes {
		return ghrelease.Asset{}, "", fmt.Errorf("the %s of %s cannot be read", sumsAsset, pin.Tag)
	}
	list, err := checksum.ParseHashFile(bytes.NewReader(raw))
	if err != nil {
		return ghrelease.Asset{}, "", err
	}
	for _, s := range list {
		if s.Name == pin.Name && s.Kind == checksum.SHA256 {
			return asset, strings.ToLower(s.Hex), nil
		}
	}
	return ghrelease.Asset{}, "", fmt.Errorf("the %s of %s has no entry for %s", sumsAsset, pin.Tag, pin.Name)
}

func download(ctx context.Context, a ghrelease.Asset, dst string, progress func(done, total int64)) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	total := a.Size
	if total <= 0 {
		total = -1
	}
	_, copyErr := ghrelease.FetchWithin(ctx, a, httpx.ProgressWriter(f, total, progress), maxBuildBytes, fetchTimeout)
	if err := f.Close(); copyErr == nil {
		copyErr = err
	}
	return copyErr
}

// unzipInto writes every file of the archive under dir, keeping its folders,
// and refuses an entry whose path would land outside dir.
func unzipInto(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("the archive entry %q is not a plain file", f.Name)
		}
		rel := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("the archive entry %q points outside its folder", f.Name)
		}
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, dst); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, maxBuildBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxBuildBytes {
		err = fmt.Errorf("the archive entry %q is larger than %d bytes", f.Name, maxBuildBytes)
	}
	return err
}

// swapDir moves the staged folder to final, the old one out of the way first.
// On Windows a folder whose program runs cannot be moved, which fails here
// before anything is lost.
func swapDir(stage, final string) error {
	old := final + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return fmt.Errorf("the qBittorrent in %s could not be moved aside, so nothing was replaced: %w", final, err)
		}
	}
	if err := os.Rename(stage, final); err != nil {
		_ = os.Rename(old, final)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

// smokeTest runs the staged build once, which tells a build for another libc
// or a data folder mounted noexec from one that works.
func smokeTest(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	out, err := nowindow.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("qbittorrent-nox was fetched and verified but does not run on this machine: %v", err)
	}
	v := versionLine(string(out))
	if v == "" {
		return "", fmt.Errorf("qbittorrent-nox printed %q for its version", strings.TrimSpace(string(out)))
	}
	return v, nil
}

// versionLine reads "qBittorrent v4.5.5".
func versionLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "qBittorrent v"); ok {
			return v
		}
	}
	return ""
}
