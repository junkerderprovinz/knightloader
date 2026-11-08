package hoster

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// script answers each Unlock with the next of its answers, the last one for
// good.
type script struct {
	mu        sync.Mutex
	answers   []answer
	passwords []string
}

type answer struct {
	link Link
	err  error
}

func (s *script) Unlock(_ context.Context, _, password string) (Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.passwords = append(s.passwords, password)
	a := s.answers[0]
	if len(s.answers) > 1 {
		s.answers = s.answers[1:]
	}
	return a.link, a.err
}

type handoff struct {
	url     string
	headers map[string]string
	conns   int
	relink  func(context.Context) (string, error)
}

// fakeEngine records what the backend hands on.
type fakeEngine struct {
	mu      sync.Mutex
	got     chan handoff
	paused  []string
	removed []string
}

func newFakeEngine() *fakeEngine { return &fakeEngine{got: make(chan handoff, 4)} }

func (f *fakeEngine) HandoverHeaders(_, url string, headers map[string]string, conns int, relink func(context.Context) (string, error)) {
	f.got <- handoff{url, headers, conns, relink}
}

func (f *fakeEngine) Pause(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, id)
}

func (f *fakeEngine) Resume(string) {}

func (f *fakeEngine) Remove(id string, _ bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
}

// updates collects what the backend reports and the waits it asks for.
type updates struct {
	mu    sync.Mutex
	all   []core.Update
	waits []time.Duration
	// block makes every wait last until it is called off.
	block   bool
	waiting chan struct{}
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.all = append(u.all, up)
}

func (u *updates) sleep(ctx context.Context, d time.Duration) error {
	u.mu.Lock()
	u.waits = append(u.waits, d)
	block := u.block
	u.mu.Unlock()
	if !block {
		return ctx.Err()
	}
	u.waiting <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func (u *updates) list() []core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.all)
}

func (u *updates) waited() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	var sum time.Duration
	for _, w := range u.waits {
		sum += w
	}
	return sum
}

func newBackend(s *script) (*Backend, *fakeEngine, *updates) {
	eng := newFakeEngine()
	ups := &updates{waiting: make(chan struct{}, 4)}
	b := NewBackend("Gofile", s, eng, ups.add)
	b.sleep = ups.sleep
	return b, eng, ups
}

func awaitHandoff(t *testing.T, eng *fakeEngine) handoff {
	t.Helper()
	select {
	case h := <-eng.got:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was handed to the engine")
		return handoff{}
	}
}

