//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// suspend asks logind, through systemctl, which is the one request every
// desktop Linux answers the same way - GNOME, KDE and a bare Xorg session all
// end at the same place. There is deliberately no fallback chain onto
// dbus-send, loginctl or /sys/power/state: each one fails differently, three
// of them in a row turn one clear refusal into three confusing ones, and
// writing "mem" into /sys/power/state needs root, which this process does not
// have and must not ask for.
//
// THE ERROR TEXT SURVIVES VERBATIM, and that is the whole reason this
// function reads the output at all. A headless box, a seatless session or an
// SSH session gets polkit's "Interactive authentication required." back, which
// is a refusal rather than a failure and has an obvious fix - allow suspend
// for that user, or switch to the command action and sleep the machine from a
// script of your own. A tidier message of our own making would take away the
// one string that says which of those two the reader is looking at. It
// travels from here into internal/app.IdleRun.Output and out onto the
// settings page.
func suspend() error {
	out, err := exec.Command("systemctl", "suspend").CombinedOutput()
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		return fmt.Errorf("%w: %s", err, text)
	}
	return err
}
