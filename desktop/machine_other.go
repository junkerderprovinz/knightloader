//go:build !windows

package main

import "errors"

// isInstalled reports false: only the Windows installer puts a copy in place
// for all users.
func isInstalled() bool { return false }

func machineData() (string, error) {
	return "", errors.New("only Windows has a folder for all users")
}

func updateInstalled() error {
	return errors.New("--update is what the Windows installer's scheduled task runs")
}
