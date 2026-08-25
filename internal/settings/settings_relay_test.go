package settings

import "testing"

func TestSanitizeRelayNormalisesTheURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"untouched", "https://relay.example.com", "https://relay.example.com"},
		{"trims whitespace", "  https://relay.example.com  ", "https://relay.example.com"},
		{"drops a trailing slash", "https://relay.example.com/", "https://relay.example.com"},
		{"drops several", "https://relay.example.com///", "https://relay.example.com"},
		{"trims then drops", "  ws://192.168.20.11:8760/  ", "ws://192.168.20.11:8760"},
		{"empty stays empty", "   ", ""},
		{"a path is kept, only its trailing slash goes", "https://relay.example.com/kl/", "https://relay.example.com/kl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeRelay(Settings{RelayURL: c.in}).RelayURL; got != c.want {
				t.Errorf("sanitizeRelay(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestRelayURLRoundTrips proves the field survives the whole store: written
// through Set, re-read from the file by a second Load, and normalised on the
// way in rather than kept verbatim.
func TestRelayURLRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get()
	n.RelayURL = "  https://relay.example.com/  "
	if _, err := s.Set(n); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().RelayURL; got != "https://relay.example.com" {
		t.Errorf("RelayURL after Set = %q, want it sanitised", got)
	}

	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().RelayURL; got != "https://relay.example.com" {
		t.Errorf("RelayURL after reload = %q, want it persisted", got)
	}
}

// TestRelayURLDefaultsToEmpty pins that a fresh install dials nothing: the
// relay is opt-in, and a default address would be this project operating a
// service on everyone's behalf, which is the one thing the design spec rules
// out.
func TestRelayURLDefaultsToEmpty(t *testing.T) {
	if got := Defaults().RelayURL; got != "" {
		t.Errorf("Defaults().RelayURL = %q, want empty", got)
	}
}