// awaitFailure waits for the task to fail and returns that update.
func awaitFailure(t *testing.T, ups *updates) core.Update {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, u := range ups.list() {
			if u.Status == core.StatusError {
				return u
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the task never failed: %+v", ups.list())
	return core.Update{}
}

func TestTheLinkGoesToTheEngineWithItsHeadersAndTheHostersConnectionCap(t *testing.T) {
	s := &script{answers: []answer{{link: Link{URL: "https://store1.example/f", Headers: map[string]string{"Cookie": "accountToken=t"}, Conns: 3, Name: "a.mkv", Size: 42}}}}
	b, eng, ups := newBackend(s)
	b.Password = func(string) string { return "hunter2" }

	b.Download("t1", "https://gofile.io/d/abc", nil, 8)
	h := awaitHandoff(t, eng)
	if h.url != "https://store1.example/f" || h.headers["Cookie"] != "accountToken=t" || h.conns != 3 {
		t.Errorf("handed on %+v, want the link with its cookie on three connections", h)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool { return u.Name == "a.mkv" && u.Size == 42 }) {
		t.Errorf("the task never got the hoster's name and size: %+v", ups.list())
	}
	if s.passwords[0] != "hunter2" {
		t.Errorf("unlocked with password %q, want the task's", s.passwords[0])
	}
}

func TestFewerConnectionsAskedForThanTheHosterAllowsStayFewer(t *testing.T) {
	b, eng, _ := newBackend(&script{answers: []answer{{link: Link{URL: "u", Conns: 15}}}})
	b.Download("t1", "l", nil, 4)
	if h := awaitHandoff(t, eng); h.conns != 4 {
		t.Errorf("conns = %d, want the task's 4", h.conns)
	}
}

func TestAWaitShowsInTheNoteAndIsNoStallThenTheLinkIsHandedOn(t *testing.T) {
	s := &script{answers: []answer{
		{err: &WaitError{Wait: 95 * time.Second, Why: "Mediafire limits free downloads"}},
		{link: Link{URL: "https://download1.example/f"}},
	}}
	b, eng, ups := newBackend(s)
	ups.block = true

	b.Download("t1", "l", nil, 1)
	<-ups.waiting
	if !b.Waiting("t1") {
		t.Error("the task sitting out the wait does not say so")
	}
	if last := ups.list()[len(ups.list())-1]; last.Status != core.StatusRunning || last.Note != "Mediafire limits free downloads, next try in 95 s" {
		t.Errorf("last update %+v, want a running task saying why and how long", last)
	}

	ups.mu.Lock()
	ups.block = false
	ups.mu.Unlock()
	// Pause calls the blocked step off, and a fresh run then goes through.
	b.Pause("t1")
	b.Resume("t1")
	awaitHandoff(t, eng)
	if b.Waiting("t1") {
		t.Error("the task still counts as waiting after the link was handed on")
	}
}

func TestAWaitWithoutAReasonNamesTheHoster(t *testing.T) {
	b, _, _ := newBackend(nil)
	if got := b.note(&WaitError{Wait: 37 * time.Minute}, 37*time.Minute); got != "Waiting for Gofile: 37 min left" {
		t.Errorf("note = %q", got)
	}
}

func TestAHosterThatKeepsAskingForWaitsFailsWithTheWaitsReason(t *testing.T) {
	s := &script{answers: []answer{{err: &WaitError{Wait: time.Minute, Reason: core.ReasonLimit}}}}
	b, _, ups := newBackend(s)

	b.Download("t1", "l", nil, 1)
	u := awaitFailure(t, ups)
	if u.Reason != core.ReasonLimit {
		t.Errorf("reason = %q, want limit", u.Reason)
	}
	if got := ups.waited(); got != maxWaits*time.Minute {
		t.Errorf("waited %s, want %d rounds of a minute", got, maxWaits)
	}
}

func TestARefusalFailsTheTaskWithItsReason(t *testing.T) {
	b, eng, ups := newBackend(&script{answers: []answer{{err: Gone("pixeldrain")}}})
	b.Download("t1", "l", nil, 1)
	u := awaitFailure(t, ups)
	if u.Reason != core.ReasonGone || !strings.Contains(u.Err, "pixeldrain") {
		t.Errorf("failed with %+v, want gone, naming the hoster", u)
	}
	if len(eng.got) != 0 {
		t.Error("a refused download was handed on")
	}
}

func TestAnUnclassifiedErrorFailsWithoutAReason(t *testing.T) {
	b, _, ups := newBackend(&script{answers: []answer{{err: errors.New("pixeldrain: the server answered 500")}}})
	b.Download("t1", "l", nil, 1)
	if u := awaitFailure(t, ups); u.Reason != core.ReasonUnknown {
		t.Errorf("reason = %q, want none", u.Reason)
	}
}

func TestRelinkAsksTheHosterAgainAndSitsOutAWaitQuietly(t *testing.T) {
	s := &script{answers: []answer{
		{link: Link{URL: "https://first"}},
		{err: &WaitError{Wait: 30 * time.Second}},
		{link: Link{URL: "https://second"}},
	}}
	b, eng, ups := newBackend(s)
	b.Download("t1", "l", nil, 1)
	h := awaitHandoff(t, eng)
	notes := len(ups.list())

	got, err := h.relink(context.Background())
	if err != nil || got != "https://second" {
		t.Errorf("relink = %q, %v; want the fresh link", got, err)
	}
	if ups.waited() != 30*time.Second {
		t.Errorf("waited %s, want the 30 s the hoster asked for", ups.waited())
	}
	if len(ups.list()) != notes {
		t.Errorf("relinking wrote to the task: %+v", ups.list()[notes:])
	}
}

func TestPausingOrRemovingDuringAWaitCallsItOff(t *testing.T) {
	s := &script{answers: []answer{{err: &WaitError{Wait: time.Hour}}}}
	b, eng, ups := newBackend(s)
	ups.block = true

	b.Download("t1", "l", nil, 1)
	<-ups.waiting
	b.Pause("t1")
	if last := ups.list()[len(ups.list())-1]; last.Status != core.StatusPaused {
		t.Errorf("last update %+v, want paused", last)
	}

	b.Resume("t1")
	<-ups.waiting
	b.Remove("t1", true)
	time.Sleep(20 * time.Millisecond)
	if len(eng.got) != 0 || len(eng.removed) != 0 {
		t.Error("a task removed during its wait reached the engine")
	}
}

func TestPauseAndRemoveReachTheEngineOnceTheLinkIsHandedOn(t *testing.T) {
	b, eng, _ := newBackend(&script{answers: []answer{{link: Link{URL: "u"}}}})
	b.Download("t1", "l", nil, 1)
	awaitHandoff(t, eng)

	b.Pause("t1")
	b.Remove("t1", true)
	if !slices.Equal(eng.paused, []string{"t1"}) || !slices.Equal(eng.removed, []string{"t1"}) {
		t.Errorf("paused %v, removed %v; want both passed to the engine", eng.paused, eng.removed)
	}
}

func TestWaitsAreWrittenAsPeopleReadThem(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Second:               "45 s",
		119 * time.Second:              "119 s",
		37 * time.Minute:               "37 min",
		time.Hour:                      "1 h",
		time.Hour + 4*time.Minute + 30: "1 h 5 min",
	} {
		if got := Span(d); got != want {
			t.Errorf("Span(%s) = %q, want %q", d, got, want)
		}
	}
}
