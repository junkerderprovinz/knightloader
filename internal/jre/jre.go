// Package jre fetches the Java runtime the desktop app runs its private
// JDownloader on: a pinned Eclipse Temurin JRE from Adoptium's releases on
// GitHub, checked against a SHA-256 kept in this file and unpacked into the
// app's data folder. A newer KnightLoader that pins a newer runtime replaces
// the old one on its first start. The container image carries its own JRE and
// never uses this.
package jre

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// Vendor and Version name the runtime, as the settings show it.
const (
	Vendor  = "Eclipse Temurin"
	Version = "21.0.13+9"
)

// ReleaseURL is the release the archives come from, which also links its
// source code.
const ReleaseURL = "https://github.com/adoptium/temurin21-binaries/releases/tag/jdk-21.0.13%2B9"

// downloadBase is where the archives are fetched from; the tests point it at a
// stand-in.
var downloadBase = "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.13%2B9/"

type archive struct {
	name   string
	sha256 string
}

// pinned is each platform's archive with its digest from the release's
// .sha256.txt files. Adoptium builds no JRE for Windows on Arm, where the x64
// one runs under Windows' own emulation.
var pinned = map[string]archive{
	"windows/amd64": {"OpenJDK21U-jre_x64_windows_hotspot_21.0.13_9.zip", "2332c0b4cff6bed722da3567c91d5e47568bcba863fa4945d0a8d6fc76158628"},
	"windows/arm64": {"OpenJDK21U-jre_x64_windows_hotspot_21.0.13_9.zip", "2332c0b4cff6bed722da3567c91d5e47568bcba863fa4945d0a8d6fc76158628"},
	"linux/amd64":   {"OpenJDK21U-jre_x64_linux_hotspot_21.0.13_9.tar.gz", "a535a1df9c6284d9618872984e026ae0352d1f7e9e0605425c0e6114f47351dd"},
	"linux/arm64":   {"OpenJDK21U-jre_aarch64_linux_hotspot_21.0.13_9.tar.gz", "b1734db7ade49c3f6acc6ae06233b8c2dd90e3e2f00f67000ae1c972aaa64d8c"},
	"darwin/amd64":  {"OpenJDK21U-jre_x64_mac_hotspot_21.0.13_9.tar.gz", "c909674b1c10c261b9a3fb7e9c0efebd303e33f88a4fe84be7135b5e2d711f89"},
	"darwin/arm64":  {"OpenJDK21U-jre_aarch64_mac_hotspot_21.0.13_9.tar.gz", "225bab0a09491ef06835488cd942f5d2befa9a0410a2484f0c8bbcf7506ec09a"},
}

// The archives are about 50 MB and unpack to about 130 MB. The caps are
// headroom, and the deadline lets one arrive over a line of a few megabits.
var (
	maxArchiveBytes  int64 = 200 << 20
	maxUnpackedBytes int64 = 600 << 20
)

const fetchTimeout = 45 * time.Minute

const recordFile = "knightloader-runtime.json"

