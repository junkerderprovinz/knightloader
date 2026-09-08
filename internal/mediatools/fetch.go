package mediatools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
)

// ytdlpRepo is yt-dlp's own repository. Unauthenticated, and deliberately no
// token anywhere in this package: GitHub allows 60 unauthenticated requests an
// hour per address, which is generous for a button somebody presses, while a
// stored credential would turn a version check into a secret this project has
// to protect, rotate and keep out of the diagnostics bundle.
const ytdlpRepo = "yt-dlp/yt-dlp"

// checksumAsset is the file yt-dlp publishes alongside every release binary. It
// is plain sha256sum output, which internal/checksum already parses (the
// "<hex>  <name>" format, leading-asterisk and backslash quirks included), so
// nothing about the format is reimplemented here.
const checksumAsset = "SHA2-256SUMS"

// maxBinaryBytes caps a downloaded yt-dlp. The PyInstaller builds are around 30
// MB; this is headroom, not a fit, and exists so a hostile or broken host
// cannot fill the data volume.
const maxBinaryBytes = 120 << 20

// maxChecksumBytes caps the sums file. It is a few dozen short lines.
const maxChecksumBytes = 1 << 20

// smokeTimeout bounds the one run of the freshly staged binary. Generous
// compared with probeTimeout on purpose: a PyInstaller bundle unpacks itself
// into a temp directory the first time it starts, and on a slow disk that is
// genuinely several seconds before it prints anything.
const smokeTimeout = 30 * time.Second

// Latest is the answer to "what does GitHub say the newest release is", with
// the comparison against what is installed already made.
type Latest struct {
	Checked bool   `json:"checked"`
	Tag     string `json:"tag,omitempty"`
	URL     string `json:"url,omitempty"`
	// Compare is one of the four CompareX constants. Never a bool: see
	// CompareVersions for why "cannot be ordered" has to be sayable.
	Compare string `json:"compare"`
	// Detail carries GitHub's own refusal verbatim when Checked is false - the
	// rate-limit sentence, the 404 - instead of collapsing every failure into
	// "could not check" the way internal/update does. "Wait an hour" and "that
	// repository moved" are different problems with different answers.
	Detail string `json:"detail,omitempty"`
}

// CheckLatest asks GitHub for yt-dlp's newest release and orders it against
// `installed` (what the running yt-dlp printed, which may be empty when there
// is no yt-dlp at all).
//
// This is the ONLY function in this package that touches the network on its
// own, and nothing calls it except an explicit press of the button or the
// opt-in "ask when this page opens" switch. Probing versions and reading the
// record never leave the box.
func CheckLatest(ctx context.Context, installed string) Latest {
	rel, err := ghrelease.Latest(ctx, ytdlpRepo)
	if err != nil {
		return Latest{Compare: CompareUnknown, Detail: err.Error()}
	}
	out := Latest{Checked: true, Tag: rel.Tag, URL: rel.HTMLURL, Compare: CompareUnknown}
	if installed != "" {
		out.Compare = CompareVersions(rel.Tag, installed)
	}
	return out
}

// Install fetches the newest yt-dlp release, verifies it, proves it runs on
// this machine, and only then puts it where ResolveYtdlp will find it.
//
// THE ORDER IS THE ENTIRE SAFETY STORY, so it is written out here as well as
// being visible below:
//
//  1. ask GitHub for the release;
//  2. refuse outright if the release has no SHA2-256SUMS - never fall back to
//     an unverified download, for exactly the reason internal/update gives for
//     its own checksums.txt: a release missing its digests is a broken or
//     tampered release, not an old one this should be lenient about;
//  3. for each candidate asset for this platform, best first: download it to a
//     ".part" file under a size cap and check the byte count against what the
//     release says;
//  4. verify its SHA-256 against the entry for that exact asset name, requiring
//     a 256-bit digest;
//  5. make it executable;
//  6. RUN IT, and require exit 0 and the release's own tag on stdout. This is
//     the genuinely new step and the one that cannot be skipped: the glibc
//     build refuses to start on the musl container with an error that reads
//     like a missing file, and a /data mounted noexec accepts the chmod and
//     then refuses the exec. Neither is detectable any other way;
//  7. only now swap it into place, rename-aside first so a Windows lock cannot
//     leave nothing installed at all;
//  8. write the record.
//
// Until step 7 the copy that was already working keeps working, and the
// system's own yt-dlp is never touched at any point.
func Install(ctx context.Context, dataDir string) (ManagedRecord, error) {
	var rec ManagedRecord
	if dataDir == "" {
		return rec, errors.New("no data directory, so there is nowhere to put a fetched copy")
	}
	names := candidates()
	if len(names) == 0 {
		return rec, fmt.Errorf("yt-dlp publishes no build for %s/%s, so there is nothing to fetch; install yt-dlp yourself and point KL_YTDLP at it", runtime.GOOS, runtime.GOARCH)
	}

	rel, err := ghrelease.Latest(ctx, ytdlpRepo)
	if err != nil {
		return rec, err
	}
	sumsAsset, ok := rel.Find(checksumAsset)
	if !ok {
		return rec, fmt.Errorf("release %s has no %s file, so nothing downloaded from it could be verified; refusing to install it", rel.Tag, checksumAsset)
	}
	sumsRaw, err := ghrelease.Bytes(ctx, sumsAsset, maxChecksumBytes)
	if err != nil {
		return rec, err
	}
	sums, err := checksum.ParseHashFile(bytes.NewReader(sumsRaw))
	if err != nil {
		return rec, fmt.Errorf("release %s: %s could not be read: %w", rel.Tag, checksumAsset, err)
	}
	return install(ctx, dataDir, names, rel, sums, downloadAsset)
}

