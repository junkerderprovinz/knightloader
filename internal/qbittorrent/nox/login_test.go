package nox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

func TestAFirstStartMakesALoginAndLaterStartsReadItBack(t *testing.T) {
	profile := t.TempDir()
	first, err := LoadLogin(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Username == "" || len(first.Password) < 32 {
		t.Fatalf("login %+v", first)
	}
	again, err := LoadLogin(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("the second start read %+v, the first made %+v", again, first)
	}
	other, err := LoadLogin(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.Password == first.Password {
		t.Fatal("two profiles got the same password")
	}
}

func TestWithAnAppKeyThePasswordIsSealedAtRest(t *testing.T) {
	key, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	l, err := LoadLogin(profile, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(profile, loginFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), l.Password) {
		t.Fatal("the password is in the file in the clear")
	}
	back, err := LoadLogin(profile, key)
	if err != nil || back != l {
		t.Fatalf("read back %+v, %v; want %+v", back, err, l)
	}
	if _, err := LoadLogin(profile, nil); err == nil {
		t.Fatal("a sealed password was read without the key")
	}
}

func TestAKeyAddedLaterSealsAPlainPassword(t *testing.T) {
	profile := t.TempDir()
	l, err := LoadLogin(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := appkey.Generate()
	if back, err := LoadLogin(profile, key); err != nil || back != l {
		t.Fatalf("read back %+v, %v; want %+v", back, err, l)
	}
	raw, _ := os.ReadFile(filepath.Join(profile, loginFile))
	if strings.Contains(string(raw), l.Password) {
		t.Fatal("the password stayed in the clear")
	}
}

func TestThePortIsKeptWithTheLogin(t *testing.T) {
	profile := t.TempDir()
	l, err := LoadLogin(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Port = 40123
	if err := SaveLogin(profile, nil, l); err != nil {
		t.Fatal(err)
	}
	if back, _ := LoadLogin(profile, nil); back.Port != 40123 {
		t.Fatalf("port %d", back.Port)
	}
}
