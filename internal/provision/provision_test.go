package provision

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteAPIConfig(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	p.Port = 3128
	if err := p.WriteAPIConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "cfg", "org.jdownloader.api.RemoteAPIConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["deprecatedapienabled"] != true {
		t.Errorf("deprecatedapienabled = %v, want true", cfg["deprecatedapienabled"])
	}
	if cfg["deprecatedapiport"].(float64) != 3128 {
		t.Errorf("deprecatedapiport = %v, want 3128", cfg["deprecatedapiport"])
	}
	if cfg["deprecatedapilocalhostonly"] != true {
		t.Errorf("deprecatedapilocalhostonly = %v, want true", cfg["deprecatedapilocalhostonly"])
	}
}

func TestFindJavaFromJavaHome(t *testing.T) {
	// Build a fake JAVA_HOME with a bin/java(.exe) file and confirm it's found.
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	javaPath := filepath.Join(bin, name)
	if err := os.WriteFile(javaPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_HOME", home)

	got, err := FindJava()
	if err != nil {
		t.Fatal(err)
	}
	if got != javaPath {
		t.Fatalf("FindJava = %q, want %q", got, javaPath)
	}
}

func TestProvisionedFalseOnEmptyDir(t *testing.T) {
	p := New(t.TempDir())
	if p.Provisioned() {
		t.Error("Provisioned() = true on an empty dir")
	}
	if p.URL() != "http://127.0.0.1:3128" {
		t.Errorf("URL() = %q", p.URL())
	}
}

// If this fails, the installer jar is being fetched over a channel that anyone
// on the network path can rewrite, and the JVM will happily execute whatever
// they substitute. That is remote code execution, not a style question.
func TestJarURLIsHTTPS(t *testing.T) {
	if !strings.HasPrefix(defaultJarURL, "https://") {
		t.Fatalf("defaultJarURL = %q, must be https", defaultJarURL)
	}
}

// fakeJar returns the bytes of a small but genuinely valid jar, so the
// verification path sees exactly the shape a real download has.
func fakeJar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("META-INF/MANIFEST.MF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("Manifest-Version: 1.0\nMain-Class: org.jdownloader.Launcher\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveBody stands in for installer.jdownloader.org and reports how often it was
// asked for the jar. Nothing in this package's tests touches the real host.
func serveBody(t *testing.T, contentType string, body []byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// shrinkCap lowers the download cap for one test so an "oversized" body can be a
// few kilobytes rather than tens of megabytes.
func shrinkCap(t *testing.T, n int64) {
	t.Helper()
	old := maxJarBytes
	maxJarBytes = n
	t.Cleanup(func() { maxJarBytes = old })
}

// assertNoJar pins the fail-safe half of a refusal: a rejected download must
// leave nothing behind, or the next start would find a file, consider itself
// provisioned and run it.
func assertNoJar(t *testing.T, p *Provisioner) {
	t.Helper()
	if _, err := os.Stat(p.jarPath()); err == nil {
		t.Error("a refused download was still moved into place")
	}
	if _, err := os.Stat(p.jarPath() + ".part"); err == nil {
		t.Error("the partial download was left on disk")
	}
}

func TestEnsureJarStoresVerifiedJar(t *testing.T) {
	jar := fakeJar(t)
	srv, hits := serveBody(t, "application/java-archive", jar)
	p := New(t.TempDir())
	p.jarURL = srv.URL

	if err := p.EnsureJar(context.Background()); err != nil {
		t.Fatalf("EnsureJar: %v", err)
	}
	got, err := os.ReadFile(p.jarPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jar) {
		t.Error("stored jar does not match what the server sent")
	}
	if !p.Provisioned() {
		t.Error("Provisioned() = false after a successful download")
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
}

// The server declares the friendliest possible Content-Type and still serves an
// error page. If this test fails we are trusting a header instead of the bytes,
// which means anyone able to answer the request can get their own code executed.
func TestEnsureJarRefusesWrongMagicBytes(t *testing.T) {
	srv, _ := serveBody(t, "application/java-archive", []byte("<html><body>captive portal, please log in</body></html>"))
	p := New(t.TempDir())
	p.jarURL = srv.URL

	err := p.EnsureJar(context.Background())
	if err == nil {
		t.Fatal("EnsureJar accepted a body that is not a jar")
	}
	if !strings.Contains(err.Error(), "magic bytes") {
		t.Errorf("error = %v, want it to name the magic-byte check", err)
	}
	assertNoJar(t, p)
}

// Right first four bytes, broken archive: the magic-byte check alone would let a
// truncated (or deliberately padded) download through to the JVM.
func TestEnsureJarRefusesTruncatedArchive(t *testing.T) {
	jar := fakeJar(t)
	srv, _ := serveBody(t, "application/java-archive", jar[:len(jar)/2])
	p := New(t.TempDir())
	p.jarURL = srv.URL

	if err := p.EnsureJar(context.Background()); err == nil {
		t.Fatal("EnsureJar accepted a truncated archive")
	}
	assertNoJar(t, p)
}

// Both cases matter: a server that declares an absurd length is rejected before
// the transfer, and one that declares nothing (chunked) has to be cut off while
// it streams. Without the second, an endless body fills the user's disk.
func TestEnsureJarRefusesOversizedBody(t *testing.T) {
	const limit = 4 << 10
	tests := []struct {
		name    string
		chunked bool
	}{
		{"declared length", false},
		{"undeclared length", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shrinkCap(t, limit)
			chunk := bytes.Repeat([]byte{'A'}, 1<<10)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tc.chunked {
					w.Write(bytes.Repeat([]byte{'A'}, limit*4))
					return
				}
				// Flushing before the body is complete forces chunked encoding,
				// so the client never learns how much is coming.
				for written := 0; written < limit*4; {
					n, err := w.Write(chunk)
					if err != nil {
						return
					}
					written += n
					w.(http.Flusher).Flush()
				}
			}))
			defer srv.Close()

			p := New(t.TempDir())
			p.jarURL = srv.URL
			err := p.EnsureJar(context.Background())
			if err == nil {
				t.Fatal("EnsureJar accepted a body past the size cap")
			}
			if !strings.Contains(err.Error(), "cap") {
				t.Errorf("error = %v, want it to mention the cap", err)
			}
			assertNoJar(t, p)
		})
	}
}

