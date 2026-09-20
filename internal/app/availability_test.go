package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// Only 404 and 410 mean the file is gone; other errors are the host declining
// to answer, which the "remove offline links" button must not confuse with it.
func TestAStatusCodeIsNotAVerdict(t *testing.T) {
	cases := map[int]core.Availability{
		200: core.AvailOnline,
		206: core.AvailOnline,
		302: core.AvailOnline,
		400: core.AvailUncheckable,
		401: core.AvailUncheckable,
		403: core.AvailUncheckable, // a hoster that will not be probed
		404: core.AvailOffline,
		405: core.AvailUncheckable, // a host with no HEAD at all
		410: core.AvailOffline,
		429: core.AvailUncheckable,
		500: core.AvailUncheckable,
		503: core.AvailUncheckable,
	}
	for status, want := range cases {
		if got := availabilityFor(status); got != want {
			t.Errorf("HTTP %d = %q, want %q", status, got, want)
		}
	}
}

// A probe that cannot reach the host (DNS failure, reset, no network) learns
// nothing about the link and must not mark it offline.
func TestAFlakyMinuteDoesNotKillALiveLink(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.Probe = probeFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: lookup host.example: no such host")
	})
	task := putTask(t, a, core.Task{
		URL: "https://host.example/live.mkv", Name: "live.mkv",
		Status: core.StatusCollected, Enabled: true,
	})

	a.RecheckTasks([]string{task.ID})

	live := snapshot(t, a, task.ID)
	if live.Online != core.AvailUncheckable {
		t.Errorf("availability = %q, want uncheckable; nothing was reached, so nothing was learned", live.Online)
	}
	if live.Reason != core.ReasonNetwork {
		t.Errorf("reason = %q, want network", live.Reason)
	}
	// No error text: the error column is for downloads that failed.
	if live.Error != "" {
		t.Errorf("error = %q, want nothing at all on a link that was never asked about", live.Error)
	}
}

// A 404 still reads as offline.
func TestAMissingFileIsStillOffline(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		return probeAnswer(req, http.StatusNotFound), nil
	})
	task := putTask(t, a, core.Task{
		URL: "https://host.example/gone.mkv", Name: "gone.mkv",
		Status: core.StatusCollected, Enabled: true,
	})

	a.RecheckTasks([]string{task.ID})

	live := snapshot(t, a, task.ID)
	if live.Online != core.AvailOffline {
		t.Errorf("availability = %q, want offline", live.Online)
	}
	if live.Reason != core.ReasonGone {
		t.Errorf("reason = %q, want gone", live.Reason)
	}
	if !strings.Contains(live.Error, "404") {
		t.Errorf("error = %q, want the status the host actually sent", live.Error)
	}
}

// Services rate-limit by account or address, so a backend is asked once for
// all its links; that is why resolver.Checker takes a slice.
func TestABackendIsAskedOnceForTheWholeBatch(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	svc := &batchResolver{verdicts: map[string]core.Availability{
		"https://batch.example/live.bin": core.AvailOnline,
		"https://batch.example/dead.bin": core.AvailOffline,
		"https://batch.example/shy.bin":  core.AvailUncheckable,
	}}
	a.Registry.Register(svc)

	ids := map[string]string{}
	for url := range svc.verdicts {
		ids[url] = putTask(t, a, core.Task{
			URL: url, Name: url, Status: core.StatusCollected, Enabled: true,
		}).ID
	}

	a.RecheckTasks(nil)

	if len(svc.batches) != 1 {
		t.Fatalf("the backend was asked %d times, want once for the lot", len(svc.batches))
	}
	if got := svc.batches[0]; got != len(svc.verdicts) {
		t.Errorf("the one call carried %d links, want all %d", got, len(svc.verdicts))
	}
	for url, want := range svc.verdicts {
		live := snapshot(t, a, ids[url])
		if live.Online != want {
			t.Errorf("%s = %q, want %q", url, live.Online, want)
		}
	}
	// Offline and uncheckable call for different actions.
	dead := snapshot(t, a, ids["https://batch.example/dead.bin"])
	if dead.Reason != core.ReasonGone {
		t.Errorf("a link the service called dead reads reason %q, want gone", dead.Reason)
	}
	// The verdict names the backend, which is different evidence from our own
	// HEAD.
	if !strings.Contains(dead.Error, "batchtest") {
		t.Errorf("error = %q, want the backend that gave the verdict named", dead.Error)
	}
	if live := snapshot(t, a, ids["https://batch.example/shy.bin"]); live.Error != "" {
		t.Errorf("an uncheckable link reads %q, want no sentence at all", live.Error)
	}
}

