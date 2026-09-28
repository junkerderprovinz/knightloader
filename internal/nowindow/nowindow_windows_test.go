package nowindow

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplyKeepsTheCommandLine(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /c "x.bat"`}
	Apply(cmd)
	if cmd.SysProcAttr.CmdLine != `cmd.exe /c "x.bat"` {
		t.Errorf("CmdLine is %q", cmd.SysProcAttr.CmdLine)
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Error("CREATE_NO_WINDOW is not set")
	}
}

func TestAppliedProgramStillRuns(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "ok")
	Apply(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out[:2]) != "ok" {
		t.Errorf("output is %q", out)
	}
}
