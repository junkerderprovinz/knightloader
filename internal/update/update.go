// Package update checks whether a newer KnightLoader release exists,
// surfaced on both deployments' General tab now (jdp, 2026-08-24: a
// container user hit the card's old desktop-only gate and asked where the
// toggle had gone - checking GitHub and saying so is exactly as harmless
// for a container as for desktop). What differs by deployment is never
// whether the check runs, only what happens once "update available" is
// true: desktop can fetch, verify and install the new release itself when
// the user asks it to (Download/Apply/Relaunch, further down), while a
// container - which cannot replace itself from the inside - is instead
// pointed at the release page and told to update the way it was deployed
// (docker pull, Unraid Community Applications, ...); see
// routes_features.go's updaterReason for the fuller version of that split.
//
// Check itself only checks and reports, and that half is identical on both
// deployments. Download/Apply/Relaunch, further down, are desktop-only
// (App.RequestUpdateInstall is nil on the container build - a container has
// no running binary of its own to swap) and are never triggered
// automatically: updaterReason is explicit that installing a fetched
// release "is still a manual step there, same as any other desktop app
// before it grows a silent auto-apply" - a background auto-updater that
// replaces its own binary unattended is a real attack surface regardless of
// how well it verifies what it downloads, and that line is deliberately not
// crossed here. What does run, once the user asks: fetch the matching
// platform zip, verify its SHA-256 against a checksums.txt published in the
// same release (see INTEGRITY, below), atomically swap it into place, and
// relaunch. Full code-signature verification is the one piece of that chain
// still not attempted - see INTEGRITY for exactly what is, and is not,
// covered.
package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
)

// releasesAPI is GitHub's own "latest release" endpoint - not the releases
// list, which would need pagination and a definition of "latest" this
// package would then have to reinvent. Unauthenticated: fine for a public
// repo, and returns 404 for a private one (github.com/junkerderprovinz/
// knightloader is private until the v1 release - see the project's own
// vault note) rather than anything Check needs to treat specially, since a
// 404 already reads as "not available" through the same status check every
// other non-200 response gets.
const releasesAPI = "https://api.github.com/repos/junkerderprovinz/knightloader/releases/latest"

// Info is what the settings page shows. Checked is false whenever Check
// could not complete (network error, private repo, rate limit) - distinct
// from Available=false, which means the check succeeded and the answer was
// "no, you already have the latest".
type Info struct {
	Checked   bool   `json:"checked"`
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
}

// ghAsset is one file attached to a GitHub release - desktop.yml's "attach"
// job uploads exactly one zip per platform, named
// "knightloader-{tag}-{slug}.zip" (assetName below builds the same string).
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

// fetchLatestRelease is Check's and Download's shared GitHub call - one
// request, one decode, both callers read the same response shape (Check
// only needs TagName/HTMLURL; Download also needs Assets).
func fetchLatestRelease(ctx context.Context) (ghRelease, error) {
	var rel ghRelease
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 404 (private repo, no releases yet), rate-limited, or any other
		// non-200 - all read the same way to a caller: could not check.
		return rel, fmt.Errorf("github: unexpected status %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return rel, err
	}
	return rel, nil
}

// Check compares `current` (buildinfo.Version) against the latest GitHub
// release tag. "dev" - every untagged local/main build - can never be
// meaningfully compared to a released version, so it is reported as
// checked-but-not-available rather than a version parse this package would
// have to guess at.
func Check(ctx context.Context, current string) Info {
	info := Info{Current: current}
	if current == "" || current == "dev" {
		info.Checked = true
		return info
	}

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return info
	}

	info.Checked = true
	latest := strings.TrimPrefix(rel.TagName, "v")
	currentTrimmed := strings.TrimPrefix(current, "v")
	newer, ok := isNewer(latest, currentTrimmed)
	if !ok {
		// A tag that does not parse as X.Y.Z is not something this package
		// can order against the running version - reported as checked, not
		// available, rather than guessing.
		return info
	}
	info.Available = newer
	if newer {
		info.Latest = rel.TagName
		info.URL = rel.HTMLURL
	}
	return info
}

