package extract

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three archives below are carried as base64 rather than as files under
// testdata, for two reasons. A repository is no place for opaque binaries, and
// more importantly they have to come from somebody else's implementation: an
// encrypted zip this package also wrote would only prove that it agrees with
// itself, and the whole risk in a hand-written AES layer is agreeing with
// yourself about the wrong thing. aesZip and zipCryptoZip were produced by
// 7-Zip 24 (-tzip -mem=AES256 and -mem=ZipCrypto), rar5Archive by WinRAR
// (-ma5 -m0). All three are a few dozen bytes.
const (
	// aesZip holds note.txt, deflated, WinZip AES-256, password "keep". Its
	// entry declares compression method 99 with the real method in the 0x9901
	// extra field, and being AE-2 it carries a zero CRC on purpose.
	aesZip = "UEsDBDMAAQBjAERqCV0AAAAAPgAAAFABAAAIAAsAbm90ZS50eHQBmQcAAgBBRQMIAK2WFy6j6gWq" +
		"RQQ8qw1Kyl5W6gFvqQpRD0MWd3eJzGudpaEyyu2TLFl43wVMDpn5c9KealmZXtW3esR5xg4UUEsB" +
		"Aj8AMwABAGMARGoJXQAAAAA+AAAAUAEAAAgALwAAAAAAAAAgAAAAAAAAAG5vdGUudHh0CgAgAAAA" +
		"AAABABgAgJHIwPAn3QEAAAAAAAAAAAAAAAAAAAAAAZkHAAIAQUUDCABQSwUGAAAAAAEAAQBlAAAA" +
		"bwAAAAAA"

	// zipCryptoZip holds the same note.txt under PKWARE's original cipher, the
	// one every tool labels "legacy encryption". Password "keep".
	zipCryptoZip = "UEsDBBQAAQAIAERqCV1bMHvNLgAAAFABAAAIAAAAbm90ZS50eHR3U1WomitWeaGhAe+RqwBkJyvs" +
		"9fjz0D4tCvVFUxKKz344qfoneyL86s1NMeJQUEsBAj8AFAABAAgARGoJXVswe80uAAAAUAEAAAgA" +
		"JAAAAAAAAAAgAAAAAAAAAG5vdGUudHh0CgAgAAAAAAABABgAgJHIwPAn3QEAAAAAAAAAAAAAAAAA" +
		"AAAAUEsFBgAAAAABAAEAWgAAAFQAAAAAAA=="

	// rar5Archive holds r.txt containing "hoarse", stored, unencrypted.
	rar5Archive = "UmFyIRoHAQAzkrXlCgEFBgAFAQGAgAB89dHdIQIDC4YABIYAILJfIqOAAAAFci50eHQKAwJ5gDJy" +
		"8SfdAWhvYXJzZR13VlEDBQQA"
)

// fixturePassword opens all three encrypted fixtures.
const fixturePassword = "keep"

// fixtureNote is what note.txt holds in both encrypted fixtures. It is long
// enough that 7-Zip deflated it, which is the point: the real compression
// method then lives in the 0x9901 extra field and a reader that trusts
// method 99 has something visibly wrong to write out.
var fixtureNote = strings.Repeat("the raven himself is hoarse\n", 12)

