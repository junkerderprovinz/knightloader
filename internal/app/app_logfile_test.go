package app

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// disarm closes the process-wide file sink when the test ends, so later tests
// do not write into a directory the framework has already removed.
func disarm(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = logring.CloseFile() })
}

func TestApplyLogFileArmsAndDisarmsWithoutARestart(t *testing.T) {
	disarm(t)
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if logring.FileStatus().Enabled {
		t.Fatal("a fresh instance is already writing a log file; this feature ships off")
	}

	a.applyLogFile(settings.LogFile{Enabled: true, MaxMB: 1, Keep: 1})
	st := logring.FileStatus()
	if !st.Enabled {
		t.Fatalf("switching the setting on did not arm the sink: %+v", st)
	}
	want := filepath.Join(a.LogDir(), logring.Name)
	if st.Path != want {
		t.Errorf("the log is at %q, want %q", st.Path, want)
	}

	marker := "app-logfile-marker-3qz7"
	log.Print(marker)
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("the file the sink named is not there: %v", err)
	}
	if !strings.Contains(string(body), marker) {
		t.Errorf("the line logged after arming is not in the file:\n%s", body)
	}

	a.applyLogFile(settings.LogFile{Enabled: false, MaxMB: 1, Keep: 1})
	if logring.FileStatus().Enabled {
		t.Error("switching the setting off left the sink writing")
	}
	before := len(body)
	log.Print("app-logfile-after-the-switch-went-off")
	after, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != before {
		t.Errorf("the file grew from %d to %d bytes after the setting was switched off", before, len(after))
	}
}

// TestTaskTagIsFindableByTheLogReader keeps the tag this package writes in step
// with what internal/logring reads.
func TestTaskTagIsFindableByTheLogReader(t *testing.T) {
	const id = "00112233445566aa"

	line := "2026/09/08 14:18:22 checksum big.mkv: bad hash" + taskTag(id)
	if got := logring.TaskIDOf(line); got != id {
		t.Errorf("the log reader found %q in %q, want %q", got, line, id)
	}
	if got := logring.SourceOf(line); got != "checksum" {
		t.Errorf("SourceOf(%q) = %q, want \"checksum\"", line, got)
	}

	if got := taskTag(""); got != "" {
		t.Errorf("taskTag(\"\") = %q, want nothing at all", got)
	}
}

func TestApplyLogFileOffIsTheDefaultAndDoesNothing(t *testing.T) {
	disarm(t)
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	a.applyLogFile(settings.DefaultLogFile())
	if logring.FileStatus().Enabled {
		t.Error("the shipped defaults armed the log file")
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); err == nil {
		t.Error("the shipped defaults created a logs folder on a volume nobody asked to write to")
	}
}

func TestLogDirIsFixedUnlessTheEnvironmentSaysOtherwise(t *testing.T) {
	disarm(t)
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if got, want := a.LogDir(), filepath.Join(dir, "logs"); got != want {
		t.Errorf("LogDir() = %q, want %q beside the database", got, want)
	}

	elsewhere := t.TempDir()
	t.Setenv(LogDirEnv, elsewhere)
	if got := a.LogDir(); got != elsewhere {
		t.Errorf("with %s set, LogDir() = %q, want %q", LogDirEnv, got, elsewhere)
	}

	t.Setenv(LogDirEnv, "   ")
	if got, want := a.LogDir(), filepath.Join(dir, "logs"); got != want {
		t.Errorf("with %s set to whitespace, LogDir() = %q, want %q", LogDirEnv, got, want)
	}
}

// TestABrokenLogDirIsReportedRatherThanCrashing also guards against the sink
// logging its own failure: that would re-enter the ring's mutex and hang here.
func TestABrokenLogDirIsReportedRatherThanCrashing(t *testing.T) {
	disarm(t)
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// A regular file where the directory should go, so MkdirAll fails on every
	// platform.
	blocker := filepath.Join(t.TempDir(), "in-the-way")
	if werr := os.WriteFile(blocker, []byte("not a directory"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	t.Setenv(LogDirEnv, filepath.Join(blocker, "logs"))

	a.applyLogFile(settings.LogFile{Enabled: true, MaxMB: 1, Keep: 1})

	st := logring.FileStatus()
	if st.Enabled {
		t.Error("the sink claims to be writing into a directory that could not be created")
	}
	if st.Problem == "" {
		t.Error("no problem was recorded, so the card would show the same thing as \"switched off\"")
	}

	marker := "app-logfile-broken-dir-marker-1v4t"
	log.Print(marker)
	found := false
	for _, line := range logring.Lines() {
		if strings.Contains(line, marker) {
			found = true
		}
	}
	if !found {
		t.Error("a log file that could not be opened cost the ring its lines")
	}
}
