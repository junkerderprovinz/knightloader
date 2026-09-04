package api

// The relay mode, and the one thing it exists to make expressible: no relay at
// all.
//
// jdp, 2026-09-04, asked for two cards with a switch each and then asked the
// question that decides the whole shape: "was gilt, wenn du beide
// ausschaltest?" He chose "kein Relay, Instanzen finden sich nur im LAN", and
// that answer had nowhere to live. The old code inferred the relay from
// RelayURL - empty meant the project's, set meant your own - which has room
// for two answers and needed a third.
//
// These tests pin the three things that inference could not do: the third
// state, the migration for every install that predates the field, and the
// switch actually taking effect rather than being a stored string nothing
// reads.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestRelayModeOfReadsAnOldInstallTheWayItBehaved is the migration, and it is
// a READ rather than a write on purpose: nothing rewrites settings.json on
// upgrade, so an install that never opens this page keeps behaving exactly as
// it did, and a downgrade to an older build still works.
func TestRelayModeOfReadsAnOldInstallTheWayItBehaved(t *testing.T) {
	// The two shapes an install from before the field can be in.
	if got := (settings.Settings{}).RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an old install with no relay address = %q, want %q - that install used the project's relay",
			got, settings.RelayModeProject)
	}
	old := settings.Settings{RelayURL: "wss://relay.example.com/relay/connect"}
	if got := old.RelayModeOf(); got != settings.RelayModeOwn {
		t.Errorf("an old install with an address = %q, want %q - that install used its own relay", got, settings.RelayModeOwn)
	}
	// Whitespace is not an address. Without the trim, a field somebody cleared
	// by selecting and typing a space would read as "own relay" and dial an
	// empty address for ever.
	blank := settings.Settings{RelayURL: "   "}
	if got := blank.RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an address of only whitespace = %q, want %q", got, settings.RelayModeProject)
	}
}

// TestAnExplicitModeAlwaysWinsOverTheInference is the half that makes the
// switch a switch. Somebody who types an address, tries it, and then switches
// back to the project's relay leaves that address in the field - and if the
// inference still won, the switch would appear to do nothing.
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

// TestAnUnknownModeFallsBackRatherThanBreaking: a value this build does not
// know is treated as "not set", which is the same reading an older build gives
// a mode written by a newer one. The alternative - refusing to resolve - would
// turn a settings file written by a version somebody rolled back from into an
// instance that reaches nothing.
func TestAnUnknownModeFallsBackRatherThanBreaking(t *testing.T) {
	s := settings.Settings{RelayMode: "mesh", RelayURL: "wss://relay.example.com/relay/connect"}
	if got := s.RelayModeOf(); got != settings.RelayModeOwn {
		t.Errorf("an unknown mode with an address = %q, want the legacy reading %q", got, settings.RelayModeOwn)
	}
	if got := (settings.Settings{RelayMode: "mesh"}).RelayModeOf(); got != settings.RelayModeProject {
		t.Errorf("an unknown mode with no address = %q, want %q", got, settings.RelayModeProject)
	}
}
