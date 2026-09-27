//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/update"
)

// isInstalled reports whether this program is the copy the installer put in
// place for all users. A portable copy anywhere else updates itself.
func isInstalled() bool {
	loc := update.InstallLocation(product)
	if loc == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return strings.EqualFold(filepath.Dir(exe), filepath.Clean(loc))
}

// machineData returns the installation's folder under ProgramData.
func machineData() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, product), nil
}

// updateInstalled is what the scheduled task runs as the system account, which
// may write to Program Files: one update of the installed copy, without a
// window, logged beside the switch it obeys.
func updateInstalled() error {
	dir, err := machineData()
	if err != nil {
		return err
	}
	logger := openUpdateLog(filepath.Join(dir, "update.log"))
	if !isInstalled() {
		logger.Printf("update: this is not the copy installed for all users, so --update leaves it alone")
		return errors.New("not the installed copy")
	}

	up := newUpdater(logger.Printf, func(string) {})
	up.u.UninstallKey = product
	up.u.Cleanup()
	if buildinfo.Version == "dev" {
		logger.Printf("update: a dev build has no version to compare, so it does not update itself")
		return nil
	}
	if !settings.ReadAutoUpdate(filepath.Join(dir, machineSettingsFile)) {
		logger.Printf("update: Update automatically is off, so this run does nothing")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	up.once(ctx)
	return nil
}

// openUpdateLog appends to the log at path, since the task has no console to
// say why an update did not happen. A log grown past 256 KiB starts over.
func openUpdateLog(path string) *log.Logger {
	var w io.Writer = os.Stderr
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > 256<<10 {
		flags |= os.O_TRUNC
	}
	if f, err := os.OpenFile(path, flags, 0o644); err == nil {
		w = f
	}
	return log.New(w, "", log.LstdFlags)
}
