package diskspace

// What can honestly be pinned about a call that reads the machine it runs on:
// not a byte count, but the CONTRACT around it. That the platform's answer
// reaches the caller, that a folder which does not exist yet is still answered
// for through its parent, and that a volume nobody can measure says so rather
// than saying zero.

import (
	"path/filepath"
	"testing"
)

// TestFreeAnswersForARealDirectory pins the two halves of the return value
// against each other. The number itself is whatever this machine has, so the
// assertion is on the shape: a platform that says it can answer must produce a
// non-zero one, because a temp dir on a volume with literally no bytes left
// could not have been created a line earlier.
func TestFreeAnswersForARealDirectory(t *testing.T) {
	got, ok := Free(t.TempDir())
	if ok != supported {
		t.Fatalf("Free answered ok=%v on a build whose supported is %v", ok, supported)
	}
	if !ok {
		return
	}
	if got == 0 {
		t.Error("Free reported 0 bytes for a directory the test framework had just created")
	}
}

// TestFreeAnswersForAFolderThatDoesNotExistYet is the case the guard actually
// asks about, and the one a naive statfs gets wrong: a download folder is made
// by whoever writes the first file into it, so at the moment somebody wants to
// know whether the next download fits, the folder is usually still missing.
// Answered through the nearest existing ancestor, which is on the same volume
// and is therefore the same answer.
func TestFreeAnswersForAFolderThatDoesNotExistYet(t *testing.T) {
	base := t.TempDir()
	here, ok := Free(base)
	if !ok {
		t.Skip("this platform cannot measure free space")
	}
	missing := filepath.Join(base, "not", "created", "yet")
	got, ok := Free(missing)
	if !ok {
		t.Fatal("Free gave up on a folder that does not exist instead of walking up to its parent")
	}
	// Compared loosely on purpose: the two readings are taken moments apart on
	// a live machine, and something else writing a file in between must not
	// fail this. A tenth of the volume would be an implausible amount to lose
	// between two statfs calls, and it is far short of the difference that
	// walking up to the WRONG volume would produce.
	if got < here/10 {
		t.Errorf("Free(%q) = %d, far below Free(%q) = %d: the walk up did not land on the same volume",
			missing, got, base, here)
	}
}

// TestFreeSaysSoWhenItCannotMeasure is the fail-open half written down as a
// test, because it is the half that will be tempting to "fix" into a zero one
// day. A path whose volume root does not exist has no filesystem to ask, and
// the caller has to be able to tell that apart from a full disk.
func TestFreeSaysSoWhenItCannotMeasure(t *testing.T) {
	// A path under a root that cannot resolve on either platform family: on
	// Windows an unmapped drive letter, on unix an absolute path whose walk up
	// ends at "/" - which does exist, so the unix half of this is asserted
	// through the Windows-shaped path only. Naming a drive letter on unix is
	// simply a relative path fragment, so the walk ends at the working
	// directory and answers true; that is why the assertion below accepts an
	// answer and only refuses a WRONG one.
	got, ok := Free(`\\?\Q:\nothing\here`)
	if ok && got == 0 {
		t.Error("Free reported ok with 0 bytes free: a volume that cannot be measured must answer ok=false, never a zero that reads as a full disk")
	}
}
