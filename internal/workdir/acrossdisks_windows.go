package workdir

import "syscall"

// crossDevice is ERROR_NOT_SAME_DEVICE, what Windows answers a rename onto
// another volume with.
const crossDevice = syscall.Errno(17)
