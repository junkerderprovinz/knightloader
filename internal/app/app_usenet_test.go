package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

func TestADeepbridAccountTakesNZBsAndTorrents(t *testing.T) {
	t.Setenv("KL_JD", "")
	a := newAccountsTestApp(t)
	slotTestHosts(t, "deepbrid-wired.example")
	seedAccount(t, a, "deepbrid", "", "deepbrid-key")

	a.rewireBackends()

	if label, ok := a.UsenetService(); !ok || label != "Deepbrid" {
		t.Errorf("UsenetService = %q, %v; want Deepbrid to take an .nzb", label, ok)
	}
	if _, ok := a.backendFor("deepbrid").(*debrid.TorrentBackend); !ok {
		t.Errorf("backendFor(deepbrid) is %T, want the torrent backend", a.backendFor("deepbrid"))
	}
}
