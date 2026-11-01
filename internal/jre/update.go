package jre

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// AdoptiumAPI is where Latest asks for the newest Temurin 21 JRE.
const AdoptiumAPI = "https://api.adoptium.net"

// Release is a Temurin 21 JRE that Adoptium offers for this platform.
type Release struct {
	// Version reads like 21.0.13+9.
	Version string
	// Name is the archive's file name, URL where it is fetched from.
	Name   string
	URL    string
	SHA256 string
}

// The weekly update keeps a runtime it fetched in nextDir until JD has been
// moved onto it, and the one it replaced in prevDir until JD has started on
// the new one.
const (
	nextDir = "jre-next"
	prevDir = "jre-prev"
)

// stagePrefix differs from Ensure's ".jre-" so that a setup run, which clears
// those, leaves a weekly download under way alone.
const stagePrefix = ".next-jre-"

// platform is Adoptium's os and architecture for this computer, read from the
// pinned archive's name so that both always pick the same build.
func platform() (osName, arch string, err error) {
	want, ok := pinned[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", "", fmt.Errorf("there is no Java runtime for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	// OpenJDK21U-jre_x64_windows_hotspot_21.0.13_9.zip
	parts := strings.Split(want.name, "_")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("cannot tell the platform from %s", want.name)
	}
	return parts[2], parts[1], nil
}

