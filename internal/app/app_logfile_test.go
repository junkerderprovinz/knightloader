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

// disarm puts the process-wide sink back where every other test in this package
// expects to find it. The sink is a singleton by design - there is one standard
// logger and one ring behind it - so a test that arms it and walks away leaves
// every later test writing into a directory the framework has already removed.
func disarm(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = logring.CloseFile() })
}

// TestApplyLogFileArmsAndDisarmsWithoutARestart is the reason this is wired
// into afterSettingsChange as well as into the boot. Somebody switching the log
// file on is already trying to catch something; a setting that needed a restart
// to take effect would lose exactly the run they were chasing.
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
		t.Error("switching the setting off left the sink writing; somebody doing that usually needs the volume back")
	}
	// Off means off, not "off from the next restart".
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

// TestTaskTagIsFindableByTheLogReader pins the one thing that makes the
// per-download log card work at all: the shape this package WRITES and the
// shape internal/logring READS have to be the same shape, and they are written
// two packages apart.
//
// It also pins the second half of the decision - the id goes at the END of the
// line, so the source picker still files a checksum failure under "checksum"
// rather than under "task", which is the bucket somebody chasing a bad hash
// would actually reach for.
func TestTaskTagIsFindableByTheLogReader(t *testing.T) {
	const id = "00112233445566aa"

	line := "2026/09/08 14:18:22 checksum big.mkv: bad hash" + taskTag(id)
	if got := logring.TaskIDOf(line); got != id {
		t.Errorf("the log reader found %q in %q, want %q - the two sides have drifted apart", got, line, id)
	}
	if got := logring.SourceOf(line); got != "checksum" {
		t.Errorf("SourceOf(%q) = %q, want \"checksum\" - the id belongs at the end so the bucket stays honest", line, got)
	}

	// A job whose task is already gone gets no parenthesis at all: "(task )"
	// would sit in the log for ever, findable by nobody and explaining nothing.
	if got := taskTag(""); got != "" {
		t.Errorf("taskTag(\"\") = %q, want nothing at all", got)
	}
}

// TestApplyLogFileOffIsTheDefaultAndDoesNothing. Every install that upgrades
// into this key arrives here, and the one thing it must not do is start writing
// files somebody did not ask for.
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

// TestLogDirIsFixedUnlessTheEnvironmentSaysOtherwise. There is deliberately no
// path field in the settings: routes_features.go reflects the whole struct into
// a free text box, and a path typed there that the container user cannot write
// stops the log with nothing on screen connecting the two.
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

	// A variable set to nothing at all is a container template with an empty
	// field in it, not an instruction to write into the working directory.
	t.Setenv(LogDirEnv, "   ")
	if got, want := a.LogDir(), filepath.Join(dir, "logs"); got != want {
		t.Errorf("with %s set to whitespace, LogDir() = %q, want %q", LogDirEnv, got, want)
	}
}

// TestABrokenLogDirIsReportedRatherThanCrashing, and - the part that matters -
// the failure travels through log.Printf from applyLogFile without the sink
// answering it with another one. A failure path INSIDE the sink that logged
// would re-enter the ring's own mutex and hang this test instead of failing it.
func TestABrokenLogDirIsReportedRatherThanCrashing(t *testing.T) {
	disarm(t)
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// A regular file where the log directory should go, so MkdirAll cannot
	// succeed on any platform.
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

	// The instance goes on logging exactly as it did, which is the sentence the
	// card puts in front of the operator: nothing else stopped.
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
