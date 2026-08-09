package app

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// finishedTask puts a task in the list as though its download had just landed,
// with the bytes really on disk. Everything below is about what happens to a
// file that exists, so a test that only builds the row would pass while the
// disk stayed untouched.
func finishedTask(t *testing.T, a *App, dir, id, name string) *core.Task {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("payload of "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &core.Task{
		ID:      id,
		URL:     "https://host.example/" + name,
		Name:    name,
		Status:  core.StatusDone,
		Enabled: true,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.mu.Unlock()
	return task
}

// liveTask reads a task out of the list the way the rest of the app sees it.
func liveTask(a *App, id string) core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.tasks[id]; t != nil {
		return *t
	}
	return core.Task{}
}

// TestFinishedDownloadIsRenamedOnDisk is the whole point of the rename action.
// A name written onto the row alone leaves the list showing one thing and the
// folder holding another, and extraction and checksum verification both build
// their path from that name — so the rule would not merely fail to rename, it
// would break the two steps that come after it.
func TestFinishedDownloadIsRenamedOnDisk(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	task := finishedTask(t, a, base, "1", "original.bin")
	task.Status = core.StatusRunning
	task.Filename = "Great.Film.2026.bin"

	a.onUpdate(task.ID, core.Update{Status: core.StatusDone})

	if _, err := os.Stat(filepath.Join(base, "Great.Film.2026.bin")); err != nil {
		t.Fatalf("the file was never renamed on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "original.bin")); err == nil {
		t.Error("the old name is still there; the file was copied rather than moved")
	}
	live := liveTask(a, task.ID)
	if live.Name != "Great.Film.2026.bin" {
		t.Errorf("the row reads %q; the list and the disk disagree", live.Name)
	}
	if live.Error != "" {
		t.Errorf("error = %q on a download that finished and renamed cleanly", live.Error)
	}
}

// TestRenamingAFinishedTaskByHandMovesTheFileNow is the same action arriving
// from the other direction. A rename typed onto something that finished an hour
// ago has to move the file, not wait for a download that will never happen
// again.
func TestRenamingAFinishedTaskByHandMovesTheFileNow(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	finishedTask(t, a, base, "1", "original.bin")

	want := "renamed.bin"
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Filename: &want}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(base, want)); err != nil {
		t.Fatalf("the finished file was not moved: %v", err)
	}
	if live := liveTask(a, "1"); live.Name != want {
		t.Errorf("the row reads %q, want %q", live.Name, want)
	}
}

// TestRenameRefusesRatherThanOverwriting covers the two ways a rename can be
// the wrong thing to do. Both leave the file where it is and say so on the
// task: a rename that quietly did not happen is worse than one that failed,
// because the row then promises a name the folder does not have.
func TestRenameRefusesRatherThanOverwriting(t *testing.T) {
	t.Run("the target name is already taken", func(t *testing.T) {
		a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
			s.Extract, s.VerifyChecksums = false, false
		})
		if err := os.WriteFile(filepath.Join(base, "taken.bin"), []byte("somebody else's"), 0o644); err != nil {
			t.Fatal(err)
		}
		task := finishedTask(t, a, base, "1", "original.bin")
		task.Status = core.StatusRunning
		task.Filename = "taken.bin"

		a.onUpdate(task.ID, core.Update{Status: core.StatusDone})

		body, err := os.ReadFile(filepath.Join(base, "taken.bin"))
		if err != nil || string(body) != "somebody else's" {
			t.Errorf("the file that was already there reads %q (%v); a rule is not a reason to destroy it", body, err)
		}
		live := liveTask(a, task.ID)
		if live.Name != "original.bin" {
			t.Errorf("the row reads %q, want the name the file actually has", live.Name)
		}
		if !strings.Contains(live.Error, "already exists") {
			t.Errorf("the task reads %q, want it to say why the rename did not happen", live.Error)
		}
	})

	t.Run("the file is one part of a multi-volume archive", func(t *testing.T) {
		a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
			s.Extract, s.VerifyChecksums = false, false
		})
		finishedTask(t, a, base, "1", "film.part01.rar")
		second := finishedTask(t, a, base, "2", "film.part02.rar")
		second.Status = core.StatusRunning
		// Exactly what a rule with a fixed name does to every part of a set: five
		// downloads, one destination, four of them overwritten.
		second.Filename = "movie.rar"

		a.onUpdate(second.ID, core.Update{Status: core.StatusDone})

		if _, err := os.Stat(filepath.Join(base, "film.part02.rar")); err != nil {
			t.Errorf("the volume was renamed out of its set: %v", err)
		}
		live := liveTask(a, second.ID)
		if live.Name != "film.part02.rar" {
			t.Errorf("the row reads %q, want the part left in the set it belongs to", live.Name)
		}
		if !strings.Contains(live.Error, "multi-volume") {
			t.Errorf("the task reads %q, want the reason the rename was refused", live.Error)
		}
	})
}

