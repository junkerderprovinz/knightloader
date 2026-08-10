//go:build linux || freebsd || openbsd || netbsd

package main

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// probeTimeout bounds the startup probe. A session bus that is present but
// unresponsive must not hang app startup waiting for it; a probe that never
// answers is exactly as unavailable as one that answers "no".
const probeTimeout = 2 * time.Second

// probeTray reports whether a tray host is actually likely to show the icon
// before ever calling systray.Run - see tray.go's doc comment for why this
// matters more than it would on Windows or macOS. GNOME ships no
// StatusNotifierWatcher at all without a shell extension, i3/sway have none
// without a bar that provides it, and a minimal container-ish desktop
// session may have no session bus at all; all three must read as "no tray",
// not as a silently invisible icon.
//
// This checks the exact bus name github.com/cardinalby/go-systray itself
// calls RegisterStatusNotifierItem on (systray_unix.go's register()), so the
// probe answers precisely the question "will that call succeed".
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
