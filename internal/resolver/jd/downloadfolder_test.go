package jd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeFolderJD records what it was told about folders and can be made to answer
// with a package status of the caller's choosing.
type fakeFolderJD struct {
	mu       sync.Mutex
	configs  [][]string // one entry per /config/set, as its raw query parts
	adds     []string   // the raw addLinks query
	setDirs  []string   // the raw setDownloadDirectory queries
	status   string     // what queryPackages reports for the package
	linkCall int
}

func (f *fakeFolderJD) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		q := r.URL.RawQuery
		switch r.URL.Path {
		case "/config/set":
			f.configs = append(f.configs, strings.Split(q, "&"))
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/linkgrabberv2/addLinks":
			f.adds = append(f.adds, q)
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		case "/downloadsV2/setDownloadDirectory":
			f.setDirs = append(f.setDirs, q)
			_, _ = w.Write([]byte(`{"data":""}`))
		case "/downloadsV2/queryPackages":
			// The package exists once the link has been added.
			if len(f.adds) == 0 {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":9,"name":"KL-t1","status":"` + f.status + `"}]}`))
		case "/downloadsV2/queryLinks":
			f.linkCall++
			_, _ = w.Write([]byte(`{"data":[{"uuid":1,"name":"a.bin","bytesTotal":100,"bytesLoaded":10,"speed":5}]}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	})
}

func (f *fakeFolderJD) snapshot() ([][]string, []string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.configs...), append([]string(nil), f.adds...), append([]string(nil), f.setDirs...)
}

// decoded joins a raw query's URL-encoded JSON parts back into readable text,
// so an assertion can look for a path rather than for percent escapes.
func decoded(raw string) string {
	var out []string
	for _, part := range strings.Split(raw, "&") {
		d, err := url.QueryUnescape(part)
		if err != nil {
			d = part
		}
		out = append(out, d)
	}
	return strings.Join(out, " ")
}

func TestSetDownloadFolderTellsJDWhereToWrite(t *testing.T) {
	fake := &fakeFolderJD{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	if err := b.SetDownloadFolder("/data/download"); err != nil {
		t.Fatalf("SetDownloadFolder: %v", err)
	}

	configs, _, _ := fake.snapshot()
	if len(configs) != 1 {
		t.Fatalf("config/set calls = %d, want 1", len(configs))
	}
	got := decoded(strings.Join(configs[0], "&"))
	if !strings.Contains(got, "DefaultDownloadFolder") || !strings.Contains(got, "/data/download") {
		t.Errorf("config/set sent %q, want the DefaultDownloadFolder key and the path", got)
	}
}

// addLinks' destinationFolder only names the parent of a folder named after
// the package, so the poller pins the task's folder with setDownloadDirectory.
func TestDownloadPinsThePackageToTheTasksFolder(t *testing.T) {
	fake := &fakeFolderJD{status: "Downloading"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Dir = func(string) string { return "/data/download/Meine Serie" }
	b.Download("t1", "http://example.invalid/a.bin", nil, 1)
	defer b.Remove("t1", false)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, dirs := fake.snapshot(); len(dirs) > 0 {
			got := decoded(dirs[0])
			if !strings.Contains(got, "/data/download/Meine Serie") {
				t.Errorf("setDownloadDirectory sent %q, want the task's own folder", got)
			}
			// The add carries it too, for the moment before the pin lands.
			_, adds, _ := fake.snapshot()
			if len(adds) == 0 || !strings.Contains(decoded(adds[0]), "destinationFolder") {
				t.Errorf("addLinks did not carry a destinationFolder: %v", adds)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the package was never pinned to a folder")
}

// JD reports "Invalid download directory" only as a package status while its
// links look healthy.
func TestAFatalPackageStatusIsReportedAtOnce(t *testing.T) {
	fake := &fakeFolderJD{status: "Invalid download directory"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	updates := make(chan core.Update, 4)
	b := NewBackend(srv.URL, func(_ string, u core.Update) { updates <- u })
	b.Dir = func(string) string { return "/data/download" }
	b.Download("t1", "http://example.invalid/a.bin", nil, 1)
	defer b.Remove("t1", false)

	select {
	case u := <-updates:
		if u.Status != core.StatusError {
			t.Fatalf("status = %q, want an error", u.Status)
		}
		if !strings.Contains(u.Err, "Invalid download directory") {
			t.Errorf("error = %q, want JD's own sentence in it", u.Err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("a package JD refuses to write produced no update at all")
	}
}

func TestAPassingPackageStatusIsNotTreatedAsFatal(t *testing.T) {
	for _, s := range []string{"", "Downloading", "Waiting for reconnect", "[2] Wait 5m for new IP"} {
		if fatalPackageStatus(s) {
			t.Errorf("fatalPackageStatus(%q) = true, want false - that is a passing state", s)
		}
	}
	for _, s := range []string{"Invalid download directory", "invalid download directory (/root/Downloads)"} {
		if !fatalPackageStatus(s) {
			t.Errorf("fatalPackageStatus(%q) = false, want true", s)
		}
	}
}

// The status strings are the ones JD reported during a free-mode rapidgator
// download.
func TestCaptchaSkippedIsToldApartFromCaptchaInProgress(t *testing.T) {
	working := []string{
		"Captcha recognition (rapidgator.net)",
		"Waiting for user input",
		"",
		"Downloading",
	}
	for _, s := range working {
		if captchaSkipped(s) {
			t.Errorf("captchaSkipped(%q) = true, want false - JD is still working on it", s)
		}
	}
	givenUp := []string{
		"Skipped - Captcha is required",
		"skipped: captcha required",
	}
	for _, s := range givenUp {
		if !captchaSkipped(s) {
			t.Errorf("captchaSkipped(%q) = false, want true - JD has given up", s)
		}
	}
	if captchaSkipped("Invalid download directory") {
		t.Error("a folder problem was read as a skipped captcha")
	}
	if fatalPackageStatus("Skipped - Captcha is required") {
		t.Error("a skipped captcha was read as a folder problem")
	}
}
