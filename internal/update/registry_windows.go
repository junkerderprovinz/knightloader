package update

import (
	"errors"
	"path/filepath"
	"strings"

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
