package signinbrowser

import (
	"strings"
	"testing"
)

func TestWindowsLooksForEdgeFirstWhereItsInstallerSaysItIs(t *testing.T) {
	appPath := func(exe string) string {
		if exe == "msedge.exe" {
			return `"D:\Edge\msedge.exe"`
		}
		return ""
	}
	env := map[string]string{"ProgramFiles(x86)": `C:\Program Files (x86)`, "ProgramFiles": `C:\Program Files`, "LOCALAPPDATA": `C:\Users\k\AppData\Local`}
	got := windowsCandidates(appPath, func(k string) string { return env[k] })
	if got[0] != (Browser{Name: "Microsoft Edge", Path: `D:\Edge\msedge.exe`}) {
		t.Errorf("first candidate = %+v, want Edge where App Paths names it", got[0])
	}
	if got[1].Path != `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe` {
		t.Errorf("second candidate = %+v, want Edge under Program Files (x86)", got[1])
	}
	last := got[len(got)-1]
	if last.Name != "Brave" || !strings.HasSuffix(last.Path, `BraveSoftware\Brave-Browser\Application\brave.exe`) {
		t.Errorf("last candidate = %+v, want Brave", last)
	}
}

// Every Windows computer has Edge.
func TestThisComputersEdgeIsFound(t *testing.T) {
	b, ok := Installed()
	if !ok || b.Name != "Microsoft Edge" || !strings.HasSuffix(strings.ToLower(b.Path), `msedge.exe`) {
		t.Errorf("Installed() = %+v, %v; want Edge", b, ok)
	}
}
