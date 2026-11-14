package signinbrowser

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Edge comes with Windows, so it is looked for first.
func candidates() []Browser { return windowsCandidates(appPath, os.Getenv) }

func windowsCandidates(appPath func(string) string, getenv func(string) string) []Browser {
	var out []Browser
	for _, b := range []struct{ name, exe, dir string }{
		{"Microsoft Edge", "msedge.exe", `Microsoft\Edge\Application`},
		{"Google Chrome", "chrome.exe", `Google\Chrome\Application`},
		{"Brave", "brave.exe", `BraveSoftware\Brave-Browser\Application`},
	} {
		if p := strings.Trim(appPath(b.exe), `"`); p != "" {
			out = append(out, Browser{Name: b.name, Path: p})
		}
		for _, root := range []string{getenv("ProgramFiles(x86)"), getenv("ProgramFiles"), getenv("LOCALAPPDATA")} {
			if root != "" {
				out = append(out, Browser{Name: b.name, Path: filepath.Join(root, b.dir, b.exe)})
			}
		}
	}
	return out
}

// appPath is where a browser's installer registered it under App Paths, for
// this user or the machine.
func appPath(exe string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}
