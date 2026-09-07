package reclaim

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
)

// put writes one file and answers its length, which is what every request in
// this file needs as its expected size.
func put(t *testing.T, dir, name, body string) int64 {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return int64(len(body))
}

func sha256Of(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// alwaysFinished is the record tier saying yes to everything, so a test can be
// about what the tier is allowed to decide rather than about a history table.
func alwaysFinished(string, int64) bool { return true }

// TestARightSizedFileWithTheWrongBytesIsNeverSettled is the promise this whole
// package rests on, and it is worth stating why it is not a corner case.
//
// The embedded engine's download library creates its destination file at the
// full final length before it fetches a byte of it (gopeed v1.9.3,
// internal/controller's Touch: os.Create then os.Truncate(name, size)). So a
// download that died at two per cent leaves a file at the final name with
// exactly the right number of bytes in it, and every cheap test in this
// package would call that a finished download. The checksum is the only thing
// that can tell them apart, and when it says no the answer has to be "fetch it
// again" no matter how trusting the instance has been told to be. One download
// too many costs line time; the other reading costs somebody a file that will
// not open and a row that says it is fine.
func TestARightSizedFileWithTheWrongBytesIsNeverSettled(t *testing.T) {
	for _, trust := range Modes() {
		t.Run(string(trust), func(t *testing.T) {
			dir := t.TempDir()
			size := put(t, dir, "movie.mkv", "................")
			// The hash of what the file SHOULD have been. Same length, so
			// nothing cheaper than a hash can see the difference.
			want := sha256Of("the real film!!!")

			got := Options{Trust: trust, Finished: alwaysFinished}.Scan(Request{
				TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size, ExpectedHash: want,
			})
			if got.Verdict != Mismatch {
				t.Fatalf("verdict = %q (%s), want mismatch: the file is the right length and the wrong file", got.Verdict, got.Detail)
			}
			if got.Basis != BasisNone {
				t.Errorf("basis = %q; a refusal rests on nothing", got.Basis)
			}
			if !strings.Contains(got.Detail, "not this download") {
				t.Errorf("detail = %q, want it to say plainly that this is not the download", got.Detail)
			}
		})
	}
}

// TestAVerifiedChecksumSettlesTheDownload is the other half: the one thing
// that is allowed to stop a transfer happening at all.
func TestAVerifiedChecksumSettlesTheDownload(t *testing.T) {
	dir := t.TempDir()
	const body = "the real film!!!"
	size := put(t, dir, "movie.mkv", body)

	got := Options{Trust: TrustChecksum}.Scan(Request{
		TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size, ExpectedHash: sha256Of(body),
	})
	if got.Verdict != Complete || got.Basis != BasisChecksum {
		t.Fatalf("verdict = %q basis = %q, want complete on a checksum (%s)", got.Verdict, got.Basis, got.Detail)
	}
	if got.Bytes != size {
		t.Errorf("bytes = %d, want the %d that are on the disk", got.Bytes, size)
	}
}

// TestNameAndSizeAloneDoNotSettleADownload pins the default. A full-length
// file with nothing to check it against is a third answer and not a match:
// see the preallocation trap in TestARightSizedFileWithTheWrongBytesIsNeverSettled.
func TestNameAndSizeAloneDoNotSettleADownload(t *testing.T) {
	dir := t.TempDir()
	size := put(t, dir, "movie.mkv", "................")

	got := Options{}.Scan(Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size})
	if got.Verdict != Unproven {
		t.Fatalf("verdict = %q, want unproven under the default tier (%s)", got.Verdict, got.Detail)
	}
}

// TestTheMostTrustingTierTakesNameAndSize is the opt-in, for somebody who
// knows what is in their own folder. It has to actually do something, or the
// setting is decoration.
func TestTheMostTrustingTierTakesNameAndSize(t *testing.T) {
	dir := t.TempDir()
	size := put(t, dir, "movie.mkv", "................")

	got := Options{Trust: TrustSize}.Scan(Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size})
	if got.Verdict != Complete || got.Basis != BasisSize {
		t.Fatalf("verdict = %q basis = %q, want complete on size (%s)", got.Verdict, got.Basis, got.Detail)
	}
}

// TestThisInstancesOwnRecordVouchesForAFile is the default tier doing its job:
// the history is a note this app wrote itself when the last byte landed, and
// it survives the list being cleared, which is one of the three situations
// this package exists for.
func TestThisInstancesOwnRecordVouchesForAFile(t *testing.T) {
	dir := t.TempDir()
	size := put(t, dir, "movie.mkv", "................")

	seen := map[string]bool{"movie.mkv": true}
	got := Options{Finished: func(n string, s int64) bool { return seen[n] && s == size }}.Scan(
		Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size})
	if got.Verdict != Complete || got.Basis != BasisRecord {
		t.Fatalf("verdict = %q basis = %q, want complete on the record (%s)", got.Verdict, got.Basis, got.Detail)
	}
}

