//go:build !windows

package update

func recordVersion(keyName, exe, version string) error {
	return nil
}
