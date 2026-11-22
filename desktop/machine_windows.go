//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

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

// updateInstalled is what the scheduled task runs as the system account, and
// Update now as an administrator, both of whom may write to Program Files: one
// update of the installed copy, without a window, logged beside the switch the
// task obeys. Update now passes now, which goes past the switch and keeps a
// record of the run for the window that asked.
func updateInstalled(now bool) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if now {
		return up.updateNow(ctx, filepath.Join(dir, progressFile))
	}
	if !settings.ReadAutoUpdate(filepath.Join(dir, machineSettingsFile)) {
		logger.Printf("update: Update automatically is off, so this run does nothing")
		return nil
	}
	up.once(ctx)
	return nil
}

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// shellExecuteInfo is SHELLEXECUTEINFOW, which x/sys/windows does not have.
type shellExecuteInfo struct {
	size          uint32
	mask          uint32
	window        windows.HWND
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       windows.Handle
	idList        uintptr
	class         *uint16
	classKey      windows.Handle
	hotKey        uint32
	iconOrMonitor windows.Handle
	process       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x40
	seeMaskFlagNoUI       = 0x400
)

// runElevatedUpdate starts this program as an administrator to run Update
// now, so Windows asks for permission first, and follows the record that run
// keeps until it ends.
func runElevatedUpdate(ctx context.Context, progress func(update.Progress)) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := machineData()
	if err != nil {
		return err
	}
	record := filepath.Join(dir, progressFile)
	started := time.Now()
	proc, err := startElevated(exe, updateFlag+" "+nowFlag)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(proc)
	for {
		ev, err := windows.WaitForSingleObject(proc, 250)
		if err != nil {
			return err
		}
		if p, ok := recordSince(record, started); ok {
			progress(p)
		}
		if ev == windows.WAIT_OBJECT_0 {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(proc, &code); err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	if p, ok := recordSince(record, started); ok && p.State == update.Failed {
		return errors.New(p.Error)
	}
	return fmt.Errorf("the update ended with exit code %d", code)
}

// recordSince reads the record at path unless an earlier run left it.
func recordSince(path string, started time.Time) (update.Progress, bool) {
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Before(started) {
		return update.Progress{}, false
	}
	p, err := readProgress(path)
	return p, err == nil
}

// startElevated runs exe with params through the runas verb and returns the
// process. Saying no to Windows ends up here as an error.
func startElevated(exe, params string) (windows.Handle, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	args, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoCloseProcess | seeMaskFlagNoUI,
		verb:       verb,
		file:       file,
		parameters: args,
		directory:  dir,
		show:       windows.SW_HIDE,
	}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return 0, errors.New("permission to run it was refused")
		}
		return 0, err
	}
	if info.process == 0 {
		return 0, errors.New("Windows started no process for the update")
	}
	return info.process, nil
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
