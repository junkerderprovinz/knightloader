//go:build windows

package main

import "syscall"

var procGetClipboardSequenceNumber = syscall.NewLazyDLL("user32.dll").NewProc("GetClipboardSequenceNumber")

// openClipboard reads the clipboard only after its sequence number moved, so
// the watch does not hold the clipboard open every second against other
// programs that want it.
func openClipboard(read func() (string, bool)) (clipboardReader, bool) {
	return &polledClipboard{read: read, stamp: clipboardSequence}, true
}

// clipboardSequence is 0 for a session without clipboard access, and then the
// clipboard is read every time.
func clipboardSequence() (uint64, bool) {
	n, _, _ := procGetClipboardSequenceNumber.Call()
	return uint64(n), n != 0
}
