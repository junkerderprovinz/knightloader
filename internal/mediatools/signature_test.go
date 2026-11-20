package mediatools

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// releaseSums reads the sums and signature of yt-dlp 2026.10.26 as the release
// published them.
func releaseSums(t *testing.T) (sums, sig []byte) {
	t.Helper()
	sums, err := os.ReadFile(filepath.Join("testdata", "SHA2-256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err = os.ReadFile(filepath.Join("testdata", "SHA2-256SUMS.sig"))
	if err != nil {
		t.Fatal(err)
	}
	return sums, sig
}

func TestARealReleaseSignatureIsAccepted(t *testing.T) {
	sums, sig := releaseSums(t)
	if err := verifySums(sums, sig); err != nil {
		t.Fatalf("yt-dlp's own signature was refused: %v", err)
	}
}

func TestEditedSumsAreRefused(t *testing.T) {
	sums, sig := releaseSums(t)
	edited := bytes.Replace(sums, []byte("yt-dlp_musllinux\n"), []byte("yt-dlp_musllinux_\n"), 1)
	if bytes.Equal(edited, sums) {
		t.Fatal("the test data has no yt-dlp_musllinux line to edit")
	}
	if err := verifySums(edited, sig); err == nil {
		t.Fatal("sums edited after signing were accepted")
	}
}

func TestASignatureByAnotherKeyIsRefused(t *testing.T) {
	sums, _ := releaseSums(t)
	other, err := openpgp.NewEntity("someone else", "", "someone@example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, other, bytes.NewReader(sums), nil); err != nil {
		t.Fatal(err)
	}
	if err := verifySums(sums, sig.Bytes()); err == nil {
		t.Fatal("a signature by a key other than yt-dlp's was accepted")
	}
}

func TestAnEmptySignatureIsRefused(t *testing.T) {
	sums, _ := releaseSums(t)
	if err := verifySums(sums, nil); err == nil {
		t.Fatal("no signature at all was accepted")
	}
}
