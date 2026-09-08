package startupcheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// entries is what is actually lying in a directory, so a test can say "and
// nothing was created" rather than only "the verdict looked right". Every one of
// the failures this package exists to prevent is a failure of the second kind:
// a check that created what it was measuring and then reported that it was
// there.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	out := make([]string, 0, len(des))
	for _, de := range des {
		out = append(out, de.Name())
	}
	return out
}

// TestFolderAtBootLooksAndWritesNothing is the owner's decision, pinned.
//
// The boot pass stats and stops. On Unraid a write into /mnt/user/... spins up
// the array disk holding that share, so a probe into every configured folder at
// every start would wake a sleeping array on every container restart. If this
// test ever goes red because a probe file appeared, the feature has quietly
// grown back the behaviour that was ruled out.
func TestFolderAtBootLooksAndWritesNothing(t *testing.T) {
	dir := t.TempDir()

	c := Folder(context.Background(), dir, RoleDownloads, false, time.Second)

	if c.Verdict != VerdictOK {
		t.Errorf("verdict = %q, want %q for a folder that is there", c.Verdict, VerdictOK)
	}
	if c.Code != "" {
		t.Errorf("code = %q, want none", c.Code)
	}
	if c.Probed {
		t.Error("probed = true after a pass that was not allowed to write")
	}
	if c.Measured != dir {
		t.Errorf("measured = %q, want the folder itself %q", c.Measured, dir)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("the boot pass left %v in the folder; it must write nothing at all", got)
	}
}

// TestFolderProbeWritesAndTakesItBack is the other half: the human press does
// write, and it does not leave anything behind.
func TestFolderProbeWritesAndTakesItBack(t *testing.T) {
	dir := t.TempDir()

	c := Folder(context.Background(), dir, RoleDownloads, true, time.Second)

	if c.Verdict != VerdictOK || c.Code != "" {
		t.Errorf("verdict = %q code = %q, want an ok row for a writable folder", c.Verdict, c.Code)
	}
	if !c.Probed {
		t.Error("probed = false after a pass that wrote the test file")
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("the probe file was left behind: %v", got)
	}
}

// TestProbeNameHidesFromTheWatchFolderAndIsUniquePerPass covers the two
// properties of the file name that are not cosmetic.
//
// The dot: internal/watch/poller.go picks up any .txt/.crawljob in the watched
// folder that does NOT start with one, so a visibly named probe dropped there is
// read as a link list and staged as downloads.
//
// The uniqueness: both probes this app already has use fixed names, and two
// instances sharing one share would each delete the other's and both report
// failure.
func TestProbeNameHidesFromTheWatchFolderAndIsUniquePerPass(t *testing.T) {
	a, b := probeName(), probeName()
	if !strings.HasPrefix(a, ".") {
		t.Errorf("probe name %q does not start with a dot, so the watch poller would read it", a)
	}
	if a == b {
		t.Errorf("two passes produced the same probe name %q; one instance would delete the other's file", a)
	}
}

// TestFolderMissingAnswersWithTheNearestExistingParentAndCreatesNothing is trap
// number one.
//
// settings.Validate is the obvious thing to reuse and it MkdirAll's first: on a
// box whose share did not mount that creates the path inside the container's own
// writable layer, the write test passes, the report says "fine", and every
// download lands inside the container to be destroyed by the next image pull.
// The report would have laundered the exact failure it exists to catch.
func TestFolderMissingAnswersWithTheNearestExistingParentAndCreatesNothing(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "share", "media", "downloads")

	c := Folder(context.Background(), missing, RoleDownloads, true, time.Second)

	if c.Verdict != VerdictWarn {
		t.Errorf("verdict = %q, want %q - a download folder that does not exist yet is the normal state of a fresh install", c.Verdict, VerdictWarn)
	}
	if c.Code != CodeMissing {
		t.Errorf("code = %q, want %q", c.Code, CodeMissing)
	}
	if c.Measured != base {
		t.Errorf("measured = %q, want the nearest existing folder above it %q - that substitution is the sentence that says a mount is down", c.Measured, base)
	}
	if got := entries(t, base); len(got) != 0 {
		t.Errorf("something was created under the missing path: %v", got)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Errorf("%s exists after the check ran; nothing here may ever create a folder", missing)
	}
}

