//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static long pasteboardChangeCount(void) {
	return (long)[[NSPasteboard generalPasteboard] changeCount];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// openClipboard reads the pasteboard only after its change count moved. The
// count is free to read, while reading the text itself is what newer macOS
// versions may ask the user about.
func openClipboard(read func() (string, bool)) (clipboardReader, bool) {
	return &polledClipboard{read: read, stamp: pasteboardChangeCount}, true
}

func pasteboardChangeCount() (uint64, bool) {
	n := application.InvokeSyncWithResult(func() C.long { return C.pasteboardChangeCount() })
	return uint64(n), true
}
