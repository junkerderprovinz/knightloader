package jd

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJDDownloads answers the two calls the poller makes - "which package is
// this" and "what are its links doing" - and counts how often it is asked. The
// count IS the test: a task nobody is watching produces no questions.
type fakeJDDownloads struct {
	mu      sync.Mutex
	fragen  int
	enabled []bool
}

func (f *fakeJDDownloads) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/downloadsV2/queryPackages":
			f.fragen++
			// The name the backend derives from the task id (pkgName): a package
			// under any other name is one PackageUUID will not match.
			_, _ = w.Write([]byte(`{"data":[{"uuid":7,"name":"KL-t1"},{"uuid":8,"name":"KL-t2"}]}`))
		case "/downloadsV2/queryLinks":
			_, _ = w.Write([]byte(`{"data":[{"uuid":100,"name":"a.bin","bytesTotal":4096,"bytesLoaded":1024,"speed":512}]}`))
		case "/linkgrabberv2/setEnabled", "/downloadsV2/setEnabled":
			f.enabled = append(f.enabled, true)
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	})
}

func (f *fakeJDDownloads) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fragen
}

// Resume has to start watching again, and this is a regression of my own from
// 2026-09-01 rather than an old gap.
//
// Pause was changed that morning to close its poller, correctly. What it took
// away was something another file was quietly relying on: dispatchLocked does
// NOT hand an already-started task back to Start - it puts it in a.active and
// calls Resume, on the assumption that whatever was watching it still is. After
// the change a stopped-and-restarted JD task held a concurrency slot with
// nothing left to report on it, showing "running" at zero bytes for ever. Two
// of exactly those were sitting on the live instance when this was written.
func TestResumeStartsWatchingAgain(t *testing.T) {
	fake := &fakeJDDownloads{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	var mu sync.Mutex
	var meldungen []core.Update
	b := NewBackend(srv.URL, func(_ string, u core.Update) {
		mu.Lock()
		meldungen = append(meldungen, u)
		mu.Unlock()
	})

	// A task JD already knows, stopped: exactly the state the dispatcher's
	// `a.started[id]` branch calls Resume on.
	b.Pause("t1")
	vorher := fake.count()

	b.Resume("t1")
	defer b.Remove("t1", false)

	frist := time.Now().Add(4 * time.Second)
	for time.Now().Before(frist) {
		mu.Lock()
		n := len(meldungen)
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	n := len(meldungen)
	mu.Unlock()
	t.Fatalf("Resume produced %d updates and %d extra polls (was %d): nothing is watching the task, so it holds a slot for ever",
		n, fake.count()-vorher, vorher)
}

// The other half: Resume must not stack a SECOND poller on a task that is
// already being watched, or every reported byte would be counted twice.
func TestResumeDoesNotStackASecondPoller(t *testing.T) {
	fake := &fakeJDDownloads{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Resume("t2")
	defer b.Remove("t2", false)
	time.Sleep(300 * time.Millisecond)
	einer := fake.count()

	// A plain unpause that never went through the dispatcher.
	b.Resume("t2")
	time.Sleep(600 * time.Millisecond)
	zwei := fake.count()

	// One poller asks at a steady rate; two ask at twice that. Compared as a
	// ratio over the same window rather than as an absolute count, so the test
	// does not depend on the tick landing on a particular millisecond.
	proMs := float64(einer) / 300.0
	erwartet := proMs * 900.0
	if float64(zwei) > erwartet*1.6 {
		t.Errorf("after a second Resume: %d polls in 900ms, one poller would give about %.0f - a second poller is running",
			zwei, erwartet)
	}
}
