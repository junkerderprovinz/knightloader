package jd

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJDOfflineCheck is a JD whose link check has judged the one link of task
// t1, and which keeps the link in its grabber instead of confirming it.
type fakeJDOfflineCheck struct {
	availability string

	mu      sync.Mutex
	added   bool
	removed bool
}

func (f *fakeJDOfflineCheck) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/linkgrabberv2/addLinks":
			f.added = true
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		case "/linkgrabberv2/queryPackages":
			if !f.added || f.removed {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":7,"name":"KL-t1"},{"uuid":8,"name":"his own package"}]}`))
		case "/linkgrabberv2/queryLinks":
			_, _ = w.Write([]byte(`{"data":[{"uuid":70,"url":"https://rapidgator.example/file/aaa","packageUUID":7,"availability":"` + f.availability + `"}]}`))
		case "/linkgrabberv2/removeLinks":
			f.removed = true
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/downloadsV2/queryPackages":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	})
}

// A link JD found offline stays in its grabber and never becomes a download,
// so the task fails with JD's verdict instead of waiting out the appear limit.
func TestALinkJDFoundOfflineFailsAtOnce(t *testing.T) {
	f := &fakeJDOfflineCheck{availability: "OFFLINE"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	got := make(chan core.Update, 8)
	b := NewBackend(srv.URL, func(_ string, u core.Update) { got <- u })

	b.Download("t1", "https://rapidgator.example/file/aaa", nil, 0)
	defer b.Remove("t1", false)

	select {
	case u := <-got:
		if u.Status != core.StatusError || u.Reason != core.ReasonGone {
			t.Fatalf("update %+v, want a failure saying the file is gone", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the offline link was not reported")
	}
	f.mu.Lock()
	removed := f.removed
	f.mu.Unlock()
	if !removed {
		t.Error("the offline link was left in JD's grabber, where it would hold back the next attempt")
	}
}

// A link JD has not judged yet, or found online, is waiting for the confirm and
// is left alone.
func TestALinkJDHasNotFoundOfflineKeepsWaiting(t *testing.T) {
	for _, availability := range []string{"UNKNOWN", "ONLINE"} {
		t.Run(availability, func(t *testing.T) {
			f := &fakeJDOfflineCheck{availability: availability}
			srv := httptest.NewServer(f.handler())
			defer srv.Close()
			got := make(chan core.Update, 8)
			b := NewBackend(srv.URL, func(_ string, u core.Update) { got <- u })

			b.Download("t1", "https://rapidgator.example/file/aaa", nil, 0)
			defer b.Remove("t1", false)

			select {
			case u := <-got:
				t.Fatalf("update %+v for a link JD has not found offline", u)
			case <-time.After(2 * time.Second):
			}
		})
	}
}
