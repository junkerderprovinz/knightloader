package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// mirrorApp is an app that merges on the file name alone, which is the policy
// that catches the case people actually paste: one release, two hosters, no
// byte count known yet because nothing has been downloaded.
func mirrorApp(t *testing.T, keep bool) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		s.MirrorPolicy = string(dedupe.PolicyFilenameOnly)
		s.KeepMirrors = keep
	})
	return a
}

// mirrorPair stages the same file from two hosters and returns what the second
// paste produced.
func mirrorPair(t *testing.T, a *App) (first, second []*core.Task) {
	t.Helper()
	first = a.AddLinks([]string{"https://one.example/film.rar"}, "Release")
	if len(first) != 1 {
		t.Fatalf("the first link staged %d tasks, want 1", len(first))
	}
	return first, a.AddLinks([]string{"https://two.example/film.rar"}, "Release")
}

// TestAMirrorIsStillDroppedByDefault pins the behaviour every existing install
// has. Two copies of one file is two downloads of one file, and the list is long
// enough already; keeping the second one is a choice somebody makes.
func TestAMirrorIsStillDroppedByDefault(t *testing.T) {
	a := mirrorApp(t, false)
	_, second := mirrorPair(t, a)
	if len(second) != 0 {
		t.Fatalf("the mirror staged %d tasks, want none by default", len(second))
	}
	if skipped := a.SkippedLinks(); len(skipped) != 1 || skipped[0].Kind != "mirror" {
		t.Errorf("the folded link left the trace %+v, want one mirror entry", skipped)
	}
}

// TestAKeptMirrorNamesWhatItIsACopyOf is the field's reason to exist. Until
// this, Task.MirrorOf was persisted, read back and rendered, and nothing in the
// tree ever wrote it - a column promising an answer it could not have.
func TestAKeptMirrorNamesWhatItIsACopyOf(t *testing.T) {
	a := mirrorApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	sib := second[0]
	if sib.MirrorOf != first[0].ID {
		t.Errorf("mirrorOf = %q, want the task it is a copy of (%q)", sib.MirrorOf, first[0].ID)
	}
	if sib.URL != "https://two.example/film.rar" {
		t.Errorf("the sibling carries %q, want the second hoster's link", sib.URL)
	}
	if !sib.Hold {
		t.Error("the sibling is not on hold, so the queue would fetch the same file twice")
	}
	if !sib.Enabled {
		t.Error("the sibling was switched off; Enabled is the user's own switch and nothing here may write it")
	}
	if skipped := a.SkippedLinks(); len(skipped) != 0 {
		t.Errorf("the kept link was also recorded as skipped: %+v", skipped)
	}
}

// TestAKeptMirrorIsNotDispatched is what Hold is doing there. A sibling that the
// queue picks up is not a spare copy, it is the same file downloaded twice -
// which is the behaviour the whole mirror set exists to prevent.
func TestAKeptMirrorIsNotDispatched(t *testing.T) {
	a := mirrorApp(t, true)
	_, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	a.StartTasks(nil)
	a.mu.Lock()
	dispatched := a.active[second[0].ID]
	a.mu.Unlock()
	if dispatched {
		t.Error("\"start everything\" handed the sibling to a backend")
	}
}

// TestTheSameLinkTwiceIsStillARefusal is the line the setting does not move. The
// same URL a second time is a fact rather than a guess about two files, and
// staging it as a "sibling" would put two rows in the list pointing at the same
// bytes on the same hoster.
func TestTheSameLinkTwiceIsStillARefusal(t *testing.T) {
	a := mirrorApp(t, true)
	if created := a.AddLinks([]string{"https://one.example/film.rar"}, "Release"); len(created) != 1 {
		t.Fatalf("the first paste staged %d tasks", len(created))
	}
	again := a.AddLinks([]string{"https://one.example/film.rar"}, "Release")
	if len(again) != 0 {
		t.Fatalf("the same URL pasted twice staged %d more tasks, want none", len(again))
	}
	if skipped := a.SkippedLinks(); len(skipped) != 1 || skipped[0].Kind != "duplicate" {
		t.Errorf("the second paste left %+v, want one duplicate entry", skipped)
	}
}

// TestAThirdCopyIsMeasuredAgainstTheSiblingToo is why putSibling files the link
// in the mirror set. Left out, the set would still only know the original, and
// re-pasting the sibling's own URL would stage a second sibling of the same
// download every time.
func TestAThirdCopyIsMeasuredAgainstTheSiblingToo(t *testing.T) {
	a := mirrorApp(t, true)
	if _, second := mirrorPair(t, a); len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	again := a.AddLinks([]string{"https://two.example/film.rar"}, "Release")
	if len(again) != 0 {
		t.Errorf("the sibling's own URL pasted again staged %d tasks, want none", len(again))
	}
}

// TestAKeptMirrorSurvivesARestart is the difference the setting actually buys.
// A dropped mirror lives on only in an in-memory trace that the next restart
// clears; a sibling is a task, and a task is in the store.
func TestAKeptMirrorSurvivesARestart(t *testing.T) {
	a := mirrorApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range stored {
		if task.ID == second[0].ID {
			if task.MirrorOf != first[0].ID {
				t.Errorf("the stored sibling says mirrorOf %q, want %q", task.MirrorOf, first[0].ID)
			}
			if !task.Hold {
				t.Error("the stored sibling is not on hold, so a restart would start it")
			}
			return
		}
	}
	t.Fatal("the sibling is not in the store at all")
}
