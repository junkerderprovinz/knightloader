//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// suspend is pmset's own "sleep now", which is what the Apple menu's Sleep
// item ends up asking for as well. It needs no administrator rights for the
// current user's own machine, which is why it is preferred over an
// osascript "tell application System Events to sleep" - that one goes through
// the automation permission prompt, and a prompt is exactly what an
// unattended end-of-queue action cannot answer.
//
// Run and WAITED FOR, unlike files_darwin.go's launches: the caller wants to
// know whether this worked, and the record on the settings page is built from
// the answer. The combined output travels with a failure because pmset says
// what it refused and why, and that sentence is the only thing an operator
// can act on.
func suspend() error {
	out, err := exec.Command("pmset", "sleepnow").CombinedOutput()
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		return fmt.Errorf("%w: %s", err, text)
	}
	return err
}
