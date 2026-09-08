package fileowner

// The rule this package exists for, and the two ways it is easy to get wrong.
//
// MOST OF THIS FILE DRIVES judge WITH CONSTRUCTED MEASUREMENTS RATHER THAN WITH
// A REAL FOLDER, and that is the point rather than a shortcut. The failure being
// tested - a folder this process CAN write to whose files still come out
// belonging to somebody else - needs a share owned by another account, and a
// test that could only reach it on a NAS is a test that runs nowhere. Splitting
// the measurement from the judgement means the judgement is pinned on every
// platform, Windows included, which is also the only reason this file says
// anything at all on the machine most of it is written on.
//
// The handful of tests that do touch the disk are the ones about the SIDE
// EFFECTS - that a check leaves nothing behind, that a folder which is not there
// is not created - because those are true on every filesystem and are the part
// that would quietly litter somebody's download share.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// measured is a probe as it comes back from a folder that this process could
// write to perfectly well: the file and the sub-folder were both created, and
// every mode is the ordinary one a umask of 022 produces. Each test then breaks
// exactly one thing about it, so what is being judged is never in doubt.
func measured() Probe {
	return Probe{
		Dir: "/downloads", Exists: true, Known: true,
		DirUID: 99, DirGID: 100, DirUser: "nobody", DirGroup: "users", DirMode: 0o755,
		FileUID: 99, FileGID: 100, FileUser: "nobody", FileGroup: "users", FileMode: 0o644,
		SubdirMode: 0o755,
	}
}

// TestAWritableFolderStillFailsWhenTheFilesComeOutOwnedByTheWrongAccount is the
// bug this whole package was written for, and the one an ordinary write probe
// cannot see. settings.Validate writes .knightloader-write-test into the folder
// and removes it again; on a share mounted through shfs that write SUCCEEDS
// while the process is uid 1000 and the share is 99:100, and the file simply
// lands owned by 1000. Every check built on "could I write" then reports green
// while the media server next door goes on reporting an empty library.
//
// So the assertion is deliberately about a probe with NO error in it at all.
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

// TestTheGroupOfTheFileCountsAndNotOnlyItsOwner covers the half of the same
// question a set-group-id folder produces: the uid matches because this process
// created the file, and the GROUP is the folder's rather than the process's, or
// the other way round. Comparing uids alone would call that a match.
func TestTheGroupOfTheFileCountsAndNotOnlyItsOwner(t *testing.T) {
	p := measured()
	p.FileGID, p.FileGroup = 1000, "knight"
	if got := judge(p); got != VerdictOwnerMismatch {
		t.Errorf("a file in the right hands but the wrong group was judged %q, not %q", got, VerdictOwnerMismatch)
	}
}

// TestTheSubFolderIsJudgedBeforeTheFile is the trap a file-only probe walks
// into. With a umask of 077 the FILE can be 0640 and perfectly readable by the
// group while the sub-folder it sits in is 0700, and nothing gets inside that
// folder whatever the file allows. Every download in this app lands in a
// sub-folder (the per-package level, and the extractor's own output directory),
// so the folder is the answer that has to be reported first.
func TestTheSubFolderIsJudgedBeforeTheFile(t *testing.T) {
	p := measured()
	p.SubdirMode = 0o700
	p.FileMode = 0o600
	p.FileUID = 1000 // and the owner is wrong too, so the ORDER is what is tested
	if got := judge(p); got != VerdictDirUnreadable {
		t.Errorf("a sub-folder nothing can enter was judged %q, not %q; the smaller problem was reported and the door is still locked", got, VerdictDirUnreadable)
	}
}

// TestASubFolderThatCanBeListedButNotEnteredStillFails pins the second half of
// the 0o050 mask. Read without execute lists the names and opens nothing, which
// on a media server is the worst of the three states: the scan finds files and
// every one of them fails.
func TestASubFolderThatCanBeListedButNotEnteredStillFails(t *testing.T) {
	p := measured()
	p.SubdirMode = 0o745 // group r, no group x
	if got := judge(p); got != VerdictDirUnreadable {
		t.Errorf("a sub-folder the group may list but not enter was judged %q, not %q", got, VerdictDirUnreadable)
	}
}

// TestAFileWithNoGroupReadIsReportedEvenWhenTheOwnerIsRight is the umask half on
// its own: everything belongs to the right account and nothing else can open it.
func TestAFileWithNoGroupReadIsReportedEvenWhenTheOwnerIsRight(t *testing.T) {
	p := measured()
	p.FileMode = 0o600
	if got := judge(p); got != VerdictGroupUnreadable {
		t.Errorf("a file only its owner can open was judged %q, not %q", got, VerdictGroupUnreadable)
	}
}

// TestAHealthyFolderIsNotReportedAsAProblem is the other direction, and it
// matters as much: a check that cried wolf on the ordinary 99:100 share with a
// umask of 022 would be switched off by everyone within a week.
func TestAHealthyFolderIsNotReportedAsAProblem(t *testing.T) {
	if got := judge(measured()); got != VerdictOK {
		t.Errorf("the ordinary healthy folder was judged %q, not %q", got, VerdictOK)
	}
}

