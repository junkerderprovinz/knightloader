package fileowner

// Most tests drive judge with constructed measurements, since the real case (a
// share owned by another account) only exists on a NAS; this way the rule is
// tested on every platform. The tests that touch the disk cover side effects:
// nothing left behind, nothing created.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// measured is a probe from a healthy, writable folder with a 022 umask. Each
// test breaks one thing about it.
func measured() Probe {
	return Probe{
		Dir: "/downloads", Exists: true, Known: true,
		DirUID: 99, DirGID: 100, DirUser: "nobody", DirGroup: "users", DirMode: 0o755,
		FileUID: 99, FileGID: 100, FileUser: "nobody", FileGroup: "users", FileMode: 0o644,
		SubdirMode: 0o755,
	}
}

// TestAWritableFolderStillFailsWhenTheFilesComeOutOwnedByTheWrongAccount is the
// case a write probe passes: the write succeeds, but the file belongs to the
// process (1000) rather than the share (99:100).
func TestAWritableFolderStillFailsWhenTheFilesComeOutOwnedByTheWrongAccount(t *testing.T) {
	p := measured()
	p.FileUID, p.FileGID = 1000, 1000
	p.FileUser, p.FileGroup = "knight", "knight"
	if got := judge(p); got != VerdictOwnerMismatch {
		t.Errorf("a folder owned by %d:%d whose files come out owned by %d:%d was judged %q, not %q; "+
			"this is exactly the case a write probe passes and the library stays broken",
			p.DirUID, p.DirGID, p.FileUID, p.FileGID, got, VerdictOwnerMismatch)
	}
}

// TestTheGroupOfTheFileCountsAndNotOnlyItsOwner covers a matching uid with a
// different group, as a set-group-id folder produces.
func TestTheGroupOfTheFileCountsAndNotOnlyItsOwner(t *testing.T) {
	p := measured()
	p.FileGID, p.FileGroup = 1000, "knight"
	if got := judge(p); got != VerdictOwnerMismatch {
		t.Errorf("a file in the right hands but the wrong group was judged %q, not %q", got, VerdictOwnerMismatch)
	}
}

// TestTheSubFolderIsJudgedBeforeTheFile: downloads land in sub-folders, so a
// sub-folder nobody can enter is reported ahead of the file's problems.
func TestTheSubFolderIsJudgedBeforeTheFile(t *testing.T) {
	p := measured()
	p.SubdirMode = 0o700
	p.FileMode = 0o600
	p.FileUID = 1000 // wrong owner too, so the order is what is tested
	if got := judge(p); got != VerdictDirUnreadable {
		t.Errorf("a sub-folder nothing can enter was judged %q, not %q; the smaller problem was reported and the door is still locked", got, VerdictDirUnreadable)
	}
}

// TestASubFolderThatCanBeListedButNotEnteredStillFails: read without execute
// lists names but opens nothing.
func TestASubFolderThatCanBeListedButNotEnteredStillFails(t *testing.T) {
	p := measured()
	p.SubdirMode = 0o745 // group r, no group x
	if got := judge(p); got != VerdictDirUnreadable {
		t.Errorf("a sub-folder the group may list but not enter was judged %q, not %q", got, VerdictDirUnreadable)
	}
}

func TestAFileWithNoGroupReadIsReportedEvenWhenTheOwnerIsRight(t *testing.T) {
	p := measured()
	p.FileMode = 0o600
	if got := judge(p); got != VerdictGroupUnreadable {
		t.Errorf("a file only its owner can open was judged %q, not %q", got, VerdictGroupUnreadable)
	}
}

func TestAHealthyFolderIsNotReportedAsAProblem(t *testing.T) {
	if got := judge(measured()); got != VerdictOK {
		t.Errorf("the ordinary healthy folder was judged %q, not %q", got, VerdictOK)
	}
}

// TestAPlatformWithNoOwnersSaysSoRatherThanComparingTwoZeroes: without the
// Known check two unread zeros would compare equal and read as healthy.
func TestAPlatformWithNoOwnersSaysSoRatherThanComparingTwoZeroes(t *testing.T) {
	p := Probe{Dir: `C:\Downloads`, Exists: true} // Known false, every number zero
	if got := judge(p); got != VerdictUnknown {
		t.Errorf("a platform with no file owners was judged %q, not %q", got, VerdictUnknown)
	}
}

// TestTheSetGroupIdBitSurvivesIntoTheReadout: FileMode.Perm drops setgid,
// which explains a new file's group.
func TestTheSetGroupIdBitSurvivesIntoTheReadout(t *testing.T) {
	if got := permBits(os.ModeDir | os.ModeSetgid | 0o775); got != 0o2775 {
		t.Errorf("a set-group-id folder came out as %s, not %s", Octal(got), Octal(0o2775))
	}
	if got := permBits(0o644); got != 0o644 {
		t.Errorf("an ordinary file came out as %s, not 0644", Octal(got))
	}
}

