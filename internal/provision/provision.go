// Package provision sets up a private, headless JDownloader for the desktop
// build so the user gets full hoster coverage out of the box without ever
// seeing JD's own UI. It downloads JDownloader.jar on first run, enables the
// local Deprecated API KnightLoader talks to, and launches it in the
// background. Nothing here is bundled in the repo: JD is fetched from its
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
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// defaultJarURL is JDownloader's official self-updating launcher jar. The
// scheme is load-bearing: whatever comes back from this URL is handed straight
// to a JVM as executable code, so over plain HTTP anyone on the path between
// us and the host (a hostile access point, a transparent proxy, an ISP box)
// would pick what runs on the user's machine. HTTPS makes that a certificate
// problem instead.
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

	mu   sync.Mutex
	proc *jdProcess // the JD we started, nil when we did not start one
}

// jdProcess is a JD this process started.
type jdProcess struct {
	cmd    *exec.Cmd
	exited chan struct{} // closed once Wait has returned
	err    error         // what Wait returned, read only after exited is closed
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
// The check is on the bytes on disk and not on the Content-Type header, which
// is only a claim by whoever answered the request and costs an attacker, or a
// captive portal serving its login page, nothing to set. The file is what gets
// executed, so the file is what is inspected. Refusing is the right answer when
// it does not look like a jar: not having JD is a missing feature, running an
// attacker's jar is a compromised machine.
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
// download is verified before it is moved into place, and a jar already on
// disk that no longer verifies is thrown away and fetched again, so a file
// left behind by an older, unchecked build is not executed on every start.
func (p *Provisioner) EnsureJar(ctx context.Context) error {
	if p.Provisioned() {
		if err := verifyJar(p.jarPath()); err == nil {
			return nil
		}
		if err := os.Remove(p.jarPath()); err != nil {
			return fmt.Errorf("provision: discard unusable JD jar: %w", err)
		}
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
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
	// The declared length is a hint that can lie or be absent, so the cap that
	// counts is the one on the bytes written below. Looking at it first only
	// saves pulling down something that would be rejected anyway.
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

// confirmAnswers are the questions JD asks while it moves links from its
// grabber to its download list, answered ahead in the config file each one is
// read from. JD's default for all three is ASK, and a JD without a window has
// nobody to answer, so the confirm waits and the link stays in the grabber.
//
// Every answer keeps the link KnightLoader asked for:
//
//   - defaultonaddeddupeslinksaction, a link JD already holds, is one of
//     OnDupesLinksAction's INCLUDE, EXCLUDE, EXCLUDE_AND_REMOVE, ASK and
//     GLOBAL.
//   - defaultonaddedofflinelinksaction, a link JD's check found offline, is one
//     of OnOfflineLinksAction's INCLUDE_OFFLINE, EXCLUDE_OFFLINE,
//     EXCLUDE_OFFLINE_AND_REMOVE, ASK and GLOBAL. Excluded, the link stays in
//     the grabber with JD's verdict on it, which the jd backend reads to fail
//     the download at once. Included, it would sit in the download list as a
//     download that never moves until the backend's stall limit.
//   - confirmincompletearchiveaction, an archive with parts JD has not seen, is
//     one of ConfirmIncompleteArchiveAction's ASK, KEEP_IN_LINKGRABBER,
//     MOVE_TO_DOWNLOADLIST and DELETE. KnightLoader hands JD one link per
//     download, so every part of a multi-part archive is such an archive.
//     LinkgrabberSettings' handleincompletearchiveonconfirmlatestselection
//     only preselects an entry in the question, so it is not the one to set.
//
// The names and values are those of JDownloader's LinkgrabberSettings,
// GraphicalUserInterfaceSettings and ConfirmLinksContextAction, which
// ConfirmLinksContextAction.confirmSelection reads on every confirm, the
// automatic one included.
var confirmAnswers = map[string]map[string]json.RawMessage{
	"org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings.json": {
		"defaultonaddeddupeslinksaction":   json.RawMessage(`"INCLUDE"`),
		"defaultonaddedofflinelinksaction": json.RawMessage(`"EXCLUDE_OFFLINE"`),
	},
	"org.jdownloader.settings.GraphicalUserInterfaceSettings.json": {
		"confirmincompletearchiveaction": json.RawMessage(`"MOVE_TO_DOWNLOADLIST"`),
	},
}

// WriteConfirmAnswers writes confirmAnswers into JD's config files. The rest
// of each file is JD's own and is kept.
func (p *Provisioner) WriteConfirmAnswers() error {
	cfgDir := filepath.Join(p.Dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return err
	}
	for file, answers := range confirmAnswers {
		path := filepath.Join(cfgDir, file)
		// Raw, so JD's values go back exactly as JD wrote them.
		cfg := map[string]json.RawMessage{}
		if b, err := os.ReadFile(path); err == nil {
			// A file JD cannot parse is reset by JD itself, so nothing is lost
			// by starting from an empty one.
			var kept map[string]json.RawMessage
			if json.Unmarshal(b, &kept) == nil {
				maps.Copy(cfg, kept)
			}
		}
		maps.Copy(cfg, answers)
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ErrNoJava is FindJava finding no Java anywhere it looks.
var ErrNoJava = errors.New("provision: no Java runtime found; install one or point JAVA_HOME at it")

// FindJava returns a runnable java: JAVA_HOME first, then PATH, then where the
// usual installers put one without touching either, such as the runtime
// JDownloader 2 brings. The container image ships a JRE; the desktop builds
// bring none and run JDownloader only on a Java the machine already has.
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
	for _, pattern := range javaPlaces() {
		matches, _ := filepath.Glob(pattern)
		// Reverse name order usually puts the newest version first.
		slices.Sort(matches)
		slices.Reverse(matches)
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
				return m, nil
			}
		}
	}
	return "", ErrNoJava
}

// javaPlaces are glob patterns for the java of the usual Java installers and of
// JDownloader 2, which installs under Program Files for everyone or under
// LocalAppData for one person.
func javaPlaces() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "JDownloader", "jre", "bin", "java.exe"),
				filepath.Join(base, "JDownloader 2.0", "jre", "bin", "java.exe"),
			)
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			for _, vendor := range []string{"Eclipse Adoptium", "Java", "Microsoft", "Zulu", "Amazon Corretto", "BellSoft"} {
				out = append(out, filepath.Join(pf, vendor, "*", "bin", "java.exe"))
			}
		}
		return out
	case "darwin":
		return []string{"/Library/Java/JavaVirtualMachines/*/Contents/Home/bin/java"}
	default:
		return []string{"/usr/lib/jvm/*/bin/java"}
	}
}