// TestTheStrictTierIgnoresTheRecord. The tiers are a ladder, so the strictest
// one must not quietly consult the rung above it.
func TestTheStrictTierIgnoresTheRecord(t *testing.T) {
	dir := t.TempDir()
	size := put(t, dir, "movie.mkv", "................")

	got := Options{Trust: TrustChecksum, Finished: alwaysFinished}.Scan(
		Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size})
	if got.Verdict != Unproven {
		t.Fatalf("verdict = %q, want unproven: the strict tier settles on a checksum and nothing else (%s)", got.Verdict, got.Detail)
	}
}

// TestAShortFileIsABeginningAndItsLengthIsTheOffset. Length can never confirm
// a download, but it can measure one, and that costs a single stat.
func TestAShortFileIsABeginningAndItsLengthIsTheOffset(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "movie.mkv", "half")

	got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(
		Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: 1000})
	if got.Verdict != Partial {
		t.Fatalf("verdict = %q, want partial (%s)", got.Verdict, got.Detail)
	}
	if got.Bytes != 4 {
		t.Errorf("bytes = %d, want the 4 that are actually there", got.Bytes)
	}
}

// TestAPartFileOutranksWhateverSitsAtTheFinalName. A part file is written by a
// transfer that did not finish and removed by one that did, so it is the only
// evidence here that cannot be a coincidence of naming. Read the other way
// round, a stale finished file beside a live part file would settle the task
// and abandon the bytes that were still arriving.
func TestAPartFileOutranksWhateverSitsAtTheFinalName(t *testing.T) {
	dir := t.TempDir()
	const body = "the real film!!!"
	size := put(t, dir, "movie.mkv", body)
	put(t, dir, "movie.mkv"+PartSuffix, "newer")

	got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(Request{
		TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size, ExpectedHash: sha256Of(body),
	})
	if got.Verdict != Partial {
		t.Fatalf("verdict = %q, want partial: a part file means a transfer into this destination did not finish (%s)", got.Verdict, got.Detail)
	}
	if got.Bytes != 5 {
		t.Errorf("bytes = %d, want the 5 in the part file", got.Bytes)
	}
}

// TestATorrentIsNeverSettledFromFileLengths. A torrent client lays out the
// whole file set at full length before it fetches a piece, so name and size
// match for a torrent that has downloaded nothing at all. Anything but a
// separate verdict here would mark empty torrents finished.
func TestATorrentIsNeverSettledFromFileLengths(t *testing.T) {
	dir := t.TempDir()
	const body = "the real film!!!"
	size := put(t, dir, "movie.mkv", body)

	got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(Request{
		TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size, ExpectedHash: sha256Of(body), Torrent: true,
	})
	if got.Verdict != Recheck {
		t.Fatalf("verdict = %q, want recheck: only the download library can answer for a torrent (%s)", got.Verdict, got.Detail)
	}
}

// TestNothingIsFoundWhenNothingIsThere keeps the ordinary answer ordinary, and
// keeps it apart from Unproven: "there is no file" and "there is a file I
// cannot vouch for" are different sentences to put in front of somebody.
func TestNothingIsFoundWhenNothingIsThere(t *testing.T) {
	dir := t.TempDir()
	got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(
		Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: 16})
	if got.Verdict != Absent {
		t.Fatalf("verdict = %q, want absent (%s)", got.Verdict, got.Detail)
	}
	if got.Bytes != 0 {
		t.Errorf("bytes = %d, want 0", got.Bytes)
	}
}

// TestALongerFileIsNotJudged. Longer than the download is supposed to be means
// either somebody else's file or a wrong expected size, and neither is
// something to act on.
func TestALongerFileIsNotJudged(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "movie.mkv", "much more than expected")

	got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(
		Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: 4})
	if got.Verdict != Unproven {
		t.Fatalf("verdict = %q, want unproven (%s)", got.Verdict, got.Detail)
	}
}

