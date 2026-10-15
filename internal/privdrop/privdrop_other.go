//go:build !linux

package privdrop

// Drop does nothing outside Linux. The image is Linux only, and the desktop
// builds run under the account that started them.
func Drop([]string) (Result, error) { return Result{}, nil }