// isNewer reports whether `latest` sorts after `current` under plain X.Y.Z
// comparison - the project's own versioning convention (patch/minor/major,
// no pre-release suffixes to weigh) has no use for a general semver library
// here. ok is false when either string does not parse as three numeric
// parts.
func isNewer(latest, current string) (newer bool, ok bool) {
	lp, lok := parts(latest)
	cp, cok := parts(current)
	if !lok || !cok {
		return false, false
	}
	for i := 0; i < 3; i++ {
		if lp[i] != cp[i] {
			return lp[i] > cp[i], true
		}
	}
	return false, true
}

func parts(v string) ([3]int, bool) {
	var out [3]int
	seg := strings.SplitN(v, ".", 3)
	if len(seg) != 3 {
		return out, false
	}
	for i, s := range seg {
		n, err := strconv.Atoi(s)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Install: download, verify and apply a newer release, then relaunch.
//
// Desktop only - a container cannot replace itself from the inside (see this
// package's own doc comment). App.RequestUpdateInstall is nil on the
// container build, and the route above it refuses before any of this runs.
//
// Everything below is deployment-agnostic and independently testable
// (update_install_test.go exercises asset selection and the atomic swap
// against real temp files/dirs); the one thing this package cannot do
// itself is decide HOW to relaunch and exit the OLD process afterward -
// that stays a callback on app.App, wired only by desktop/main.go, the same
// "App owns no process lifecycle of its own" split RequestExit already
// established.
//
// INTEGRITY. downloadAsset pins the download to GitHub's own asset hosts
// rather than following browser_download_url blindly, and confirms the
// downloaded size matches the asset metadata GitHub itself reported (bytes
// came from this repo's own release, since only this repo's own Actions
// runner can attach an asset to its releases in the first place) - that much
// has been true since the very first cut of this package.
//
// Beyond it, desktop.yml's "attach" job now also generates a checksums.txt
// (sha256sum of every platform zip, produced in the same job run that zips
// them - see that workflow's own "Checksums" step) and publishes it as a
// release asset next to the bundles. Download fetches it alongside the
// platform zip and verifyChecksum checks the downloaded file's own SHA-256
// against the matching line in it before Download ever returns a path for
// Apply to unpack. That closes the gap this comment used to name here: a
// download can no longer be truncated, corrupted, or tampered with in
// transit or in a compromised CDN cache without Download refusing to hand it
// to Apply.
//
// It is deliberately still not code-signature verification. A published
// sha256 proves the bytes match what the release pipeline itself produced;
// it says nothing about whether that pipeline was trustworthy in the first
// place, since both the zip and its checksums.txt are generated and
// published by the exact same Actions job - a compromise of that job could
// forge a matching pair as easily as a real release. Only a signature tied
// to an identity outside the build pipeline (a hardware key, a separate
// signing service) could close that remaining gap, and that stays the
// honest gap this package's doc comment names, not attempted today.
//
// A release whose latest tag has a platform zip but no checksums.txt asset
// is treated as a hard failure by Download, not a warn-and-proceed
// fallback for "older releases predate this feature" - deliberately, not by
// oversight. Download only ever looks at GitHub's "latest" release, and
// every release cut from this point forward publishes checksums.txt in the
// same workflow revision, at the same tag, in the same job run, that
// publishes the platform zips themselves - so "latest has a zip but no
// checksums.txt" cannot legitimately happen once this change has shipped;
// it can only mean the attach job's Checksums step failed or the asset was
// removed after publishing, i.e. exactly the kind of broken/incomplete
// release this package already refuses to install from (see the identical
// treatment of a missing platform-zip asset in Download below). Silently
// falling back to unverified in that case would quietly defeat the point of
// adding verification at all.
// ---------------------------------------------------------------------------

// allowedAssetHosts are the only hosts downloadAsset will fetch from,
// regardless of what browser_download_url says - GitHub serves release
// assets from its own CDN host(s), never from an arbitrary redirect target,
// so pinning here is a real (if partial) integrity boundary rather than
// theatre.
var allowedAssetHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// platformSlug matches desktop.yml's own matrix.slug exactly - the three
// values that job's "attach" step ever produces a zip for. An unsupported
// GOOS/GOARCH (this package built for anything desktop.yml does not) is
// reported rather than guessed at.
func platformSlug() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "windows-amd64", nil
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "linux-amd64", nil
		}
	case "darwin":
		// desktop.yml builds one universal (amd64+arm64) bundle for macOS,
		// not one zip per arch - so both arches this package might run on
		// map to the same slug.
		return "macos-universal", nil
	}
	return "", fmt.Errorf("update: no published desktop build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// assetName is exactly the string desktop.yml's "Package" step builds:
// `knightloader-${GITHUB_REF_NAME}-${slug}.zip`.
func assetName(tag, slug string) string {
	return fmt.Sprintf("knightloader-%s-%s.zip", tag, slug)
}

// executableName is the file wails.json's outputfilename produces per
// platform - a bare binary on Linux, ".exe" on Windows, and a full ".app"
// bundle (a directory, not a single file) on macOS.
func executableName() string {
	switch runtime.GOOS {
	case "windows":
		return "KnightLoader.exe"
	case "darwin":
		return "KnightLoader.app"
	default:
		return "KnightLoader"
	}
}

// checksumAssetName is the file desktop.yml's "attach" job writes alongside
// the platform zips (see that workflow's "Checksums" step) - a plain
// sha256sum-format text file, one line per zip, filenames bare with no path
// prefix.
const checksumAssetName = "checksums.txt"

// Download fetches the release asset matching this platform from the
// latest GitHub release (only when it is actually newer than `current` -
// mirrors Check's own "dev never compares" and X.Y.Z-only rules so a caller
// cannot download a same-or-older build by mistake) into a fresh temp file
// and returns its path. Before returning, it also fetches that same
// release's checksums.txt asset and verifies the downloaded zip's own
// SHA-256 against the entry in it for this platform's filename - see this
// package's own doc comment for what that does and does not prove, and for
// why a release missing checksums.txt entirely is a hard failure here
// rather than a fallback to unverified. Deleting the returned path once
// Apply has consumed it is the caller's job (os.RemoveAll is safe to call
// on a path that no longer exists).
func Download(ctx context.Context, current string) (zipPath string, tag string, err error) {
	if current == "" || current == "dev" {
		return "", "", errors.New("update: cannot compare an untagged build to a release")
	}
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", "", err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	currentTrimmed := strings.TrimPrefix(current, "v")
	newer, ok := isNewer(latest, currentTrimmed)
	if !ok {
		return "", "", fmt.Errorf("update: could not compare %q against %q", rel.TagName, current)
	}
	if !newer {
		return "", "", fmt.Errorf("update: %s is already the latest release", current)
	}

	slug, err := platformSlug()
	if err != nil {
		return "", "", err
	}
	want := assetName(rel.TagName, slug)
	var asset *ghAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == want {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return "", "", fmt.Errorf("update: release %s has no asset named %s", rel.TagName, want)
	}

	var checksums *ghAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == checksumAssetName {
			checksums = &rel.Assets[i]
			break
		}
	}
	if checksums == nil {
		// Treated exactly like the missing-zip-asset case just above, not as
		// a softer "older release predates this feature" fallback - see this
		// package's doc comment for the reasoning. In short: Download only
		// ever looks at "latest", and every release from this feature
		// onward publishes checksums.txt in the same job run that publishes
		// the platform zips, so this can only mean the release is broken or
		// was tampered with after publishing - either way, not something to
		// quietly proceed past.
		return "", "", fmt.Errorf("update: release %s has no %s asset - refusing to install an unverifiable download", rel.TagName, checksumAssetName)
	}

	path, err := downloadAsset(ctx, *asset)
	if err != nil {
		return "", "", err
	}

	checksumsData, err := downloadChecksums(ctx, *checksums)
	if err != nil {
		os.Remove(path)
		return "", "", err
	}
	if err := verifyChecksum(path, want, checksumsData); err != nil {
		os.Remove(path)
		return "", "", err
	}

	return path, rel.TagName, nil
}

