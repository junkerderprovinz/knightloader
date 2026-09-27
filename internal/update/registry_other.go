//go:build !windows

package update

// InstallLocation returns "", since only Windows has an installer that
// records one.
func InstallLocation(keyName string) string { return "" }

// InstalledVersion returns "" for the same reason.
func InstalledVersion(keyName string) string { return "" }

func recordVersion(keyName, exe, version string) error {
	return nil
}
