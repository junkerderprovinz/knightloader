//go:build linux

package main

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/junkerderprovinz/knightloader/internal/flatpak"
)

// inhibitTimeout keeps a bus that does not answer from holding up the guard's
// pass.
const inhibitTimeout = 5 * time.Second

// preventSleep asks logind for a sleep inhibitor lock and holds it until the
// returned release is called. logind hands the lock over as a file
// descriptor, and closing it is the release; if this process dies, the kernel
// closes it, so a crash cannot keep the machine up.
//
// "sleep" in block mode stops suspend and hibernate and leaves the screen
// alone. An active local session may take one without a password in logind's
// default policy; a session over SSH may be refused, and the reason is logged.
func preventSleep() (func() error, error) {
	if flatpak.ID() != "" {
		return portalInhibit()
	}
	ctx, cancel := context.WithTimeout(context.Background(), inhibitTimeout)
	defer cancel()
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("no D-Bus system bus to ask logind on: %w", err)
	}
	var fd dbus.UnixFD
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").CallWithContext(
		ctx, "org.freedesktop.login1.Manager.Inhibit", 0,
		"sleep", "KnightLoader", "Downloads are running", "block",
	).Store(&fd)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("logind would not hold off sleep: %w", err)
	}
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() {
			closeErr = syscall.Close(int(fd))
			conn.Close()
		})
		return closeErr
	}, nil
}

// suspendFlag is the Inhibit portal's flag for suspending the machine.
const suspendFlag = 4

// portalInhibit holds off sleep for a Flatpak through the Inhibit portal,
// since Flathub does not let a sandbox talk to logind. The portal ties the
// lock to this connection, so it ends with the process as logind's does.
func portalInhibit() (func() error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), inhibitTimeout)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("no D-Bus session bus to ask the Inhibit portal on: %w", err)
	}
	var handle dbus.ObjectPath
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").CallWithContext(
		ctx, "org.freedesktop.portal.Inhibit.Inhibit", 0,
		"", uint32(suspendFlag), map[string]dbus.Variant{"reason": dbus.MakeVariant("Downloads are running")},
	).Store(&handle)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the Inhibit portal would not hold off sleep: %w", err)
	}
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() {
			closeErr = conn.Object("org.freedesktop.portal.Desktop", handle).
				Call("org.freedesktop.portal.Request.Close", 0).Err
			conn.Close()
		})
		return closeErr
	}, nil
}