// fetchAsset builds the validated request and does the GET common to both
// downloadAsset (the platform zip, staged to a temp file) and
// downloadChecksums (checksums.txt, small enough to keep in memory) - host
// pinning and redirect re-validation only need writing once. The caller owns
// resp.Body and must close it.
func fetchAsset(ctx context.Context, asset ghAsset) (*http.Response, error) {
	u, err := url.Parse(asset.BrowserDownloadURL)
	if err != nil {
		return nil, fmt.Errorf("update: bad asset URL: %w", err)
	}
	if u.Scheme != "https" || !allowedAssetHosts[u.Hostname()] {
		return nil, fmt.Errorf("update: refusing to download from untrusted host %q", u.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// A plain http.Client follows redirects (including cross-host ones) by
	// default - GitHub's download URL 302s to its own CDN host, which is
	// exactly the release-assets.githubusercontent.com case
	// allowedAssetHosts already covers, but a client here is built with a
	// CheckRedirect that re-validates every hop against the same allowlist
	// rather than trusting Go's default "follow anywhere" behaviour.
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if !allowedAssetHosts[r.URL.Hostname()] || r.URL.Scheme != "https" {
				return fmt.Errorf("update: refusing redirect to untrusted host %q", r.URL.Hostname())
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update: download failed: %s", resp.Status)
	}
	return resp, nil
}

func downloadAsset(ctx context.Context, asset ghAsset) (string, error) {
	resp, err := fetchAsset(ctx, asset)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	out, err := os.CreateTemp("", "knightloader-update-*.zip")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if err != nil {
		os.Remove(out.Name())
		return "", err
	}
	if closeErr != nil {
		os.Remove(out.Name())
		return "", closeErr
	}
	if asset.Size > 0 && n != asset.Size {
		os.Remove(out.Name())
		return "", fmt.Errorf("update: downloaded %d bytes, release reports %d - refusing a partial/corrupt asset", n, asset.Size)
	}
	return out.Name(), nil
}

// downloadChecksums fetches checksums.txt through the same host-pinned,
// redirect-revalidated path as downloadAsset, but keeps the bytes in memory
// instead of staging a temp file - the file is a handful of short text
// lines (one per platform zip), nothing that benefits from disk staging the
// way a multi-hundred-megabyte platform bundle does. The 1 MiB cap is
// generous headroom over anything three platforms' worth of "<hex>
// <filename>" lines could ever need; it exists only so a compromised or
// misbehaving host cannot turn this into an unbounded read.
func downloadChecksums(ctx context.Context, asset ghAsset) ([]byte, error) {
	resp, err := fetchAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if asset.Size > 0 && int64(len(body)) != asset.Size {
		return nil, fmt.Errorf("update: downloaded %s is %d bytes, release reports %d - refusing a partial/corrupt asset", checksumAssetName, len(body), asset.Size)
	}
	return body, nil
}

// verifyChecksum checks the file at zipPath against the entry for
// wantAssetName in checksumsData (checksums.txt's raw bytes, as downloaded)
// - the integrity half this package's doc comment names: TLS plus host
// pinning (fetchAsset) already guarantee the bytes came from this repo's own
// GitHub release, this additionally guarantees they were not truncated,
// corrupted, or altered after the release pipeline published them, by
// checking them against a digest published as its own asset at release
// time.
//
// The actual parsing and hashing is internal/checksum's job, not
// reimplemented here: that package already reads the exact
// "<hex>  <filename>" sha256sum format desktop.yml's "Checksums" step
// writes (ParseHashFile, shared with .sfv/md5sum/sha1sum downloads
// elsewhere in this codebase) and already hashes-and-compares a file on
// disk against one parsed entry (Verify) with its own test coverage - this
// function is just the two calls plus finding the one entry that matters
// for this asset name.
func verifyChecksum(zipPath, wantAssetName string, checksumsData []byte) error {
	sums, err := checksum.ParseHashFile(bytes.NewReader(checksumsData))
	if err != nil {
		return fmt.Errorf("update: %s: %w", checksumAssetName, err)
	}
	var want *checksum.Sum
	for i := range sums {
		if sums[i].Name == wantAssetName {
			want = &sums[i]
			break
		}
	}
	if want == nil {
		return fmt.Errorf("update: %s has no entry for %s", checksumAssetName, wantAssetName)
	}
	if want.Kind != checksum.SHA256 {
		// desktop.yml only ever writes sha256sum output - a different digest
		// length here means checksums.txt was hand-edited or generated by
		// something else, not the format this package knows how to trust.
		return fmt.Errorf("update: %s entry for %s is %s, not sha256", checksumAssetName, wantAssetName, want.Kind)
	}
	ok, err := checksum.Verify(zipPath, *want)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("update: checksum mismatch for %s against %s - refusing to install a download that does not match its published digest", wantAssetName, checksumAssetName)
	}
	return nil
}

// CurrentExecutable resolves the running process's own image, and - on
// macOS specifically - the enclosing .app bundle that is the actual unit
// Apply swaps (os.Executable there returns the binary buried inside
// Contents/MacOS/, not the bundle Finder/Wails' own installer deals in).
// installPath is what Apply's exePath argument expects; runnablePath is
// what Relaunch actually execs - identical to installPath on Windows and
// Linux, where the "install" IS the one runnable file.
func CurrentExecutable() (installPath string, runnablePath string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", "", err
	}
	if runtime.GOOS != "darwin" {
		return exe, exe, nil
	}
	// .../KnightLoader.app/Contents/MacOS/KnightLoader -> .../KnightLoader.app
	dir := filepath.Dir(exe)    // .../Contents/MacOS
	dir = filepath.Dir(dir)     // .../Contents
	bundle := filepath.Dir(dir) // .../KnightLoader.app
	if !strings.HasSuffix(bundle, ".app") {
		return "", "", fmt.Errorf("update: %s does not look like it is running from a .app bundle", exe)
	}
	return bundle, exe, nil
}

