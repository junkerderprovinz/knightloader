package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/update"
)

const testInstaller = "knightloader-test-installer.exe"

// fakeGitHub serves a latest release with files and a checksums.txt that
// sums lists, the way release.yml publishes one.
func fakeGitHub(t *testing.T, tag string, files map[string][]byte, sums string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/"+update.Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		assets := []asset{{update.ChecksumsFile, srv.URL + "/download/" + update.ChecksumsFile}}
		for name := range files {
			assets = append(assets, asset{name, srv.URL + "/download/" + name})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "html_url": srv.URL, "assets": assets})
	})
	mux.HandleFunc("GET /download/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		data, ok := files[name]
		if name == update.ChecksumsFile {
			data, ok = []byte(sums), true
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sumsOf(files map[string][]byte) string {
	var b strings.Builder
	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return b.String()
}

// testUpdater runs v1.3.0 from a program file of its own and asks srv for
// releases. It offers testInstaller as this platform's installer and records
// what it is asked to launch instead of starting it.
func testUpdater(t *testing.T, srv *httptest.Server) (*updater, *[]string) {
	t.Helper()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	prevInstallers, prevLaunch := update.Installers, launchInstaller
	t.Cleanup(func() { update.Installers, launchInstaller = prevInstallers, prevLaunch })
	update.Installers = map[string]string{platform: testInstaller}
	var launched []string
	launchInstaller = func(path string) error {
		launched = append(launched, path)
		return nil
	}

	prog := filepath.Join(t.TempDir(), "KnightLoader.exe")
	if err := os.WriteFile(prog, []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	up := newUpdater(t.Logf, func(string) {})
	up.u.Version = "v1.3.0"
	up.u.API = srv.URL
	up.u.Client = srv.Client()
	up.u.Path = prog
	up.u.Assets = map[string]string{platform: "knightloader-test.zip"}
	return up, &launched
}

// settled waits for an install to stop downloading and returns where it ended.
func settled(t *testing.T, up *updater) update.Progress {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p := up.currentProgress()
		if p.State != update.Downloading && p.State != update.Starting {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the install is still at %+v", up.currentProgress())
	return update.Progress{}
}

func TestInstallStartsTheVerifiedInstallerOfTheNewRelease(t *testing.T) {
	setup := bytes.Repeat([]byte("installer"), 1000)
	files := map[string][]byte{testInstaller: setup}
	srv := fakeGitHub(t, "v1.4.0", files, sumsOf(files))
	up, launched := testUpdater(t, srv)

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Started || got.Version != "1.4.0" {
		t.Fatalf("install ended at %+v, want the installer of 1.4.0 started", got)
	}
	if len(*launched) != 1 {
		t.Fatalf("launched %v, want the installer once", *launched)
	}
	data, err := os.ReadFile((*launched)[0])
	if err != nil || !bytes.Equal(data, setup) {
		t.Fatalf("launched something other than the release's installer: %v", err)
	}
	if filepath.Ext((*launched)[0]) != ".exe" {
		t.Errorf("launched %s, which Windows would not run as a program", (*launched)[0])
	}
}

func TestInstallRefusesAnInstallerThatFailsItsChecksum(t *testing.T) {
	files := map[string][]byte{testInstaller: []byte("installer")}
	sums := sumsOf(map[string][]byte{testInstaller: []byte("another installer")})
	srv := fakeGitHub(t, "v1.4.0", files, sums)
	up, launched := testUpdater(t, srv)

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Failed || !strings.Contains(got.Error, "checksum") {
		t.Fatalf("install ended at %+v, want a failure that names the checksum", got)
	}
	if len(*launched) != 0 {
		t.Errorf("launched %v after a checksum mismatch", *launched)
	}
}

func TestInstallSaysWhenTheInstallerDidNotStart(t *testing.T) {
	files := map[string][]byte{testInstaller: []byte("installer")}
	srv := fakeGitHub(t, "v1.4.0", files, sumsOf(files))
	up, _ := testUpdater(t, srv)
	launchInstaller = func(string) error { return errors.New("permission to run it was refused") }

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Failed || !strings.Contains(got.Error, "permission") {
		t.Fatalf("install ended at %+v, want the refusal passed on", got)
	}
}

func TestInstallOfAPortableCopyPutsTheNewVersionInPlace(t *testing.T) {
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, _ := zw.Create("KnightLoader.exe")
	w.Write([]byte("new program"))
	zw.Close()
	files := map[string][]byte{"knightloader-test.zip": zipped.Bytes()}
	srv := fakeGitHub(t, "v1.4.0", files, sumsOf(files))
	up, launched := testUpdater(t, srv)

	if err := up.install(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Ready || got.Version != "1.4.0" {
		t.Fatalf("install ended at %+v, want 1.4.0 ready for the next start", got)
	}
	if data, _ := os.ReadFile(up.u.Path); string(data) != "new program" {
		t.Errorf("program is %q after the update", data)
	}
	if len(*launched) != 0 {
		t.Errorf("a portable copy launched an installer: %v", *launched)
	}
}

func TestInstallWaitsForAnUpdateUnderWay(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, _ := testUpdater(t, srv)
	up.busy.Store(true)

	if err := up.install(context.Background(), true); !errors.Is(err, errUpdating) {
		t.Fatalf("install during another update = %v, want errUpdating", err)
	}
}
