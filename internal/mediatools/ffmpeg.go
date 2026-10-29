package mediatools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// ffmpegRepo publishes static LGPL builds of FFmpeg master for Windows and
// Linux, with a checksums file in every release. yt-dlp's own fork of it
// carries GPL builds only. Its dated releases are pruned after a couple of
// weeks, so the rolling "latest" release is the only one an installed app can
// count on finding again.
const ffmpegRepo = "BtbN/FFmpeg-Builds"

const ffmpegChecksums = "checksums.sha256"

// The Windows archive is about 180 MB. The cap is headroom, and the deadline
// lets it arrive over a line of a few megabits.
const (
	maxFFmpegArchiveBytes = 600 << 20
	maxFFmpegBinaryBytes  = 400 << 20
	ffmpegFetchTimeout    = 45 * time.Minute
)

// FFmpegRefresh is how old a fetched ffmpeg may get before the daily update
// replaces it. Master is rebuilt every day, and a download of this size every
// day would buy nothing a media download notices.
const FFmpegRefresh = 30 * 24 * time.Hour

// ffmpegAsset is the release asset for this platform, or "" where there is
// none: the repository builds nothing for macOS.
func ffmpegAsset() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "ffmpeg-master-latest-win64-lgpl.zip"
	case "windows/arm64":
		return "ffmpeg-master-latest-winarm64-lgpl.zip"
	case "linux/amd64":
		return "ffmpeg-master-latest-linux64-lgpl.tar.xz"
	case "linux/arm64":
		return "ffmpeg-master-latest-linuxarm64-lgpl.tar.xz"
	}
	return ""
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// FFmpegPath is where a fetched ffmpeg is put, beside the fetched yt-dlp.
func FFmpegPath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), exeName("ffmpeg")) }

// FFprobePath is where the ffprobe fetched with it is put.
func FFprobePath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), exeName("ffprobe")) }

func ffmpegRecordPath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), "ffmpeg.json") }