// Record is what was put in place, kept inside the runtime's folder.
type Record struct {
	Vendor    string    `json:"vendor"`
	Version   string    `json:"version"`
	Archive   string    `json:"archive"`
	SHA256    string    `json:"sha256"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Dir is where the runtime lives in the data folder.
func Dir(dataDir string) string { return filepath.Join(dataDir, "jre") }

// Java is the runtime's java under dataDir.
func Java(dataDir string) string {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	return filepath.Join(Dir(dataDir), "bin", name)
}

// Load reads the record of the runtime in place, or returns nil when there is
// none.
func Load(dataDir string) (*Record, error) {
	b, err := os.ReadFile(filepath.Join(Dir(dataDir), recordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// smokeTest runs the unpacked java once before it is put in place; the tests
// replace it, since their archives hold no real runtime.
var smokeTest = func(ctx context.Context, java string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := nowindow.CommandContext(ctx, java, "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the Java runtime was downloaded and verified but does not run on this computer: %v", err)
	}
	if !strings.Contains(string(out), "Temurin") {
		return fmt.Errorf("the Java runtime identifies itself as %q", strings.TrimSpace(string(out)))
	}
	return nil
}

// Ensure returns the java of the pinned runtime, fetching it first when the
// data folder holds no runtime or an older one. progress hears how much of the
// archive has arrived, and of how much; total is -1 while unknown.
func Ensure(ctx context.Context, dataDir string, progress func(done, total int64)) (string, error) {
	want, ok := pinned[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("there is no Java runtime for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if rec, err := Load(dataDir); err == nil && rec != nil && rec.Version == Version && rec.SHA256 == want.sha256 {
		if fi, err := os.Stat(Java(dataDir)); err == nil && fi.Mode().IsRegular() {
			return Java(dataDir), nil
		}
	}
	// A stage an interrupted run left behind is of no use to this one.
	leftovers, _ := filepath.Glob(filepath.Join(dataDir, ".jre-*"))
	for _, l := range leftovers {
		os.RemoveAll(l)
	}

	stage, err := os.MkdirTemp(dataDir, ".jre-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	file := filepath.Join(stage, want.name)
	if err := download(ctx, downloadBase+want.name, file, progress); err != nil {
		return "", fmt.Errorf("downloading the Java runtime: %w", err)
	}
	if got, err := sha256File(file); err != nil {
		return "", err
	} else if got != want.sha256 {
		return "", fmt.Errorf("the Java runtime download has SHA-256 %s instead of %s, so it was thrown away", got, want.sha256)
	}

	home := filepath.Join(stage, "jre")
	if err := unpack(file, home); err != nil {
		return "", fmt.Errorf("unpacking %s: %w", want.name, err)
	}
	staged := filepath.Join(home, "bin", filepath.Base(Java(dataDir)))
	if err := smokeTest(ctx, staged); err != nil {
		return "", err
	}
	rec := Record{Vendor: Vendor, Version: Version, Archive: want.name, SHA256: want.sha256, FetchedAt: time.Now().UTC()}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(home, recordFile), append(b, '\n'), 0o644); err != nil {
		return "", err
	}
	if err := swapIn(home, Dir(dataDir)); err != nil {
		return "", err
	}
	return Java(dataDir), nil
}

func download(ctx context.Context, url, dst string, progress func(done, total int64)) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpx.New(httpx.Options{Timeout: httpx.NoTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > maxArchiveBytes {
		return fmt.Errorf("the archive is larger than the %d byte cap", maxArchiveBytes)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	var w io.Writer = f
	if progress != nil {
		w = &counter{w: f, total: resp.ContentLength, report: progress}
	}
	n, copyErr := io.Copy(w, io.LimitReader(resp.Body, maxArchiveBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if n > maxArchiveBytes {
		return fmt.Errorf("the archive is larger than the %d byte cap", maxArchiveBytes)
	}
	return closeErr
}

type counter struct {
	w      io.Writer
	done   int64
	total  int64
	report func(done, total int64)
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.done += int64(n)
	c.report(c.done, c.total)
	return n, err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// unpack writes the archive into dir without its top folder, and on macOS
// without the bundle around the runtime, so java is at bin/java everywhere.
// No name inside the archive decides where a file lands outside dir.
func unpack(file, dir string) error {
	var written int64
	place := func(name string) (string, bool) {
		parts := strings.Split(path.Clean(name), "/")
		if len(parts) < 2 {
			return "", false
		}
		rel := parts[1:]
		if len(rel) >= 2 && rel[0] == "Contents" && rel[1] == "Home" {
			rel = rel[2:]
		} else if len(rel) > 0 && rel[0] == "Contents" {
			return "", false
		}
		if len(rel) == 0 {
			return "", false
		}
		for _, p := range rel {
			if p == ".." || p == "" {
				return "", false
			}
		}
		return filepath.Join(append([]string{dir}, rel...)...), true
	}
	write := func(dst string, mode fs.FileMode, r io.Reader) error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0o600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(r, maxUnpackedBytes-written+1))
		written += n
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if written > maxUnpackedBytes {
			return fmt.Errorf("the runtime unpacks to more than %d bytes", maxUnpackedBytes)
		}
		return closeErr
	}

	if strings.HasSuffix(file, ".zip") {
		zr, err := zip.OpenReader(file)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			dst, ok := place(f.Name)
			if !ok || !f.Mode().IsRegular() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = write(dst, f.Mode().Perm(), rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		dst, ok := place(h.Name)
		if !ok {
			continue
		}
		switch h.Typeflag {
		case tar.TypeReg:
			if err := write(dst, fs.FileMode(h.Mode).Perm(), tr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// The licence files of most modules link to java.base's. A link
			// that would lead out of the runtime is left out.
			target := filepath.Join(filepath.Dir(dst), filepath.FromSlash(h.Linkname))
			if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(h.Linkname), dst); err != nil {
				return err
			}
		}
	}
}

// swapIn puts the staged runtime at dst. An older runtime there is moved aside
// first, so a failed rename leaves it working.
func swapIn(staged, dst string) error {
	var old string
	if _, err := os.Stat(dst); err == nil {
		var b [4]byte
		rand.Read(b[:])
		old = filepath.Join(filepath.Dir(dst), ".jre-old-"+hex.EncodeToString(b[:]))
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("moving the old Java runtime aside: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		if old != "" {
			os.Rename(old, dst)
		}
		return err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return nil
}
