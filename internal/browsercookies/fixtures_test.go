package browsercookies

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

type firefoxRow struct {
	origin, host, name, value, path string
	expiry                          int64
	secure, httpOnly                bool
	lastAccessed                    time.Time
}

const firefoxSchema = `CREATE TABLE moz_cookies (
	id INTEGER PRIMARY KEY, originAttributes TEXT NOT NULL DEFAULT '', name TEXT, value TEXT,
	host TEXT, path TEXT, expiry INTEGER, lastAccessed INTEGER, creationTime INTEGER,
	isSecure INTEGER, isHttpOnly INTEGER, inBrowserElement INTEGER DEFAULT 0,
	sameSite INTEGER DEFAULT 0, rawSameSite INTEGER DEFAULT 0, schemeMap INTEGER DEFAULT 0)`

// writeFirefoxDB writes a cookies.sqlite of the given schema version and
// returns the open database, which the caller may keep open to hold a WAL.
func writeFirefoxDB(t *testing.T, path string, version int, wal bool, rows []firefoxRow) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	stmts := []string{firefoxSchema, "PRAGMA user_version = " + strconv.Itoa(version)}
	if wal {
		stmts = append(stmts, "PRAGMA journal_mode = WAL", "PRAGMA wal_autocheckpoint = 0")
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rows {
		path := r.path
		if path == "" {
			path = "/"
		}
		_, err := db.Exec(`INSERT INTO moz_cookies (originAttributes, name, value, host, path, expiry, lastAccessed, creationTime, isSecure, isHttpOnly)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.origin, r.name, r.value, r.host, path, r.expiry, r.lastAccessed.UnixMicro(), r.lastAccessed.UnixMicro(), r.secure, r.httpOnly)
		if err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type chromiumRow struct {
	host, name, path string
	plain            string
	encrypted        []byte
	expires          time.Time
	lastAccess       time.Time
	topFrame         string
	secure, httpOnly bool
}

const chromiumSchema = `CREATE TABLE meta (key LONGVARCHAR NOT NULL UNIQUE PRIMARY KEY, value LONGVARCHAR);
CREATE TABLE cookies (creation_utc INTEGER NOT NULL, host_key TEXT NOT NULL, top_frame_site_key TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL, value TEXT NOT NULL, encrypted_value BLOB NOT NULL, path TEXT NOT NULL,
	expires_utc INTEGER NOT NULL, is_secure INTEGER NOT NULL, is_httponly INTEGER NOT NULL,
	last_access_utc INTEGER NOT NULL, has_expires INTEGER NOT NULL DEFAULT 1, is_persistent INTEGER NOT NULL DEFAULT 1,
	priority INTEGER NOT NULL DEFAULT 1, samesite INTEGER NOT NULL DEFAULT -1, source_scheme INTEGER NOT NULL DEFAULT 0,
	source_port INTEGER NOT NULL DEFAULT -1, last_update_utc INTEGER NOT NULL DEFAULT 0)`

func writeChromiumDB(t *testing.T, path string, metaVersion int, rows []chromiumRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(chromiumSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('version', ?)`, strconv.Itoa(metaVersion)); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		path := r.path
		if path == "" {
			path = "/"
		}
		enc := r.encrypted
		if enc == nil {
			enc = []byte{}
		}
		_, err := db.Exec(`INSERT INTO cookies (creation_utc, host_key, top_frame_site_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly, last_access_utc)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chromeTime(r.lastAccess), r.host, r.topFrame, r.name, r.plain, enc, path, chromeTime(r.expires), r.secure, r.httpOnly, chromeTime(r.lastAccess))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func chromeTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro() + chromeEpochOffset*1e6
}

func writeLocalState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Local State"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// sealCBC encrypts a value the way Chromium does on macOS and Linux, with the
// host's hash in front as a cookie database of version 24 and later has it.
func sealCBC(t *testing.T, prefix, password string, iterations int, host, value string, hashed bool) []byte {
	t.Helper()
	key := pbkdf2.Key([]byte(password), []byte("saltysalt"), iterations, 16, sha1.New)
	plain := []byte(value)
	if hashed {
		sum := sha256.Sum256([]byte(host))
		plain = append(sum[:], plain...)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	for range pad {
		plain = append(plain, byte(pad))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte("                ")).CryptBlocks(out, plain)
	return append([]byte(prefix), out...)
}

// sealGCM encrypts a value the way Chromium does on Windows.
func sealGCM(t *testing.T, key []byte, host, value string, hashed bool) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	plain := []byte(value)
	if hashed {
		sum := sha256.Sum256([]byte(host))
		plain = append(sum[:], plain...)
	}
	return append(append([]byte("v10"), nonce...), aead.Seal(nil, nonce, plain, nil)...)
}