// Start launches headless JD in the background using the given java binary. The
// process is kept so Stop can terminate it later; the returned command is for
// callers that want to watch it, not for owning it.
//
// Without a display JD runs headless by itself, which is how the container
// gets it. A desktop has one, so JD would open its own window and its update
// dialogs there unless told not to.
func (p *Provisioner) Start(java string) (*exec.Cmd, error) {
	cmd := exec.Command(java, "-Djava.awt.headless=true", "-jar", p.jarPath())
	cmd.Dir = p.Dir
	nowindow.Apply(cmd)
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
	proc := &jdProcess{cmd: cmd, exited: make(chan struct{})}
	// Waited on from the start rather than in Stop, so a JD that exits by
	// itself to restart is reaped at once and Stop can tell it is gone.
	go func() {
		proc.err = cmd.Wait()
		close(proc.exited)
	}()
	p.mu.Lock()
	p.proc = proc
	p.mu.Unlock()
	return cmd, nil
}

// Stop terminates the JD this process started, or the one it restarted into.
// It is a no-op when we did not start one.
func (p *Provisioner) Stop() error {
	p.mu.Lock()
	proc := p.proc
	// Dropped while still holding the lock, so a second Stop finds nothing to do:
	// by then the pid may well belong to somebody else's process.
	p.proc = nil
	p.mu.Unlock()
	if proc == nil {
		return nil
	}

	select {
	case <-proc.exited:
		return p.stopSuccessor()
	default:
	}

	// Ask before shooting: JD writes its link list and settings out on shutdown,
	// and losing those is worse than waiting a few seconds. Windows has no
	// Unix-style signals and says so instead of delivering anything, so there is
	// nothing to wait for there and the kill below is the only option.
	if err := proc.cmd.Process.Signal(os.Interrupt); err == nil {
		select {
		case <-proc.exited:
			return stopResult(proc.err)
		case <-time.After(stopGrace):
		}
	}
	if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("provision: kill JD: %w", err)
	}
	<-proc.exited
	return stopResult(proc.err)
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

