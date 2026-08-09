// Package provision sets up a private, headless JDownloader for the desktop
// build so the user gets full hoster coverage out of the box without ever
// seeing JD's own UI. It downloads JDownloader.jar on first run, enables the
// local Deprecated API KnightLoader talks to, and launches it in the
// background. Nothing here is bundled in the repo — JD is fetched from its
// official source at runtime.
package provision

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// defaultJarURL is JDownloader's official self-updating launcher jar. The
// scheme is load-bearing: whatever comes back from this URL is handed straight
// to a JVM as executable code, so over plain HTTP anyone on the path between us
// and the host — a hostile access point, a transparent proxy, an ISP box —
// would get to pick what runs on the user's machine. HTTPS makes that a
// certificate problem instead of a free-for-all.
const defaultJarURL = "https://installer.jdownloader.org/JDownloader.jar"

// maxJarBytes caps the download. The launcher jar is a couple of megabytes;
// this is a sanity bound so a hostile or broken server cannot fill the user's
// disk, not a tight fit. It is a var only so the tests can shrink it.
var maxJarBytes int64 = 64 << 20

// downloadTimeout bounds the fetch on its own. The caller's context may be
// generous (first-run JD self-update is slow) or have no deadline at all, and a
// host that accepts the connection and then stalls must not be able to wedge
// startup forever.
const downloadTimeout = 5 * time.Minute

// stopGrace is how long JD gets to shut down by itself after we ask, before we
// kill it. It is a var so the tests do not have to sit it out.
var stopGrace = 10 * time.Second

// jarClient is our own client rather than http.DefaultClient: the default one
// is process-global, anyone can reconfigure it, and it has no timeout at all.
var jarClient = httpx.New(httpx.Options{Timeout: downloadTimeout})

// Provisioner owns a private JD home directory and the local API port.
type Provisioner struct {
	Dir  string // JD home: the jar and its cfg/ live here
	Port int    // Deprecated API port (127.0.0.1 only)

	jarURL string // download source; overridden in tests

	mu  sync.Mutex
	cmd *exec.Cmd // the JD we started, nil when we did not start one
}

// New returns a provisioner rooted at dir (default port 3128).
func New(dir string) *Provisioner {
	return &Provisioner{Dir: dir, Port: 3128, jarURL: defaultJarURL}
}

// URL is the KL_JD base for the provisioned instance.
func (p *Provisioner) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", p.Port) }

func (p *Provisioner) jarPath() string { return filepath.Join(p.Dir, "JDownloader.jar") }

// source is the URL to fetch the jar from, tolerating a Provisioner that was
// built as a struct literal instead of through New.
func (p *Provisioner) source() string {
	if p.jarURL == "" {
		return defaultJarURL
	}
	return p.jarURL
}

// Provisioned reports whether the jar is already in place.
func (p *Provisioner) Provisioned() bool {
	fi, err := os.Stat(p.jarPath())
	return err == nil && fi.Size() > 0
}

// jarMagic is the ZIP local file header signature. A jar is a zip, and every
// real one starts with a local file header.
var jarMagic = []byte{'P', 'K', 0x03, 0x04}

// verifyJar refuses anything that is not really a jar.
//
// The check is deliberately on the bytes on disk and not on the Content-Type
// header. A header is only a claim made by whoever answered the request, and
// setting it to application/java-archive costs an attacker (or a captive portal
// serving its login page) exactly nothing. The file is the thing that gets
// executed, so the file is the thing that has to be inspected. Refusing is
// always the right answer when it does not look like a jar: not having JD is a
// missing feature, running an attacker's jar is a compromised machine.
func verifyJar(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	head := make([]byte, len(jarMagic))
	_, readErr := io.ReadFull(f, head)
	f.Close()
	if readErr != nil || !bytes.Equal(head, jarMagic) {
		return fmt.Errorf("provision: %s is not a jar (wrong magic bytes); refusing to run it", filepath.Base(path))
	}
	// The magic bytes cover four bytes, which a truncated or padded download can
	// have just as well. Opening the central directory proves the archive is
	// complete and readable before java is pointed at it.
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("provision: %s is not a readable zip archive; refusing to run it: %w", filepath.Base(path), err)
	}
	zr.Close()
	return nil
}

// EnsureJar downloads JDownloader.jar into Dir if it is not there yet. The
// download is verified before it is moved into place, and a jar already on disk
// that no longer verifies is thrown away and fetched again — a file left behind
// by an older, unchecked build would otherwise keep being executed on every
// start.
func (p *Provisioner) EnsureJar(ctx context.Context) error {
	if p.Provisioned() {
		if err := verifyJar(p.jarPath()); err == nil {
			return nil
		}
		if err := os.Remove(p.jarPath()); err != nil {
			return fmt.Errorf("provision: discard unusable JD jar: %w", err)
		}
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.source(), nil)
	if err != nil {
		return err
	}
	resp, err := jarClient.Do(req)
	if err != nil {
		return fmt.Errorf("provision: download JD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provision: download JD: HTTP %d", resp.StatusCode)
	}
	// The declared length is only a hint — it can lie or be absent entirely — so
	// the cap that counts is the one on the bytes actually written below. Looking
	// at it first merely saves pulling down something we would reject anyway.
	if resp.ContentLength > maxJarBytes {
		return fmt.Errorf("provision: download JD: declared size %d exceeds the %d byte cap", resp.ContentLength, maxJarBytes)
	}

	tmp := p.jarPath() + ".part"
	// Whatever goes wrong from here on, no half-written file is left for the next
	// run to trip over. After a successful rename there is nothing left to remove
	// and the error is uninteresting.
	defer os.Remove(tmp)

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// One byte past the cap is enough to tell "exactly at the limit" from "over
	// it" without reading the rest of an endless body.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxJarBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("provision: download JD: %w", err)
	}
	if n > maxJarBytes {
		return fmt.Errorf("provision: download JD: body is larger than the %d byte cap", maxJarBytes)
	}
	if err := verifyJar(tmp); err != nil {
		return err
	}
	return os.Rename(tmp, p.jarPath())
}

