package logring

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read is the file's whole content, or "" when it is not there.
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

// TestTheFileGetsTheLiveLines is the feature at its plainest.
func TestTheFileGetsTheLiveLines(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	r.Write([]byte("hello from the ring\n"))
	if got := read(t, filepath.Join(dir, Name)); !strings.Contains(got, "hello from the ring") {
		t.Errorf("the log file does not contain the line that was logged: %q", got)
	}
	st := r.FileStatus()
	if !st.Enabled || st.Problem != "" {
		t.Errorf("FileStatus() = %+v, want enabled with no problem", st)
	}
	if len(st.Generations) != 1 || st.Generations[0].Index != 0 {
		t.Errorf("generations = %+v, want exactly the file being written, at index 0", st.Generations)
	}
}

// TestArmingReplaysWhatIsAlreadyInTheRing is the trap this feature is built
// around. The sink cannot be attached where the ring is: the data directory and
// the setting that asks for a file are both unknown until well into the boot,
// so everything that explains a BAD boot has already been logged by the time
// the file opens. Without the replay the file starts mid-boot and silently
// omits exactly the lines somebody switched it on to read.
func TestArmingReplaysWhatIsAlreadyInTheRing(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	r.Write([]byte("data dir: /config\n"))
	r.Write([]byte("JD provisioning failed: no java\n"))

	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 1}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()
	r.Write([]byte("listening on :8080\n"))

	got := read(t, filepath.Join(dir, Name))
	for _, want := range []string{"data dir: /config", "JD provisioning failed: no java", "listening on :8080"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log file is missing %q; it reads:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "line(s) logged before this file was opened") {
		t.Errorf("the replayed lines carry no marker, so nobody can tell them from live ones:\n%s", got)
	}
	// Order matters as much as presence: a file that starts with the live line
	// and then goes back in time is unreadable.
	if strings.Index(got, "data dir") > strings.Index(got, "listening on") {
		t.Errorf("the replayed boot lines landed AFTER the live one:\n%s", got)
	}
}

// TestArmingTwiceDoesNotReplayTwice. A saved settings page re-arms the sink,
// which reopens the same path in append mode. A watermark that started again
// with each sink would write the whole ring in a second time and the operator
// would read the same morning twice.
func TestArmingTwiceDoesNotReplayTwice(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	r.Write([]byte("boot line alpha\n"))

	opts := FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 1}
	if err := r.OpenFile(opts); err != nil {
		t.Fatal(err)
	}
	if err := r.CloseFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenFile(opts); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	got := read(t, filepath.Join(dir, Name))
	if n := strings.Count(got, "boot line alpha"); n != 1 {
		t.Errorf("\"boot line alpha\" appears %d times after two arms, want 1:\n%s", n, got)
	}
}

// TestRotationKeepsExactlyWhatWasAskedFor. Keep+1 files, the live one at index
// 0, and nothing past the last generation left lying on the volume.
func TestRotationKeepsExactlyWhatWasAskedFor(t *testing.T) {
	dir := t.TempDir()
	r := New(500)
	// Small enough that each line rotates: 40 bytes is under one record.
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 40, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	for i := 1; i <= 8; i++ {
		fmt.Fprintf(r, "rotation line number %d padded out\n", i)
	}

	base := filepath.Join(dir, Name)
	if got := read(t, base); !strings.Contains(got, "number 8") {
		t.Errorf("the newest line is not in the file being written: %q", got)
	}
	if got := read(t, base+".1"); got == "" {
		t.Error("no first generation was kept")
	}
	if got := read(t, base+".2"); got == "" {
		t.Error("no second generation was kept")
	}
	if got := read(t, base+".3"); got != "" {
		t.Errorf("a third generation survived a Keep of 2: %q", got)
	}
	// The renamed file next to the live one has to be OLDER than it, or the
	// shift ran the wrong way round and reading the log backwards is nonsense.
	if strings.Contains(read(t, base+".1"), "number 8") {
		t.Error("the newest line ended up in .1 rather than in the live file")
	}

	st := r.FileStatus()
	if len(st.Generations) != 3 {
		t.Errorf("FileStatus lists %d generations, want the live file plus two", len(st.Generations))
	}
	if st.Bytes > st.MaxBytes {
		t.Errorf("the live file is %d bytes against a cap of %d", st.Bytes, st.MaxBytes)
	}
}

