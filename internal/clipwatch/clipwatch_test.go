package clipwatch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func ids(list []Watcher) string {
	out := make([]string, len(list))
	for i, w := range list {
		out[i] = w.ID
	}
	return strings.Join(out, ",")
}

func TestAWatcherIsListedWhileItsLeaseRuns(t *testing.T) {
	r := New()
	if _, err := r.Renew(Watcher{ID: "a", Name: "Firefox, Windows", Kind: KindExtension}, t0); err != nil {
		t.Fatal(err)
	}
	if got := ids(r.List(t0.Add(Lease - time.Second))); got != "a" {
		t.Fatalf("within the lease the list is %q, want a", got)
	}
	if got := ids(r.List(t0.Add(Lease))); got != "" {
		t.Fatalf("after the lease the list is %q, want it empty", got)
	}
}

func TestRenewingKeepsAWatcherListed(t *testing.T) {
	r := New()
	w := Watcher{ID: "a", Kind: KindWeb}
	for i := 0; i < 5; i++ {
		if _, err := r.Renew(w, t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if got := ids(r.List(t0.Add(6 * time.Minute))); got != "a" {
		t.Fatalf("a watcher renewing every minute is %q, want listed", got)
	}
}

func TestAStoppedWatcherLearnsItOnceAndLeavesTheList(t *testing.T) {
	r := New()
	w := Watcher{ID: "a", Kind: KindDesktop}
	if _, err := r.Renew(w, t0); err != nil {
		t.Fatal(err)
	}
	if !r.Stop("a", t0) {
		t.Fatal("Stop did not find the watcher it holds")
	}
	if got := ids(r.List(t0)); got != "" {
		t.Fatalf("a stopped watcher is still listed: %q", got)
	}
	stop, err := r.Renew(w, t0.Add(time.Minute))
	if err != nil || !stop {
		t.Fatalf("the renewal after a stop = %v, %v, want stop", stop, err)
	}
	if got := ids(r.List(t0.Add(time.Minute))); got != "" {
		t.Fatalf("the renewal that learnt of the stop listed the watcher again: %q", got)
	}
	stop, _ = r.Renew(w, t0.Add(2*time.Minute))
	if stop {
		t.Fatal("a watcher switched on again is told to stop a second time")
	}
	if got := ids(r.List(t0.Add(2 * time.Minute))); got != "a" {
		t.Fatalf("a watcher switched on again is %q, want listed", got)
	}
}

func TestStoppingAnUnknownWatcherReportsIt(t *testing.T) {
	r := New()
	if r.Stop("nobody", t0) {
		t.Fatal("Stop claims to hold a watcher that never registered")
	}
	stop, _ := r.Renew(Watcher{ID: "nobody", Kind: KindWeb}, t0)
	if stop {
		t.Fatal("a stop for a watcher this registry did not hold reached it later")
	}
}

func TestLeavingDropsAWatcherAndAnyPendingStop(t *testing.T) {
	r := New()
	w := Watcher{ID: "a", Kind: KindWeb}
	_, _ = r.Renew(w, t0)
	r.Stop("a", t0)
	r.Leave("a")
	if stop, _ := r.Renew(w, t0.Add(time.Second)); stop {
		t.Fatal("a watcher that left and came back is told to stop")
	}
}

func TestPausingDropsAWatcherButKeepsAPendingStop(t *testing.T) {
	r := New()
	w := Watcher{ID: "a", Kind: KindWeb}
	_, _ = r.Renew(w, t0)
	r.Pause("a")
	if got := ids(r.List(t0)); got != "" {
		t.Fatalf("a paused watcher is still listed: %q", got)
	}

	_, _ = r.Renew(w, t0.Add(time.Second))
	r.Stop("a", t0.Add(time.Second))
	r.Pause("a")
	if stop, _ := r.Renew(w, t0.Add(2*time.Second)); !stop {
		t.Fatal("a watcher that paused and came back did not learn of the stop asked of it")
	}
}

func TestTheListIsBoundedAndDropsTheStalest(t *testing.T) {
	r := New()
	for i := 0; i < maxWatchers; i++ {
		_, _ = r.Renew(Watcher{ID: fmt.Sprintf("w%02d", i), Kind: KindWeb}, t0.Add(time.Duration(i)*time.Second))
	}
	_, _ = r.Renew(Watcher{ID: "new", Kind: KindWeb}, t0.Add(time.Minute))
	list := r.List(t0.Add(time.Minute))
	if len(list) != maxWatchers {
		t.Fatalf("%d watchers listed, want the cap of %d", len(list), maxWatchers)
	}
	for _, w := range list {
		if w.ID == "w00" {
			t.Fatal("the watcher seen longest ago was kept while a new one came in")
		}
	}
}

func TestRenewRefusesWhatCannotBeAWatcher(t *testing.T) {
	r := New()
	for _, w := range []Watcher{
		{ID: "", Kind: KindWeb},
		{ID: strings.Repeat("x", maxIDBytes+1), Kind: KindWeb},
		{ID: "a", Kind: "phone"},
	} {
		if _, err := r.Renew(w, t0); !errors.Is(err, ErrInvalid) {
			t.Errorf("Renew(%q, %q) = %v, want ErrInvalid", w.ID, w.Kind, err)
		}
	}
}

func TestALongNameIsCutAtACharacter(t *testing.T) {
	r := New()
	_, _ = r.Renew(Watcher{ID: "a", Kind: KindWeb, Name: strings.Repeat("ä", maxNameBytes)}, t0)
	name := r.List(t0)[0].Name
	if len(name) > maxNameBytes || !strings.HasPrefix(strings.Repeat("ä", maxNameBytes), name) {
		t.Fatalf("name cut to %d bytes as %q", len(name), name)
	}
}

func TestAWatcherCannotNameTheInstanceItself(t *testing.T) {
	r := New()
	_, _ = r.Renew(Watcher{ID: "a", Kind: KindWeb, Instance: "somewhere"}, t0)
	if got := r.List(t0)[0].Instance; got != "" {
		t.Fatalf("the registry kept instance %q from the watcher", got)
	}
}

func TestAStopOutlivesARestartOfTheInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clipwatch.json")
	w := Watcher{ID: "a", Kind: KindExtension}
	r := Open(path, t0)
	_, _ = r.Renew(w, t0)
	if !r.Stop("a", t0) {
		t.Fatal("Stop did not find the watcher it holds")
	}

	// The instance was down for longer than a lease, so the watcher could not
	// renew in between.
	back := t0.Add(10 * time.Minute)
	restarted := Open(path, back)
	if stop, _ := restarted.Renew(w, back.Add(time.Minute)); !stop {
		t.Fatal("the first renewal after the restart did not learn of the stop")
	}
	if stop, _ := Open(path, back.Add(2*time.Minute)).Renew(w, back.Add(2*time.Minute)); stop {
		t.Fatal("a stop the watcher already learnt came back after another restart")
	}
}

func TestAKeptStopRunsOutALeaseAfterTheRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clipwatch.json")
	w := Watcher{ID: "a", Kind: KindWeb}
	r := Open(path, t0)
	_, _ = r.Renew(w, t0)
	r.Stop("a", t0)

	back := t0.Add(time.Hour)
	if stop, _ := Open(path, back).Renew(w, back.Add(Lease)); stop {
		t.Fatal("a stop nobody came for in a whole lease after the restart still reached the watcher")
	}
}

func TestAStopThatRanOutBeforeTheInstanceClosedIsNotKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clipwatch.json")
	w := Watcher{ID: "a", Kind: KindExtension}
	r := Open(path, t0)
	_, _ = r.Renew(w, t0)
	r.Stop("a", t0)
	r.Close(t0.Add(Lease + time.Second))

	back := t0.Add(time.Hour)
	if stop, _ := Open(path, back).Renew(w, back); stop {
		t.Fatal("a stop whose lease ran out while the instance was up came back after the restart")
	}
}
