package api

// The relay mode makes "no relay, instances only find each other on the LAN"
// expressible, which inferring the relay from RelayURL could not.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestRelayModeOfReadsAnOldInstallTheWayItBehaved checks the migration, which
// is a read rather than a rewrite of settings.json, so a downgrade still works.
func TestRelayModeOfReadsAnOldInstallTheWayItBehaved(t *testing.T) {
	if got := (settings.Settings{}).RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an old install with no relay address = %q, want %q; that install used the project's relay",
			got, settings.RelayModeProject)
	}
	old := settings.Settings{RelayURL: "wss://relay.example.com/relay/connect"}
	if got := old.RelayModeOf(); got != settings.RelayModeOwn {
		t.Errorf("an old install with an address = %q, want %q; that install used its own relay", got, settings.RelayModeOwn)
	}
	// Whitespace is not an address.
	blank := settings.Settings{RelayURL: "   "}
	if got := blank.RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an address of only whitespace = %q, want %q", got, settings.RelayModeProject)
	}
}

// TestAnExplicitModeAlwaysWinsOverTheInference covers switching back to the
// project relay while an address is still in the field.
func TestAnExplicitModeAlwaysWinsOverTheInference(t *testing.T) {
	withAddress := "wss://relay.example.com/relay/connect"
	cases := []struct {
		name string
		in   settings.Settings
		want string
	}{
		{"project chosen while an address is still stored",
			settings.Settings{RelayMode: settings.RelayModeProject, RelayURL: withAddress}, settings.RelayModeProject},
		{"off chosen while an address is still stored",
			settings.Settings{RelayMode: settings.RelayModeOff, RelayURL: withAddress}, settings.RelayModeOff},
		{"own chosen with no address yet typed",
			settings.Settings{RelayMode: settings.RelayModeOwn}, settings.RelayModeOwn},
		{"off chosen on an install with nothing configured",
			settings.Settings{RelayMode: settings.RelayModeOff}, settings.RelayModeOff},
	}
	for _, c := range cases {
		if got := c.in.RelayModeOf(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAnUnknownModeFallsBackRatherThanBreaking checks that a mode written by a
// newer build reads as not set, so rolling back does not cut the instance off.
func TestAnUnknownModeFallsBackRatherThanBreaking(t *testing.T) {
	s := settings.Settings{RelayMode: "mesh", RelayURL: "wss://relay.example.com/relay/connect"}
	if got := s.RelayModeOf(); got != settings.RelayModeOwn {
		t.Errorf("an unknown mode with an address = %q, want the legacy reading %q", got, settings.RelayModeOwn)
	}
	if got := (settings.Settings{RelayMode: "mesh"}).RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an unknown mode with no address = %q, want %q", got, settings.RelayModeProject)
	}
}
