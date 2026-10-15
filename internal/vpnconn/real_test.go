package vpnconn

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestRealHideMe connects hide.me through the Manager with the account in the
// JSON file KL_VPN_HIDEME_CRED ({"u": user, "p": password}).
func TestRealHideMe(t *testing.T) {
	path := os.Getenv("KL_VPN_HIDEME_CRED")
	if path == "" {
		t.Skip("KL_VPN_HIDEME_CRED is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cred struct{ U, P string }
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	m, err := New(Options{Store: store, Logf: t.Logf, ManualPort: func() int { return 51413 }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Connect("hideme", map[string]string{"username": cred.U, "password": cred.P}); err != nil {
		t.Fatal(err)
	}
	var p ProviderState
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		for _, q := range m.State().Providers {
			if q.ID == "hideme" {
				p = q
			}
		}
		if p.State == "failed" || p.State == "connected" && p.ForwardedPort != 0 && p.ExitIP != "" {
			break
		}
	}
	t.Logf("hide.me through the manager: %s, exit %s, forwarded port %d, %s %s", p.State, p.ExitIP, p.ForwardedPort, p.ErrorCode, p.Error)
	if p.State != "connected" || p.ForwardedPort == 0 {
		t.Fatal("not connected with a forwarded port")
	}
	if login := m.login("hideme"); login[accessTokenField] == "" {
		t.Error("the access token was not kept")
	}
	m.Disconnect()
}

func TestRealHideMeRefusesAWrongPassword(t *testing.T) {
	if os.Getenv("KL_VPN_HIDEME_CRED") == "" {
		t.Skip("KL_VPN_HIDEME_CRED is not set")
	}
	m, err := New(Options{Store: &memStore{}, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Connect("hideme", map[string]string{"username": "knightloader-probe", "password": "not-the-password"})
	var p ProviderState
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline) && p.State != "failed"; time.Sleep(200 * time.Millisecond) {
		for _, q := range m.State().Providers {
			if q.ID == "hideme" {
				p = q
			}
		}
	}
	if p.ErrorCode != CodeLoginRefused {
		t.Errorf("a wrong password ended %s %q %s", p.State, p.ErrorCode, p.Error)
	}
}

// TestRealProvidersRefuseAWrongLogin logs in to the providers KnightLoader
// reaches through their APIs with a login none of them has, when
// KL_VPN_LIVE is set.
func TestRealProvidersRefuseAWrongLogin(t *testing.T) {
	if os.Getenv("KL_VPN_LIVE") == "" {
		t.Skip("KL_VPN_LIVE is not set")
	}
	for id, fields := range map[string]map[string]string{
		"nordvpn":  {"token": "0000000000000000000000000000000000000000000000000000000000000000"},
		"pia":      {"username": "p0000000", "password": "not-the-password", "location": "de_berlin"},
		"torguard": {"username": "knightloader-probe", "password": "not-the-password"},
	} {
		t.Run(id, func(t *testing.T) {
			m, err := New(Options{Store: &memStore{}, Logf: t.Logf})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if _, err := m.Connect(id, fields); err != nil {
				t.Fatal(err)
			}
			var p ProviderState
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline) && p.State != "failed"; time.Sleep(200 * time.Millisecond) {
				for _, q := range m.State().Providers {
					if q.ID == id {
						p = q
					}
				}
			}
			t.Logf("%s: %s %q %s", id, p.State, p.ErrorCode, p.Error)
			if p.ErrorCode != CodeLoginRefused {
				t.Errorf("a wrong login ended %s %q %s", p.State, p.ErrorCode, p.Error)
			}
		})
	}
}
