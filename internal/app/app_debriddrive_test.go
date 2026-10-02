package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

func TestADriveFolderIsNamedAfterItsServiceAndAccount(t *testing.T) {
	cases := map[string]string{
		"realdebrid":                            "Real-Debrid",
		resolver.SlotID("realdebrid", "work"):   "Real-Debrid (work)",
		resolver.SlotID("torbox", "family"):     "TorBox (family)",
		resolver.SlotID("nosuchservice", "one"): "nosuchservice (one)",
	}
	for slot, want := range cases {
		if got := driveFolder(slot); got != want {
			t.Errorf("driveFolder(%q) = %q, want %q", slot, got, want)
		}
	}
}
