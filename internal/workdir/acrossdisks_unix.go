//go:build !windows

package workdir

import "syscall"

// crossDevice is what a rename onto another filesystem fails with.
const crossDevice = syscall.EXDEV
