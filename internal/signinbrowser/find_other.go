//go:build !windows && !darwin

package signinbrowser

import "os/exec"

func candidates() []Browser {
	var out []Browser
	for _, b := range []struct {
		name string
		cmds []string
	}{
		{"Google Chrome", []string{"google-chrome", "google-chrome-stable"}},
		{"Microsoft Edge", []string{"microsoft-edge", "microsoft-edge-stable"}},
		{"Brave", []string{"brave-browser", "brave"}},
		{"Chromium", []string{"chromium", "chromium-browser"}},
	} {
		for _, cmd := range b.cmds {
			if p, err := exec.LookPath(cmd); err == nil {
				out = append(out, Browser{Name: b.name, Path: p})
			}
		}
	}
	// Alpine's and Debian's Chromium binary, behind the launcher script that
	// adds switches of its own.
	return append(out, Browser{Name: "Chromium", Path: "/usr/lib/chromium/chromium"})
}
