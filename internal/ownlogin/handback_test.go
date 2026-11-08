package ownlogin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

var (
	errUnreadable = errors.New("hoster: the answer does not read")
	errOutage     = errors.New("hoster: the API is having trouble")
	errGone       = errors.New("hoster: the file is gone")
	errNone       = errors.New("hoster: no login is stored")
)

// reasonOf is a hoster's reading of the errors above.
func reasonOf(err error) core.Reason {
	var refused *LoginError
	switch {
	case errors.Is(err, errOutage):
		return core.ReasonUnavailable
	case errors.Is(err, errGone):
		return core.ReasonGone
	case errors.As(err, &refused), errors.Is(err, errNone):
		return core.ReasonAuth
	}
	return core.ReasonUnknown
}

// signer signs in with whatever answer() returns at the time.
type signer struct {
	mu  sync.Mutex
	err error
}

func (s *signer) set(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *signer) login(context.Context, string, string) (string, Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", Account{}, s.err
	}
	return "", Account{Premium: true}, nil
}

func newTestLogin(s *signer) (*Login, *atomic.Int32) {
	l := NewLogin(Signer{Login: s.login, Reason: reasonOf, None: errNone})
	moved := new(atomic.Int32)
	l.Moved = func() { moved.Add(1) }
	l.SetLogin("john@example.com", "hunter2")
	return l, moved
}

func TestASignInAnsweredInWordsTheClientCannotReadHandsTheLoginToJDownloader(t *testing.T) {
	l, moved := newTestLogin(&signer{err: errUnreadable})
	st := l.Check(context.Background())
	if !st.Handed || l.Ready() {
		t.Errorf("status = %+v, ready = %v, want the login handed to JDownloader and out of routing", st, l.Ready())
	}
	if moved.Load() != 1 {
		t.Errorf("Moved was called %d times, want once", moved.Load())
	}
}

func TestAWrongPasswordStaysARefusalAndIsNotHandedOver(t *testing.T) {
	l, moved := newTestLogin(&signer{err: &LoginError{Msg: "hoster: check the email address and password"}})
	st := l.Check(context.Background())
	if st.Handed || !st.Refused || moved.Load() != 0 {
		t.Errorf("status = %+v after %d moves, want a refusal KnightLoader keeps", st, moved.Load())
	}
}

func TestAHosterOutOfReachKeepsTheLoginWithKnightLoader(t *testing.T) {
	down := &url.Error{Op: "Post", URL: "https://hoster.example/api", Err: errors.New("connection refused")}
	l, _ := newTestLogin(&signer{err: down})
	if st := l.Check(context.Background()); st.Handed || !l.Ready() {
		t.Errorf("status = %+v, want the login kept for when the hoster is back", st)
	}
}

func TestALoginThatWorksAgainOnACheckComesBackToKnightLoader(t *testing.T) {
	s := &signer{err: errUnreadable}
	l, moved := newTestLogin(s)
	l.Check(context.Background())
	s.set(nil)
	if st := l.Check(context.Background()); st.Handed || !st.Premium || !l.Ready() {
		t.Errorf("status = %+v, want the login back with KnightLoader", st)
	}
	if moved.Load() != 2 {
		t.Errorf("Moved was called %d times, want twice, there and back", moved.Load())
	}
}

func TestACheckThatStillFailsLeavesTheLoginWithJDownloader(t *testing.T) {
	l, moved := newTestLogin(&signer{err: errUnreadable})
	l.Check(context.Background())
	if st := l.Check(context.Background()); !st.Handed || moved.Load() != 1 {
		t.Errorf("status = %+v after %d moves, want the login still with JDownloader", st, moved.Load())
	}
}

func TestAnotherLoginStartsWithKnightLoader(t *testing.T) {
	l, _ := newTestLogin(&signer{err: errUnreadable})
	l.Check(context.Background())
	l.SetLogin("jane@example.com", "hunter3")
	if l.Status().Handed || !l.Ready() {
		t.Errorf("status = %+v, want a fresh login with KnightLoader", l.Status())
	}
}

// recorder keeps the updates of a run.
type recorder struct {
	mu  sync.Mutex
	ups []core.Update
	end chan core.Update
}

func newRecorder() *recorder { return &recorder{end: make(chan core.Update, 8)} }

func (r *recorder) add(_ string, u core.Update) {
	r.mu.Lock()
	r.ups = append(r.ups, u)
	r.mu.Unlock()
	if u.Status == core.StatusError {
		r.end <- u
	}
}

