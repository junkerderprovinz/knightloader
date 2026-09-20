//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

// revealInFolder asks Explorer to select the file. "/select," and the path
// must be one argument; split, Explorer opens the home folder instead.
func revealInFolder(path string) error {
	cmd := exec.Command("explorer", "/select,"+path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Explorer: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively uses cmd's start, which resolves the default handler as a
// double-click does. The empty argument is the window title; without it start
// would take a quoted path as the title.
func openNatively(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	reap(cmd)
	return nil
}

// reap waits for a launched process in the background so it does not linger.
// The exit code is ignored because explorer.exe exits non-zero on success.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
