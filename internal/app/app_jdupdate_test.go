package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func shortJDUpdateWait(t *testing.T) {
	t.Helper()
	oldWait, oldPoll := jdUpdateWait, jdUpdatePoll
	jdUpdateWait, jdUpdatePoll = 200*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { jdUpdateWait, jdUpdatePoll = oldWait, oldPoll })
}

// updatingJD is a JD whose update check finds an update after readyAfter
// questions, or never when readyAfter is zero.
func updatingJD(t *testing.T, readyAfter int32) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/help", "/update/runUpdateCheck":
			_, _ = w.Write([]byte(`{"data":""}`))
		case "/update/isUpdateAvailable":
			n := asked.Add(1)
			if readyAfter > 0 && n >= readyAfter {
				_, _ = w.Write([]byte(`{"data":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KL_JD", srv.URL)
	return &asked
}

func TestCheckJDUpdateFindsNoUpdateOnceTheWaitIsOver(t *testing.T) {
	shortJDUpdateWait(t)
	asked := updatingJD(t, 0)
	start := time.Now()
	available, err := (&App{}).CheckJDUpdate(context.Background())
	if err != nil || available {
		t.Fatalf("CheckJDUpdate = %v, %v; want no update and no error", available, err)
	}
	if time.Since(start) < jdUpdateWait {
		t.Errorf("answered after %s, before JD had %s to finish its check", time.Since(start), jdUpdateWait)
	}
	if asked.Load() < 2 {
		t.Errorf("asked JD %d times, want it asked again while the check runs", asked.Load())
	}
}

// JD answers runUpdateCheck before the check is done, so the first question
// can come too early.
func TestCheckJDUpdateWaitsForTheCheckToFindTheUpdate(t *testing.T) {
	shortJDUpdateWait(t)
	asked := updatingJD(t, 3)
	available, err := (&App{}).CheckJDUpdate(context.Background())
	if err != nil || !available {
		t.Fatalf("CheckJDUpdate = %v, %v; want the update JD found on its third answer", available, err)
	}
	if asked.Load() != 3 {
		t.Errorf("asked JD %d times, want to stop at the answer that found it", asked.Load())
	}
}

func TestCheckJDUpdateWithoutJDIsUnreachable(t *testing.T) {
	t.Setenv("KL_JD", "")
	if _, err := (&App{}).CheckJDUpdate(context.Background()); !errors.Is(err, ErrJDUnreachable) {
		t.Errorf("CheckJDUpdate = %v, want ErrJDUnreachable", err)
	}
	if err := (&App{}).StartJDUpdate(); !errors.Is(err, ErrJDUnreachable) {
		t.Errorf("StartJDUpdate = %v, want ErrJDUnreachable", err)
	}
}
