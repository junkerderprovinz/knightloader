package jre

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRuntime is an archive laid out like Adoptium's for this platform: one
// top folder, inside the bundle on macOS, with bin/java and a licence file.
func fakeRuntime(t *testing.T) (string, []byte) {
	t.Helper()
	top := "jdk-21.0.13+9-jre/"
	if runtime.GOOS == "darwin" {
		top += "Contents/Home/"
	}
	files := map[string]string{top + "bin/" + filepath.Base(Java("")): "java", top + "legal/java.base/LICENSE": "GPLv2+CE"}
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for name, body := range files {
			w, _ := zw.Create(name)
			w.Write([]byte(body))
		}
		zw.Close()
		return "runtime.zip", buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.WriteHeader(&tar.Header{Name: top + "legal/java.sql/LICENSE", Linkname: "../java.base/LICENSE", Typeflag: tar.TypeSymlink})
	tw.WriteHeader(&tar.Header{Name: top + "escape", Linkname: "../../../outside", Typeflag: tar.TypeSymlink})
	tw.Close()
	gz.Close()
	return "runtime.tar.gz", buf.Bytes()
}

// serve stands in for Adoptium's release with the archive under name and pins
// this platform to it with digest. up switches the server between answering and
// failing every request.
func serve(t *testing.T, name string, body []byte, digest string) (hits *atomic.Int64, up *atomic.Bool) {
	t.Helper()
	hits, up = new(atomic.Int64), new(atomic.Bool)
	up.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !up.Load() || r.URL.Path != "/"+name {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	platform := runtime.GOOS + "/" + runtime.GOARCH
	prevBase, prevPin, prevSmoke := downloadBase, pinned[platform], smokeTest
	t.Cleanup(func() { downloadBase, pinned[platform], smokeTest = prevBase, prevPin, prevSmoke })
	downloadBase = srv.URL + "/"
	pinned[platform] = archive{name, digest}
	smokeTest = func(context.Context, string) error { return nil }
	return hits, up
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestEnsureFetchesTheRuntimeIntoTheDataFolder(t *testing.T) {
	name, body := fakeRuntime(t)
	serve(t, name, body, digest(body))
	data := t.TempDir()
	var done, total int64
	java, err := Ensure(context.Background(), data, func(d, t int64) { done, total = d, t })
	if err != nil {
		t.Fatal(err)
	}
	if java != Java(data) {
		t.Errorf("java = %s, want %s", java, Java(data))
	}
	if b, err := os.ReadFile(java); err != nil || string(b) != "java" {
		t.Fatalf("java in place is %q, %v", b, err)
	}
	if done != int64(len(body)) || total != int64(len(body)) {
		t.Errorf("last progress %d of %d, want %d of %d", done, total, len(body), len(body))
	}
	rec, err := Load(data)
	if err != nil || rec == nil || rec.Version != Version || rec.Vendor != Vendor {
		t.Fatalf("record = %+v, %v", rec, err)
	}
	if runtime.GOOS != "windows" {
		if b, err := os.ReadFile(filepath.Join(Dir(data), "legal", "java.sql", "LICENSE")); err != nil || string(b) != "GPLv2+CE" {
			t.Errorf("the licence link inside the runtime does not resolve: %q, %v", b, err)
		}
		if _, err := os.Lstat(filepath.Join(Dir(data), "escape")); err == nil {
			t.Error("a link leading out of the runtime was unpacked")
		}
	}
	if left, _ := filepath.Glob(filepath.Join(data, ".jre-*")); len(left) != 0 {
		t.Errorf("left %v behind", left)
	}
}

func TestEnsureLeavesARuntimeAlreadyInPlace(t *testing.T) {
	name, body := fakeRuntime(t)
	hits, _ := serve(t, name, body, digest(body))
	data := t.TempDir()
	if _, err := Ensure(context.Background(), data, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(context.Background(), data, nil); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("downloaded %d times, want once", n)
	}
}

func TestEnsureReplacesTheRuntimeOfAnEarlierVersion(t *testing.T) {
	name, body := fakeRuntime(t)
	hits, _ := serve(t, name, body, digest(body))
	data := t.TempDir()
	if _, err := Ensure(context.Background(), data, nil); err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(mustRead(t, filepath.Join(Dir(data), recordFile)), Version, "17.0.1+12", 1)
	if err := os.WriteFile(filepath.Join(Dir(data), recordFile), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(context.Background(), data, nil); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("downloaded %d times, want a second time for the new version", n)
	}
	if rec, _ := Load(data); rec == nil || rec.Version != Version {
		t.Errorf("record = %+v, want %s", rec, Version)
	}
}

func TestEnsureThrowsAwayADownloadThatFailsItsChecksum(t *testing.T) {
	name, body := fakeRuntime(t)
	serve(t, name, body, digest([]byte("another runtime")))
	data := t.TempDir()
	_, err := Ensure(context.Background(), data, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Ensure = %v, want a checksum refusal", err)
	}
	if _, err := os.Stat(Dir(data)); err == nil {
		t.Error("a runtime that failed its checksum was put in place")
	}
	if left, _ := filepath.Glob(filepath.Join(data, ".jre-*")); len(left) != 0 {
		t.Errorf("left %v behind", left)
	}
}

func TestEnsureFetchesAgainOnceTheNetworkIsBack(t *testing.T) {
	name, body := fakeRuntime(t)
	_, up := serve(t, name, body, digest(body))
	data := t.TempDir()
	up.Store(false)
	if _, err := Ensure(context.Background(), data, nil); err == nil || !strings.Contains(err.Error(), "downloading the Java runtime") {
		t.Fatalf("Ensure while offline = %v, want the download's failure", err)
	}
	up.Store(true)
	if _, err := Ensure(context.Background(), data, nil); err != nil {
		t.Fatalf("Ensure once back online: %v", err)
	}
}

func TestEnsureKeepsTheOldRuntimeWhenTheNewOneDoesNotRun(t *testing.T) {
	name, body := fakeRuntime(t)
	serve(t, name, body, digest(body))
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(Dir(data), "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Java(data), []byte("old java"), 0o755); err != nil {
		t.Fatal(err)
	}
	smokeTest = func(context.Context, string) error { return os.ErrPermission }
	if _, err := Ensure(context.Background(), data, nil); err == nil {
		t.Fatal("a runtime that does not run was put in place")
	}
	if got := mustRead(t, Java(data)); got != "old java" {
		t.Errorf("java is %q, want the old one kept", got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
