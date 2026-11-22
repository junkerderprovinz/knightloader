package mediatools

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// signatureAsset is the detached GPG signature yt-dlp publishes over
// SHA2-256SUMS. The sums come from the same release as the binary, so they
// prove only that the download arrived intact; the signature is what ties
// them to yt-dlp's maintainers.
const signatureAsset = "SHA2-256SUMS.sig"

// signingKey is yt-dlp's release signing key, "Simon Sawicki (yt-dlp signing
// key)", copied from https://github.com/yt-dlp/yt-dlp/blob/master/public.key
// (unchanged there since commit 12647e03d417), which yt-dlp's README names for
// verifying the sums. The Dockerfile checks the image's yt-dlp with this file
// too.
//
//go:embed ytdlp-signing-key.asc
var signingKey []byte

// signingFingerprint is that key's fingerprint, checked against the signer so a
// different key swapped into the file is refused as well.
const signingFingerprint = "AC0CBBE6848D6A873464AF4E57CF65933B5A7581"

// verifySums checks that sig is a valid signature over sums by yt-dlp's
// signing key.
func verifySums(sums, sig []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(signingKey))
	if err != nil {
		return fmt.Errorf("yt-dlp's signing key could not be read: %w", err)
	}
	signer, err := openpgp.CheckDetachedSignature(keyring, bytes.NewReader(sums), bytes.NewReader(sig), nil)
	if err != nil {
		return fmt.Errorf("%s is not signed by yt-dlp's signing key: %w", checksumAsset, err)
	}
	if got := fmt.Sprintf("%X", signer.PrimaryKey.Fingerprint); !strings.EqualFold(got, signingFingerprint) {
		return errors.New("the signing key on file is not yt-dlp's")
	}
	return nil
}