// TestFolderRefusesARelativePath. It cannot be measured from here at all: it
// resolves against whatever the process's working directory happens to be, which
// is the same reason settings.sanitizePaths refuses to store one.
func TestFolderRefusesARelativePath(t *testing.T) {
	c := Folder(context.Background(), filepath.Join("downloads", "media"), RoleDownloads, true, time.Second)
	if c.Verdict != VerdictFail || c.Code != CodeRelative {
		t.Errorf("verdict = %q code = %q, want fail/%s", c.Verdict, c.Code, CodeRelative)
	}
}

// TestFolderNotADir: a file where a folder should be is its own answer, and not
// a permission problem.
func TestFolderNotADir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "downloads")
	if err := os.WriteFile(file, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := Folder(context.Background(), file, RoleDownloads, true, time.Second)
	if c.Verdict != VerdictFail || c.Code != CodeNotADir {
		t.Errorf("verdict = %q code = %q, want fail/%s", c.Verdict, c.Code, CodeNotADir)
	}
}

// TestFolderDeniedIsItsOwnCode. "There and unwritable" and "not there" send
// somebody to two completely different places, so they may never share a code.
//
// Unix only, and skipped as root: a chmod means nothing to uid 0, so the check
// would pass for a reason that has nothing to do with the code under test - a
// test that cannot fail is worse than no test.
func TestFolderDeniedIsItsOwnCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not deny a write on Windows the way this test needs")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the mode this test sets")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	// Put back before the harness tries to delete it, or t.TempDir's cleanup
	// fails on a directory it may not empty.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	c := Folder(context.Background(), locked, RoleDownloads, true, time.Second)
	if c.Verdict != VerdictFail || c.Code != CodeDenied {
		t.Errorf("verdict = %q code = %q, want fail/%s", c.Verdict, c.Code, CodeDenied)
	}
	if c.Err == "" {
		t.Error("err is empty; the system's own message is the evidence somebody quotes in a bug report")
	}
}

// TestFolderTimeoutIsAFindingRatherThanASilence. A folder on a mount that has
// gone away blocks in a syscall nothing can interrupt, so the pass walks away
// from it - and the row it leaves has to SAY that, because an absent row is
// invisible and a timeout row is a finding.
//
// Driven by an expired context rather than by a real dead mount, which is a
// state the program genuinely reaches: Run gives the whole pass a deadline, and
// every folder after the one that ate it arrives here exactly like this.
func TestFolderTimeoutIsAFindingRatherThanASilence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := Folder(ctx, filepath.Join(t.TempDir(), "somewhere"), RoleCategory, true, time.Second)
	if c.Code != CodeTimeout {
		t.Errorf("code = %q, want %q", c.Code, CodeTimeout)
	}
	if c.Verdict != VerdictWarn {
		t.Errorf("verdict = %q, want %q - a spun-down array disk answers slowly and is not broken", c.Verdict, VerdictWarn)
	}
	if c.Subject == "" {
		t.Error("a timed-out row with no subject names nothing; the point of the row is which folder it was")
	}
}

// TestDeepestExisting walks up and stops at the first real directory, which is
// what turns "/mnt/user/media is missing" into "and /mnt is the nearest thing
// above it that is there".
func TestDeepestExisting(t *testing.T) {
	base := t.TempDir()
	if got := DeepestExisting(base); got != base {
		t.Errorf("DeepestExisting(%q) = %q, want the folder itself", base, got)
	}
	deep := filepath.Join(base, "a", "b", "c")
	if got := DeepestExisting(deep); got != base {
		t.Errorf("DeepestExisting(%q) = %q, want %q", deep, got, base)
	}
}
