//go:build !updatetest

package main

import "testing"

func TestAReleaseKeepsItsKeyAndUpdatesWhereTheyAlwaysWere(t *testing.T) {
	if keyringService != "KnightLoader" {
		t.Errorf("the release keeps its key under %q, want KnightLoader", keyringService)
	}
	if updateAPI != "https://api.github.com" {
		t.Errorf("the release asks %q for updates, want GitHub", updateAPI)
	}
}
