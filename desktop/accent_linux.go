//go:build linux

package main

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// readAccent asks the settings portal, which speaks for GNOME, KDE and the
// other desktops that implement it. A desktop without the portal, or one that
// names no accent, has no system colour.
func readAccent() string {
	conn, err := dbus.SessionBus()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var v dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "accent-color").
		Store(&v)
	if err != nil {
		return ""
	}
	return portalAccent(v)
}
