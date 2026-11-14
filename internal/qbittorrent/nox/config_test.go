package nox

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readConfig(t *testing.T, profile string) string {
	t.Helper()
	b, err := os.ReadFile(ConfigPath(profile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestThePasswordIsStoredAsTheHashQBittorrentChecks(t *testing.T) {
	profile := t.TempDir()
	if err := WriteConfig(profile, Config{Port: 18080, Username: "knightloader", Password: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	conf := readConfig(t, profile)
	if strings.Contains(conf, "s3cret") {
		t.Fatal("the password itself is in the file")
	}
	m := regexp.MustCompile(`(?m)^WebUI\\Password_PBKDF2="@ByteArray\(([^:]+):([^)]+)\)"\r?$`).FindStringSubmatch(conf)
	if m == nil {
		t.Fatalf("no PBKDF2 line in\n%s", conf)
	}
	salt, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || len(salt) != 16 {
		t.Fatalf("salt %q: %v", m[1], err)
	}
	want, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		t.Fatal(err)
	}
	got, err := pbkdf2.Key(sha512.New, "s3cret", salt, 100000, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the stored hash is not PBKDF2-SHA512 of the password")
	}
}

func TestTheWebUIIsBoundToLoopbackWithItsProtectionsOn(t *testing.T) {
	profile := t.TempDir()
	if err := WriteConfig(profile, Config{Port: 18080, Username: "knightloader", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	conf := readConfig(t, profile)
	for _, line := range []string{
		`WebUI\Address=127.0.0.1`,
		`WebUI\Port=18080`,
		`WebUI\Username=knightloader`,
		`WebUI\LocalHostAuth=true`,
		`WebUI\HostHeaderValidation=true`,
		`WebUI\CSRFProtection=true`,
		`WebUI\UseUPnP=false`,
		`WebUI\AuthSubnetWhitelistEnabled=false`,
		`Accepted=true`,
	} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(line) + `\r?$`).MatchString(conf) {
			t.Errorf("missing %q in\n%s", line, conf)
		}
	}
}

func TestOwnKeysReplaceTheirOldValuesAndEverythingElseStays(t *testing.T) {
	profile := t.TempDir()
	path := ConfigPath(profile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := "[BitTorrent]\nSession\\Port=51413\n\n[Preferences]\nGeneral\\Locale=de\nWebUI\\Port=8080\nWebUI\\Address=*\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(profile, Config{Port: 18081, Username: "knightloader", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	conf := readConfig(t, profile)
	for _, keep := range []string{`Session\Port=51413`, `General\Locale=de`, "[BitTorrent]"} {
		if !strings.Contains(conf, keep) {
			t.Errorf("lost %q:\n%s", keep, conf)
		}
	}
	if strings.Contains(conf, `WebUI\Port=8080`) || strings.Contains(conf, `WebUI\Address=*`) {
		t.Errorf("an old value survived:\n%s", conf)
	}
	if n := strings.Count(conf, "[Preferences]"); n != 1 {
		t.Errorf("[Preferences] appears %d times:\n%s", n, conf)
	}
}

func TestTheConfigIsReadableOnlyByItsOwner(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows keeps the folder private through its ACL, not the mode bits")
	}
	profile := t.TempDir()
	if err := WriteConfig(profile, Config{Port: 1, Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ConfigPath(profile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}