func TestEnsureJarSkipsFetchWhenAlreadyProvisioned(t *testing.T) {
	srv, hits := serveBody(t, "application/java-archive", fakeJar(t))
	p := New(t.TempDir())
	p.jarURL = srv.URL
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.jarPath(), fakeJar(t), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.EnsureJar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0 — an existing jar was re-downloaded", n)
	}
}

// A jar put there by an older build that never verified anything must not be
// trusted just because it exists; it gets replaced, not executed.
func TestEnsureJarReplacesUnusableJarOnDisk(t *testing.T) {
	jar := fakeJar(t)
	srv, hits := serveBody(t, "application/java-archive", jar)
	p := New(t.TempDir())
	p.jarURL = srv.URL
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.jarPath(), []byte("not a jar at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.EnsureJar(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p.jarPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jar) {
		t.Error("the unusable jar was kept instead of being replaced")
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
}

func TestEnsureJarRefusesNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	p := New(t.TempDir())
	p.jarURL = srv.URL

	if err := p.EnsureJar(context.Background()); err == nil {
		t.Fatal("EnsureJar accepted an HTTP 404 body")
	}
	assertNoJar(t, p)
}

const sleeperEnv = "KL_PROVISION_TEST_SLEEPER"

// TestSleeperHelperProcess is not a test of its own: the Stop tests re-execute
// this binary with sleeperEnv set to get a child process that stays alive until
// something terminates it. Re-executing ourselves keeps the tests free of any
// external program and behaves the same on every platform.
func TestSleeperHelperProcess(t *testing.T) {
	if os.Getenv(sleeperEnv) != "1" {
		t.Skip("helper process, not a test")
	}
	time.Sleep(2 * time.Minute)
}

func helperCmd() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestSleeperHelperProcess", "-test.timeout=0")
	cmd.Env = append(os.Environ(), sleeperEnv+"=1")
	return cmd
}

// A short grace keeps the tests quick on platforms where the polite signal is
// not delivered and we have to wait it out before killing.
func quickGrace(t *testing.T) {
	t.Helper()
	old := stopGrace
	stopGrace = 300 * time.Millisecond
	t.Cleanup(func() { stopGrace = old })
}

// Shutdown paths call Stop more than once (explicit stop plus a deferred one).
// The second call must not wait on an already-reaped command or signal a pid
// that now belongs to somebody else.
func TestStopIsSafeToCallTwice(t *testing.T) {
	quickGrace(t)
	p := New(t.TempDir())
	cmd, err := p.start(helperCmd())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("first Stop returned before the process was reaped")
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStopIsNoOpWithoutAStartedProcess(t *testing.T) {
	p := New(t.TempDir())
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop on a provisioner that started nothing: %v", err)
	}
}

// Restarting used to leak the old JVM: nothing held a handle on it, so it kept
// running and kept the API port. Starting again must reap the previous one.
func TestStartStopsThePreviousProcess(t *testing.T) {
	quickGrace(t)
	p := New(t.TempDir())
	first, err := p.start(helperCmd())
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.start(helperCmd())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	if first.ProcessState == nil {
		t.Error("the first process is still running after a second Start")
	}
	if second.ProcessState != nil {
		t.Error("the second process was terminated by its own Start")
	}
}
