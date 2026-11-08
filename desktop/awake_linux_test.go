package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// Flathub does not let a sandbox talk to logind, so a Flatpak holds off sleep
// through the Inhibit portal on the session bus instead.
func TestAFlatpakAsksThePortalToHoldOffSleep(t *testing.T) {
	dir := t.TempDir()
	system, err := net.Listen("unix", filepath.Join(dir, "system"))
	if err != nil {
		t.Fatal(err)
	}
	defer system.Close()
	dialed := make(chan struct{}, 1)
	go func() {
		if conn, err := system.Accept(); err == nil {
			conn.Close()
			dialed <- struct{}{}
		}
	}()
	t.Setenv("FLATPAK_ID", "io.github.junkerderprovinz.knightloader")
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "system"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "no-session"))

	if release, err := preventSleep(); err == nil {
		release()
		t.Fatal("a sleep lock was held without a session bus to ask the portal on")
	}
	select {
	case <-dialed:
		t.Error("a Flatpak went to logind on the system bus")
	case <-time.After(200 * time.Millisecond):
	}
}
