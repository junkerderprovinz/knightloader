//go:build linux

package main

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

// inhibitTimeout keeps a system bus that does not answer from holding up the
// guard's pass.
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
