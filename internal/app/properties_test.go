package app

// The properties panel's contract, from the server's side: what a request that
// mentions one field does to the other five, and what a rename does to a task in
// each of the states it can be in.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newQuietApp is an app with the two things that would otherwise run on their
// own when a download settles switched off, so a test about names is about
// names.
func newQuietApp(t *testing.T) (*App, string) {
	t.Helper()
	return newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
}

// optionsFromJSON decodes a request the way the route does, so a test states the wire
// shape the panel actually sends rather than a struct literal that can express
// things JSON cannot - the difference between "absent" and "empty" being the
// whole subject below.
func optionsFromJSON(t *testing.T, body string) TaskOptions {
	t.Helper()
	var o TaskOptions
	if err := json.Unmarshal([]byte(body), &o); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return o
}

// TestUntouchedFieldsSurviveAnEditToTheSelection is the rule the whole panel is
// built around, and the one whose absence destroys data quietly.
//
// The panel edits every selected row at once, so a box that is empty because the
// rows disagree looks exactly like a box somebody emptied on purpose. Sent
// either way, the first reading wipes a comment, a folder and a password off
// forty downloads in one click, and nothing on screen would connect the loss to
// the field that was never touched.
func TestUntouchedFieldsSurviveAnEditToTheSelection(t *testing.T) {
	a, base := newQuietApp(t)
	for _, id := range []string{"1", "2", "3"} {
		finishedTask(t, a, base, id, "file"+id+".bin")
		editTask(a, id, func(t *core.Task) {
			t.Comment = "note " + id
			t.Password = "secret " + id
			t.Dir = base
		})
	}
	ids := []string{"1", "2", "3"}

	// A request that carries only the comment. Everything else was left alone in
	// the panel and is therefore not in the request at all.
	if err := a.SetTaskOptions(ids, optionsFromJSON(t, `{"comment":"one note for all three"}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		live := liveTask(a, id)
		if live.Comment != "one note for all three" {
			t.Errorf("task %s comment = %q, want the edited value", id, live.Comment)
		}
		if live.Password != "secret "+id {
			t.Errorf("task %s password = %q; a field nobody touched was written over", id, live.Password)
		}
		if live.Dir != base {
			t.Errorf("task %s folder = %q; a field nobody touched was written over", id, live.Dir)
		}
	}

	// And the other way round: a folder edit must not take the comments with it.
	if err := a.SetTaskOptions(ids, optionsFromJSON(t, `{"dir":`+quoteJSON(base)+`}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if live := liveTask(a, id); live.Comment != "one note for all three" {
			t.Errorf("task %s comment = %q after an unrelated edit, want it untouched", id, live.Comment)
		}
	}

	// An empty string that IS in the request is a deliberate clearing, and it has
	// to work - otherwise a comment is a one-way door and the rule above would
	// only be half a rule.
	if err := a.SetTaskOptions(ids, optionsFromJSON(t, `{"comment":""}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if live := liveTask(a, id); live.Comment != "" {
			t.Errorf("task %s comment = %q, want it cleared", id, live.Comment)
		}
	}
}

// TestRenameFollowsTheStatus is the rule per status, and each branch is a
// different way for the list and the folder to end up disagreeing.
func TestRenameFollowsTheStatus(t *testing.T) {
	t.Run("a finished download moves on disk", func(t *testing.T) {
		a, base := newQuietApp(t)
		finishedTask(t, a, base, "1", "original.bin")

		want := "Great.Film.2026.bin"
		if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want}); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(base, want)); err != nil {
			t.Fatalf("the finished file was not moved: %v", err)
		}
		if _, err := os.Stat(filepath.Join(base, "original.bin")); err == nil {
			t.Error("the old name is still there; the file was copied rather than moved")
		}
		if live := liveTask(a, "1"); live.Name != want {
			t.Errorf("the row reads %q, want %q", live.Name, want)
		}
	})

	t.Run("a running download keeps the file the backend has open", func(t *testing.T) {
		a, base := newQuietApp(t)
		finishedTask(t, a, base, "1", "original.bin")
		editTask(a, "1", func(t *core.Task) { t.Status = core.StatusRunning })

		want := "renamed.bin"
		if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want}); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(base, "original.bin")); err != nil {
			t.Fatalf("the file under the backend's own name was moved while it was open: %v", err)
		}
		live := liveTask(a, "1")
		if live.Name != "original.bin" {
			t.Errorf("the row reads %q while the file is still %q; the two disagree", live.Name, "original.bin")
		}
		if live.Filename != want {
			t.Fatalf("the override reads %q, so the rename was dropped rather than deferred", live.Filename)
		}

		// The deferral is only honest if the settle path really carries it out.
		a.onUpdate("1", core.Update{Status: core.StatusDone})
		if _, err := os.Stat(filepath.Join(base, want)); err != nil {
			t.Fatalf("the rename never landed when the download finished: %v", err)
		}
		if live := liveTask(a, "1"); live.Name != want {
			t.Errorf("the row reads %q after the download settled, want %q", live.Name, want)
		}
	})

	t.Run("a link that has not started takes the name at once", func(t *testing.T) {
		a, _ := newQuietApp(t)
		a.mu.Lock()
		a.tasks["1"] = &core.Task{
			ID: "1", URL: "https://host.example/original.bin", Name: "original.bin",
			Status: core.StatusCollected, Enabled: true,
		}
		a.mu.Unlock()

		want := "renamed.bin"
		if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want}); err != nil {
			t.Fatal(err)
		}

		live := liveTask(a, "1")
		if live.Name != want {
			t.Errorf("the row reads %q; a staged link has no file to disagree with", live.Name)
		}
		if live.Filename != want {
			t.Errorf("the override reads %q, want the name carried to the finished file too", live.Filename)
		}
	})

	t.Run("a refusal comes back with its reason", func(t *testing.T) {
		a, base := newQuietApp(t)
		if err := os.WriteFile(filepath.Join(base, "taken.bin"), []byte("somebody else's"), 0o644); err != nil {
			t.Fatal(err)
		}
		finishedTask(t, a, base, "1", "original.bin")

		want := "taken.bin"
		err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want})
		if err == nil {
			t.Fatal("the rename reported success over a file that already existed")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("error = %q, want it to say why the rename did not happen", err)
		}
		body, readErr := os.ReadFile(filepath.Join(base, "taken.bin"))
		if readErr != nil || string(body) != "somebody else's" {
			t.Errorf("the file that was already there reads %q (%v)", body, readErr)
		}
		if live := liveTask(a, "1"); live.Name != "original.bin" {
			t.Errorf("the row reads %q, want the name the file actually has", live.Name)
		}
	})
}

// TestRenameIsRefusedOverASelection keeps one name off forty files. A name is an
// identity, not a property: given to a whole selection it would point every
// download at one destination, and renameFinishedLocked would carry out the
// first and refuse the rest one at a time.
func TestRenameIsRefusedOverASelection(t *testing.T) {
	a, base := newQuietApp(t)
	finishedTask(t, a, base, "1", "one.bin")
	finishedTask(t, a, base, "2", "two.bin")

	want := "same.bin"
	err := a.SetTaskOptions([]string{"1", "2"}, TaskOptions{Name: &want})
	if err == nil {
		t.Fatal("two downloads were given one name")
	}
	if !strings.Contains(err.Error(), "one file") {
		t.Errorf("error = %q, want it to say a name belongs to one file", err)
	}
	for id, name := range map[string]string{"1": "one.bin", "2": "two.bin"} {
		if live := liveTask(a, id); live.Name != name || live.Filename != "" {
			t.Errorf("task %s was edited anyway: name %q, override %q", id, live.Name, live.Filename)
		}
		if _, statErr := os.Stat(filepath.Join(base, name)); statErr != nil {
			t.Errorf("%s was moved despite the refusal: %v", name, statErr)
		}
	}

	// Renaming to nothing is the other request that cannot mean anything. It is
	// refused rather than read as "clear the name", which would leave a row with
	// no way to describe itself.
	blank := "   "
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &blank}); err == nil {
		t.Error("a download was renamed to nothing")
	}
}

// TestRenameIsCutToOneSegment is the path-escape guard, and the point of the
// test is as much WHERE the cut comes from as what it does: it is the rule
// engine's own, so a name typed into the panel and a name written by a
// Packagizer rename cannot drift into two different ideas of what a file name
// is. The one that drifts is the one that lets "../../etc/x" through.
func TestRenameIsCutToOneSegment(t *testing.T) {
	for _, in := range []string{"../../escape.bin", "sub/file.bin", `sub\file.bin`, "..."} {
		t.Run(in, func(t *testing.T) {
			a, base := newQuietApp(t)
			a.mu.Lock()
			a.tasks["1"] = &core.Task{
				ID: "1", URL: "https://host.example/original.bin", Name: "original.bin",
				Status: core.StatusCollected, Enabled: true,
			}
			a.mu.Unlock()

			name := in
			if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &name}); err != nil {
				t.Fatal(err)
			}

			live := liveTask(a, "1")
			if want := rules.FileSegment(in); live.Name != want {
				t.Errorf("name = %q, want the rule engine's own cut %q", live.Name, want)
			}
			if strings.ContainsAny(live.Name, `/\`) {
				t.Errorf("name = %q, which is a path and not a name", live.Name)
			}
			if got := filepath.Join(base, live.Name); filepath.Dir(got) != filepath.Clean(base) {
				t.Errorf("the file would land in %q, outside %q", filepath.Dir(got), base)
			}
		})
	}
}
