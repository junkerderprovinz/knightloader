//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// revealInFolder uses open -R, which selects the file in Finder.
func revealInFolder(path string) error {
	cmd := exec.Command("open", "-R", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Finder: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively lets Launch Services pick the application, as a double-click in
// Finder does.
func openNatively(path string) error {
	cmd := exec.Command("open", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	reap(cmd)
	return nil
}

// reap waits for a launched process in the background so it does not linger as
// a zombie.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