// A backend without a checker reports uncheckable after a recheck, not
// unknown, which would look as if Check did nothing.
func TestABackendThatCannotCheckSaysSo(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.Registry.Register(plainResolver{})
	task := putTask(t, a, core.Task{
		URL: "https://plain.example/movie.bin", Name: "movie.bin",
		Status: core.StatusCollected, Enabled: true,
		// A stale verdict the recheck must replace.
		Online: core.AvailOffline, Error: "offline (HTTP 404)", Reason: core.ReasonGone,
	})

	a.RecheckTasks([]string{task.ID})

	live := snapshot(t, a, task.ID)
	if live.Online != core.AvailUncheckable {
		t.Errorf("availability = %q, want uncheckable", live.Online)
	}
	if live.Error != "" {
		t.Errorf("error = %q, want the stale one cleared", live.Error)
	}
}

// Non-direct resolvers answer with the URL as a placeholder name, which must
// not replace a real name on a recheck.
func TestRecheckDoesNotThrowAwayARealName(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.Registry.Register(plainResolver{})
	task := putTask(t, a, core.Task{
		URL: "https://plain.example/movie.bin", Name: "A Real Movie Title.mkv",
		Status: core.StatusCollected, Enabled: true,
	})

	a.RecheckTasks([]string{task.ID})

	if live := snapshot(t, a, task.ID); live.Name != "A Real Movie Title.mkv" {
		t.Errorf("name = %q, want the real name preserved instead of the resolver's own placeholder", live.Name)
	}
}

// A refused key must leave the batch uncheckable, not mark every link
// offline.
func TestARefusedKeyIsNotAPileOfDeadLinks(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	svc := &batchResolver{err: errors.New("alldebrid: the auth apikey is invalid (AUTH_BAD_APIKEY)")}
	a.Registry.Register(svc)
	var ids []string
	for _, url := range []string{"https://batch.example/a.bin", "https://batch.example/b.bin"} {
		ids = append(ids, putTask(t, a, core.Task{
			URL: url, Name: url, Status: core.StatusCollected, Enabled: true,
		}).ID)
	}

	a.RecheckTasks(nil)

	for _, id := range ids {
		if live := snapshot(t, a, id); live.Online != core.AvailUncheckable {
			t.Errorf("availability = %q, want uncheckable; the key was refused, the links were never asked about", live.Online)
		}
	}
}

// A short answer read back by position must not put one link's verdict on
// another.
func TestAShortAnswerDoesNotSlideOntoTheWrongLink(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	svc := &batchResolver{short: 1, verdicts: map[string]core.Availability{
		"https://batch.example/a.bin": core.AvailOffline,
		"https://batch.example/b.bin": core.AvailOffline,
	}}
	a.Registry.Register(svc)
	var ids []string
	for _, url := range []string{"https://batch.example/a.bin", "https://batch.example/b.bin"} {
		ids = append(ids, putTask(t, a, core.Task{
			URL: url, Name: url, Status: core.StatusCollected, Enabled: true,
		}).ID)
	}

	a.RecheckTasks(nil)

	var offline, uncheckable int
	for _, id := range ids {
		switch snapshot(t, a, id).Online {
		case core.AvailOffline:
			offline++
		case core.AvailUncheckable:
			uncheckable++
		}
	}
	if offline != 1 || uncheckable != 1 {
		t.Errorf("%d offline and %d uncheckable, want one of each: the answer covered one link", offline, uncheckable)
	}
}

// snapshot copies a task under the lock, since the app keeps writing to the
// live one.
func snapshot(t *testing.T, a *App, id string) core.Task {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	live := a.tasks[id]
	if live == nil {
		t.Fatalf("task %s is gone", id)
	}
	return *live
}

// batchResolver is a backend that can check, and records how it was asked.
type batchResolver struct {
	verdicts map[string]core.Availability
	err      error
	// short drops that many verdicts off the end, like a service that skipped
	// a link it did not recognise.
	short int

	mu      sync.Mutex
	batches []int
}

func (*batchResolver) Info() resolver.Info { return resolver.Info{ID: "batchtest", Prio: 90} }

func (*batchResolver) Match(raw string) bool { return strings.Contains(raw, "batch.example") }

func (*batchResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

func (r *batchResolver) Check(_ context.Context, urls []string) ([]core.Availability, error) {
	r.mu.Lock()
	r.batches = append(r.batches, len(urls))
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	out := make([]core.Availability, 0, len(urls))
	for _, u := range urls {
		out = append(out, r.verdicts[u])
	}
	return out[:max(0, len(out)-r.short)], nil
}

// plainResolver claims links but cannot check them, like every backend that
// only learns by starting a download.
type plainResolver struct{}

func (plainResolver) Info() resolver.Info { return resolver.Info{ID: "plaintest", Prio: 90} }

func (plainResolver) Match(raw string) bool { return strings.Contains(raw, "plain.example") }

func (plainResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}
