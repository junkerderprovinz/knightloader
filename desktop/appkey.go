package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// The app key lives in the operating system's credential store (Windows
// Credential Manager, the macOS Keychain, the Secret Service on Linux) under
// this service and account, since a desktop has no template to pass
// KL_APP_KEY in. The variable still wins when it is set, which is how the
// data folder is opened on another machine.
const (
	keyringService = "KnightLoader"
	keyringUser    = "app-key"
)

// keyStore is the part of go-keyring loadAppKey uses, so a test can stand in
// for the operating system's store.
type keyStore interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

type osKeyStore struct{}

func (osKeyStore) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osKeyStore) Set(service, user, secret string) error   { return keyring.Set(service, user, secret) }

// loadAppKey returns the key for dataDir, making one on the first start. A
// machine without a usable credential store runs without a key. A new key is never made over a data folder that is
// already sealed, because it could not open it; the start then stops with
// the reason instead.
func loadAppKey(store keyStore, dataDir string) (*appkey.Key, error) {
	if k, err := appkey.FromEnv(); err != nil || k != nil {
		return k, err
	}
	v, err := store.Get(keyringService, keyringUser)
	if err == nil {
		return appkey.Parse(v)
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		log.Printf("app key: the credential store cannot be read (%v); secrets in %s stay as they are", err, dataDir)
		return nil, nil
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, auth.FileName)); err == nil && appkey.IsSealed(string(b)) {
		return nil, nil
	}
	k, err := appkey.Generate()
	if err != nil {
		return nil, err
	}
	if err := store.Set(keyringService, keyringUser, k.Hex()); err != nil {
		log.Printf("app key: the credential store refused a new key (%v); secrets in %s stay as they are", err, dataDir)
		return nil, nil
	}
	return k, nil
}
