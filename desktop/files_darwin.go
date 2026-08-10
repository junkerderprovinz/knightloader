//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// revealInFolder is Finder's own "reveal": open -R selects the file inside
// its folder rather than merely opening the folder.
func revealInFolder(path string) error {
	cmd := exec.Command("open", "-R", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Finder: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively is launchservices' own resolution of "what opens this file",
// the same one a double-click in Finder uses.
func openNatively(path string) error {
	cmd := exec.Command("open", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	reap(cmd)
	return nil
}

// reap waits on a launched GUI process off to the side so it does not sit as
// a zombie process-table entry for the rest of this app's run - Start without
// a matching Wait leaves exactly that behind on every call.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