// WriteAPIConfig enables the Deprecated API (plain-HTTP local JSON) that
// KnightLoader uses, writing JD's config file so it is picked up on boot.
func (p *Provisioner) WriteAPIConfig() error {
	cfgDir := filepath.Join(p.Dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return err
	}
	cfg := map[string]any{
		"deprecatedapienabled":       true,
		"deprecatedapiport":          p.Port,
		"deprecatedapilocalhostonly": true, // desktop: same machine only
		"jdanywhereapienabled":       false,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(cfgDir, "org.jdownloader.api.RemoteAPIConfig.json")
	return os.WriteFile(path, b, 0o644)
}

// FindJava returns a runnable java, preferring JAVA_HOME then PATH. The desktop
// installer bundles a JRE; this also lets a system Java be used.
func FindJava() (string, error) {
	bin := "java"
	if runtime.GOOS == "windows" {
		bin = "java.exe"
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		cand := filepath.Join(home, "bin", bin)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, nil
		}
	}
	if path, err := exec.LookPath(bin); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("provision: no Java runtime found (set JAVA_HOME or bundle a JRE)")
}

// Start launches headless JD in the background using the given java binary. The
// process is kept so Stop can terminate it later; the returned command is for
// callers that want to watch it, not for owning it.
func (p *Provisioner) Start(java string) (*exec.Cmd, error) {
	cmd := exec.Command(java, "-jar", p.jarPath())
	cmd.Dir = p.Dir
	return p.start(cmd)
}

// start runs cmd and records it as the JD this process owns. It is split out of
// Start so the tests can exercise the ownership rules with a process that is not
// a JVM.
func (p *Provisioner) start(cmd *exec.Cmd) (*exec.Cmd, error) {
	// A second Start would otherwise orphan the first JD: nothing else holds a
	// handle on it, so it would keep the API port and run until the machine is
	// rebooted, and the new instance would fail to bind.
	if err := p.Stop(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("provision: start JD: %w", err)
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()
	return cmd, nil
}

// Stop terminates a JD that this process started. It is a no-op when we did not
// start one.
func (p *Provisioner) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	// Dropped while still holding the lock, so a second Stop finds nothing to do:
	// Wait must never run twice on the same command, and by then the pid may well
	// belong to somebody else's process.
	p.cmd = nil
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Only this goroutine ever waits on the command, so its result can be read
	// after the kill below without racing anyone.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Ask before shooting: JD writes its link list and settings out on shutdown,
	// and losing those is worse than waiting a few seconds. Windows has no
	// Unix-style signals and says so instead of delivering anything, so there is
	// nothing to wait for there and the kill below is the only option.
	if err := cmd.Process.Signal(os.Interrupt); err == nil {
		select {
		case werr := <-done:
			return stopResult(werr)
		case <-time.After(stopGrace):
		}
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("provision: kill JD: %w", err)
	}
	return stopResult(<-done)
}

// stopResult swallows the non-zero exit status a process reports when it is
// terminated on request: that is Stop working, not Stop failing.
func stopResult(err error) error {
	var ee *exec.ExitError
	if err == nil || errors.As(err, &ee) {
		return nil
	}
	return fmt.Errorf("provision: stop JD: %w", err)
}

// WaitReachable polls the Deprecated API until it answers or the context ends.
// JD self-updates on first run, so allow a generous deadline.
// looksLikeJD checks that the thing answering on the API port really is
// JDownloader. Its ping answers with a small JSON object, so anything that is
// not JSON is something else entirely.
func looksLikeJD(body []byte) bool {
	t := bytes.TrimSpace(body)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

func (p *Provisioner) WaitReachable(ctx context.Context) error {
	ping := p.URL() + "/device/ping"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// 3128 is also Squid's default port. Accepting any HTTP answer would let a
	// proxy that happens to be listening there pass as JDownloader, and every
	// call afterwards would fail for a reason nobody could trace back to here.
	client := httpx.New(httpx.Options{Timeout: 10 * time.Second})
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ping, nil)
		if resp, err := client.Do(req); err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode <= 299 && looksLikeJD(body) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Ensure runs the full first-run sequence: download the jar (once), write the
// API config, launch JD, and wait for it to answer. Returns the running command
// and the KL_JD URL. The caller sets KL_JD (or the app's jd backend) to URL().
// A JD that started but never answered keeps running on purpose (it is probably
// mid self-update); it stays tracked, so Stop still terminates it at shutdown.
func (p *Provisioner) Ensure(ctx context.Context) (*exec.Cmd, string, error) {
	if err := p.EnsureJar(ctx); err != nil {
		return nil, "", err
	}
	if err := p.WriteAPIConfig(); err != nil {
		return nil, "", err
	}
	java, err := FindJava()
	if err != nil {
		return nil, "", err
	}
	cmd, err := p.Start(java)
	if err != nil {
		return nil, "", err
	}
	if err := p.WaitReachable(ctx); err != nil {
		return cmd, "", err
	}
	return cmd, p.URL(), nil
}
