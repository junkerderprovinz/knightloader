package jd

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJDDownloads answers the two calls the poller makes and counts how often
// it is asked; a task nobody watches produces no questions.
type fakeJDDownloads struct {
	mu      sync.Mutex
	queries int
	enabled []bool
}

func (f *fakeJDDownloads) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/downloadsV2/queryPackages":
			f.queries++
			// Named as pkgName derives them from the task ids.
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
	return f.queries
}

// The dispatcher resumes an already started task through Resume, so Resume
// must start a poller or the task holds its slot at "running" for ever.
func TestResumeStartsWatchingAgain(t *testing.T) {
	fake := &fakeJDDownloads{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	var mu sync.Mutex
	var updates []core.Update
	b := NewBackend(srv.URL, func(_ string, u core.Update) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	})

	// A stopped task JD already knows, the state the dispatcher resumes.
	b.Pause("t1")
	before := fake.count()

	b.Resume("t1")
	defer b.Remove("t1", false)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(updates)
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	n := len(updates)
	mu.Unlock()
	t.Fatalf("Resume produced %d updates and %d extra polls (was %d): nothing is watching the task, so it holds a slot for ever",
		n, fake.count()-before, before)
}

// A second poller on a watched task would count every byte twice.
func TestResumeDoesNotStackASecondPoller(t *testing.T) {
	fake := &fakeJDDownloads{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Resume("t2")
	defer b.Remove("t2", false)
	time.Sleep(300 * time.Millisecond)
	first := fake.count()

	// A plain unpause that never went through the dispatcher.
	b.Resume("t2")
	time.Sleep(600 * time.Millisecond)
	total := fake.count()

	// Compared as a rate so the test does not depend on tick timing.
	perMs := float64(first) / 300.0
	expected := perMs * 900.0
	if float64(total) > expected*1.6 {
		t.Errorf("after a second Resume: %d polls in 900ms, one poller would give about %.0f - a second poller is running",
			total, expected)
	}
}
