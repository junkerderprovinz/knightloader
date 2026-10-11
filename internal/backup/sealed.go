package backup

import (
	"bytes"
	"errors"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// sealedMagic starts an archive sealed with the app key, so a restore can tell
// it from a zip before trying either.
var sealedMagic = []byte("KnightLoader sealed backup 1\n")

const sealPurpose = "backup"

// SealedExt is the file extension of a sealed archive. It is not .zip, since
// nothing but KnightLoader can open one.
const SealedExt = ".sealed"

var (
	// ErrNeedsKey is a sealed archive uploaded to an instance without an app
	// key.
	ErrNeedsKey = errors.New("this backup is encrypted with an app key and this instance has none; " +
		"start it with the " + appkey.EnvVar + " of the instance that made the backup, then restore again")
	// ErrWrongKey is a sealed archive the instance's app key does not open.
	ErrWrongKey = errors.New("this backup is encrypted with a different app key; " +
		"restoring it needs the " + appkey.EnvVar + " of the instance that made it")
)

// Seal encrypts a whole archive with key. The archive holds every secret in
// settings.json in the clear, so with a key it never leaves the instance any
// other way.
func Seal(key *appkey.Key, archive []byte) ([]byte, error) {
	sealed, err := key.Seal(sealPurpose, archive)
	if err != nil {
		return nil, err
	}
	return append(bytes.Clone(sealedMagic), sealed...), nil
}

// Unseal returns the archive inside a sealed upload, and an unsealed upload
// as it is, so a backup made before the key still restores.
func Unseal(key *appkey.Key, upload []byte) ([]byte, error) {
	if !bytes.HasPrefix(upload, sealedMagic) {
		return upload, nil
	}
	if key == nil {
		return nil, ErrNeedsKey
	}
	archive, err := key.OpenForeign(sealPurpose, upload[len(sealedMagic):])
	if err != nil {
		return nil, ErrWrongKey
	}
	return archive, nil
}
