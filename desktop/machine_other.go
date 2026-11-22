//go:build !windows

package main

import (
	"context"
	"errors"

	"github.com/junkerderprovinz/knightloader/internal/update"
)

// isInstalled reports false: only the Windows installer puts a copy in place
// for all users.
func isInstalled() bool { return false }

func machineData() (string, error) {
	return "", errors.New("only Windows has a folder for all users")
}

func runElevatedUpdate(context.Context, func(update.Progress)) error {
	return errors.New("only Windows has a copy installed for all users")
}

func updateInstalled(bool) error {
	return errors.New("--update is what the Windows installer's scheduled task runs")
}