// TestExtractionSwitchIsReadFromTheVolumeThatGetsOpened is the multi-volume
// trap. extractCandidateLocked hands back the FIRST volume of a set, never the
// part that happened to finish last, so an override read off the finishing part
// would make one archive extract or not depending on which of its parts the
// hoster served quickest — the same rule, the same set, a different answer on
// every run.
func TestExtractionSwitchIsReadFromTheVolumeThatGetsOpened(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name          string
		first, last   *bool
		global        bool
		wantExtracted bool
	}{
		{"the first volume's no beats a global yes", &no, nil, true, false},
		{"and the part that finished last cannot overrule it", &no, &yes, true, false},
		{"the first volume's yes beats a global no", &yes, nil, false, true},
		{"and the part that finished last cannot switch that off either", &yes, &no, false, true},
		{"with no override anywhere the global decides", nil, nil, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract = tc.global })
			p1 := &core.Task{ID: "1", Name: "film.part01.rar", Status: core.StatusDone, AutoExtract: tc.first}
			p2 := &core.Task{ID: "2", Name: "film.part02.rar", Status: core.StatusDone, AutoExtract: tc.last}
			a.mu.Lock()
			a.tasks[p1.ID], a.tasks[p2.ID] = p1, p2
			target, path := a.extractionDueLocked(p2, a.Settings.Get())
			a.mu.Unlock()

			if (target != nil) != tc.wantExtracted {
				t.Fatalf("extraction due = %v, want %v", target != nil, tc.wantExtracted)
			}
			if !tc.wantExtracted {
				return
			}
			if target.ID != p1.ID {
				t.Errorf("opened %q, want the first volume %q", target.Name, p1.Name)
			}
			if !strings.HasSuffix(path, "film.part01.rar") {
				t.Errorf("path handed to the extractor = %q", path)
			}
		})
	}
}

// TestExtractionSwitchedOnAfterTheDownloadFinished is why the switch is read at
// extraction time and not at download time. Read when the bytes stopped moving,
// turning unpacking on later would be a control that does nothing to anything
// already in the list, and the only way to use it would be to download the
// archive again.
func TestExtractionSwitchedOnAfterTheDownloadFinished(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	writeZip(t, filepath.Join(base, "release.zip"), "inside.txt", "unpacked")
	task := &core.Task{
		ID: "1", URL: "https://host.example/release.zip", Name: "release.zip",
		Status: core.StatusDone, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[task.ID] = task
	due, _ := a.extractionDueLocked(task, a.Settings.Get())
	a.mu.Unlock()
	if due != nil {
		t.Fatal("the archive was due for unpacking with the global switch off and no override")
	}

	on := true
	if err := a.SetTaskOptions([]string{task.ID}, TaskOptions{AutoExtract: TriBool{Set: true, Value: &on}}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the archive being unpacked after the switch was turned on", func() bool {
		_, err := os.Stat(filepath.Join(base, "release", "inside.txt"))
		return err == nil
	})
	waitFor(t, "the task settling back to done", func() bool {
		live := liveTask(a, task.ID)
		return live.Status == core.StatusDone && live.Error == ""
	})
}

// TestExtractionOverrideReachesEveryPartOfTheSet is the hand-edit half of the
// multi-volume trap. The user clicks the part they can see and asks for it to be
// unpacked; the first volume is what actually gets opened, so an override
// written onto that one part alone is a switch that silently does nothing.
func TestExtractionOverrideReachesEveryPartOfTheSet(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	finishedTask(t, a, base, "1", "film.part01.rar")
	finishedTask(t, a, base, "2", "film.part02.rar")

	on := true
	// Asked of the second part, which is the one a user is most likely to have
	// selected: it is the row that just turned green.
	if err := a.SetTaskOptions([]string{"2"}, TaskOptions{AutoExtract: TriBool{Set: true, Value: &on}}); err != nil {
		t.Fatal(err)
	}

	first := liveTask(a, "1")
	if first.AutoExtract == nil || !*first.AutoExtract {
		t.Fatalf("the first volume's switch = %v; the set is opened through it", first.AutoExtract)
	}
	// The two parts hold two values, not one shared pointer: written through, one
	// row's edit would otherwise change every part of the set.
	second := liveTask(a, "2")
	if second.AutoExtract == first.AutoExtract {
		t.Error("both parts share one pointer; editing either would change both")
	}

	// The truncated .rar cannot be opened, and that is what makes this legible:
	// the failure is recorded on the volume the extractor was pointed at.
	waitFor(t, "the extractor being pointed at the first volume", func() bool {
		return strings.HasPrefix(liveTask(a, "1").Error, "extract:")
	})
	if msg := liveTask(a, "2").Error; msg != "" {
		t.Errorf("the second part reads %q; only the volume that was opened carries the outcome", msg)
	}
}

