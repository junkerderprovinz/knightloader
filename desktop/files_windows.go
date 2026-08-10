//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

// revealInFolder asks Explorer to select the file. /select, and the path are
// one argument, not two - Explorer's own command-line parsing reads them as a
// single token, and splitting them the way a normal flag would be passed
// leaves Explorer opening the user's home folder instead.
func revealInFolder(path string) error {
	cmd := exec.Command("explorer", "/select,"+path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Explorer: %w", err)
	}
	reap(cmd)
	return nil
}

// openNatively goes through cmd's own "start" rather than exec.Command(path)
// directly, because start is what resolves the file's default handler the
// same way double-clicking it would; Explorer given a bare file path does not
// reliably do that for every file type. The empty quoted argument keeps start
// from reading the path itself as a window title, which is what it does with
// the first quoted argument when more follow.
func openNatively(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	reap(cmd)
	return nil
}

// reap waits on a launched GUI process off to the side so it does not sit as
// a zombie process-table entry for the rest of this app's run - Start without
// a matching Wait leaves exactly that behind on every call. Nothing here
// blocks on it or reads its result: explorer.exe in particular exits non-zero
// on success, a well-known Windows quirk rather than a failure signal, so its
// exit code is not a success/failure signal worth keeping.
func reap(cmd *exec.Cmd) {
	go func() { _ = cmd.Wait() }()
}