// TestANameThatClimbsOutOfTheFolderIsRefused. A task's name arrives from
// whoever uploaded the file, and this package opens and hashes what it
// resolves to.
func TestANameThatClimbsOutOfTheFolderIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(outside, []byte("not yours"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	for _, name := range []string{"../secret.txt", `..\secret.txt`} {
		got := Options{Trust: TrustSize, Finished: alwaysFinished}.Scan(
			Request{TaskID: "x", Dir: dir, Name: name, Size: 9})
		if got.Verdict != Unproven {
			t.Errorf("%q: verdict = %q, want unproven with nothing reported about a file outside the folder (%s)", name, got.Verdict, got.Detail)
		}
		if got.Path != "" {
			t.Errorf("%q: path = %q, want nothing named outside the download folder", name, got.Path)
		}
	}
}

// TestASumsFileBesideTheDownloadIsConsulted proves the third checksum source
// actually reaches the decision, since it is the only one that costs a
// directory read and is therefore the one an optimisation would drop.
func TestASumsFileBesideTheDownloadIsConsulted(t *testing.T) {
	dir := t.TempDir()
	const body = "the real film!!!"
	size := put(t, dir, "movie.mkv", body)
	sum := sha256.Sum256([]byte(body))
	put(t, dir, "checksums.sha256", hex.EncodeToString(sum[:])+"  movie.mkv\n")

	asked := false
	got := Options{Trust: TrustChecksum, Sum: func(d, n string) (checksum.Sum, bool) {
		asked = true
		f, err := os.Open(filepath.Join(d, "checksums.sha256"))
		if err != nil {
			return checksum.Sum{}, false
		}
		defer f.Close()
		sums, err := checksum.ParseHashFile(f)
		if err != nil || len(sums) == 0 {
			return checksum.Sum{}, false
		}
		return sums[0], true
	}}.Scan(Request{TaskID: "x", Dir: dir, Name: "movie.mkv", Size: size})
	if !asked {
		t.Fatal("the sums-file lookup was never called")
	}
	if got.Verdict != Complete || got.Basis != BasisChecksum {
		t.Fatalf("verdict = %q basis = %q, want complete on the sums file (%s)", got.Verdict, got.Basis, got.Detail)
	}
}

// TestParseHashReadsBothFormsAndRefusesADisagreement. A label that contradicts
// its own digest length means one of the two was mistyped, and there is no way
// to know which.
func TestParseHashReadsBothFormsAndRefusesADisagreement(t *testing.T) {
	const md5hex = "0123456789abcdef0123456789abcdef"
	cases := []struct {
		raw  string
		kind checksum.Kind
		hex  string
		ok   bool
	}{
		{"sha256:" + strings.Repeat("a", 64), checksum.SHA256, strings.Repeat("a", 64), true},
		{strings.Repeat("a", 40), checksum.SHA1, strings.Repeat("a", 40), true},
		// Upper case on both halves: .sfv listings are traditionally upper
		// case and a label typed by hand is whatever the person felt like.
		{"MD5:" + strings.ToUpper(md5hex), checksum.MD5, md5hex, true},
		// The label says one hash and the digest is the length of another.
		{"sha256:" + md5hex, "", "", false},
		{strings.Repeat("a", 30), "", "", false},
		{"sha256:zzzz", "", "", false},
		{"   ", "", "", false},
	}
	for _, c := range cases {
		got, ok := ParseHash("movie.mkv", c.raw)
		if ok != c.ok {
			t.Errorf("ParseHash(%q) ok = %v, want %v", c.raw, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Kind != c.kind {
			t.Errorf("ParseHash(%q) kind = %q, want %q", c.raw, got.Kind, c.kind)
		}
		if got.Hex != c.hex {
			t.Errorf("ParseHash(%q) hex = %q, want %q in lower case", c.raw, got.Hex, c.hex)
		}
		if got.Name != "movie.mkv" {
			t.Errorf("ParseHash(%q) name = %q, want the file the hash is about", c.raw, got.Name)
		}
	}
}

// TestOrphansReportsWhatNoTaskClaimsAndTouchesNothing. The reporting is the
// feature: an orphaned part file is thirty gigabytes somebody either wants
// back or wants gone, and nothing here can tell which, so deleting it is a
// button and not a sweep.
func TestOrphansReportsWhatNoTaskClaimsAndTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "abandoned.mkv"+PartSuffix, "left behind")
	put(t, dir, "running.mkv"+PartSuffix, "still arriving")
	put(t, dir, "finished.mkv", "done")

	claimed := map[string]bool{PartPath(dir, "running.mkv"): true}
	got, err := Orphans(dir, claimed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("found %+v, want only the part file no task claims", got)
	}
	if got[0].Path != PartPath(dir, "abandoned.mkv") {
		t.Errorf("path = %q, want the abandoned part file", got[0].Path)
	}
	if got[0].Bytes != 11 {
		t.Errorf("bytes = %d, want 11", got[0].Bytes)
	}
	for _, name := range []string{"abandoned.mkv" + PartSuffix, "running.mkv" + PartSuffix, "finished.mkv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed by a pass that only reports: %v", name, err)
		}
	}
}

// TestAnUnknownTrustValueFallsBackToTheDefault. A settings file written by
// another build must not silently become the most trusting tier.
func TestAnUnknownTrustValueFallsBackToTheDefault(t *testing.T) {
	for _, in := range []string{"", "  ", "everything", "yes"} {
		if got := ParseTrust(in); got != DefaultTrust {
			t.Errorf("ParseTrust(%q) = %q, want %q", in, got, DefaultTrust)
		}
	}
	if got := ParseTrust("  SIZE "); got != TrustSize {
		t.Errorf("ParseTrust of a padded, upper-case value = %q, want %q", got, TrustSize)
	}
}