// writeFixture decodes one of the base64 archives above into dir under name,
// and returns the path. The name is a parameter because half these tests are
// about a file being called something its bytes disagree with.
func writeFixture(t *testing.T, dir, name, b64 string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// filesUnder counts the regular files below dir. A missing dir counts as zero,
// because "the extraction never got far enough to create it" and "it created it
// and wrote nothing" are the same result to a caller.
func filesUnder(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return n
}

// TestDetectReadsTheBytesNotTheName pins the probe itself. Everything downstream
// trusts its verdict over the file name, so a signature drifting here would
// quietly route whole formats to the wrong reader.
func TestDetectReadsTheBytesNotTheName(t *testing.T) {
	pad := func(head []byte, n int) []byte {
		out := make([]byte, n)
		copy(out, head)
		return out
	}
	ustar := pad([]byte("hello.txt"), 512)
	copy(ustar[257:], "ustar\x00")

	cases := []struct {
		name string
		head []byte
		want archiveFormat
	}{
		{"zip", []byte("PK\x03\x04rest of it"), formatZip},
		{"empty zip", []byte("PK\x05\x06"), formatZip},
		{"rar4", []byte("Rar!\x1a\x07\x00more"), formatRar},
		{"rar5", []byte("Rar!\x1a\x07\x01\x00more"), formatRar},
		{"7z", []byte("7z\xbc\xaf\x27\x1c\x00\x04"), formatSevenZip},
		{"gzip", []byte("\x1f\x8b\x08\x00"), formatGzip},
		{"bzip2", []byte("BZh9AY&SY"), formatBzip2},
		{"xz", []byte("\xfd7zXZ\x00\x00\x04"), formatXz},
		{"zstd", []byte("\x28\xb5\x2f\xfd\x04\x58"), formatZstd},
		{"tar", ustar, formatTar},
		{"arj", []byte{0x60, 0xea, 0x2e, 0x00, 0x22, 0x0b, 0x01, 0x02, 0x10, 0x00, 0x02, 0x00}, formatArj},
		{"lzh", []byte("\x1f\x20-lh5-\x00\x00"), formatLzh},
		{"ace", []byte("\x00\x00\x00\x00\x00\x00\x00**ACE**"), formatAce},
		{"plain text", []byte("BZh is a band, not an archive"), formatUnknown},
		{"too short", []byte("PK"), formatUnknown},
		{"nothing at all", nil, formatUnknown},
	}
	for _, c := range cases {
		if got := detect(c.head); got != c.want {
			t.Errorf("detect(%s) = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestProbeBeatsSuffixForRenamedRar is the case the probe exists for: a rar
// handed over with a .zip name, which the old name-only dispatch opened with the
// zip reader and reported as a broken download.
func TestProbeBeatsSuffixForRenamedRar(t *testing.T) {
	dir := t.TempDir()
	arc := writeFixture(t, dir, "film.zip", rar5Archive)

	res, err := Extract(arc)
	if err != nil {
		t.Fatalf("a rar named .zip did not extract: %v", err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "film", "r.txt"))
	if err != nil || string(b) != "hoarse" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// TestProbeBeatsSuffixForRenamedZip is the same trade in the other direction,
// and it also proves the probe did not simply start ignoring the zip reader.
func TestProbeBeatsSuffixForRenamedZip(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "film.rar")
	f, err := os.Create(arc)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("docs/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(arc); err != nil {
		t.Fatalf("a zip named .rar did not extract: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "film", "docs", "readme.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// encryptedFixtures drives every password test over both schemes, because the
// two are entirely separate code paths that only look alike from outside.
var encryptedFixtures = []struct {
	name string
	b64  string
}{
	{"winzip-aes", aesZip},
	{"zipcrypto", zipCryptoZip},
}

// TestEncryptedZipOpensWithAPasswordFromTheList is the row itself: before this,
// a password-protected zip failed as though it were corrupt and neither the
// per-task password nor the global list ever reached it.
func TestEncryptedZipOpensWithAPasswordFromTheList(t *testing.T) {
	for _, fx := range encryptedFixtures {
		t.Run(fx.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := writeFixture(t, dir, "secret.zip", fx.b64)

			// The wrong password first, so this also pins that the list is
			// walked rather than only its head being tried.
			res, err := ExtractWith(arc, []string{"wrong", fixturePassword})
			if err != nil {
				t.Fatalf("encrypted zip did not open: %v", err)
			}
			if res.Files != 1 {
				t.Fatalf("Files = %d, want 1", res.Files)
			}
			b, err := os.ReadFile(filepath.Join(dir, "secret", "note.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != fixtureNote {
				t.Fatalf("decrypted %d bytes, want %d", len(b), len(fixtureNote))
			}
		})
	}
}

// TestEncryptedZipWithoutPasswordAsksForOne pins that the archive is reported as
// "needs a password" and not as damaged. The distinction is the whole reason
// ErrPasswordRequired exists: one of the two states is recoverable by the user.
func TestEncryptedZipWithoutPasswordAsksForOne(t *testing.T) {
	for _, fx := range encryptedFixtures {
		t.Run(fx.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := writeFixture(t, dir, "secret.zip", fx.b64)

			if _, err := Extract(arc); !errors.Is(err, ErrPasswordRequired) {
				t.Fatalf("err = %v, want ErrPasswordRequired", err)
			}
		})
	}
}

// TestWrongPasswordWritesNothing pins the pre-flight. Finding out entry by entry
// would leave a directory of half-written files behind every wrong password in
// the list, and the next attempt would be walking over its own debris.
func TestWrongPasswordWritesNothing(t *testing.T) {
	for _, fx := range encryptedFixtures {
		t.Run(fx.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := writeFixture(t, dir, "secret.zip", fx.b64)

			if _, err := ExtractWith(arc, []string{"wrong"}); !errors.Is(err, ErrPasswordRequired) {
				t.Fatalf("err = %v, want ErrPasswordRequired", err)
			}
			if n := filesUnder(t, filepath.Join(dir, "secret")); n != 0 {
				t.Fatalf("%d files written on a wrong password, want none", n)
			}
		})
	}
}

// winZipAESExtra builds the 0x9901 extra field a WinZip AES entry carries.
func winZipAESExtra(version uint16, strength byte, method uint16) []byte {
	return []byte{
		0x01, 0x99, 0x07, 0x00,
		byte(version), byte(version >> 8),
		'A', 'E',
		strength,
		byte(method), byte(method >> 8),
	}
}

// writeRawZip writes a one-entry zip whose body bytes are stored verbatim, so a
// test can build the malformed shapes no honest writer produces.
func writeRawZip(t *testing.T, path string, h *zip.FileHeader, body []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	h.CompressedSize64 = uint64(len(body))
	h.UncompressedSize64 = uint64(len(body))
	w, err := zw.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWinZipAESIsNeverWrittenAsCiphertext is the edge case that is worse than a
// failure. Compression method 99 is not a method, it is a pointer to the 0x9901
// extra field, and a reader that shrugs at an unknown method and stores the
// bytes produces a file full of AES ciphertext with a green tick beside it.
// Every shape below is one this reader must refuse outright, and the assertion
// that matters is not the error but the empty directory: nothing may be written.
func TestWinZipAESIsNeverWrittenAsCiphertext(t *testing.T) {
	// Long enough that a refusal cannot be mistaken for "too short to bother".
	body := []byte("CIPHERTEXT-MARKER-that-must-never-reach-the-disk")

	cases := []struct {
		name  string
		flags uint16
		extra []byte
		body  []byte
	}{
		{
			// The trap in its purest form: the entry says method 99 but does
			// not set the encrypted flag, so a flag-driven reader concludes
			// "not encrypted, unknown method" and stores the ciphertext.
			name:  "not flagged encrypted",
			flags: 0,
			extra: winZipAESExtra(2, 3, zip.Deflate),
			body:  body,
		},
		{
			name:  "no 0x9901 header at all",
			flags: flagEncrypted,
			extra: nil,
			body:  body,
		},
		{
			name:  "strength outside the three the format defines",
			flags: flagEncrypted,
			extra: winZipAESExtra(2, 9, zip.Deflate),
			body:  body,
		},
		{
			name:  "the real method is method 99 again",
			flags: flagEncrypted,
			extra: winZipAESExtra(2, 3, methodWinZipAES),
			body:  body,
		},
		{
			// Shorter than salt plus verification value plus authentication
			// code. Subtracting that overhead unsigned is how a two-byte entry
			// becomes a multi-gigabyte read.
			name:  "shorter than its own overhead",
			flags: flagEncrypted,
			extra: winZipAESExtra(2, 3, zip.Deflate),
			body:  []byte("tiny"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, "trap.zip")
			writeRawZip(t, arc, &zip.FileHeader{
				Name:   "payload.bin",
				Method: methodWinZipAES,
				Flags:  c.flags,
				Extra:  c.extra,
			}, c.body)

			_, err := ExtractWith(arc, []string{fixturePassword})
			if err == nil {
				t.Fatal("a method 99 entry we cannot resolve extracted without an error")
			}
			// Not merely "an error": these are refusals about the entry's
			// shape, and reporting one as a wrong password would send the user
			// off trying passwords against an archive no password can fix.
			// It is also what gives this test teeth, because dropping any of
			// the checks in aesHeader turns the refusal into exactly that.
			if errors.Is(err, ErrPasswordRequired) {
				t.Fatalf("err = %v, want a refusal about the entry rather than a password prompt", err)
			}
			if !strings.Contains(err.Error(), "payload.bin") {
				t.Fatalf("err = %q, want it to name the offending entry", err)
			}
			if n := filesUnder(t, filepath.Join(dir, "trap")); n != 0 {
				t.Fatalf("%d files written, want none: ciphertext reached the disk", n)
			}
		})
	}
}

// TestParseAESExtraReadsTheHeader pins the one field the fixtures cannot cover.
// Both were written by 7-Zip and are therefore AE-2, which zeroes the CRC on
// purpose; the ae2 flag is what stops that zero from being checked against real
// data and reported as corruption on every single AE-2 archive in existence.
func TestParseAESExtraReadsTheHeader(t *testing.T) {
	ae1 := parseAESExtra(winZipAESExtra(1, 3, zip.Deflate))
	if ae1 == nil || ae1.ae2 || ae1.strength != 3 || ae1.method != zip.Deflate {
		t.Fatalf("AE-1 header parsed as %+v", ae1)
	}
	ae2 := parseAESExtra(winZipAESExtra(2, 1, zip.Store))
	if ae2 == nil || !ae2.ae2 || ae2.strength != 1 || ae2.method != zip.Store {
		t.Fatalf("AE-2 header parsed as %+v", ae2)
	}
	// A record whose declared size runs past the end of the field: the walk has
	// to stop rather than read whatever follows as header bytes.
	if got := parseAESExtra([]byte{0x01, 0x99, 0x40, 0x00, 0x02, 0x00}); got != nil {
		t.Fatalf("truncated record parsed as %+v, want nil", got)
	}
	// A record that is not ours must be stepped over, not stopped at.
	skipped := append([]byte{0x55, 0x54, 0x02, 0x00, 0x01, 0x00}, winZipAESExtra(2, 3, zip.Deflate)...)
	if got := parseAESExtra(skipped); got == nil {
		t.Fatal("0x9901 behind another extra record was not found")
	}
}

// TestRetiredFormatsAreRefusedByName pins that each dead format says which one
// it is and why. A generic "unsupported archive" sends the user looking for a
// broken download instead of for another tool.
func TestRetiredFormatsAreRefusedByName(t *testing.T) {
	cases := []struct {
		file string
		says string
	}{
		{"dos.arj", "ARJ"},
		{"amiga.lzh", "LHA/LZH"},
		{"amiga.lha", "LHA/LZH"},
		{"scene.ace", "ACE"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, c.file)
			if err := os.WriteFile(arc, bytes.Repeat([]byte("x"), 64), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Extract(arc)
			if !errors.Is(err, ErrFormatRetired) {
				t.Fatalf("err = %v, want ErrFormatRetired", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("err = %q, want it to name %s", err, c.says)
			}
		})
	}
}

// TestRetiredFormatRecognisedByMagic covers both halves at once: the probe wins
// over the name, and what it found is refused by name rather than fed to the
// zip reader that the extension asked for.
func TestRetiredFormatRecognisedByMagic(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "movie.zip")
	head := make([]byte, 64)
	copy(head[7:], "**ACE**")
	if err := os.WriteFile(arc, head, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Extract(arc)
	if !errors.Is(err, ErrFormatRetired) {
		t.Fatalf("err = %v, want ErrFormatRetired", err)
	}
	if !strings.Contains(err.Error(), "ACE") {
		t.Fatalf("err = %q, want it to name ACE", err)
	}
}

// TestPayloadNameWhenTheProbeOverrulesTheName covers the one thing the probe
// broke and had to put back: the payload of a single-stream archive is named
// after the archive minus its compression suffix, and when the name carries the
// wrong suffix entirely there is no suffix to subtract. Writing the payload
// under the archive's own name would truncate the archive mid-read.
func TestPayloadNameWhenTheProbeOverrulesTheName(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "blob.zip")
	writeCompressed(t, arc, gzipWriter, []byte("gzip wearing a zip name"))

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "blob"))
	if err != nil || string(b) != "gzip wearing a zip name" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
	if fi, err := os.Stat(arc); err != nil || fi.Size() == 0 {
		t.Fatalf("the archive itself was overwritten: %v", err)
	}
}
