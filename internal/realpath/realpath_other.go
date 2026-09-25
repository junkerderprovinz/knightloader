//go:build !windows

package realpath

import "path/filepath"

// Resolve is the path p really names, with every symlink on the way resolved.
func Resolve(p string) (string, error) { return filepath.EvalSymlinks(p) }
