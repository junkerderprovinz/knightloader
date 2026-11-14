package nox

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// Login is the WebUI account KnightLoader made for its qBittorrent, and the
// port it last ran on.
type Login struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port,omitempty"`
}

const (
	loginFile    = "webui.json"
	loginPurpose = "qbittorrent.webui"
	loginUser    = "knightloader"
)

// LoadLogin reads the login kept in profile, making one with a random password
// on the first start. With an app key the password is sealed at rest, and a
// plain one left from before the key is sealed on the spot.
func LoadLogin(profile string, key *appkey.Key) (Login, error) {
	path := filepath.Join(profile, loginFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return Login{}, err
		}
		l := Login{Username: loginUser, Password: base64.RawURLEncoding.EncodeToString(raw)}
		return l, SaveLogin(profile, key, l)
	}
	if err != nil {
		return Login{}, err
	}
	var l Login
	if err := json.Unmarshal(b, &l); err != nil {
		return Login{}, fmt.Errorf("%s cannot be read: %w", path, err)
	}
	if !appkey.IsSealed(l.Password) {
		if key != nil {
			return l, SaveLogin(profile, key, l)
		}
		return l, nil
	}
	plain, err := key.OpenString(loginPurpose, l.Password)
	if err != nil {
		return Login{}, fmt.Errorf("the qBittorrent login in %s cannot be opened: %w", path, err)
	}
	l.Password = plain
	return l, nil
}

// SaveLogin writes l into profile, sealing the password when there is a key.
func SaveLogin(profile string, key *appkey.Key, l Login) error {
	if key != nil {
		sealed, err := key.SealString(loginPurpose, l.Password)
		if err != nil {
			return err
		}
		l.Password = sealed
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return err
	}
	path := filepath.Join(profile, loginFile)
	tmp := path + ".kl"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
