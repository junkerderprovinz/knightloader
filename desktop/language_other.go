//go:build !windows && !linux

package main

// installerChoices is "": only the Windows and Linux installers leave a
// language behind.
func installerChoices() string { return "" }
