//go:build !windows

package api

// volumeRoots is the one filesystem root everywhere but Windows.
func volumeRoots(p string) []string {
	return []string{volumeRoot(p)}
}