// pidFile is where a JD running from Dir keeps its pid. JD removes it when it
// exits.
func (p *Provisioner) pidFile() string { return filepath.Join(p.Dir, "JDownloader.pid") }

// stopSuccessor stops the JD that took over from the one this process started.
// JD restarts, after its first-run update or when asked to install one, by
// starting a new process and exiting, so the JD running from Dir can be one
// this process never started. It is asked through its API rather than
// signalled, since the pid in its file may be stale and name somebody else's
// process.
func (p *Provisioner) stopSuccessor() error {
	if _, err := os.Stat(p.pidFile()); err != nil {
		return nil
	}
	client := httpx.New(httpx.Options{Timeout: 5 * time.Second})
	if !p.answers(context.Background(), client) {
		return nil
	}
	resp, err := client.Get(p.URL() + "/system/exitJD")
	if err != nil {
		return fmt.Errorf("provision: ask JD to exit: %w", err)
	}
	resp.Body.Close()
	deadline := time.Now().Add(stopGrace)
	for {
		if _, err := os.Stat(p.pidFile()); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provision: JD did not exit within %s of being asked", stopGrace)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// looksLikeJD checks that the thing answering on the API port really is
// JDownloader. Its ping answers with a small JSON object, so anything that is
// not JSON is something else entirely.
func looksLikeJD(body []byte) bool {
	t := bytes.TrimSpace(body)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// answers reports whether JDownloader answers on the API port. 3128 is also
// Squid's default port. Accepting any HTTP answer would let a proxy that
// happens to be listening there pass as JDownloader, and every call afterwards
// would fail for a reason nobody could trace back to here.
func (p *Provisioner) answers(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL()+"/device/ping", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode >= 200 && resp.StatusCode <= 299 && looksLikeJD(body)
}

// WaitReachable polls the Deprecated API until it answers or the context ends.
// JD self-updates on first run, so allow a generous deadline.
func (p *Provisioner) WaitReachable(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	client := httpx.New(httpx.Options{Timeout: 10 * time.Second})
	for !p.answers(ctx, client) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

// Ensure runs the full first-run sequence: download the jar (once), write the
// API config and the confirm answers, launch JD, and wait for it to answer. Returns
// the running command and the KL_JD URL. The caller sets KL_JD (or the app's
// jd backend) to URL().
// A JD that started but never answered keeps running on purpose (it is probably
// mid self-update); it stays tracked, so Stop still terminates it at shutdown.
func (p *Provisioner) Ensure(ctx context.Context) (*exec.Cmd, string, error) {
	// JD keeps the hoster logins it is handed under cfg and inherits the
	// umask, which on Unraid is 000. A folder only this account can enter
	// keeps them private whatever modes JD gives its files, and the Chmod
	// covers a folder that already exists with wider ones.
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return nil, "", err
	}
	if err := os.Chmod(p.Dir, 0o700); err != nil {
		return nil, "", err
	}
	if err := p.EnsureJar(ctx); err != nil {
		return nil, "", err
	}
	if err := p.WriteAPIConfig(); err != nil {
		return nil, "", err
	}
	if err := p.WriteConfirmAnswers(); err != nil {
		return nil, "", err
	}
	java, err := FindJava()
	if err != nil {
		return nil, "", err
	}
	// A caller that gave up while the jar was downloading may already have
	// run Stop, and a JD started after that would outlive it.
	if err := ctx.Err(); err != nil {
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
