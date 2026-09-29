package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain runs main itself when a test starts this binary again with
// KL_TEST_RUN_MAIN set, so a test sees what the program does with its flags.
func TestMain(m *testing.M) {
	if os.Getenv("KL_TEST_RUN_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVersionFlagPrintsVersionAndStartsNothing(t *testing.T) {
	// A server that started instead would run until the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dataDir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-version")
	cmd.Env = append(os.Environ(), "KL_TEST_RUN_MAIN=1", "KL_DATA="+dataDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("-version: %v", err)
	}
	if !strings.HasPrefix(string(out), "knightloader ") {
		t.Fatalf("printed %q", out)
	}
	entries, _ := os.ReadDir(dataDir)
	if len(entries) > 0 {
		t.Fatalf("-version wrote %d entries into the data directory", len(entries))
	}
}
