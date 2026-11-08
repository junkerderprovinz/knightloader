// Package flatpak tells whether the program runs inside a Flatpak sandbox,
// where Flatpak installs its updates and the sandbox allows bus names under
// the app's own ID alone.
package flatpak

import "os"

// ID returns the application ID the program runs under inside a Flatpak, or ""
// outside one. Flatpak sets FLATPAK_ID for every program it starts.
func ID() string { return os.Getenv("FLATPAK_ID") }
