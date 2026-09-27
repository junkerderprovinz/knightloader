//go:build updatetest

package main

import (
	"os"
	"time"
)

// A build with the updatetest tag takes its releases from a local stand-in for
// GitHub and can check sooner. Release builds leave the tag out, so nothing in
// the environment can change where an update comes from.
func init() {
	if api := os.Getenv("KL_UPDATE_API"); api != "" {
		updateAPI = api
	}
	if d, err := time.ParseDuration(os.Getenv("KL_UPDATE_DELAY")); err == nil {
		firstCheck = d
	}
}
