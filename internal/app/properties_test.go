package app

// The properties panel's contract, from the server's side: what a request that
// mentions one field does to the other five, and what a rename does to a task in
// each of the states it can be in.

import (
	"encoding/json"
	"errors"
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

// optionsFromJSON decodes a request the way the route does, so a test states
// the wire shape the panel sends. A struct literal cannot tell an absent field
// from an empty one, which is the subject below.
func optionsFromJSON(t *testing.T, body string) TaskOptions {
	t.Helper()
	var o TaskOptions
	if err := json.Unmarshal([]byte(body), &o); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return o
}

// The panel edits every selected row at once, so a box left empty because the
// rows disagree looks like a box somebody emptied on purpose. Sending it either
// way would wipe a comment, a folder and a password off a whole selection in one
// click.
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

	// An empty string that is in the request clears the field, or a comment
	// would be a one-way door.
	if err := a.SetTaskOptions(ids, optionsFromJSON(t, `{"comment":""}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if live := liveTask(a, id); live.Comment != "" {
			t.Errorf("task %s comment = %q, want it cleared", id, live.Comment)
		}
	}
}

// A rename per status. Each branch is a way for the list and the folder to end
// up disagreeing.
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

// A name is an identity rather than a property: given to a whole selection it
// would point every download at one destination, and renameFinishedLocked would
// carry out the first and refuse the rest.
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

	// Renaming to nothing is refused rather than read as "clear the name", which
	// would leave a row with no way to describe itself.
	blank := "   "
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &blank}); err == nil {
		t.Error("a download was renamed to nothing")
	}
}

// stagedLink puts one link in the collector under the name original.bin.
func stagedLink(a *App) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks["1"] = &core.Task{
		ID: "1", URL: "https://host.example/original.bin", Name: "original.bin",
		Status: core.StatusCollected, Enabled: true,
	}
}

// A name that spells out folders is refused with the reason rather than cut
// into one: "Season 1/Episode 2" cut to "Season 1-Episode 2" is a name nobody
// typed, and the person who typed the slash is still looking at the window.
func TestRenameRefusesAPath(t *testing.T) {
	for _, in := range []string{"../../escape.bin", "sub/file.bin", `sub\file.bin`, "...", ".."} {
		t.Run(in, func(t *testing.T) {
			a, _ := newQuietApp(t)
			stagedLink(a)

			name := in
			if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &name}); err == nil {
				t.Fatalf("%q was taken as a name", in)
			}
			if live := liveTask(a, "1"); live.Name != "original.bin" || live.Filename != "" {
				t.Errorf("the row was edited anyway: name %q, override %q", live.Name, live.Filename)
			}
		})
	}
}

// What one file system or another cannot hold is still cut, and by the rule
// engine's own function, so a name typed into a window and a name written by a
// Packagizer rename cannot drift apart.
func TestRenameCutsWhatSomeFileSystemsRefuse(t *testing.T) {
	a, _ := newQuietApp(t)
	stagedLink(a)

	in := `Film: "Director's Cut"?.mkv`
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &in}); err != nil {
		t.Fatal(err)
	}
	if live, want := liveTask(a, "1"), rules.FileSegment(in); live.Name != want {
		t.Errorf("name = %q, want the rule engine's own cut %q", live.Name, want)
	}
}

// Downloads for which a rename could only be recorded and never carried out.
// Recording it anyway would leave a name waiting on the row that nothing
// applies, so each refuses and leaves the row alone.
func TestRenameRefusesWhatItCouldNotCarryOut(t *testing.T) {
	cases := []struct {
		name string
		edit func(*core.Task)
		says string
	}{
		{"a download being unpacked", func(t *core.Task) { t.Status = core.StatusExtracting }, "unpacked"},
		{"a torrent", func(t *core.Task) { t.InfoHash = "c12fe1c06bba254a9dc9f519b335aa7c1367a88a" }, "torrent"},
		{"a running JDownloader download", func(t *core.Task) {
			t.Status = core.StatusRunning
			t.Resolver = "jd"
		}, "JDownloader"},
		{"a JDownloader link still waiting", func(t *core.Task) {
			t.Status = core.StatusQueued
			t.Resolver = "jd"
		}, "JDownloader"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, base := newQuietApp(t)
			finishedTask(t, a, base, "1", "original.bin")
			editTask(a, "1", c.edit)

			want := "renamed.bin"
			err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want})
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("error = %v, want a refusal that says %q", err, c.says)
			}
			if live := liveTask(a, "1"); live.Name != "original.bin" || live.Filename != "" {
				t.Errorf("the row was edited anyway: name %q, override %q", live.Name, live.Filename)
			}
			if _, statErr := os.Stat(filepath.Join(base, "original.bin")); statErr != nil {
				t.Errorf("the file moved despite the refusal: %v", statErr)
			}
		})
	}
}

// The settle path keeps every part of a multi-volume set under its own name, so
// a part renamed before it has finished would only turn the rename into an
// error on the row once it had. It is refused while the person is still
// looking at the window.
func TestRenameRefusesOnePartOfAMultiVolumeArchive(t *testing.T) {
	a, base := newQuietApp(t)
	finishedTask(t, a, base, "1", "film.part1.rar")
	putTask(t, a, core.Task{ID: "2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Status: core.StatusQueued, Enabled: true})

	for _, id := range []string{"1", "2"} {
		want := "movie.rar"
		err := a.SetTaskOptions([]string{id}, TaskOptions{Name: &want})
		if err == nil || !strings.Contains(err.Error(), "multi-volume") {
			t.Fatalf("part %s: error = %v, want a refusal that names the archive", id, err)
		}
		if live := liveTask(a, id); live.Name == want || live.Filename != "" {
			t.Errorf("part %s was edited anyway: name %q, override %q", id, live.Name, live.Filename)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "film.part1.rar")); err != nil {
		t.Errorf("the finished part left its set: %v", err)
	}
}

// The rename window says why in the reader's language, so every refusal it
// can meet carries a code and the name it is about beside the English.
func TestARefusedRenameSaysWhyAsACode(t *testing.T) {
	a, base := newQuietApp(t)
	finishedTask(t, a, base, "part1", "film.part1.rar")
	putTask(t, a, core.Task{ID: "part2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Status: core.StatusCollected, Enabled: true})
	finishedTask(t, a, base, "movie", "movie.mkv")
	finishedTask(t, a, base, "taken", "taken.mkv")
	finishedTask(t, a, base, "torrent", "linux.iso")
	editTask(a, "torrent", func(x *core.Task) { x.InfoHash = "c12fe1c06bba254a9dc9f519b335aa7c1367a88a" })

	cases := []struct{ id, to, code, name string }{
		{"part2", "other.rar", "volume", "film.part2.rar"},
		{"movie", "taken.mkv", "exists", "taken.mkv"},
		{"torrent", "other.iso", "torrent", "linux.iso"},
		{"movie", "...", "dots", "..."},
		{"movie", "a/b.mkv", "separator", "a/b.mkv"},
		{"movie", "  ", "empty", ""},
	}
	for _, c := range cases {
		to := c.to
		err := a.SetTaskOptions([]string{c.id}, TaskOptions{Name: &to})
		var r *RenameRefusal
		if !errors.As(err, &r) {
			t.Errorf("renaming %s to %q: error %v carries no code", c.id, c.to, err)
			continue
		}
		if r.Code != c.code || r.Name != c.name {
			t.Errorf("renaming %s to %q: code %q about %q, want %q about %q", c.id, c.to, r.Code, r.Name, c.code, c.name)
		}
	}
	if _, err := a.RenamePackage([]string{"movie"}, "Season 1/Episode 2"); !errors.As(err, new(*RenameRefusal)) {
		t.Errorf("a package name with a slash is refused with %v, which carries no code", err)
	}
}

// A download settles in the working folder and is moved to its destination
// afterwards. A rename looking only at the destination finds no file there and
// fails for every download that has not been moved yet.
func TestRenameFindsAFileStillInTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	task := putTask(t, a, core.Task{ID: "1", URL: "https://host.example/original.bin",
		Name: "original.bin", Status: core.StatusDone, Enabled: true})
	a.mu.Lock()
	dir := a.workDirFor(task)
	a.mu.Unlock()
	if dir == a.TaskFolder("1") {
		t.Fatal("the working folder is the destination, so this test would not tell the two apart")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "original.bin"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := "renamed.bin"
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &want}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		t.Errorf("the file in the working folder was not renamed: %v", err)
	}
	if live := liveTask(a, "1"); live.Name != want {
		t.Errorf("the row reads %q, want %q", live.Name, want)
	}
}
