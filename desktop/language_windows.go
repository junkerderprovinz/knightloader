package main

import "path/filepath"

// installerChoices is the file under ProgramData where the installer keeps the
// update switch and the language.
func installerChoices() string {
	dir, err := machineData()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, machineSettingsFile)
}