// FFmpegRecord is what was fetched and when, kept so the daily update knows
// when the copy is due again.
type FFmpegRecord struct {
	Tag       string    `json:"tag"`
	Asset     string    `json:"asset"`
	SHA256    string    `json:"sha256"`
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// LoadFFmpegRecord reads the record, or returns nil when nothing was fetched.
func LoadFFmpegRecord(dataDir string) (*FFmpegRecord, error) {
	b, err := os.ReadFile(ffmpegRecordPath(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec FFmpegRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// FFmpegTools is which ffmpeg and ffprobe run, and where each came from.
type FFmpegTools struct {
	FFmpeg        string
	FFmpegSource  Source
	FFprobe       string
	FFprobeSource Source
}

// ResolveFFmpeg prefers what is on PATH, since an ffmpeg somebody installed is
// their choice and the fetched copy only stands in where there is none. Each
// of the two is resolved on its own: a system with ffmpeg but no ffprobe still
// gets the fetched ffprobe.
func ResolveFFmpeg(dataDir string) FFmpegTools {
	var t FFmpegTools
	t.FFmpeg, t.FFmpegSource = resolveOne("ffmpeg", dataDir, FFmpegPath)
	t.FFprobe, t.FFprobeSource = resolveOne("ffprobe", dataDir, FFprobePath)
	return t
}

func resolveOne(name, dataDir string, fetched func(string) string) (string, Source) {
	if found, err := exec.LookPath(name); err == nil {
		return found, SourcePath
	}
	if dataDir != "" {
		if fi, err := os.Stat(fetched(dataDir)); err == nil && fi.Mode().IsRegular() {
			return fetched(dataDir), SourceManaged
		}
	}
	return name, SourceNone
}

// CanInstallFFmpeg reports why InstallFFmpeg could not put a copy in place
// here, or nil when it could, without asking GitHub.
func CanInstallFFmpeg(dataDir string) error {
	if ffmpegAsset() == "" {
		return fmt.Errorf("%s publishes no ffmpeg for %s/%s", ffmpegRepo, runtime.GOOS, runtime.GOARCH)
	}
	return toolsWritable(dataDir)
}

// InstallFFmpeg fetches the newest LGPL build, verifies the archive against
// the release's checksums, unpacks ffmpeg and ffprobe, runs both, and only
// then puts them where ResolveFFmpeg finds them.
func InstallFFmpeg(ctx context.Context, dataDir string) (FFmpegRecord, error) {
	if err := CanInstallFFmpeg(dataDir); err != nil {
		return FFmpegRecord{}, err
	}
	rel, err := ghrelease.Latest(ctx, ffmpegRepo)
	if err != nil {
		return FFmpegRecord{}, err
	}
	sumsAsset, ok := rel.Find(ffmpegChecksums)
	if !ok {
		return FFmpegRecord{}, fmt.Errorf("release %s has no %s, so nothing downloaded from it could be verified", rel.Tag, ffmpegChecksums)
	}
	raw, err := ghrelease.Bytes(ctx, sumsAsset, maxChecksumBytes)
	if err != nil {
		return FFmpegRecord{}, err
	}
	sums, err := checksum.ParseHashFile(bytes.NewReader(raw))
	if err != nil {
		return FFmpegRecord{}, fmt.Errorf("release %s: %s could not be read: %w", rel.Tag, ffmpegChecksums, err)
	}
	return installFFmpeg(ctx, dataDir, ffmpegAsset(), rel, sums, downloadFFmpeg)
}

func downloadFFmpeg(ctx context.Context, a ghrelease.Asset, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := ghrelease.FetchWithin(ctx, a, f, maxFFmpegArchiveBytes, ffmpegFetchTimeout)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// installFFmpeg is InstallFFmpeg once the release is in hand. The asset name
// is a parameter so a test can drive either archive format on any host.
func installFFmpeg(ctx context.Context, dataDir, name string, rel ghrelease.Release, sums []checksum.Sum, fetch fetcher) (FFmpegRecord, error) {
	var rec FFmpegRecord
	asset, ok := rel.Find(name)
	if !ok {
		return rec, fmt.Errorf("release %s of %s has no %s", rel.Tag, ffmpegRepo, name)
	}
	want, err := sumFor(ffmpegChecksums, sums, name)
	if err != nil {
		return rec, err
	}
	if err := os.MkdirAll(ToolsDir(dataDir), 0o755); err != nil {
		return rec, err
	}
	// The binaries keep their own names while staged, since Windows starts a
	// program by its extension.
	stage, err := os.MkdirTemp(ToolsDir(dataDir), ".ffmpeg-")
	if err != nil {
		return rec, err
	}
	defer os.RemoveAll(stage)

	archive := filepath.Join(stage, name)
	if err := fetch(ctx, asset, archive); err != nil {
		return rec, err
	}
	ok, err = checksum.Verify(archive, want)
	if err != nil {
		return rec, err
	}
	if !ok {
		return rec, fmt.Errorf("%s does not match the SHA-256 in %s, so it was thrown away", name, ffmpegChecksums)
	}

	bins := []string{exeName("ffmpeg"), exeName("ffprobe")}
	if err := unpack(archive, stage, bins); err != nil {
		return rec, fmt.Errorf("%s: %w", name, err)
	}
	var version string
	for _, b := range bins {
		staged := filepath.Join(stage, b)
		if err := os.Chmod(staged, 0o755); err != nil {
			return rec, err
		}
		v, err := ffmpegSmokeTest(ctx, staged, strings.TrimSuffix(b, ".exe"))
		if err != nil {
			return rec, err
		}
		if version == "" {
			version = v
		}
	}
	if err := swapIn(filepath.Join(stage, bins[0]), FFmpegPath(dataDir)); err != nil {
		return rec, err
	}
	if err := swapIn(filepath.Join(stage, bins[1]), FFprobePath(dataDir)); err != nil {
		return rec, err
	}

	rec = FFmpegRecord{Tag: rel.Tag, Asset: name, SHA256: want.Hex, Version: version, FetchedAt: time.Now().UTC()}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return rec, err
	}
	if err := os.WriteFile(ffmpegRecordPath(dataDir), append(b, '\n'), 0o644); err != nil {
		return rec, fmt.Errorf("ffmpeg is in place but its record could not be written: %w", err)
	}
	return rec, nil
}

// unpack writes the archive's bin/<name> files into dir under their bare
// names and nothing else, so no path inside the archive decides where a file
// lands.
func unpack(archive, dir string, names []string) error {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	found := map[string]bool{}
	take := func(entry string, r io.Reader) error {
		base := path.Base(entry)
		if !want[base] || path.Base(path.Dir(entry)) != "bin" || found[base] {
			return nil
		}
		found[base] = true
		return writeBounded(filepath.Join(dir, base), r)
	}

	var err error
	switch {
	case strings.HasSuffix(archive, ".zip"):
		err = unpackZip(archive, take)
	case strings.HasSuffix(archive, ".tar.xz"):
		err = unpackTarXz(archive, take)
	default:
		err = errors.New("not an archive format this can read")
	}
	if err != nil {
		return err
	}
	for _, n := range names {
		if !found[n] {
			return fmt.Errorf("the archive holds no bin/%s", n)
		}
	}
	return nil
}

func unpackZip(archive string, take func(string, io.Reader) error) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = take(f.Name, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unpackTarXz(archive string, take func(string, io.Reader) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	xr, err := xz.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if err := take(h.Name, tr); err != nil {
			return err
		}
	}
}

func writeBounded(dst string, r io.Reader) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(r, maxFFmpegBinaryBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if n > maxFFmpegBinaryBytes {
		return fmt.Errorf("%s is larger than the %d byte cap", filepath.Base(dst), maxFFmpegBinaryBytes)
	}
	return closeErr
}

// ffmpegSmokeTest runs one staged binary with -version (one dash; two make
// ffmpeg print its usage and exit 1) and requires it to name itself.
func ffmpegSmokeTest(ctx context.Context, bin, name string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	out, err := nowindow.CommandContext(runCtx, bin, "-version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s downloaded and verified but does not run on this machine: %v", name, err)
	}
	line := firstLine(string(out))
	if !strings.HasPrefix(line, name+" version ") {
		return "", fmt.Errorf("%s identifies itself as %q", name, line)
	}
	return ffmpegVersion(line), nil
}
