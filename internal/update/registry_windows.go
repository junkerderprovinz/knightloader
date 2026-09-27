package update

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const uninstallRoot = `Software\Microsoft\Windows\CurrentVersion\Uninstall\`

// recordVersion keeps the version in Windows' list of installed apps in step
// with the program that will run. The entry belongs to the installed copy, so a
// portable copy elsewhere leaves it alone.
func recordVersion(keyName, exe, version string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallRoot+keyName, registry.QUERY_VALUE|registry.SET_VALUE)
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
