//go:build updatetest

package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A build with the updatetest tag takes its releases from a local stand-in for
// GitHub and can check sooner. Release builds leave the tag out, so nothing in
// the environment can change where an update comes from.
//
// It also installs under a name of its own, the one desktop/README.md builds
// its installer with, and keeps its app key under that name, so trying it
// leaves a real installation alone. The scheduled task starts it with no
// environment of ours, and finds the stand-in's address in the folder under
// ProgramData instead. Without a stand-in it asks a host that never resolves,
// so Update now cannot fetch a real installer over the real installation.
func init() {
	product = "KnightLoader Test"
	appID = "com.junkerderprovinz.knightloader.test"
	keyringService = product
	updateAPI = "https://update-stand-in.invalid"
	if api := os.Getenv("KL_UPDATE_API"); api != "" {
		updateAPI = api
	} else if dir, err := machineData(); err == nil {
		if body, err := os.ReadFile(filepath.Join(dir, "updatetest-api")); err == nil {
			updateAPI = strings.TrimSpace(string(body))
		}
	}
	if d, err := time.ParseDuration(os.Getenv("KL_UPDATE_DELAY")); err == nil {
		firstCheck = d
		followEvery = d
	}
}
