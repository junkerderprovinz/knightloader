package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

type memKeyStore struct {
	values map[string]string
	getErr error
}

func (m *memKeyStore) Get(service, user string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (m *memKeyStore) Set(service, user, secret string) error {
	m.values[service+"/"+user] = secret
	return nil
}

func TestTheFirstStartMakesAKeyAndLaterStartsReuseIt(t *testing.T) {
	t.Setenv(appkey.EnvVar, "")
	store := &memKeyStore{values: map[string]string{}}
	dir := t.TempDir()
	first, err := loadAppKey(store, dir)
	if err != nil || first == nil {
		t.Fatalf("first start = %v, %v; want a new key", first, err)
	}
	second, err := loadAppKey(store, dir)
	if err != nil || second == nil || second.Hex() != first.Hex() {
		t.Fatal("a later start did not read back the key the first one stored")
	}
}

func TestNoNewKeyIsMadeOverASealedDataFolder(t *testing.T) {
	t.Setenv(appkey.EnvVar, "")
	dir := t.TempDir()
	old, _ := appkey.Generate()
	if _, err := auth.OpenWithKey(dir, old); err != nil {
		t.Fatal(err)
	}
	store := &memKeyStore{values: map[string]string{}}
	k, err := loadAppKey(store, dir)
	if err != nil || k != nil || len(store.values) > 0 {
		t.Fatalf("with the stored key gone a new one was made (%v, %v); it could not open %s", k, err, filepath.Join(dir, auth.FileName))
	}
}

func TestAnUnreachableCredentialStoreRunsWithoutAKey(t *testing.T) {
	t.Setenv(appkey.EnvVar, "")
	store := &memKeyStore{values: map[string]string{}, getErr: errors.New("no secret service")}
	if k, err := loadAppKey(store, t.TempDir()); k != nil || err != nil {
		t.Fatalf("loadAppKey = %v, %v; want no key and no error", k, err)
	}
}

func TestTheVariableWinsOverTheCredentialStore(t *testing.T) {
	want, _ := appkey.Generate()
	t.Setenv(appkey.EnvVar, want.Hex())
	stored, _ := appkey.Generate()
	store := &memKeyStore{values: map[string]string{keyringService + "/" + keyringUser: stored.Hex()}}
	k, err := loadAppKey(store, t.TempDir())
	if err != nil || k == nil || k.Hex() != want.Hex() {
		t.Fatal("KL_APP_KEY did not win over the credential store")
	}
}
