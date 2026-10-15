package vpnconn

import (
	"os"
	"testing"
	"time"
)

// TestLiveProvidersRefuseAnUnknownAccount asks the real APIs of the providers
// KnightLoader logs in to itself with an account none of them has, when
// KL_VPN_LIVE is set.
func TestLiveProvidersRefuseAnUnknownAccount(t *testing.T) {
	if os.Getenv("KL_VPN_LIVE") == "" {
		t.Skip("KL_VPN_LIVE is not set")
	}
	cases := []struct {
		id     string
		fields map[string]string
	}{
		{"mullvad", map[string]string{"accountNumber": "0000000000000000"}},
		{"obscura", map[string]string{"accountNumber": "00000000000000000000"}},
		{"azirevpn", map[string]string{"username": "knightloader-probe", "password": "not-the-password"}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			m, err := New(Options{Store: &memStore{}, Logf: t.Logf})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if _, err := m.Connect(c.id, c.fields); err != nil {
				t.Fatal(err)
			}
			var p ProviderState
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline) && p.State != "failed"; time.Sleep(200 * time.Millisecond) {
				for _, q := range m.State().Providers {
					if q.ID == c.id {
						p = q
					}
				}
			}
			t.Logf("%s: %s %q %s", c.id, p.State, p.ErrorCode, p.Error)
			if p.ErrorCode != CodeLoginRefused {
				t.Errorf("an unknown account ended %s %q", p.State, p.ErrorCode)
			}
		})
	}
}
