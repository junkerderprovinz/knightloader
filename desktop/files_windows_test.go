//go:build windows

package main

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// explorerSees is the command line Explorer is started with. Explorer reads
// it raw instead of splitting it the way Go quotes arguments, which is where
// a path can go astray.
func explorerSees(cmd *exec.Cmd) string {
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		return cmd.SysProcAttr.CmdLine
	}
	quoted := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		quoted[i] = syscall.EscapeArg(a)
	}
	return strings.Join(quoted, " ")
}

func TestExplorerSelectsAFileWithSpacesCommasOrAmpersandsInItsPath(t *testing.T) {
	for _, path := range []string{
		`D:\Downloads\movie.mkv`,
		`D:\Downloads\My Package\Some File.mkv`,
		`D:\Downloads\Films\Movie, The (1999).mkv`,
		`D:\Downloads\Tom&Jerry\Tom&Jerry 01.mkv`,
		`\\nas\media\Neue Filme\film.mkv`,
	} {
		want := `explorer.exe /select,"` + path + `"`
		if got := explorerSees(revealCommand(path)); got != want {
			t.Errorf("revealing %s starts %s; want %s", path, got, want)
		}
	}
}
