//go:build bindings

package main

// wails build runs the program once with the bindings tag to read its bound
// methods. The tray and the updater have no business in that run, and on macOS
// a tray loop that wins the main thread from Wails aborts it.
func init() {
	generatingBindings = true
}
