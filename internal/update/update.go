// Package update checks whether a newer KnightLoader release exists,
// surfaced on both deployments' General tab now (jdp, 2026-08-24: a
// container user hit the card's old desktop-only gate and asked where the
// toggle had gone - checking GitHub and saying so is exactly as harmless
// for a container as for desktop). What differs by deployment is never
// whether the check runs, only what "update available" tells you to do
// about it: desktop hands you the release page to fetch an installer from,
// and a container - which cannot replace itself from the inside - is
// pointed at the same release page but told to update the way it was
// deployed instead (docker pull, Unraid Community Applications, ...); see
// routes_features.go's updaterReason for the fuller version of that split.
//
// This package only CHECKS and reports. It does not download, verify or
// apply anything: a desktop auto-updater that silently replaces its own
// binary is a real attack surface (an unverified download executed with the
// user's own privileges), and building that safely - code-signature
// verification, an atomic swap with rollback, per-platform install
// mechanics - is its own careful piece of work, not something to bolt onto
// a UI change. What ships here is the honest first slice: tell the user a
// newer version exists and hand them the release page to fetch it
// themselves, the same tier of "auto-update" a great many desktop apps ship
// before ever attempting a silent one.
package update

import (
	"archive/zip"
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
// INTEGRITY. desktop.yml's release pipeline does not publish a checksums
// file today, so there is nothing to verify the download against beyond
// what TLS to a real GitHub asset host already guarantees (transport
// integrity + that the bytes came from this repo's own release, since only
// this repo's own Actions runner can attach an asset to its releases in the
// first place). downloadAsset therefore pins the download to GitHub's own
// asset hosts rather than following browser_download_url blindly, and
// confirms the downloaded size matches the asset metadata GitHub itself
// reported. Full code-signature verification (checking the binary was
// signed by the same identity as this running one) remains the honest gap
// this package's own doc comment already names - add a checksums.txt to
// desktop.yml's "attach" job and verify against it here as the natural next
// hardening step, not attempted today.
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

// Download fetches the release asset matching this platform from the
// latest GitHub release (only when it is actually newer than `current` -
// mirrors Check's own "dev never compares" and X.Y.Z-only rules so a caller
// cannot download a same-or-older build by mistake) into a fresh temp file
// and returns its path. Deleting it once Apply has consumed it is the
// caller's job (os.RemoveAll is safe to call on a path that no longer
// exists).
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

	path, err := downloadAsset(ctx, *asset)
	if err != nil {
		return "", "", err
	}
	return path, rel.TagName, nil
}

func downloadAsset(ctx context.Context, asset ghAsset) (string, error) {
	u, err := url.Parse(asset.BrowserDownloadURL)
	if err != nil {
		return "", fmt.Errorf("update: bad asset URL: %w", err)
	}
	if u.Scheme != "https" || !allowedAssetHosts[u.Hostname()] {
		return "", fmt.Errorf("update: refusing to download from untrusted host %q", u.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
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
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: download failed: %s", resp.Status)
	}

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