// fetcher writes one release asset to a path on disk. It is a parameter of
// install rather than a direct call so that the sequence that MATTERS - verify,
// make executable, run, and only then replace - can be exercised without a
// network at all. There is no way to point the live path at a test server:
// internal/ghrelease pins every request to GitHub's own hosts, which is the
// property that makes running the downloaded file safe in the first place, and
// weakening it to make a test easier would be trading the guarantee for the
// test of the guarantee.
type fetcher func(ctx context.Context, a ghrelease.Asset, dst string) error

func downloadAsset(ctx context.Context, a ghrelease.Asset, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := ghrelease.Fetch(ctx, a, f, maxBinaryBytes)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// install is everything after the release metadata is in hand: the candidate
// walk, the verification, the smoke test and the swap. names is passed in
// rather than read from candidates() so a test can drive the two-candidate
// fallback on any host - that fallback is real on linux/amd64, which is the
// deployment this feature exists for, and unreachable on the machine the tests
// usually run on.
func install(ctx context.Context, dataDir string, names []string, rel ghrelease.Release, sums []checksum.Sum, fetch fetcher) (ManagedRecord, error) {
	var rec ManagedRecord
	if err := os.MkdirAll(ToolsDir(dataDir), 0o755); err != nil {
		return rec, err
	}
	staged := BinaryPath(dataDir) + ".part"
	// Whatever happens below, no half-written file is left for the next run to
	// find. After a successful rename there is nothing left to remove and the
	// error is uninteresting.
	defer func() { _ = os.Remove(staged) }()

	// Every candidate's own reason for failing is kept, because "yt-dlp_linux
	// did not start (exec format error), yt-dlp needs python3 and there is
	// none" is a diagnosis and "install failed" is not.
	var tried []string
	for _, name := range names {
		asset, ok := rel.Find(name)
		if !ok {
			tried = append(tried, fmt.Sprintf("%s: release %s does not contain it", name, rel.Tag))
			continue
		}
		version, err := stage(ctx, asset, sums, staged, rel.Tag, fetch)
		if err != nil {
			tried = append(tried, fmt.Sprintf("%s: %v", name, err))
			_ = os.Remove(staged)
			continue
		}
		if err := swapIn(staged, BinaryPath(dataDir)); err != nil {
			return rec, err
		}
		rec = ManagedRecord{
			Tag:       rel.Tag,
			Asset:     name,
			SHA256:    digestFor(sums, name),
			Version:   version,
			FetchedAt: time.Now().UTC(),
		}
		if err := saveRecord(dataDir, rec); err != nil {
			return rec, fmt.Errorf("%s is in place but its record could not be written: %w", BinaryPath(dataDir), err)
		}
		return rec, nil
	}

	return rec, fmt.Errorf("nothing was replaced; the copy you had is still running. Tried: %s", strings.Join(tried, "; "))
}

// stage downloads one candidate, verifies it, and runs it once. It returns the
// version the staged file actually printed.
func stage(ctx context.Context, asset ghrelease.Asset, sums []checksum.Sum, staged, tag string, fetch fetcher) (string, error) {
	if err := fetch(ctx, asset, staged); err != nil {
		return "", err
	}

	want, err := sumFor(sums, asset.Name)
	if err != nil {
		return "", err
	}
	ok, err := checksum.Verify(staged, want)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("the download does not match the SHA-256 %s publishes for it, so it was thrown away", checksumAsset)
	}

	// Before the smoke test, not after: on Unix the file cannot be started at
	// all without it, and the whole point of the next step is to find out
	// whether it starts.
	if err := os.Chmod(staged, 0o755); err != nil {
		return "", err
	}
	return smokeTest(ctx, staged, tag)
}

