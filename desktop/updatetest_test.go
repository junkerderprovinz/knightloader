//go:build updatetest

package main

import (
	"strings"
	"testing"
)

func TestTheTestBuildKeepsAKeyOfItsOwn(t *testing.T) {
	if keyringService != "KnightLoader Test" || keyringService != product {
		t.Errorf("the test build keeps its key under %q, want its product %q", keyringService, product)
	}
}

func TestTheTestBuildNeverAsksGitHubForUpdates(t *testing.T) {
	if strings.Contains(updateAPI, "github.com") {
		t.Errorf("the test build asks %q for updates, want a stand-in", updateAPI)
	}
}