func (r *recorder) failure(t *testing.T) core.Update {
	t.Helper()
	select {
	case u := <-r.end:
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("the download never ended")
		return core.Update{}
	}
}

// handEngine hears every link handed to it.
type handEngine struct {
	idleEngine
	got chan string
}

func (e handEngine) Handover(taskID, _ string, _ int, _ func(context.Context) (string, error)) {
	e.got <- taskID
}

// failingTasks runs downloads on l whose unlock fails with whatever next
// returns, counting the unlocks.
func failingTasks(l *Login, rec *recorder, eng Downloader, next func() error) (*Tasks, *atomic.Int32) {
	unlocks := new(atomic.Int32)
	tasks := NewTasks(l, eng, rec.add, Config{
		Name: "Hoster",
		Unlock: func(context.Context, string, string, func(time.Duration) error) (string, int64, error) {
			unlocks.Add(1)
			if err := next(); err != nil {
				return "", 0, err
			}
			return "https://dl.hoster.example/file", 1 << 30, nil
		},
	})
	return tasks, unlocks
}

func TestADownloadTheClientCannotReadGoesToTheNextBackendAndTheLoginWithIt(t *testing.T) {
	l, moved := newTestLogin(&signer{})
	rec := newRecorder()
	tasks, _ := failingTasks(l, rec, idleEngine{}, func() error { return errors.New("hoster: the download was answered without a link") })
	t.Cleanup(func() { tasks.Remove("t1", false) })

	tasks.Download("t1", "https://hoster.example/file/1", nil, 4)
	u := rec.failure(t)
	if !u.Unsupported || u.Takeover != "Hoster" {
		t.Errorf("update = %+v, want the link handed on with the takeover named", u)
	}
	if !l.Status().Handed || moved.Load() != 1 {
		t.Errorf("status = %+v after %d moves, want the login with JDownloader", l.Status(), moved.Load())
	}
}

func TestADownloadStartingAfterTheHandoverGoesOnAtOnce(t *testing.T) {
	l, _ := newTestLogin(&signer{err: errUnreadable})
	l.Check(context.Background())
	rec := newRecorder()
	tasks, unlocks := failingTasks(l, rec, idleEngine{}, func() error { return nil })
	t.Cleanup(func() { tasks.Remove("t1", false) })

	tasks.Download("t1", "https://hoster.example/file/1", nil, 4)
	if u := rec.failure(t); !u.Unsupported || u.Takeover != "Hoster" {
		t.Errorf("update = %+v, want the link handed on", u)
	}
	if unlocks.Load() != 0 {
		t.Errorf("the hoster was asked %d times for a login it no longer has", unlocks.Load())
	}
}

func TestOutagesTheHosterDoesNotExplainHandTheLoginOverOnlyWhenTheyRepeat(t *testing.T) {
	l, _ := newTestLogin(&signer{})
	rec := newRecorder()
	eng := handEngine{got: make(chan string, 1)}
	// The success in the middle starts the count again.
	answers := []error{errOutage, errOutage, nil, errOutage, errOutage, errOutage}
	var mu sync.Mutex
	tasks, _ := failingTasks(l, rec, eng, func() error {
		mu.Lock()
		defer mu.Unlock()
		err := answers[0]
		answers = answers[1:]
		return err
	})

	script, last := slices.Clone(answers), len(answers)-1
	for i, ans := range script {
		id := fmt.Sprintf("t%d", i)
		t.Cleanup(func() { tasks.Remove(id, false) })
		tasks.Download(id, "https://hoster.example/file/1", nil, 4)
		if ans == nil {
			<-eng.got
			continue
		}
		if u := rec.failure(t); u.Unsupported != (i == last) {
			t.Fatalf("outage in download %d: handed on = %v", i+1, u.Unsupported)
		}
	}
	if !l.Status().Handed {
		t.Error("the login stayed with KnightLoader after three outages in a row")
	}
}

func TestAFileGoneSaysNothingAgainstTheClient(t *testing.T) {
	l, _ := newTestLogin(&signer{})
	rec := newRecorder()
	tasks, _ := failingTasks(l, rec, idleEngine{}, func() error { return errGone })
	t.Cleanup(func() { tasks.Remove("t1", false) })

	for range 4 {
		tasks.Download("t1", "https://hoster.example/file/1", nil, 4)
		if u := rec.failure(t); u.Unsupported || u.Reason != core.ReasonGone {
			t.Fatalf("update = %+v, want the file reported gone", u)
		}
	}
	if l.Status().Handed {
		t.Error("a file that is gone handed the login over")
	}
}
