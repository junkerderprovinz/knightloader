package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Passkey is one registered WebAuthn credential: a private key held by the
// operator's phone, laptop or security key, of which this instance stores
// only the public half.
//
// The relying-party id is a column because a passkey is bound to the domain it
// was created for: registered through kl.example.com, it does not exist at
// http://192.168.1.10:8749. Storing it lets the interface say which address a
// key belongs to and lets login offer only the keys that can answer.
//
// Nothing here is secret: a public key and a credential id authenticate nobody
// on their own, so unlike auth.json these are stored in the clear, in the
// database a backup already carries.
type Passkey struct {
	ID   string
	Name string
	// CredentialID is the authenticator's handle for the key, raw bytes. It is
	// unique, and a login answer is looked up by it.
	CredentialID []byte
	// PublicKey is the COSE-encoded public half.
	PublicKey []byte
	// AAGUID identifies the authenticator model (a YubiKey 5, iCloud
	// Keychain, Windows Hello).
	AAGUID []byte
	// SignCount is the authenticator's counter, updated on every login. A
	// counter going backwards signals a cloned authenticator; many modern ones
	// always report 0 and are exempt.
	SignCount uint32
	// Transports is the comma-joined hint list the authenticator reported
	// ("internal", "usb", "hybrid"), passed back at login so the browser shows
	// the right prompt.
	Transports string
	// RPID is the address this credential is bound to.
	RPID string
	// BackedUp is whether the authenticator says the key syncs to a keychain;
	// one that does not dies with the device.
	BackedUp   bool
	CreatedAt  int64
	LastUsedAt int64
}

// passkeyCols is the column list; a secrets scanner flags the words, but no
// column holds a secret.
const passkeyCols = `id, name, credential_id, public_key, aaguid, sign_count, transports, rp_id, backed_up, created_at, last_used_at` //nolint:gosec // G101: a SQL column list, and none of these columns holds a secret

// ErrPasskeyExists is the answer to registering the same authenticator twice.
var ErrPasskeyExists = errors.New("this passkey is already registered")

type passkeyScanner interface{ Scan(dest ...any) error }

func scanPasskey(s passkeyScanner) (Passkey, error) {
	var p Passkey
	err := s.Scan(&p.ID, &p.Name, &p.CredentialID, &p.PublicKey, &p.AAGUID,
		&p.SignCount, &p.Transports, &p.RPID, &p.BackedUp, &p.CreatedAt, &p.LastUsedAt)
	if err != nil {
		return Passkey{}, err
	}
	return p, nil
}

// ListPasskeys returns every registered credential, oldest first.
func (s *Store) ListPasskeys() ([]Passkey, error) {
	rows, err := s.db.Query(`SELECT ` + passkeyCols + ` FROM passkeys ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: ListPasskeys: %w", err)
	}
	defer rows.Close()

	var out []Passkey
	for rows.Next() {
		p, sErr := scanPasskey(rows)
		if sErr != nil {
			return nil, fmt.Errorf("store: ListPasskeys: %w", sErr)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PasskeysForRP returns the credentials bound to one relying-party id, the only
// ones a login at that address can use.
func (s *Store) PasskeysForRP(rpID string) ([]Passkey, error) {
	all, err := s.ListPasskeys()
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, 0, len(all))
	for _, p := range all {
		if p.RPID == rpID {
			out = append(out, p)
		}
	}
	return out, nil
}

// PasskeyByCredentialID finds the credential an authenticator's answer names.
// The bool is false when no such credential is registered.
func (s *Store) PasskeyByCredentialID(credID []byte) (Passkey, bool, error) {
	row := s.db.QueryRow(`SELECT `+passkeyCols+` FROM passkeys WHERE credential_id = ?`, credID)
	p, err := scanPasskey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Passkey{}, false, nil
	}
	if err != nil {
		return Passkey{}, false, fmt.Errorf("store: PasskeyByCredentialID: %w", err)
	}
	return p, true, nil
}

// AddPasskey stores a freshly registered credential and returns it with its id
// and timestamp filled in.
func (s *Store) AddPasskey(p Passkey) (Passkey, error) {
	if len(p.CredentialID) == 0 || len(p.PublicKey) == 0 {
		return Passkey{}, errors.New("store: AddPasskey: a credential needs an id and a public key")
	}
	if p.ID == "" {
		p.ID = newPasskeyID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}
	// A nil slice reaches SQLite as NULL, which the column refuses, and many
	// security keys report no AAGUID.
	if p.AAGUID == nil {
		p.AAGUID = []byte{}
	}
	_, err := s.db.Exec(`INSERT INTO passkeys (`+passkeyCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.CredentialID, p.PublicKey, p.AAGUID,
		p.SignCount, p.Transports, p.RPID, p.BackedUp, p.CreatedAt, p.LastUsedAt)
	if err != nil {
		// The UNIQUE index on credential_id refused a duplicate; say so
		// plainly.
		if _, ok, gErr := s.PasskeyByCredentialID(p.CredentialID); gErr == nil && ok {
			return Passkey{}, ErrPasskeyExists
		}
		return Passkey{}, fmt.Errorf("store: AddPasskey: %w", err)
	}
	return p, nil
}

// TouchPasskey records a successful login: the authenticator's new sign counter
// and when it was last used.
func (s *Store) TouchPasskey(id string, signCount uint32, at int64) error {
	if _, err := s.db.Exec(`UPDATE passkeys SET sign_count = ?, last_used_at = ? WHERE id = ?`, signCount, at, id); err != nil {
		return fmt.Errorf("store: TouchPasskey: %w", err)
	}
	return nil
}

// RenamePasskey changes a credential's label, which means nothing to the
// protocol.
func (s *Store) RenamePasskey(id, name string) error {
	res, err := s.db.Exec(`UPDATE passkeys SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("store: RenamePasskey: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: RenamePasskey: no passkey %q", id)
	}
	return nil
}

// DeletePasskey removes a credential. Deleting one that is not there is not an
// error.
func (s *Store) DeletePasskey(id string) error {
	if _, err := s.db.Exec(`DELETE FROM passkeys WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: DeletePasskey: %w", err)
	}
	return nil
}

// newPasskeyID has the same shape as a task id: 8 random bytes in hex.
func newPasskeyID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
