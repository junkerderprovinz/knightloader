//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// revealInFolder asks Explorer to select the file.
func revealInFolder(path string) error {
	cmd := revealCommand(path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Explorer: %w", err)
	}
	reap(cmd)
	return nil
}

// revealCommand writes Explorer's command line by hand, since Explorer parses
// it itself. Given "/select,<path>" in quotes, as Go quotes a path with a
// space, or a path with a bare comma, it opens Documents. A Windows path
// cannot contain a quote, so quoting the path alone holds for every path.
func revealCommand(path string) *exec.Cmd {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd
}

// openNatively hands path to the shell as a double-click does, with the
// default verb of its type, and opens a folder in Explorer. No command
// interpreter is involved, so an & or a % in a name stays part of the name.
func openNatively(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, nil, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	return nil
}

// reap waits for a launched process in the background so it does not linger.
// The exit code is ignored because explorer.exe exits non-zero on success.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
