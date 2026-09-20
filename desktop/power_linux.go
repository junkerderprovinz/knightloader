//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// suspend asks logind through systemctl, which every Linux desktop answers the
// same way. There is no fallback chain: each alternative fails differently, and
// /sys/power/state needs root. The output is kept verbatim because polkit's
// "Interactive authentication required." on a headless or SSH session tells
// the operator what to fix.
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
