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

// The folder tests, and they exist because of the single most expensive defect
// this app has had.
//
// A headless JD nobody has told otherwise downloads into its own default, which
// resolves against the JVM's home. Measured on the two live instances the day
// this was written: one had "/root/Downloads", the other "/Downloads". The
// container runs as uid 99 and can write to neither, so JD answered every
// package with the status "Invalid download directory" - fourteen out of
// fourteen - and downloaded nothing at all, silently, from the day the backend
// shipped. Five rounds of "es lädt nirgends was runter" (jdp, 2026-08-27 to
// 2026-09-01) end here.
//
// Three separate promises, one per test: JD is TOLD the folder, every package
// is PINNED to the task's own folder, and a refusal is SAID OUT LOUD instead of
// being sat out for forty-five minutes.

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

// TestSetDownloadFolderTellsJDWhereToWrite: the one call that would have
// prevented all of it. Without it JD keeps its own default, which in a
// container is a path the process cannot write.
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

// TestDownloadPinsThePackageToTheTasksFolder covers the half addLinks alone
// cannot do. JD treats addLinks' destinationFolder as a PARENT and hangs the
// package name under it (measured: "/data/download/zielA" with package
// "KL-probeA" became "/data/download/zielA/KL-probeA"), so a file would land in
// a folder named after an internal task id. setDownloadDirectory sets it
// verbatim, and the poller applies it the moment the package appears.
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
			// The add carries it too, so the package is never filed anywhere
			// unwritable even for the moment before the pin lands.
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

// TestAFatalPackageStatusIsReportedAtOnce is the difference between the bug
// being findable and not.
//
// JD reports "Invalid download directory" as a PACKAGE status and nowhere else.
// The links under it look ordinary, so the poller folded them into a perfectly
// healthy "running at 0 bytes" and sat there holding a concurrency slot, and
// after forty-five minutes said "no progress for 45m0s" - a sentence about the
// symptom that names neither the cause nor anything to do about it.
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

// TestAPassingPackageStatusIsNotTreatedAsFatal: the guard must not turn an
// ordinary waiting state into a failed download. Being wrong in this direction
// fails transfers that would have worked, which is why the fatal list is short
// and matched rather than inferred.
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
