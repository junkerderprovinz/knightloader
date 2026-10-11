package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// FileName is the file in the data folder the lock state lives in.
const FileName = "auth.json"

// sealPurpose binds the sealed auth.json to this file, so it cannot be passed
// off as another sealed value.
const sealPurpose = "auth.json"

// OpenWithKey is Open with an app key. With a key the whole file is sealed,
// hash, session key, second factor and recovery codes alike; a file written
// before the key is read as it is and sealed on the spot. Without a key a
// sealed file is refused rather than replaced, since a fresh one would drop
// the password.
func OpenWithKey(dir string, key *appkey.Key) (*Guard, error) {
	g := &Guard{path: filepath.Join(dir, FileName), appKey: key}
	s, sealed, err := readStored(g.path, key)
	if err != nil {
		return nil, err
	}
	g.hash = []byte(s.Hash)
	g.key, _ = hex.DecodeString(s.Key)
	g.totp = s.TOTP
	g.recovery = s.Recovery
	g.epoch = s.Epoch
	g.revoked = s.Revoked
	if len(g.key) == 0 {
		g.key = make([]byte, 32)
		if _, err := rand.Read(g.key); err != nil {
			return nil, err
		}
	} else if sealed || key == nil {
		return g, nil
	}
	if err := g.flush(); err != nil {
		return nil, err
	}
	return g, nil
}

// NeedsSealing reports whether dir holds an auth.json that a start with an app
// key would seal for the first time.
func NeedsSealing(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	return err == nil && !appkey.IsSealed(string(b))
}

// readStored reads auth.json and reports whether it was sealed on disk. A
// missing file reads as empty.
func readStored(path string, key *appkey.Key) (s stored, sealed bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return stored{}, false, nil
	}
	if err != nil {
		return stored{}, false, err
	}
	if appkey.IsSealed(string(b)) {
		plain, err := key.OpenString(sealPurpose, string(b))
		if err != nil {
			return stored{}, true, fmt.Errorf("%s cannot be read: %w", path, err)
		}
		b = []byte(plain)
		sealed = true
	}
	// A plain file that does not parse starts over with no password.
	_ = json.Unmarshal(b, &s)
	return s, sealed, nil
}

func encodeStored(s stored, key *appkey.Key) ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil || key == nil {
		return b, err
	}
	sealed, err := key.SealString(sealPurpose, string(b))
	if err != nil {
		return nil, err
	}
	return []byte(sealed + "\n"), nil
}
