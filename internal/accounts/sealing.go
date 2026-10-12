package accounts

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// KeyringName is the file the credential key lives in, beside accounts.json.
const KeyringName = ".keyring"

const keyringPurpose = "keyring"

// OpenWithKey is Open with an app key. The credential key in .keyring is then
// sealed with it, so accounts.json and .keyring copied together still open
// nothing. A .keyring written before the key is sealed on the spot.
func OpenWithKey(dir string, key *appkey.Key) (*Store, error) {
	s := &Store{
		keyPath: filepath.Join(dir, KeyringName),
		dbPath:  filepath.Join(dir, "accounts.json"),
		data:    map[string]string{},
		appKey:  key,
	}
	if err := s.loadKey(); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// readKeyring returns the credential key as it is stored, opening a sealed
// one. A sealed file the app key does not open is an error rather than a
// damaged file, since replacing it would lose every stored credential.
func (s *Store) readKeyring() (raw []byte, sealed bool, err error) {
	b, err := os.ReadFile(s.keyPath)
	if err != nil || !appkey.IsSealed(string(b)) {
		return b, false, err
	}
	plain, err := s.appKey.OpenString(keyringPurpose, string(b))
	if err != nil {
		return nil, true, fmt.Errorf("accounts: the credential key %s cannot be read: %w", s.keyPath, err)
	}
	return []byte(plain), true, nil
}

func (s *Store) writeKeyring(k []byte) error {
	if s.appKey == nil {
		return os.WriteFile(s.keyPath, k, 0o600)
	}
	sealed, err := s.appKey.SealString(keyringPurpose, string(k))
	if err != nil {
		return err
	}
	return writeAtomic(s.keyPath, []byte(sealed+"\n"))
}
