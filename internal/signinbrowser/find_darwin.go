package signinbrowser

import (
	"os"
	"path/filepath"
)

func candidates() []Browser {
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	var out []Browser
	for _, app := range []struct{ name, bundle, exe string }{
		{"Google Chrome", "Google Chrome.app", "Google Chrome"},
		{"Microsoft Edge", "Microsoft Edge.app", "Microsoft Edge"},
		{"Brave", "Brave Browser.app", "Brave Browser"},
		{"Chromium", "Chromium.app", "Chromium"},
	} {
		for _, root := range roots {
			out = append(out, Browser{Name: app.name, Path: filepath.Join(root, app.bundle, "Contents", "MacOS", app.exe)})
		}
	}
	return out
}
