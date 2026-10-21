package main

import (
	"os"
	"path/filepath"
)

// installerChoices is the state file the installer leaves in the folder it
// installed into, which lies under the XDG data folder.
func installerChoices() string {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, appID, "install.json")
}
