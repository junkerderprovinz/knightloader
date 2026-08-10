//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// revealInFolder opens the containing folder rather than selecting the file
// inside it. Unlike Explorer's /select, or Finder's -R, there is no
// desktop-environment-neutral way to select one file in a running file
// manager on Linux (Nautilus and Dolphin each have their own, incompatible
// flag, and neither is guaranteed installed) - this takes the same folder-only
// fallback every cross-platform desktop app takes here rather than guessing
// at which file manager is running.
func revealInFolder(path string) error {
	cmd := exec.Command("xdg-open", filepath.Dir(path))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open a file manager: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively is xdg-open's own MIME-based resolution of "what opens this
// file", the desktop-portal-standard equivalent of a double-click.
func openNatively(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	reap(cmd)
	return nil
}

// reap waits on a launched process off to the side so it does not sit as a
// zombie process-table entry for the rest of this app's run - Start without a
// matching Wait leaves exactly that behind on every call.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