// sumFor finds the digest for one asset name and insists it is a SHA-256.
// yt-dlp publishes only sha256 in this file; a shorter digest here would mean
// the file was hand-edited or produced by something else, which is not the
// format this trusts.
func sumFor(sums []checksum.Sum, name string) (checksum.Sum, error) {
	for _, s := range sums {
		if s.Name == name {
			if s.Kind != checksum.SHA256 {
				return checksum.Sum{}, fmt.Errorf("%s lists %s as %s rather than sha256", checksumAsset, name, s.Kind)
			}
			return s, nil
		}
	}
	return checksum.Sum{}, fmt.Errorf("%s has no entry for %s, so this download cannot be verified", checksumAsset, name)
}

func digestFor(sums []checksum.Sum, name string) string {
	if s, err := sumFor(sums, name); err == nil {
		return s.Hex
	}
	return ""
}

// smokeTest runs the staged file and requires it to identify itself as the
// release it came from.
//
// A CHECKSUM PROVES THE BYTES, NOT THAT THEY RUN HERE. That is the whole
// argument for this step. The two ways a perfectly intact yt-dlp still cannot
// be started on the machine that just downloaded it are both invisible until
// something tries:
//
//   - musl (the Alpine container): the glibc PyInstaller build fails to exec
//     with ENOENT, reported as "no such file or directory" for a file that is
//     right there;
//   - a /data bind mount with noexec (routine on Unraid): chmod succeeds and
//     the exec is refused with EACCES.
//
// The version comparison on top of exit 0 is what stops a mirror or a
// mislabelled asset from installing a different program that also happens to
// answer --version.
func smokeTest(ctx context.Context, staged, tag string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	out, err := exec.CommandContext(runCtx, staged, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("it downloaded and verified but does not run on this machine: %v%s", err, startAdvice(err))
	}
	got := firstLine(string(out))
	if got == "" {
		return "", errors.New("it ran but printed no version, so there is no way to tell what it is")
	}
	if strings.TrimPrefix(got, "v") != strings.TrimPrefix(tag, "v") {
		return "", fmt.Errorf("it identifies itself as %q, and the release it came from is %q", got, tag)
	}
	return got, nil
}

// startAdvice turns the two exec failures that will actually happen into a
// sentence naming what to do about them. Without it, "permission denied" and
// "no such file or directory" are both errors nobody can place: the first
// points at a file that is there and readable, the second at one that is there
// and executable.
func startAdvice(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return ". The data directory is very likely mounted so that programs inside it cannot be started (noexec). Either mount it without that restriction, or leave this alone and keep using the system's own yt-dlp"
	case errors.Is(err, fs.ErrNotExist):
		return ". The file is there, so this is the system refusing to start it rather than a missing path - which is what a build for a different C library looks like (a glibc build on a musl system such as Alpine). There is nothing to fix here by hand; the next candidate is tried automatically"
	}
	return ""
}

// swapIn puts the staged file where the resolver looks, moving the previous one
// aside first.
//
// The rename-aside is internal/update's own pattern (Apply) and it is here for
// the same Windows reason: an open file cannot be replaced by a rename, and
// while yt-dlp is spawned per download rather than held open, a swap attempted
// during one would otherwise fail with "Access is denied" and leave the
// download half-served. Renaming the old one out of the way and moving the new
// one in is permitted where an overwrite is not, and a ".old" that cannot be
// deleted yet is harmless - the next install replaces it.
func swapIn(staged, final string) error {
	old := final + ".old"
	_ = os.RemoveAll(old)

	if _, err := os.Lstat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return fmt.Errorf("the previous copy at %s could not be moved aside, so nothing was replaced: %w", final, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.Rename(staged, final); err != nil {
		// Put the old one back rather than leaving nothing installed at all.
		_ = os.Rename(old, final)
		return fmt.Errorf("the new copy could not be moved into place, and the previous one was put back: %w", err)
	}
	_ = os.RemoveAll(old)
	return nil
}