// TestAPlatformWithNoOwnersSaysSoRatherThanComparingTwoZeroes is the third
// answer, in this package's own currency. Without the Known check the Windows
// build compares uid 0 with uid 0, finds them equal, and pronounces a folder
// healthy on the strength of two numbers that were never read - which is a
// confident wrong answer, the one thing a readout must never produce.
func TestAPlatformWithNoOwnersSaysSoRatherThanComparingTwoZeroes(t *testing.T) {
	p := Probe{Dir: `C:\Downloads`, Exists: true} // Known false, every number zero
	if got := judge(p); got != VerdictUnknown {
		t.Errorf("a platform with no file owners was judged %q, not %q", got, VerdictUnknown)
	}
}

// TestTheSetGroupIdBitSurvivesIntoTheReadout. A set-group-id download folder is
// one of the two reasons this package measures instead of computing - it is
// what hands a new file a group its creator is not in - so a readout that
// dropped the bit would be describing a different folder than the one on disk.
// os.FileMode keeps it in a high bit of its own and Perm() drops it outright,
// which is the easy mistake here.
func TestTheSetGroupIdBitSurvivesIntoTheReadout(t *testing.T) {
	if got := permBits(os.ModeDir | os.ModeSetgid | 0o775); got != 0o2775 {
		t.Errorf("a set-group-id folder came out as %s, not %s", Octal(got), Octal(0o2775))
	}
	if got := permBits(0o644); got != 0o644 {
		t.Errorf("an ordinary file came out as %s, not 0644", Octal(got))
	}
}

// TestOctalIsPaddedSoTheColumnsLineUp. Four digits always: a page that printed
// "644" beside "2775" reads as two numbers of different kinds.
func TestOctalIsPaddedSoTheColumnsLineUp(t *testing.T) {
	for mode, want := range map[uint32]string{0o644: "0644", 0o22: "0022", 0o2775: "2775", 0: "0000"} {
		if got := Octal(mode); got != want {
			t.Errorf("Octal(%o) = %q, want %q", mode, got, want)
		}
	}
}

// TestACheckLeavesNothingBehind is the promise this package makes to somebody's
// download share. It writes into folders that are visible over SMB and watched
// by media scanners, so a leaked probe file is not untidiness - it is a stray
// entry in a library, or a file an operator finds later and dares not delete.
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

// TestAMissingFolderIsReportedAndNotCreated. settings.Validate already
// MkdirAll's every configured folder as it is saved, so a folder that is not
// there when this runs is a folder that went away - a mount that did not come
// up, a share that was renamed. Creating it here would hide precisely that, and
// would put a directory on disk as a side effect of a check nobody asked to
// change anything with.
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

// TestAFileIsMeasuredAsThisProcessAndNotAsThePackageThinksItShouldBe is the one
// assertion that needs a real kernel: the probe file has to come back owned by
// whoever this process actually is. It is what proves the measurement is a
// measurement - a Check that returned invented numbers would pass every test
// above this one.
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

// TestTheAdviceNamesTheFolderTheOwnerAndTheCommandToRun is the whole value of
// the boot line. Without it the operator sees `start: permission denied` with no
// path, no owner and no uid, and the fix is one chown they have no way to guess.
// The command has to be complete enough to paste.
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

// TestTheAdviceDoesNotInventAChownWhereThereIsNothingToChown. On the Windows
// desktop build the fix is an ACL, and a chown line there sends somebody to a
// command that does not exist on their machine.
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

// TestAnUnnamedAccountIsStillFullyDescribed. A container started with
// --user 99:100 runs as a uid nothing in the image's /etc/passwd mentions, and
// that is the most common configuration this feature reports on. The numbers
// alone are the answer; a line that fell back to "(unknown)" would be dropping
// the part that goes into the command.
func TestAnUnnamedAccountIsStillFullyDescribed(t *testing.T) {
	if got := describe(99, 100, "", ""); got != "99:100" {
		t.Errorf("describe(99, 100, \"\", \"\") = %q, want \"99:100\"", got)
	}
	if got := describe(99, 100, "nobody", ""); got != "99:100 (nobody:100)" {
		t.Errorf("a half-named account came out as %q", got)
	}
}

// TestAWritableFolderProducesNoBootLineAtAll. The line is a diagnosis and not a
// status report: on every healthy install it must say nothing, or it becomes one
// more line nobody reads in a log people only open when something is wrong.
func TestAWritableFolderProducesNoBootLineAtAll(t *testing.T) {
	if line, ok := Advise(t.TempDir()); ok {
		t.Errorf("a perfectly writable folder produced a warning: %s", line)
	}
	if line, ok := Advise(filepath.Join(t.TempDir(), "not-there-yet")); ok {
		t.Errorf("a folder that does not exist yet produced a warning instead of leaving it to MkdirAll: %s", line)
	}
}
