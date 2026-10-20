package accounts

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Keyring is the credential key in the clear. A backup carries it so the
// accounts open under another app key or none; the restore seals it again.
func (s *Store) Keyring() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.key)
}

// Verify reports whether accountsJSON and keyring belong together: every
// stored credential has to open with the key.
func Verify(accountsJSON, keyring []byte) error {
	if len(keyring) != 32 {
		return fmt.Errorf("accounts: the credential key is %d bytes, not 32", len(keyring))
	}
	s := &Store{key: keyring, data: map[string]string{}}
	if len(bytes.TrimSpace(accountsJSON)) > 0 {
		if err := json.Unmarshal(accountsJSON, &s.data); err != nil {
			return fmt.Errorf("accounts: accounts.json is damaged: %w", err)
		}
	}
	for k := range s.data {
		if _, err := s.openLocked(k); err != nil {
			return fmt.Errorf("accounts: the credential for %s does not open with its key: %w", serviceOf(k), err)
		}
	}
	return nil
}