func TestOctalIsPaddedSoTheColumnsLineUp(t *testing.T) {
	for mode, want := range map[uint32]string{0o644: "0644", 0o22: "0022", 0o2775: "2775", 0: "0000"} {
		if got := Octal(mode); got != want {
			t.Errorf("Octal(%o) = %q, want %q", mode, got, want)
		}
	}
}

// TestACheckLeavesNothingBehind: the probe writes into folders visible over
// SMB and watched by media scanners.
func TestACheckLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	if p := Check(dir); p.Verdict == VerdictNotWritable || p.Verdict == VerdictMissing {
		t.Fatalf("a fresh temporary directory was judged %q (%s); the probe itself is broken, not the folder", p.Verdict, p.Detail)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("the check left %v in the folder it measured", names)
	}
}

func TestAMissingFolderIsReportedAndNotCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-mounted")
	p := Check(dir)
	if p.Verdict != VerdictMissing {
		t.Errorf("a folder that is not there was judged %q, not %q", p.Verdict, VerdictMissing)
	}
	if p.Exists {
		t.Error("the probe says the folder exists")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the check created the folder it was asked to measure")
	}
}

// TestAFileIsMeasuredAsThisProcessAndNotAsThePackageThinksItShouldBe needs a
// real kernel: the probe file must come back owned by this process, which
// proves the numbers are measured.
func TestAFileIsMeasuredAsThisProcessAndNotAsThePackageThinksItShouldBe(t *testing.T) {
	if !supported {
		t.Skip("this build has no file owners to read; internal/api and the CI runner cover the branch that does")
	}
	me := Who()
	if !me.Known {
		t.Fatal("a build with owner support answered that it does not know who it is")
	}
	p := Check(t.TempDir())
	if !p.Known {
		t.Fatalf("the probe could not read owners on a platform that has them: %s", p.Detail)
	}
	if p.FileUID != me.UID {
		t.Errorf("the probe file came out owned by uid %d, but this process is uid %d", p.FileUID, me.UID)
	}
	if p.FileMode == 0 || p.SubdirMode == 0 {
		t.Errorf("the probe reported mode %s for the file and %s for the sub-folder; nothing was actually measured",
			Octal(p.FileMode), Octal(p.SubdirMode))
	}
}

// TestTheAdviceNamesTheFolderTheOwnerAndTheCommandToRun: the chown line has to
// be complete enough to paste.
func TestTheAdviceNamesTheFolderTheOwnerAndTheCommandToRun(t *testing.T) {
	folder := Owner{Path: "/data", Exists: true, IsDir: true, Known: true, UID: 0, GID: 0, User: "root", Group: "root", Mode: 0o755}
	me := Identity{Known: true, UID: 1000, GID: 1000, User: "knight", Group: "knight"}
	got := advice("/data", folder, me, errors.New("permission denied"))
	for _, want := range []string{"/data", "0:0 (root:root)", "1000:1000 (knight:knight)", "chown -R 1000:1000 /data", "permission denied"} {
		if !strings.Contains(got, want) {
			t.Errorf("the boot line does not carry %q:\n%s", want, got)
		}
	}
}

func TestTheAdviceDoesNotInventAChownWhereThereIsNothingToChown(t *testing.T) {
	folder := Owner{Path: `C:\data`, Exists: true, IsDir: true}
	got := advice(`C:\data`, folder, Identity{}, errors.New("Access is denied."))
	if strings.Contains(got, "chown") {
		t.Errorf("a platform with no file owners was told to run chown:\n%s", got)
	}
	if !strings.Contains(got, `C:\data`) || !strings.Contains(got, "Access is denied.") {
		t.Errorf("the line names neither the folder nor the reason:\n%s", got)
	}
}

// TestAnUnnamedAccountIsStillFullyDescribed: --user 99:100 has no passwd
// entry, and the numbers are what the command needs.
func TestAnUnnamedAccountIsStillFullyDescribed(t *testing.T) {
	if got := describe(99, 100, "", ""); got != "99:100" {
		t.Errorf("describe(99, 100, \"\", \"\") = %q, want \"99:100\"", got)
	}
	if got := describe(99, 100, "nobody", ""); got != "99:100 (nobody:100)" {
		t.Errorf("a half-named account came out as %q", got)
	}
}

// TestAWritableFolderProducesNoBootLineAtAll: the line is only for a problem.
func TestAWritableFolderProducesNoBootLineAtAll(t *testing.T) {
	if line, ok := Advise(t.TempDir()); ok {
		t.Errorf("a perfectly writable folder produced a warning: %s", line)
	}
	if line, ok := Advise(filepath.Join(t.TempDir(), "not-there-yet")); ok {
		t.Errorf("a folder that does not exist yet produced a warning instead of leaving it to MkdirAll: %s", line)
	}
}