var (
	versionRE = regexp.MustCompile(`^\d+(\.\d+)*(\+\d+)?$`)
	digestRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Latest asks Adoptium's API at api for the newest Temurin 21 JRE for this
// platform.
func Latest(ctx context.Context, api string) (*Release, error) {
	osName, arch, err := platform()
	if err != nil {
		return nil, err
	}
	q := url.Values{"image_type": {"jre"}, "os": {osName}, "architecture": {arch}, "vendor": {"eclipse"}}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(api, "/")+"/v3/assets/latest/21/hotspot?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.New(httpx.Options{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Adoptium answered %s", resp.Status)
	}
	var assets []struct {
		Binary struct {
			ImageType    string `json:"image_type"`
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Package      struct {
				Name     string `json:"name"`
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binary"`
		ReleaseName string `json:"release_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&assets); err != nil {
		return nil, fmt.Errorf("reading Adoptium's answer: %w", err)
	}
	var best *Release
	for _, a := range assets {
		b := a.Binary
		if b.ImageType != "jre" || b.OS != osName || b.Architecture != arch {
			continue
		}
		v := strings.TrimPrefix(a.ReleaseName, "jdk-")
		if !versionRE.MatchString(v) || !strings.HasPrefix(v, "21") {
			continue
		}
		sum := strings.ToLower(b.Package.Checksum)
		if !digestRE.MatchString(sum) || !(strings.HasSuffix(b.Package.Name, ".zip") || strings.HasSuffix(b.Package.Name, ".tar.gz")) {
			continue
		}
		if best == nil || Compare(v, best.Version) > 0 {
			best = &Release{Version: v, Name: b.Package.Name, URL: b.Package.Link, SHA256: sum}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("Adoptium lists no Temurin 21 JRE for %s on %s", osName, arch)
	}
	return best, nil
}

// Compare orders two runtime versions such as 21.0.13+9 and 21.0.4.1+1: by
// each number before the plus, then by the build after it. It returns -1, 0
// or 1.
func Compare(a, b string) int {
	av, ab := splitVersion(a)
	bv, bb := splitVersion(b)
	for i := range max(len(av), len(bv)) {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			return cmp.Compare(x, y)
		}
	}
	return cmp.Compare(ab, bb)
}

func splitVersion(v string) (nums []int, build int) {
	v = strings.TrimPrefix(v, "jdk-")
	v, b, _ := strings.Cut(v, "+")
	// A build can carry a suffix, as in 21.0.13+9-LTS.
	b, _, _ = strings.Cut(b, "-")
	build, _ = strconv.Atoi(b)
	for _, n := range strings.Split(v, ".") {
		x, _ := strconv.Atoi(n)
		nums = append(nums, x)
	}
	return nums, build
}

// Stage fetches rel, checks it against the SHA-256 Adoptium gave for it,
// unpacks it and runs it once, and keeps it beside the runtime in use until
// Promote moves it in.
func Stage(ctx context.Context, dataDir string, rel *Release) error {
	leftovers, _ := filepath.Glob(filepath.Join(dataDir, stagePrefix+"*"))
	for _, l := range leftovers {
		os.RemoveAll(l)
	}
	stage, err := os.MkdirTemp(dataDir, stagePrefix)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// The archive's own name only decides how it is unpacked, so nothing in
	// Adoptium's answer becomes a path.
	file := filepath.Join(stage, "runtime.tar.gz")
	if strings.HasSuffix(rel.Name, ".zip") {
		file = filepath.Join(stage, "runtime.zip")
	}
	if err := download(ctx, rel.URL, file, nil); err != nil {
		return fmt.Errorf("downloading Java %s: %w", rel.Version, err)
	}
	if got, err := sha256File(file); err != nil {
		return err
	} else if got != rel.SHA256 {
		return fmt.Errorf("the download of Java %s has SHA-256 %s instead of %s, so it was thrown away", rel.Version, got, rel.SHA256)
	}
	home := filepath.Join(stage, "jre")
	if err := unpack(file, home); err != nil {
		return fmt.Errorf("unpacking %s: %w", rel.Name, err)
	}
	if err := smokeTest(ctx, filepath.Join(home, "bin", filepath.Base(Java(dataDir)))); err != nil {
		return err
	}
	rec := Record{Vendor: Vendor, Version: rel.Version, Archive: rel.Name, SHA256: rel.SHA256, FetchedAt: time.Now().UTC()}
	if err := writeRecord(home, rec); err != nil {
		return err
	}
	next := filepath.Join(dataDir, nextDir)
	if err := os.RemoveAll(next); err != nil {
		return err
	}
	return os.Rename(home, next)
}

func writeRecord(dir string, rec Record) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, recordFile), append(b, '\n'), 0o644)
}

func loadRecord(dir string) (*Record, error) {
	b, err := os.ReadFile(filepath.Join(dir, recordFile))
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

// Pending is the record of the runtime Stage keeps for the next JD start, or
// nil when there is none. One that is no newer than the runtime in use, which
// a release pinning a newer runtime can bring about, is thrown away.
func Pending(dataDir string) (*Record, error) {
	next := filepath.Join(dataDir, nextDir)
	rec, err := loadRecord(next)
	if err != nil || rec == nil {
		return nil, err
	}
	cur, _ := Load(dataDir)
	_, statErr := os.Stat(filepath.Join(next, "bin", filepath.Base(Java(dataDir))))
	if statErr != nil || Compare(rec.Version, Version) <= 0 || (cur != nil && Compare(rec.Version, cur.Version) <= 0) {
		return nil, os.RemoveAll(next)
	}
	return rec, nil
}

// Switch is a staged runtime moved into place, with the one it replaced kept
// until Commit or Rollback.
type Switch struct {
	dataDir string
	// From is the runtime moved aside, To the one now in place.
	From, To *Record
}

// Promote moves the runtime Stage kept into place, with the old one kept
// aside. It returns nil when nothing waits. JD must not be running, since a
// runtime in use cannot be moved on Windows.
func Promote(dataDir string) (*Switch, error) {
	cur, prev := Dir(dataDir), filepath.Join(dataDir, prevDir)
	// A switch cut short by a crash leaves the old runtime aside: back into
	// place when nothing took its place, and otherwise of no further use.
	if _, err := os.Stat(cur); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(prev); err == nil {
			if err := os.Rename(prev, cur); err != nil {
				return nil, err
			}
		}
	} else if err := os.RemoveAll(prev); err != nil {
		return nil, err
	}
	to, err := Pending(dataDir)
	if err != nil || to == nil {
		return nil, err
	}
	from, _ := Load(dataDir)
	if err := os.Rename(cur, prev); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("moving the old Java runtime aside: %w", err)
	}
	if err := os.Rename(filepath.Join(dataDir, nextDir), cur); err != nil {
		os.Rename(prev, cur)
		return nil, fmt.Errorf("moving Java %s into place: %w", to.Version, err)
	}
	return &Switch{dataDir: dataDir, From: from, To: to}, nil
}

// Commit drops the old runtime once JD has started on the new one.
func (s *Switch) Commit() error {
	return os.RemoveAll(filepath.Join(s.dataDir, prevDir))
}

// Rollback puts the old runtime back and drops the new one. JD must not be
// running.
func (s *Switch) Rollback() error {
	cur, prev := Dir(s.dataDir), filepath.Join(s.dataDir, prevDir)
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("the old Java runtime is gone: %w", err)
	}
	if err := os.RemoveAll(cur); err != nil {
		return err
	}
	return os.Rename(prev, cur)
}
