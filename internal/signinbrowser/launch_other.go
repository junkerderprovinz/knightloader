//go:build !windows

package signinbrowser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// launch starts Chromium speaking DevTools over a pipe on its file
// descriptors 3 and 4, which nothing else on the machine can reach. Chromium
// exits when the pipe closes, so it does not outlive KnightLoader either.
func launch(ctx context.Context, bin, profile string, args []string) (transport, func(), error) {
	toBrowser, ourEnd, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	fromBrowser, theirEnd, err := os.Pipe()
	if err != nil {
		toBrowser.Close()
		ourEnd.Close()
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, bin, append(args, "--remote-debugging-pipe", "about:blank")...)
	cmd.ExtraFiles = []*os.File{toBrowser, theirEnd}
	// Chromium keeps its certificate store and font cache under HOME, which
	// a container run as another user may not have.
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(profile))
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{toBrowser, ourEnd, fromBrowser, theirEnd} {
			f.Close()
		}
		return nil, nil, err
	}
	toBrowser.Close()
	theirEnd.Close()
	return newPipe(fromBrowser, ourEnd, fromBrowser), reaper(cmd), nil
}
