//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// revealInFolder opens the containing folder without selecting the file, since
// Linux file managers share no flag for that.
func revealInFolder(path string) error {
	cmd := exec.Command("xdg-open", filepath.Dir(path))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open a file manager: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively lets xdg-open pick the application by MIME type.
func openNatively(path string) error {
	cmd := exec.Command("xdg-open", path)
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