// TestKeepZeroKeepsOnlyTheLiveFile. Zero is a real answer and not a way of
// switching the log off - somebody with a small volume means it.
func TestKeepZeroKeepsOnlyTheLiveFile(t *testing.T) {
	dir := t.TempDir()
	r := New(500)
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 40, Keep: 0}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	for i := 1; i <= 6; i++ {
		fmt.Fprintf(r, "keep zero line number %d padded\n", i)
	}
	base := filepath.Join(dir, Name)
	if got := read(t, base); !strings.Contains(got, "number 6") {
		t.Errorf("the live file lost the newest line: %q", got)
	}
	if got := read(t, base+".1"); got != "" {
		t.Errorf("a generation was kept although Keep is 0: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files in the log folder, want just the one being written", len(entries))
	}
}

// TestARecordLargerThanTheCapDoesNotLoopTheRotation. Rotating an empty file for
// an oversized record would rename a nothing, write the record anyway, and do
// it again for the next line - throwing away every generation on the disk in
// the space of a second.
func TestARecordLargerThanTheCapDoesNotLoopTheRotation(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 32, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	r.Write([]byte("keep me, I am older than the giant\n"))
	r.Write([]byte(strings.Repeat("x", 400) + "\n"))

	base := filepath.Join(dir, Name)
	if got := read(t, base); !strings.Contains(got, "xxxx") {
		t.Errorf("the oversized record was not written at all: %q", got)
	}
	if got := read(t, base+".1"); !strings.Contains(got, "keep me") {
		t.Errorf("the line before the oversized one was thrown away: %q", got)
	}
	if got := read(t, base+".2"); got != "" {
		t.Errorf("the oversized record rotated more than once: .2 holds %q", got)
	}
}

// TestAFileThatCannotBeOpenedReportsItselfRatherThanFailingSilently. The sink
// is attached even when opening failed, because a nil sink is
// indistinguishable from the switch being off - and "off" is the one thing this
// state must not look like.
func TestAFileThatCannotBeOpenedReportsItselfRatherThanFailingSilently(t *testing.T) {
	base := t.TempDir()
	// A regular FILE where the log directory should be, so MkdirAll cannot
	// succeed on any platform.
	blocker := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := New(10)
	if err := r.OpenFile(FileOptions{Dir: filepath.Join(blocker, "logs"), MaxBytes: 1 << 20, Keep: 1}); err == nil {
		t.Fatal("OpenFile succeeded although the directory could not be created")
	}
	defer r.CloseFile()

	st := r.FileStatus()
	if st.Enabled {
		t.Error("FileStatus says the log is being written when nothing was ever opened")
	}
	if st.Problem == "" {
		t.Error("FileStatus reports no problem, so the card would show the same thing as \"switched off\"")
	}
	if st.Path == "" {
		t.Error("FileStatus reports no path, so nobody can be told where it tried to write")
	}
	if st.Generations == nil {
		t.Error("generations is nil, which encodes as JSON null and throws on the page's own .map")
	}

	// The ring itself must be untouched by any of this: the lines are still in
	// memory, still on stderr, and still in the diagnostics bundle.
	r.Write([]byte("still logging perfectly well\n"))
	if got := r.Lines(); len(got) != 1 || got[0] != "still logging perfectly well" {
		t.Errorf("a broken file sink cost the ring its lines: %v", got)
	}
}

// TestAWriteFailureSwitchesTheSinkOffAndSaysWhy, and does so without recursing.
//
// The handle is closed out from under the sink, which is what a volume going
// away looks like from inside a write. If the failure path ever reported itself
// through log.Printf it would re-enter (*Ring).Write on this very goroutine,
// with the ring's mutex already held, and this test would hang rather than fail
// - which is why the structural guard below exists as well.
func TestAWriteFailureSwitchesTheSinkOffAndSaysWhy(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 1}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	r.Write([]byte("before the volume went away\n"))

	// Reaching into the sink is the point: there is no portable way to make a
	// real disk fill up inside a unit test, and a test that could not reach the
	// failure would report nothing at all.
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	sink.mu.Lock()
	_ = sink.f.Close()
	sink.mu.Unlock()

	r.Write([]byte("after the volume went away\n"))

	st := r.FileStatus()
	if st.Enabled {
		t.Error("the sink still claims to be writing after a write it could not make")
	}
	if st.Problem == "" {
		t.Fatal("no problem was recorded, so the page would say the log is fine")
	}
	if !st.FreeKnown && st.FreeBytes != 0 {
		t.Error("freeBytes carries a figure while freeKnown is false")
	}

	// Still off on the next line, and still not logging about it.
	r.Write([]byte("and again\n"))
	if r.FileStatus().Enabled {
		t.Error("the sink switched itself back on")
	}
	if got := r.Lines(); len(got) != 3 {
		t.Errorf("the ring kept %d lines through the failure, want all 3", len(got))
	}
}

