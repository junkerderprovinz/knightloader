package update

import (
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallRoot = `Software\Microsoft\Windows\CurrentVersion\Uninstall\`

// uninstallHive holds the entry the installer writes for all users. The tests
// point it at HKCU, which they can write without an administrator.
var uninstallHive = registry.LOCAL_MACHINE

// InstallLocation returns the folder the installer recorded in the uninstall
// entry keyName, or "" when KnightLoader is not installed.
func InstallLocation(keyName string) string {
	return readEntry(keyName, "InstallLocation")
}

// InstalledVersion returns the version the uninstall entry keyName names,
// which the scheduled update sets whenever it puts a new version in place.
func InstalledVersion(keyName string) string {
	return readEntry(keyName, "DisplayVersion")
}

func readEntry(keyName, value string) string {
	k, err := registry.OpenKey(uninstallHive, uninstallRoot+keyName, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, err := k.GetStringValue(value)
	if err != nil {
		return ""
	}
	return s
}

// recordVersion keeps the version in Windows' list of installed apps in step
// with the program that will run. The entry belongs to the installed copy, so a
// portable copy elsewhere leaves it alone.
func recordVersion(keyName, exe, version string) error {
	k, err := registry.OpenKey(uninstallHive, uninstallRoot+keyName, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	icon, _, err := k.GetStringValue("DisplayIcon")
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(icon), filepath.Clean(exe)) {
		return nil
	}
	return k.SetStringValue("DisplayVersion", version)
}

const (
	shcneUpdateItem   = 0x00002000
	shcneAssocChanged = 0x08000000
	shcnfPath         = 0x0005
)

var shChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

// iconChanged tells Explorer that the program at path was replaced. Without it
// Windows keeps the old program's icon in its cache, so a pinned or Start menu
// shortcut goes on showing it after an update.
var iconChanged = func(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	shChangeNotify.Call(shcneUpdateItem, shcnfPath, uintptr(unsafe.Pointer(p)), 0)
	shChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
}
