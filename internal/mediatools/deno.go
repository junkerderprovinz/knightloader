package mediatools

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

const denoRepo = "denoland/deno"

// A release zip is about 45 MB.
const (
	maxDenoArchiveBytes = 200 << 20
	denoFetchTimeout    = 15 * time.Minute
)

func denoAsset() string { return denoAssetFor(runtime.GOOS, runtime.GOARCH) }

// denoAssetFor names the release zip for one platform, or "" where Deno
// builds none. The Linux builds are linked against glibc.
func denoAssetFor(goos, goarch string) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	target := map[string]string{"windows": "pc-windows-msvc", "linux": "unknown-linux-gnu", "darwin": "apple-darwin"}[goos]
	if arch == "" || target == "" {
		return ""
	}
	return "deno-" + arch + "-" + target + ".zip"
}

// DenoPath is where a fetched Deno is put, beside the fetched yt-dlp.
func DenoPath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), exeName("deno")) }

func denoRecordPath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), "deno.json") }

// DenoRecord is what was fetched and when.
type DenoRecord struct {
	Tag       string    `json:"tag"`
	Asset     string    `json:"asset"`
	SHA256    string    `json:"sha256"`
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// LoadDenoRecord reads the record, or returns nil when nothing was fetched.
func LoadDenoRecord(dataDir string) (*DenoRecord, error) {
	return readRecord[DenoRecord](denoRecordPath(dataDir))
}

// ResolveDeno answers which Deno yt-dlp is handed, in yt-dlp's own order: the
// fetched copy, which the daily update keeps current, then KL_DENO, then PATH.
// With none of the three it returns the bare name and SourceNone.
func ResolveDeno(dataDir string) (string, Source) {
	if dataDir != "" {
		if fi, err := os.Stat(DenoPath(dataDir)); err == nil && fi.Mode().IsRegular() {
			return DenoPath(dataDir), SourceManaged
		}
	}
	if env := strings.TrimSpace(os.Getenv("KL_DENO")); env != "" {
		return env, SourceEnv
	}
	if found, err := exec.LookPath("deno"); err == nil {
		return found, SourcePath
	}
	return "deno", SourceNone
}

// DenoForYtdlp is the Deno yt-dlp is told about with --js-runtimes, or ""
// where there is none and yt-dlp is left to its own search.
func DenoForYtdlp(dataDir string) string {
	path, source := ResolveDeno(dataDir)
	if source == SourceNone {
		return ""
	}
	return path
}

// CanInstallDeno reports why InstallDeno could not put a copy in place here,
// or nil when it could, without asking GitHub.
func CanInstallDeno(dataDir string) error {
	if denoAsset() == "" {
		return fmt.Errorf("%s publishes no build for %s/%s", denoRepo, runtime.GOOS, runtime.GOARCH)
	}
	return toolsWritable(dataDir)
}

// UpdateDeno asks for the newest Deno release and, unless the fetched copy is
// that release already, verifies the zip against the checksum published beside
// it, runs the program once, and only then puts it where ResolveDeno finds it.
// current reports that nothing needed fetching. progress, when not nil, hears
// how much of the zip has arrived of how much; total is -1 while unknown.
func UpdateDeno(ctx context.Context, dataDir string, progress func(done, total int64)) (rec DenoRecord, current bool, err error) {
	if err := CanInstallDeno(dataDir); err != nil {
		return rec, false, err
	}
	name := denoAsset()
	rel, err := ghrelease.Latest(ctx, denoRepo)
	if err != nil {
		return rec, false, err
	}
	sumsAsset, ok := rel.Find(name + ".sha256sum")
	if !ok {
		return rec, false, fmt.Errorf("release %s has no %s.sha256sum, so nothing downloaded from it could be verified", rel.Tag, name)
	}
	raw, err := ghrelease.Bytes(ctx, sumsAsset, maxChecksumBytes)
	if err != nil {
		return rec, false, err
	}
	sum, err := denoSum(raw, name)
	if err != nil {
		return rec, false, fmt.Errorf("release %s: %w", rel.Tag, err)
	}
	download := func(ctx context.Context, a ghrelease.Asset, dst string) error {
		return downloadAsset(ctx, a, dst, maxDenoArchiveBytes, denoFetchTimeout, progress)
	}
	return updateDeno(ctx, dataDir, name, rel, sum, download)
}

// updateDeno is UpdateDeno once the release and its digest are in hand.
func updateDeno(ctx context.Context, dataDir, name string, rel ghrelease.Release, want checksum.Sum, fetch fetcher) (DenoRecord, bool, error) {
	if rec, err := LoadDenoRecord(dataDir); err == nil && rec != nil && rec.Tag == rel.Tag && isFile(DenoPath(dataDir)) {
		return *rec, true, nil
	}
	rec, err := installDeno(ctx, dataDir, name, rel, want, fetch)
	return rec, false, err
}

// denoSum reads the digest Deno publishes for one zip. The Linux and macOS
// builds publish sha256sum's line, the Windows builds the list PowerShell's
// Get-FileHash prints, with the path on the build machine.
func denoSum(raw []byte, name string) (checksum.Sum, error) {
	file := name + ".sha256sum"
	if sums, err := checksum.ParseHashFile(bytes.NewReader(raw)); err == nil && len(sums) > 0 {
		return sumFor(file, sums, name)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	digest := strings.ToLower(fields["Hash"])
	switch {
	case !strings.EqualFold(fields["Algorithm"], "SHA256"):
		return checksum.Sum{}, fmt.Errorf("%s names no SHA-256 digest", file)
	case path.Base(strings.ReplaceAll(fields["Path"], `\`, "/")) != name:
		return checksum.Sum{}, fmt.Errorf("%s has no entry for %s, so this download cannot be verified", file, name)
	}
	if b, err := hex.DecodeString(digest); err != nil || len(b) != 32 {
		return checksum.Sum{}, fmt.Errorf("%s holds no readable SHA-256 digest", file)
	}
	return checksum.Sum{Name: name, Kind: checksum.SHA256, Hex: digest}, nil
}

// installDeno downloads, checks and puts in place the release named.
func installDeno(ctx context.Context, dataDir, name string, rel ghrelease.Release, want checksum.Sum, fetch fetcher) (DenoRecord, error) {
	var rec DenoRecord
	asset, ok := rel.Find(name)
	if !ok {
		return rec, fmt.Errorf("release %s of %s has no %s", rel.Tag, denoRepo, name)
	}
	if err := os.MkdirAll(ToolsDir(dataDir), 0o755); err != nil {
		return rec, err
	}
	stage, err := os.MkdirTemp(ToolsDir(dataDir), ".deno-")
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
		return rec, &checksum.MismatchError{Msg: fmt.Sprintf("%s does not match the SHA-256 in %s.sha256sum, so it was thrown away", name, name)}
	}

	bin := exeName("deno")
	if err := unpack(archive, stage, []string{bin}, "."); err != nil {
		return rec, fmt.Errorf("%s: %w", name, err)
	}
	staged := filepath.Join(stage, bin)
	if err := os.Chmod(staged, 0o755); err != nil {
		return rec, err
	}
	version, err := denoSmokeTest(ctx, staged, rel.Tag)
	if err != nil {
		return rec, err
	}
	if err := swapIn(staged, DenoPath(dataDir)); err != nil {
		return rec, err
	}

	rec = DenoRecord{Tag: rel.Tag, Asset: name, SHA256: want.Hex, Version: version, FetchedAt: time.Now().UTC()}
	if err := writeRecord(denoRecordPath(dataDir), rec); err != nil {
		return rec, fmt.Errorf("deno is in place but its record could not be written: %w", err)
	}
	return rec, nil
}

// denoSmokeTest runs the staged Deno and requires it to name the release it
// came from.
func denoSmokeTest(ctx context.Context, bin, tag string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	out, err := nowindow.CommandContext(runCtx, bin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("deno downloaded and verified but does not run on this machine: %v", err)
	}
	line := firstLine(string(out))
	got := denoVersion(line)
	if !strings.HasPrefix(line, "deno ") || got != strings.TrimPrefix(tag, "v") {
		return "", fmt.Errorf("deno identifies itself as %q, and the release it came from is %q", line, tag)
	}
	return got, nil
}

// denoVersion pulls the version out of "deno 2.9.8 (stable, release,
// x86_64-pc-windows-msvc)", keeping a line of another shape whole.
func denoVersion(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 && fields[0] == "deno" {
		return fields[1]
	}
	return strings.TrimSpace(line)
}

// jsRuntimesTimeout bounds the one yt-dlp -v that JSRuntimes runs, which loads
// every extractor and, as a one-file build, unpacks itself first.
const jsRuntimesTimeout = 30 * time.Second

// JSRuntimes asks yt-dlp which JavaScript runtimes it would use with deno, read
// from the -v header it prints before refusing to run without a URL. A Deno it
// cannot use, such as one too old, reads "none" there too. ok is false when
// yt-dlp is too old to print the line.
func JSRuntimes(ctx context.Context, ytdlp, deno string) (runtimes string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, jsRuntimesTimeout)
	defer cancel()
	args := []string{"-v"}
	if deno != "" {
		args = append(args, "--js-runtimes", "deno:"+deno)
	}
	cmd := nowindow.CommandContext(ctx, ytdlp, args...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	// The exit status is 2 for the missing URL and says nothing here.
	out, _ := cmd.CombinedOutput()
	return jsRuntimesLine(string(out))
}

func jsRuntimesLine(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "[debug] JS runtimes:"); found {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}
