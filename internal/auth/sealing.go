package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// FileName is the file in the data folder the old sign-in lives in.
const FileName = "auth.json"

// sealPurpose binds the sealed auth.json to this file, so it cannot be passed
// off as another sealed value.
const sealPurpose = "auth.json"

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
		if errors.Is(err, appkey.ErrWrongKey) {
			// The seal cannot tell another key from a damaged file.
			return stored{}, true, fmt.Errorf("%s does not open: %w. If that is the key in use, the file is damaged: "+
				"restore it from a backup, or remove it to start with no password and set one again", path, err)
		}
		if err != nil {
			return stored{}, true, fmt.Errorf("%s cannot be read: %w", path, err)
		}
		b = []byte(plain)
		sealed = true
	}
	// Read as empty, a damaged file would open the instance to anybody.
	if err := json.Unmarshal(b, &s); err != nil {
		return stored{}, sealed, fmt.Errorf("%s is damaged (%v), and without it the instance would have no password: "+
			"restore it from a backup, or remove it to start with no password and set one again", path, err)
	}
	return s, sealed, nil
}

// PasswordIn reports whether state, an auth.json in the clear, has a login
// password and whether that one can be checked only with the app key it was
// set under. A state that does not parse is refused.
func PasswordIn(state []byte) (set, needsItsKey bool, err error) {
	var s stored
	if err := json.Unmarshal(state, &s); err != nil {
		return false, false, fmt.Errorf("auth.json is damaged: %w", err)
	}
	return s.Hash != "", strings.HasPrefix(s.Hash, argonPrefix), nil
}

// HasPassword reports whether the auth.json in dir has a login password,
// without writing anything.
func HasPassword(dir string, key *appkey.Key) (bool, error) {
	s, _, err := readStored(filepath.Join(dir, FileName), key)
	return s.Hash != "", err
}

// SealIn seals an auth.json in dir that a restore put there in the clear,
// for an instance with an app key.
func SealIn(dir string, key *appkey.Key) error {
	path := filepath.Join(dir, FileName)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || key == nil || appkey.IsSealed(string(b)) {
		return nil
	}
	if err != nil {
		return err
	}
	if !json.Valid(b) {
		return fmt.Errorf("%s is damaged", path)
	}
	sealed, err := key.SealString(sealPurpose, string(b))
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sealed+"\n"), 0o600)
}

// NeedsSealing reports whether dir holds an auth.json that a start with an app
// key would seal for the first time.
func NeedsSealing(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	return err == nil && !appkey.IsSealed(string(b))
}
