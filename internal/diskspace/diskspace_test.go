package diskspace

import (
	"path/filepath"
	"testing"
)

// TestFreeAnswersForARealDirectory checks the shape of the answer, since the
// number depends on the machine: a supported platform must report more than
// zero for a directory that was just created.
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
	// Loose, since other writers change free space between the two calls; a
	// different volume would differ far more.
	if got < here/10 {
		t.Errorf("Free(%q) = %d, far below Free(%q) = %d: the walk up did not land on the same volume",
			missing, got, base, here)
	}
}

// TestFreeSaysSoWhenItCannotMeasure uses an unmapped Windows drive. On unix
// the same string is a relative path that resolves to the working directory,
// so only a wrong answer (ok with zero) fails.
func TestFreeSaysSoWhenItCannotMeasure(t *testing.T) {
	got, ok := Free(`\\?\Q:\nothing\here`)
	if ok && got == 0 {
		t.Error("Free reported ok with 0 bytes free: a volume that cannot be measured must answer ok=false, never a zero that reads as a full disk")
	}
}

// TestUsedIsWhatIsOccupiedAndNotWhatThisProcessCannotHave uses figures where
// the reserve is visible: 90 blocks occupied, 5 reserved, 5 ours. Used must
// be 90, not 95.
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

// TestSpaceFromCannotReportMoreUsedThanTheVolumeHolds covers a filesystem
// reporting more free blocks than it has, which unsigned subtraction would
// turn into exabytes.
func TestSpaceFromCannotReportMoreUsedThanTheVolumeHolds(t *testing.T) {
	if sp := spaceFrom(50, 200, 100); sp.Used != 0 {
		t.Errorf("Used = %d for a volume that says it has more free than it holds, want 0", sp.Used)
	}
}

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

// TestUsageAnswersForAFolderThatDoesNotExistYet compares totals, which do not
// move while the test runs.
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

// TestUsageSaysSoWhenItCannotMeasure uses the path from
// TestFreeSaysSoWhenItCannotMeasure.
func TestUsageSaysSoWhenItCannotMeasure(t *testing.T) {
	sp, ok := Usage(`\\?\Q:\nothing\here`)
	if ok && sp.Total == 0 {
		t.Error("Usage reported ok for a volume of no size: one that cannot be measured must answer ok=false, never zeroes that read as a disk")
	}
}
