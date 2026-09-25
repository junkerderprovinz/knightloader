package extract

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
	"github.com/nwaples/rardecode/v2"
)

// The two encrypted archives come from WinRAR (-ma5 -m0), since
// extracttest.RarSet has no cipher. Each holds small.txt, which reads rarBody,
// under the password rarPassword.
const (
	// rarContentsEncrypted was made with -p: the names are in the clear and
	// only the contents are encrypted.
	rarContentsEncrypted = "UmFyIRoHAQAzkrXlCgEFBgAFAQGAgABuGSbDVgIDPJAABIYAILiHAtSAAAAJc21hbGwudHh0MAEA" +
		"Aw8o5CKxO+BgN4BjzEXOjIaK7WW72UVb8v5ucXfMrKju/hE8x6rxQASaWnumvAoDAtK080fbTN0B" +
		"qxAToKMUnfkNzCbRH+Sqkx13VlEDBQQA"

	// rarHeadersEncrypted was made with -hp, so not even the names can be read
	// without the password.
	rarHeadersEncrypted = "UmFyIRoHAQDM3erhIQQAAAEPw2UNbBUN+VTUu5BfHLxjD4MRRpIzkjfEKd+j7w4cI6wyB7rOnyTN" +
		"Cdg11O9j1wFIDefFriZ07EFeCQ6H8f5XFQ+bb3kG6tHrITqO9pZOlONOfaqKJYKxBKFunVAIGlQQ" +
		"vbIWCZbvgHjdincBCSlgmO8IE7q6fY6U2uEE9rJWYbF9/Vuq07kewkEKJtZviEbOguRnESPdfrkl" +
		"osKYnvz79Ey2XfBy/FLKnu/zBBAD8VrsEfVNpS1t2iMzDqKpZ9d+8Mc/Hvg3BU85osNKANNvUq/g" +
		"gUYQB+zSKte1bQ=="

	rarPassword = "secret"
	rarBody     = "hello\n"
)

// encryptedRar writes one of the encrypted archives into a folder of its own
// under name and returns the folder and the path.
func encryptedRar(t *testing.T, name, b64 string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	return dir, writeFixture(t, dir, name, b64)
}

// A set in three volumes with a file running across all of them, and every
// volume reported with its folder, since what "delete the archive afterwards"
// is handed has to name files that exist.
func TestAMultiVolumeRarSetIsUnpackedAndEveryVolumeReported(t *testing.T) {
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	paths := extracttest.WriteRarSet(t, dir, "set", extracttest.RarSet(4096,
		extracttest.File{Name: "movie.bin", Body: movie},
		extracttest.File{Name: "docs/small.txt", Body: []byte("hello\n")},
	))

	out, err := Run(context.Background(), Request{Path: paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "set", "movie.bin")); err != nil || !bytes.Equal(got, movie) {
		t.Fatalf("movie.bin came out as %d bytes, %v; want %d", len(got), err, len(movie))
	}
	if got, err := os.ReadFile(filepath.Join(dir, "set", "docs", "small.txt")); err != nil || string(got) != "hello\n" {
		t.Fatalf("docs/small.txt = %q, %v", got, err)
	}
	if len(out.Volumes) != len(paths) {
		t.Fatalf("Volumes = %v, want the %d parts", out.Volumes, len(paths))
	}
	for i, p := range paths {
		if out.Volumes[i] != p {
			t.Errorf("volume %d = %q, want %q", i+1, out.Volumes[i], p)
		}
	}
}

// leftover is what the download library leaves of a volume after an attempt
// that died early: the whole length reserved, the first bytes written, zeros
// after them.
func leftover(real []byte, written int) []byte {
	out := make([]byte, len(real))
	copy(out, real[:written])
	return out
}

// A leftover under a volume's name opens without complaint and fails deep
// inside, with an error that says nothing about which of the parts is wrong.
func TestTheVolumeThatBreaksASetIsNamedInTheError(t *testing.T) {
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	vols := extracttest.RarSet(4096, extracttest.File{Name: "movie.bin", Body: movie})
	vols[1] = leftover(vols[1], 1024)
	paths := extracttest.WriteRarSet(t, dir, "set", vols)

	_, err := Run(context.Background(), Request{Path: paths[0]})
	if err == nil {
		t.Fatal("a set with a leftover in the middle unpacked")
	}
	if !errors.Is(err, rardecode.ErrBadBlockHeader) {
		t.Errorf("err = %v, want the reader's own error kept", err)
	}
	if !strings.Contains(err.Error(), "set.part2.rar") {
		t.Errorf("err = %q, want it to name set.part2.rar", err)
	}
	if exists(filepath.Join(dir, "set")) {
		t.Error("the failed extraction left its folder behind")
	}
}

// rar -p leaves the names readable and encrypts only the contents, so the
// reader turns the missing password down while an entry is being written. It
// has to come back as "needs a password", or the list is never tried and the
// interface never asks.
func TestARarWithEncryptedContentsAsksForAPassword(t *testing.T) {
	dir, path := encryptedRar(t, "contents-encrypted.rar", rarContentsEncrypted)

	_, err := Run(context.Background(), Request{Path: path})
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("err = %v, want ErrPasswordRequired", err)
	}
	if exists(filepath.Join(dir, "contents-encrypted")) {
		t.Error("the refused archive left its folder behind")
	}
}

// The first password in the list is wrong. It is turned down while the file
// is written, and the right one further down still gets its turn.
func TestARarWithEncryptedContentsOpensWithAPasswordFurtherDownTheList(t *testing.T) {
	dir, path := encryptedRar(t, "contents-encrypted.rar", rarContentsEncrypted)

	out, err := Run(context.Background(), Request{
		Path:    path,
		Options: Options{Passwords: []string{"wrong", rarPassword}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "contents-encrypted", "small.txt")); err != nil || string(got) != rarBody {
		t.Fatalf("small.txt = %q, %v", got, err)
	}
	// The wrong password's attempt wrote the file too before it was refused.
	// The job counts what it produced, not what it tried.
	if out.Files != 1 || out.Bytes != int64(len(rarBody)) {
		t.Errorf("the job counted %d files and %d bytes, want 1 and %d", out.Files, out.Bytes, len(rarBody))
	}
}

// With the headers encrypted a wrong password fails the moment the set is
// opened, and that must not end the walk either.
func TestAWrongPasswordForEncryptedHeadersMovesOnToTheNext(t *testing.T) {
	dir, path := encryptedRar(t, "headers-encrypted.rar", rarHeadersEncrypted)

	if _, err := Run(context.Background(), Request{
		Path:    path,
		Options: Options{Passwords: []string{"wrong", rarPassword}},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "headers-encrypted", "small.txt")); err != nil || string(got) != rarBody {
		t.Fatalf("small.txt = %q, %v", got, err)
	}
}

// Every password in the list is wrong. Each of them got as far as creating the
// file, and none of that may outlive the job.
func TestWrongPasswordsLeaveNothingBehind(t *testing.T) {
	dir, path := encryptedRar(t, "contents-encrypted.rar", rarContentsEncrypted)

	_, err := Run(context.Background(), Request{
		Path:    path,
		Options: Options{Passwords: []string{"wrong", "worse"}},
	})
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("err = %v, want ErrPasswordRequired", err)
	}
	if exists(filepath.Join(dir, "contents-encrypted")) {
		t.Error("the attempts with the wrong passwords left their folder behind")
	}
}