// TestTheFileSinkNeverLogs is a structural guard, and it guards the one bug in
// this feature that takes the whole instance down rather than degrading it.
//
// The sink is called from inside (*Ring).Write with the ring's mutex held and
// sits downstream of the tap this package installs on the standard logger. A
// logging call from any failure path in here deadlocks on a mutex the same
// goroutine already holds - and on a build where it somehow did not, it would
// fail, log, fail again and die on a full stack.
//
// Written as a source scan because the failure this prevents is somebody
// ADDING the line, not the line misbehaving; and parsed rather than grepped,
// because the first draft of this test matched the sentence you are reading
// and failed on a comment. It cannot see a call that reaches the logger
// indirectly through another package, which is why nothing on the write path
// calls into one - see file.go's own rule 1.
func TestTheFileSinkNeverLogs(t *testing.T) {
	for _, name := range []string{"file.go", "source.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == `"log"` {
				t.Errorf("%s imports the log package; a failure path downstream of the tap "+
					"that logs re-enters Ring.Write on a mutex it already holds", name)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "log" {
				t.Errorf("%s calls log.%s; a failure path downstream of the tap that logs "+
					"re-enters Ring.Write on a mutex it already holds", name, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestGenerationPathRefusesAnIndexItDoesNotKeep. The number arrives from a URL,
// and this is the only place it is range-checked - the download route never
// joins anything into a path itself.
func TestGenerationPathRefusesAnIndexItDoesNotKeep(t *testing.T) {
	dir := t.TempDir()
	r := New(10)
	if err := r.OpenFile(FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseFile()

	for _, ok := range []int{0, 1, 2} {
		if _, found := r.GenerationPath(ok); !found {
			t.Errorf("GenerationPath(%d) refused a generation this sink keeps", ok)
		}
	}
	for _, bad := range []int{-1, 3, 99} {
		if p, found := r.GenerationPath(bad); found {
			t.Errorf("GenerationPath(%d) answered %q for a generation that cannot exist", bad, p)
		}
	}

	nothing := New(10)
	if _, found := nothing.GenerationPath(0); found {
		t.Error("GenerationPath answered on a ring with no file armed at all")
	}
}

// TestRedactedDropsThePath. The diagnostics bundle is a file people attach to
// public bug reports, and a desktop data directory is
// C:\Users\<their real name>\AppData\... - the same argument
// TestDiagnosticsShipsNoPaths already pins for the store and settings paths.
func TestRedactedDropsThePath(t *testing.T) {
	st := FileState{Enabled: true, Path: `C:\Users\Someone\AppData\kl\logs\knightloader.log`, Bytes: 42}
	got := st.Redacted()
	if got.Path != "" {
		t.Errorf("Redacted().Path = %q, want empty", got.Path)
	}
	if !got.Enabled || got.Bytes != 42 {
		t.Errorf("Redacted() dropped more than the path: %+v", got)
	}
	if got.Generations == nil {
		t.Error("Redacted() left generations nil, which encodes as JSON null")
	}
}

// TestFileStatusWithNothingArmed. The card reads this on every install that has
// never switched the feature on, which is all of them on the day it ships.
func TestFileStatusWithNothingArmed(t *testing.T) {
	r := New(10)
	st := r.FileStatus()
	if st.Enabled || st.Problem != "" || st.Path != "" {
		t.Errorf("FileStatus on an unarmed ring = %+v, want a plain off state", st)
	}
	if st.Generations == nil {
		t.Error("generations is nil, which encodes as JSON null and throws on the page's own .map")
	}
}

// TestClosingWhatWasNeverOpened. Both mains call CloseFile from their shutdown
// path unconditionally, and the overwhelmingly common case is that nothing was
// ever armed.
func TestClosingWhatWasNeverOpened(t *testing.T) {
	r := New(10)
	if err := r.CloseFile(); err != nil {
		t.Errorf("CloseFile with nothing armed = %v, want nil", err)
	}
	r.Write([]byte("still fine\n"))
	if len(r.Lines()) != 1 {
		t.Error("closing an unarmed sink cost the ring a line")
	}
}
