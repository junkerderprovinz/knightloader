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

// TestUsedIsWhatIsOccupiedAndNotWhatThisProcessCannotHave is the one assertion
// in this file that does not depend on the machine, and it is the one worth
// having most: the used figure is the single thing in this package that cannot
// be checked against reality by looking at a real volume, because on a disk
// with no reserve and no quota the right answer and the wrong one are the same
// number.
//
// The numbers below are the shape that tells them apart. Ninety blocks are
// occupied, five more are held back for somebody who is not us, and five are
// ours to write - so free and used add up to less than the volume, and the
// reserve is counted as neither. Deriving used as total minus free instead
// makes it 95 and hands the reader five blocks of files that do not exist.
func TestUsedIsWhatIsOccupiedAndNotWhatThisProcessCannotHave(t *testing.T) {
	const (
		ours    = 5
		anyones = 10
		total   = 100
	)
	sp := spaceFrom(ours, anyones, total)
	if sp.Free != ours {
		t.Errorf("Free = %d, want %d: free is what this process may write, never what the volume has left", sp.Free, ours)
	}
	if sp.Total != total {
		t.Errorf("Total = %d, want %d", sp.Total, total)
	}
	if sp.Used != total-anyones {
		t.Errorf("Used = %d, want %d: the reserve between what anybody has left and what we have left is not somebody's files", sp.Used, total-anyones)
	}
	if sp.Free+sp.Used >= sp.Total {
		t.Errorf("Free + Used = %d, which is not below Total = %d: the two are being derived from each other, and the reserve has been painted onto one of them",
			sp.Free+sp.Used, sp.Total)
	}
}

// TestSpaceFromCannotReportMoreUsedThanTheVolumeHolds covers the answer nobody
// asks for: a filesystem reporting more free blocks than it has. Unsigned
// arithmetic turns that subtraction into eighteen exabytes of files, which is
// the same class of failure the negative f_bavail conversion in the unix file
// guards against.
func TestSpaceFromCannotReportMoreUsedThanTheVolumeHolds(t *testing.T) {
	if sp := spaceFrom(50, 200, 100); sp.Used != 0 {
		t.Errorf("Used = %d for a volume that says it has more free than it holds, want 0", sp.Used)
	}
}

// TestUsageAnswersTheThreeFiguresForARealDirectory is the shape assertion for
// the second entry point, pinned the same way Free's is: the byte counts are
// whatever this machine has, so what is checked is that all three arrived and
// that they describe one volume rather than three unrelated numbers.
func TestUsageAnswersTheThreeFiguresForARealDirectory(t *testing.T) {
	sp, ok := Usage(t.TempDir())
	if ok != supported {
		t.Fatalf("Usage answered ok=%v on a build whose supported is %v", ok, supported)
	}
	if !ok {
		if sp != (Space{}) {
			t.Errorf("Usage answered ok=false with %+v: a platform that cannot measure has to hand back nothing, or the zeroes get drawn as a disk", sp)
		}
		return
	}
	if sp.Total == 0 {
		t.Error("Usage reported a volume of no size at all for a directory the test framework had just created")
	}
	if sp.Free == 0 {
		t.Error("Usage reported 0 bytes free for a directory the test framework had just created")
	}
	if sp.Used == 0 {
		t.Error("Usage reported that nothing at all occupies the volume the temp directory was created on")
	}
	if sp.Free+sp.Used > sp.Total {
		t.Errorf("Free %d + Used %d is more than the volume holds (%d)", sp.Free, sp.Used, sp.Total)
	}
}

// TestUsageAnswersForAFolderThatDoesNotExistYet is Free's own walk-up case
// asked of the three figures, and the assertion is sharper here than it can be
// there: free bytes move while the test runs, but a volume's SIZE does not, so
// a total that differs at all is a walk that landed somewhere else entirely.
func TestUsageAnswersForAFolderThatDoesNotExistYet(t *testing.T) {
	base := t.TempDir()
	here, ok := Usage(base)
	if !ok {
		t.Skip("this platform cannot measure free space")
	}
	missing := filepath.Join(base, "not", "created", "yet")
	got, ok := Usage(missing)
	if !ok {
		t.Fatal("Usage gave up on a folder that does not exist instead of walking up to its parent")
	}
	if got.Total != here.Total {
		t.Errorf("Usage(%q) describes a volume of %d bytes and Usage(%q) one of %d: the walk up did not land on the same volume",
			missing, got.Total, base, here.Total)
	}
}

// TestUsageSaysSoWhenItCannotMeasure is the fail-open half again, and it has to
// be written for this entry point separately: a readout is the caller most
// likely to be handed three zeroes and to draw them, and three zeroes are a
// volume of no size with nothing on it and nothing left, which is not a state
// any disk is in. See TestFreeSaysSoWhenItCannotMeasure for why the path below
// is shaped the way it is.
func TestUsageSaysSoWhenItCannotMeasure(t *testing.T) {
	sp, ok := Usage(`\\?\Q:\nothing\here`)
	if ok && sp.Total == 0 {
		t.Error("Usage reported ok for a volume of no size: one that cannot be measured must answer ok=false, never zeroes that read as a disk")
	}
}