// Apply extracts the platform build from zipPath and atomically swaps it in
// at installPath (a single file on Windows/Linux, a .app bundle directory
// on macOS), then removes the now-consumed zip. installPath's own parent
// directory is used as the staging area specifically so the final swap is a
// same-filesystem os.Rename - the one operation that is genuinely atomic
// (no window where installPath is half-written), rather than a copy that
// could be interrupted partway.
//
// The previous version is renamed to installPath+".old" rather than deleted
// outright: on Windows, the currently-running process still holds its own
// image open under that name, and only a rename (not a delete) of a
// running executable is reliably permitted while it is still executing -
// the same reason every self-updating Windows tool uses this exact
// rename-aside-then-move-in pattern instead of a direct overwrite. Best-
// effort cleanup is attempted immediately; if it fails (still locked) the
// leftover .old is harmless and gets replaced by the next Apply.
func Apply(zipPath, installPath string) error {
	stagingParent := filepath.Dir(installPath)
	staged, err := extractExecutable(zipPath, stagingParent)
	if err != nil {
		return err
	}
	// Extraction can leave `staged` behind on any early return below.
	cleanupStaged := true
	defer func() {
		if cleanupStaged {
			os.RemoveAll(staged)
		}
	}()

	oldPath := installPath + ".old"
	os.RemoveAll(oldPath) // a leftover from a previous, only-partially-cleaned-up Apply

	if _, err := os.Lstat(installPath); err == nil {
		if err := os.Rename(installPath, oldPath); err != nil {
			return fmt.Errorf("update: could not move the current version aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(staged, installPath); err != nil {
		// Best-effort: put the old version back rather than leaving nothing
		// installed at all.
		_ = os.Rename(oldPath, installPath)
		return fmt.Errorf("update: could not move the new version into place: %w", err)
	}
	cleanupStaged = false

	if runtime.GOOS != "windows" {
		_ = chmodExecutable(installPath)
	}
	_ = os.RemoveAll(oldPath) // best-effort; a lock here (Windows) is not fatal
	_ = os.Remove(zipPath)
	return nil
}

// extractExecutable unzips zipPath into a fresh temp directory under
// stagingDir (same filesystem as the eventual install path, for Apply's
// atomic rename) and returns the path to just the platform executable/
// bundle inside it - desktop.yml zips the *contents* of Wails' own output
// directory, which on Windows/Linux is exactly one file and on macOS is
// exactly one directory, so there is never more than one plausible match.
func extractExecutable(zipPath, stagingDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	dest, err := os.MkdirTemp(stagingDir, "knightloader-staged-*")
	if err != nil {
		return "", err
	}
	for _, f := range r.File {
		// desktop.yml's `zip -qr` runs from inside the build output
		// directory, so entries are relative paths with no leading
		// directory component to guard against traversal from - still
		// rejected defensively rather than trusted.
		cleanName := filepath.Clean(f.Name)
		if cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanName) {
			os.RemoveAll(dest)
			return "", fmt.Errorf("update: refusing a zip entry outside the extraction root: %s", f.Name)
		}
		target := filepath.Join(dest, cleanName)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				os.RemoveAll(dest)
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			os.RemoveAll(dest)
			return "", err
		}
		if err := extractFile(f, target); err != nil {
			os.RemoveAll(dest)
			return "", err
		}
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		os.RemoveAll(dest)
		return "", err
	}
	want := executableName()
	for _, e := range entries {
		if e.Name() == want {
			return filepath.Join(dest, e.Name()), nil
		}
	}
	os.RemoveAll(dest)
	return "", fmt.Errorf("update: extracted zip has no %s at its top level", want)
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, rc)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// chmodExecutable makes sure the swapped-in file is actually runnable -
// desktop.yml's `zip -qr` preserves Unix permission bits, so this is
// normally a no-op, but it is cheap insurance against a zip that did not
// (e.g. one rebuilt by hand while testing this).
func chmodExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		// The macOS .app bundle case: the bundle directory itself does not
		// need +x, only the binary inside Contents/MacOS/ does, and that
		// entry already carries whatever mode extractFile wrote it with.
		return nil
	}
	return os.Chmod(path, info.Mode()|0o111)
}

// Relaunch starts the freshly-applied build as a new, independent process
// and returns immediately without waiting for it - the caller (desktop/
// main.go, via App.RequestUpdateInstall) is expected to exit the OLD
// process right after this returns successfully, the same "spawn the
// replacement before tearing down the original" order every self-updater
// uses to avoid a window with no running instance at all.
func Relaunch(runnablePath string, args []string) error {
	cmd := exec.Command(runnablePath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// No SysProcAttr group/session detachment: a plain child process is
	// already independent of its parent's lifetime on every platform this
	// ships for (it is not killed just because the parent exits normally),
	// which is all "keeps running after the old process quits" requires.
	return cmd.Start()
}

// String is a small debug/log helper, not used by the API response itself.
func (i Info) String() string {
	if !i.Checked {
		return "update check: failed"
	}
	if !i.Available {
		return fmt.Sprintf("update check: %s is current", i.Current)
	}
	return fmt.Sprintf("update check: %s available (running %s)", i.Latest, i.Current)
}