// The chunk-count table used to live here, and it read the resolver's answer as
// a value rather than as a ceiling. It is now in chunks_test.go, whole: two
// tables for one formula is how the four readings got four answers in the first
// place.

// TestUnusableOptionsAreRefusedBeforeAnythingIsTouched keeps a bad value from
// editing half a selection. Refused halfway through, the first rows would carry
// the new folder, the rest would not, and the error message says nothing about
// where the line fell.
func TestUnusableOptionsAreRefusedBeforeAnythingIsTouched(t *testing.T) {
	escape := "../../elsewhere.bin"
	separator := "sub/file.bin"
	tooMany := 99
	negative := -1
	cases := []struct {
		name   string
		opts   TaskOptions
		wantIn string
	}{
		{"a name that climbs out of the folder", TaskOptions{Filename: &escape}, "single path segment"},
		{"a name carrying a separator", TaskOptions{Filename: &separator}, "single path segment"},
		{"more connections than the rule engine allows", TaskOptions{Chunks: &tooMany}, "outside 0..16"},
		{"a negative connection count", TaskOptions{Chunks: &negative}, "outside 0..16"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, base := newRuleApp(t, func(*settings.Settings, string) {})
			finishedTask(t, a, base, "1", "original.bin")

			err := a.SetTaskOptions([]string{"1"}, tc.opts)
			if err == nil {
				t.Fatal("the value was accepted")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantIn)
			}
			live := liveTask(a, "1")
			if live.Filename != "" || live.Chunks != 0 {
				t.Errorf("the task was edited anyway: filename %q, chunks %d", live.Filename, live.Chunks)
			}
		})
	}
}

// TestAutoExtractOverrideSurvivesAnUnrelatedEdit is the tri-state, tested where
// it can actually bite. The store column is nullable, so every task already in
// it inherits the global switch; decode that as a plain bool and a request that
// only changes the folder writes "do not unpack" onto everything it touches.
func TestAutoExtractOverrideSurvivesAnUnrelatedEdit(t *testing.T) {
	decode := func(t *testing.T, body string) TaskOptions {
		t.Helper()
		var o TaskOptions
		if err := json.Unmarshal([]byte(body), &o); err != nil {
			t.Fatalf("decoding %s: %v", body, err)
		}
		return o
	}

	cases := []struct {
		name    string
		body    string
		wantSet bool
		want    *bool
	}{
		{"a request that never mentions it", `{"dir":"/data"}`, false, nil},
		{"switched on", `{"autoExtract":true}`, true, boolPtr(true)},
		{"switched off, which is not the same as unset", `{"autoExtract":false}`, true, boolPtr(false)},
		{"put back to inheriting the global", `{"autoExtract":null}`, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decode(t, tc.body).AutoExtract
			if got.Set != tc.wantSet {
				t.Fatalf("present in the request = %v, want %v", got.Set, tc.wantSet)
			}
			switch {
			case tc.want == nil && got.Value != nil:
				t.Errorf("value = %v, want inherit", *got.Value)
			case tc.want != nil && (got.Value == nil || *got.Value != *tc.want):
				t.Errorf("value = %v, want %v", got.Value, *tc.want)
			}
		})
	}

	// And the consequence on a real task: a folder change must leave an override
	// somebody set earlier exactly as it was.
	a, base := newRuleApp(t, func(*settings.Settings, string) {})
	task := finishedTask(t, a, base, "1", "original.bin")
	task.AutoExtract = boolPtr(true)

	if err := a.SetTaskOptions([]string{"1"}, decode(t, `{"dir":`+quoteJSON(base)+`}`)); err != nil {
		t.Fatal(err)
	}
	if live := liveTask(a, "1"); live.AutoExtract == nil || !*live.AutoExtract {
		t.Errorf("the override reads %v after an unrelated edit, want it untouched", live.AutoExtract)
	}

	// Null is the way back to inheriting, and it has to be reachable or an
	// override is a one-way door.
	if err := a.SetTaskOptions([]string{"1"}, decode(t, `{"autoExtract":null}`)); err != nil {
		t.Fatal(err)
	}
	if live := liveTask(a, "1"); live.AutoExtract != nil {
		t.Errorf("the override reads %v, want it cleared back to the global", *live.AutoExtract)
	}
}

func boolPtr(v bool) *bool { return &v }

// quoteJSON spells a path as a JSON string, which on Windows is the difference
// between a folder and an escape sequence.
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// writeZip builds a one-entry archive, so an extraction test can prove a file
// came out rather than only that a status changed.
func writeZip(t *testing.T, path, entry, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
