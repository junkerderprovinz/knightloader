//go:build windows

package main

import "testing"

// The hold lasts a moment and is given back, so running this leaves the
// machine's power state as it was.
func TestWindowsGrantsAndTakesBackTheHoldOnSleep(t *testing.T) {
	release, err := preventSleep()
	if err != nil {
		t.Fatalf("preventSleep: %v", err)
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
	if err := release(); err != nil {
		t.Errorf("a second release failed: %v", err)
	}
}
