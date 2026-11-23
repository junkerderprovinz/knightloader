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
// releases. For the copy installed for all users, a stand-in for the elevated
// run records the version it puts in place in *installed.
func testUpdater(t *testing.T, srv *httptest.Server) (*updater, *string) {
	t.Helper()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	prevElevated, prevInstalled := updateElevated, installedVersion
	t.Cleanup(func() { updateElevated, installedVersion = prevElevated, prevInstalled })
	installed := "1.3.0"
	installedVersion = func() string { return installed }
	updateElevated = func(context.Context, func(update.Progress)) error {
		installed = "1.4.0"
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
	return up, &installed
}

func zippedProgram(content string) []byte {
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, _ := zw.Create("KnightLoader.exe")
	w.Write([]byte(content))
	zw.Close()
	return zipped.Bytes()
}

// settled waits for an install to stop downloading and returns where it ended.
func settled(t *testing.T, up *updater) update.Progress {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p := up.currentProgress()
		if p.State != update.Downloading {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the install is still at %+v", up.currentProgress())
	return update.Progress{}
}

func TestInstallOfTheInstalledCopyEndsReadyForARestart(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, _ := testUpdater(t, srv)
	var announced []string
	up.announce = func(v string) { announced = append(announced, v) }

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Ready || got.Version != "1.4.0" {
		t.Fatalf("install ended at %+v, want 1.4.0 ready for Restart", got)
	}
	if up.readyVersion() != "1.4.0" || len(announced) != 1 || announced[0] != "1.4.0" {
		t.Errorf("ready %q, announced %v, want 1.4.0 once", up.readyVersion(), announced)
	}
}

func TestInstallOfTheInstalledCopyShowsHowFarTheElevatedRunHasCome(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, installed := testUpdater(t, srv)
	halfway, carryOn := make(chan struct{}), make(chan struct{})
	updateElevated = func(_ context.Context, progress func(update.Progress)) error {
		progress(update.Progress{State: update.Downloading, Version: "1.4.0", Done: 40, Total: 100})
		close(halfway)
		<-carryOn
		*installed = "1.4.0"
		return nil
	}

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	<-halfway
	if p := up.currentProgress(); p.State != update.Downloading || p.Done != 40 || p.Total != 100 || p.Version != "1.4.0" {
		t.Errorf("progress halfway is %+v, want 40 of 100 bytes of 1.4.0", p)
	}
	close(carryOn)
	if got := settled(t, up); got.State != update.Ready {
		t.Fatalf("install ended at %+v", got)
	}
}

func TestInstallOfTheInstalledCopySaysWhenWindowsRefused(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, _ := testUpdater(t, srv)
	updateElevated = func(context.Context, func(update.Progress)) error {
		return errors.New("permission to run it was refused")
	}

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Failed || !strings.Contains(got.Error, "permission") {
		t.Fatalf("install ended at %+v, want the refusal passed on", got)
	}
	if up.readyVersion() != "" {
		t.Errorf("%q waits for a restart after a refusal", up.readyVersion())
	}
}

func TestInstallOfTheInstalledCopySaysWhenNothingNewerWasPutInPlace(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, _ := testUpdater(t, srv)
	updateElevated = func(context.Context, func(update.Progress)) error { return nil }

	if err := up.install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := settled(t, up)
	if got.State != update.Failed || got.Error != errNothingNewer.Error() {
		t.Fatalf("install ended at %+v, want %q", got, errNothingNewer)
	}
}

// The elevated run is Update now in a process that may write to the folder
// of the copy installed for all users. It leaves the window a record of how
// far it has come and how it ended.
func TestUpdateNowPutsTheReleaseInPlaceAndRecordsHowItEnded(t *testing.T) {
	files := map[string][]byte{"knightloader-test.zip": zippedProgram("new program")}
	srv := fakeGitHub(t, "v1.4.0", files, sumsOf(files))
	up, _ := testUpdater(t, srv)
	record := filepath.Join(t.TempDir(), progressFile)
	var seen []update.Progress
	prevWrite := writeProgress
	t.Cleanup(func() { writeProgress = prevWrite })
	writeProgress = func(path string, p update.Progress) error {
		seen = append(seen, p)
		return prevWrite(path, p)
	}

	if err := up.updateNow(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(up.u.Path); string(data) != "new program" {
		t.Errorf("program is %q after the update", data)
	}
	if len(seen) < 2 || seen[0].State != update.Downloading {
		t.Fatalf("recorded %+v, want downloading first", seen)
	}
	got, err := readProgress(record)
	if err != nil || got.State != update.Ready || got.Version != "1.4.0" {
		t.Fatalf("the record says %+v (%v), want 1.4.0 ready", got, err)
	}
}

func TestUpdateNowRecordsWhyItPutNothingInPlace(t *testing.T) {
	files := map[string][]byte{"knightloader-test.zip": zippedProgram("new program")}
	sums := sumsOf(map[string][]byte{"knightloader-test.zip": []byte("something else")})
	srv := fakeGitHub(t, "v1.4.0", files, sums)
	up, _ := testUpdater(t, srv)
	record := filepath.Join(t.TempDir(), progressFile)

	if err := up.updateNow(context.Background(), record); err == nil {
		t.Fatal("updateNow put a file in place that failed its checksum")
	}
	if data, _ := os.ReadFile(up.u.Path); string(data) != "old program" {
		t.Errorf("program is %q after a failed update", data)
	}
	got, err := readProgress(record)
	if err != nil || got.State != update.Failed || !strings.Contains(got.Error, "checksum") {
		t.Fatalf("the record says %+v (%v), want a failure that names the checksum", got, err)
	}
}

func TestInstallOfAPortableCopyPutsTheNewVersionInPlace(t *testing.T) {
	files := map[string][]byte{"knightloader-test.zip": zippedProgram("new program")}
	srv := fakeGitHub(t, "v1.4.0", files, sumsOf(files))
	up, _ := testUpdater(t, srv)
	updateElevated = func(context.Context, func(update.Progress)) error {
		t.Error("a portable copy asked for an elevated run")
		return nil
	}

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
}

func TestInstallWaitsForAnUpdateUnderWay(t *testing.T) {
	srv := fakeGitHub(t, "v1.4.0", nil, "")
	up, _ := testUpdater(t, srv)
	up.busy.Store(true)

	if err := up.install(context.Background(), true); !errors.Is(err, errUpdating) {
		t.Fatalf("install during another update = %v, want errUpdating", err)
	}
}

// The program inside an AppImage runs from a mount that is gone once it quits,
// so the update replaces the AppImage file with the release's AppImage.
func TestAnAppImageUpdatesItsOwnFile(t *testing.T) {
	const image = "/home/jane doe/Apps/KnightLoader.AppImage"
	t.Setenv("APPIMAGE", image)
	up := newUpdater(t.Logf, func(string) {})
	if up.u.Path != image {
		t.Errorf("the update replaces %q, want the AppImage", up.u.Path)
	}
	if got := up.u.Assets["linux/amd64"]; got != "knightloader-linux-amd64.AppImage" {
		t.Errorf("the update downloads %q on linux/amd64, want the AppImage", got)
	}
}
