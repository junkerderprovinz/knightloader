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
// operator's phone, laptop or security stick, of which this instance stores
// only the public half.
//
// WHY THE RELYING-PARTY ID IS A COLUMN, and it is the one thing about this table
// that is not obvious. A passkey is bound to the DOMAIN it was created for, and
// a browser will not even offer one whose relying-party id does not match the
// page. So the same instance reached two ways holds two different sets:
// register through kl.example.com and the credential simply does not exist at
// http://192.168.20.86:8749. Storing the id means the interface can say which
// address each key belongs to instead of showing a list that silently does
// nothing, and the login ceremony can offer only the keys that can actually
// answer for the address in the browser's bar.
//
// NOTHING IN THIS TABLE IS A SECRET. The public key verifies a signature and the
// credential id names which key made it; neither authenticates anybody on its
// own, which is the whole point of the scheme. So unlike the password hash in
// auth.json or the sealed account credentials, these columns are stored in the
// clear, in the database that a backup already carries.
type Passkey struct {
	ID   string
	Name string
	// CredentialID is the authenticator's own handle for the key, raw bytes.
	// Unique: it is what a login answer is looked up by.
	CredentialID []byte
	// PublicKey is the COSE-encoded public half.
	PublicKey []byte
	// AAGUID identifies the authenticator MODEL (a YubiKey 5, iCloud Keychain,
	// Windows Hello). Stored so a list can one day say what a key IS rather than
	// only what somebody named it.
	AAGUID []byte
	// SignCount is the authenticator's own counter, updated on every successful
	// login. A counter that goes BACKWARDS is the documented signal of a cloned
	// authenticator; many modern ones report 0 always and are exempt.
	SignCount uint32
	// Transports is the comma-joined hint list the authenticator reported
	// ("internal", "usb", "hybrid"), passed back at login so the browser raises
	// the right prompt.
	Transports string
	// RPID is the address this credential is bound to. See the type comment.
	RPID string
	// BackedUp is what the authenticator says about whether the key is synced to
	// a keychain. A key that is not dies with the device, which is worth telling
	// somebody who has only one.
	BackedUp   bool
	CreatedAt  int64
	LastUsedAt int64
}

// passkeyCols is the column list, not a credential: a secrets scanner matches on
// the words rather than on what they hold, and nothing in this table is secret
// at all - which is the point of public-key authentication.
const passkeyCols = `id, name, credential_id, public_key, aaguid, sign_count, transports, rp_id, backed_up, created_at, last_used_at` //nolint:gosec // G101: a SQL column list, and none of these columns holds a secret

// ErrPasskeyExists is the answer to registering the same authenticator twice. It
// is less an error than a statement: the key is already here.
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

// PasskeysForRP returns the credentials bound to one relying-party id, which is
// the only set a login at that address can use. See the Passkey type comment.
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
	// A nil slice reaches SQLite as NULL, which the column refuses. An
	// authenticator that reports no AAGUID (plenty of security keys do not) is a
	// normal case rather than an error, so it is stored as empty here instead of
	// being made every caller's problem to remember.
	if p.AAGUID == nil {
		p.AAGUID = []byte{}
	}
	_, err := s.db.Exec(`INSERT INTO passkeys (`+passkeyCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.CredentialID, p.PublicKey, p.AAGUID,
		p.SignCount, p.Transports, p.RPID, p.BackedUp, p.CreatedAt, p.LastUsedAt)
	if err != nil {
		// The UNIQUE index on credential_id is the guard, and this turns its
		// refusal into an answer somebody can read. Two rows answering for one
		// key would leave the sign-counter check comparing against whichever was
		// found first, which is the clone check quietly stopping rather than
		// failing.
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

// RenamePasskey changes a credential's label. The name is the operator's own
// text and means nothing to the protocol, which is why renaming is free and
// unregistering is not.
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
// error: the caller wanted it gone and it is gone.
func (s *Store) DeletePasskey(id string) error {
	if _, err := s.db.Exec(`DELETE FROM passkeys WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: DeletePasskey: %w", err)
	}
	return nil
}

// newPasskeyID mirrors internal/app's own newID: 8 random bytes, hex, the same
// shape a task id already has, rather than a new convention for one table.
func newPasskeyID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
