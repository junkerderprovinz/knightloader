//go:build linux || freebsd || openbsd || netbsd

package main

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// probeTimeout keeps an unresponsive session bus from hanging startup.
const probeTimeout = 2 * time.Second

// probeTray reports whether a tray host will show the icon. GNOME has no
// StatusNotifierWatcher without an extension, i3 and sway need a bar that
// provides one, and a minimal session may have no bus at all. It checks the
// bus name go-systray registers with.
func probeTray() (ok bool, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return false, "no D-Bus session bus available"
	}
	defer conn.Close()

	var hasOwner bool
	err = conn.BusObject().CallWithContext(
		ctx, "org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher",
	).Store(&hasOwner)
	if err != nil {
		return false, "could not query the D-Bus session bus"
	}
	if !hasOwner {
		return false, `no system tray host registered (StatusNotifierWatcher absent - GNOME needs the "AppIndicator and KStatusNotifierItem Support" extension; most other desktops provide one out of the box)`
	}
	return true, ""
}
