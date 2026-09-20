//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// suspend runs pmset sleepnow, which needs no administrator rights. osascript
// would trigger the automation permission prompt, which nobody is there to
// answer at the end of a queue. pmset's output goes along with a failure
// because it says what was refused.
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
