//go:build !windows

package nowindow

import "os/exec"

// Apply does nothing outside Windows, where starting a program opens no window.
func Apply(*exec.Cmd) {}
