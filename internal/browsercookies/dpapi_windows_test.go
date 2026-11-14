package browsercookies

import (
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(t *testing.T, plain []byte) []byte {
	t.Helper()
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		t.Fatal(err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return slices.Clone(unsafe.Slice(out.Data, out.Size))
}

func TestChromiumOnWindowsDecryptsWithTheKeyDPAPISeals(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	sealed := append([]byte("DPAPI"), protect(t, key)...)

	local := t.TempDir()
	root := filepath.Join(local, "Microsoft", "Edge", "User Data")
	chromiumHome(t, root, map[string]string{"Default": "Personal"}, map[string]any{
		"os_crypt": map[string]any{"encrypted_key": base64.StdEncoding.EncodeToString(sealed)},
	})
	writeChromiumDB(t, filepath.Join(root, "Default", "Network", "Cookies"), 24, []chromiumRow{
		{host: ".patreon.com", name: "session_id", encrypted: sealGCM(t, key, ".patreon.com", "S1", true), expires: future, lastAccess: used},
		{host: "www.patreon.com", name: "old", encrypted: protect(t, []byte("BEFORE-V10")), expires: future},
	})

	profiles := findProfiles("windows", dirs{localAppData: local})
	if got := labels(profiles); !slices.Equal(got, []string{"Edge (Personal)"}) {
		t.Fatalf("profiles = %q", got)
	}
	cookies, err := profiles[0].Cookies([]string{"patreon.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(cookies), []string{".patreon.com session_id=S1", "www.patreon.com old=BEFORE-V10"}; !slices.Equal(got, want) {
		t.Errorf("cookies = %q, want %q", got, want)
	}
}

func TestAWrongKeyIsReportedRatherThanGivingGarbage(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	other := make([]byte, 32)
	rand.Read(other)
	local := t.TempDir()
	root := filepath.Join(local, "Vivaldi", "User Data")
	chromiumHome(t, root, map[string]string{"Default": "Default"}, map[string]any{
		"os_crypt": map[string]any{"encrypted_key": base64.StdEncoding.EncodeToString(append([]byte("DPAPI"), protect(t, other)...))},
	})
	writeChromiumDB(t, filepath.Join(root, "Default", "Network", "Cookies"), 24, []chromiumRow{
		{host: ".patreon.com", name: "session_id", encrypted: sealGCM(t, key, ".patreon.com", "S1", true), expires: future},
	})
	_, err := findProfiles("windows", dirs{localAppData: local})[0].Cookies([]string{"patreon.com"})
	if err == nil {
		t.Fatal("a value sealed with another key was read")
	}
}
