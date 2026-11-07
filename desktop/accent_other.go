//go:build !windows && !darwin && !linux

package main

// readAccent has no source to ask on this system.
func readAccent() string { return "" }
