package torbox

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// heldTorBox answers TorBox's API in-process. A call to hold is held until
// the test lets it go, and then answered even though it was cancelled
// meanwhile when answered is set, the way an answer already on the wire
// outlives a cancel.
type heldTorBox struct {
	hold     string
	answered bool
	held     chan chan struct{}
}

func (h *heldTorBox) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == h.hold {
		release := make(chan struct{})
		h.held <- release
		<-release
		if err := r.Context().Err(); err != nil && !h.answered {
			return nil, err
		}
	}
	w := httptest.NewRecorder()
	switch r.URL.Path {
	case "/api/webdl/createwebdownload":
		writeEnv(w, `{"webdownload_id":42}`)
	case "/api/webdl/mylist":
		writeEnv(w, `{"id":42,"name":"movie.mkv","size":1000,"download_present":true,"files":[{"id":7,"name":"movie.mkv","size":1000}]}`)
	case "/api/webdl/requestdl":
		writeEnv(w, `"https://cdn.torbox.app/dl/movie.mkv"`)
	default:
		writeEnv(w, `null`)
	}
	return w.Result(), nil
}

func heldClient(h *heldTorBox) *Client {
	c := NewClient("test-key")
	c.base = "https://api.torbox.test"
	c.hc = &http.Client{Transport: h}
	return c
}

// statuses keeps every update a backend reported.
type statuses struct {
	mu   sync.Mutex
	list []core.Update
}

func (s *statuses) add(_ string, u core.Update) {
	s.mu.Lock()
	s.list = append(s.list, u)
	s.mu.Unlock()
}

func (s *statuses) failures() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, u := range s.list {
		if u.Status == core.StatusError {
			out = append(out, u.Err)
		}
	}
	return out
}

// noHandover waits long enough for a handover that should not come.
func noHandover(t *testing.T, eng *fakeEngine) {
	t.Helper()
	select {
	case url := <-eng.got:
		t.Fatalf("a stopped task was handed to the engine: %s", url)
	case <-time.After(300 * time.Millisecond):
	}
}

// A pause or a removal that comes while TorBox is being asked for the link
// stops the asking. That is the task's own doing and not a failure of the
// link, so nothing reports one, and nothing is handed to the engine.
func TestStoppingAnUnlockUnderWayIsNotAFailure(t *testing.T) {
	for _, call := range []string{"/api/webdl/createwebdownload", "/api/webdl/requestdl"} {
		for _, stop := range []string{"pause", "remove"} {
			t.Run(stop+" during "+call, func(t *testing.T) {
				h := &heldTorBox{hold: call, held: make(chan chan struct{}, 1)}
				got := &statuses{}
				eng := &fakeEngine{got: make(chan string, 1)}
				b := NewBackend(heldClient(h), eng, got.add)
				b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)

				release := <-h.held
				if stop == "pause" {
					b.Pause("t1")
				} else {
					b.Remove("t1", false)
				}
				close(release)

				noHandover(t, eng)
				if f := got.failures(); len(f) > 0 {
					t.Errorf("the %s was reported as a failure: %v", stop, f)
				}
			})
		}
	}
}

// A pause that comes as TorBox hands out the download link keeps the task out
// of the engine.
func TestAPauseAsTheLinkArrivesKeepsTheTaskOutOfTheEngine(t *testing.T) {
	h := &heldTorBox{hold: "/api/webdl/requestdl", answered: true, held: make(chan chan struct{}, 1)}
	eng := &fakeEngine{got: make(chan string, 1)}
	b := NewBackend(heldClient(h), eng, func(string, core.Update) {})
	b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)

	release := <-h.held
	b.Pause("t1")
	close(release)

	noHandover(t, eng)
}

// An unlock paused and resumed before the first one has wound down leaves the
// resumed one where the next pause reaches it.
func TestAResumedTorBoxUnlockCanBePausedAgain(t *testing.T) {
	h := &heldTorBox{hold: "/api/webdl/createwebdownload", held: make(chan chan struct{}, 2)}
	eng := &fakeEngine{got: make(chan string, 1)}
	b := NewBackend(heldClient(h), eng, func(string, core.Update) {})
	b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)

	first := <-h.held
	b.Pause("t1")
	b.Resume("t1")
	second := <-h.held
	close(first)
	// The first unlock winds down now that the second has started.
	time.Sleep(100 * time.Millisecond)
	b.Pause("t1")
	close(second)

	noHandover(t, eng)
}
