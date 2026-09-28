// Package nowindow keeps the programs KnightLoader starts from opening a
// console window. The desktop app is a GUI program without a console of its
// own, so Windows would give every console program it starts a window, and
// java, yt-dlp or a batch file would each flash one open.
package nowindow

import (
	"context"
	"os/exec"
)

// CommandContext is exec.CommandContext for a program started without a
// console window.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	Apply(cmd)
	return cmd
}
