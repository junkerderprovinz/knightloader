package main

import (
	"context"
	"net/http"
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

// TestTheServerDropsIdleConnectionsButNotLongTransfers pins the bounds that
// keep a stalled client from holding a goroutine, and the absence of the ones
// that would cut the live stream or a large transfer.
func TestTheServerDropsIdleConnectionsButNotLongTransfers(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadHeaderTimeout > 30*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want a few seconds", srv.ReadHeaderTimeout)
	}
	if srv.IdleTimeout <= 0 || srv.IdleTimeout > 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want about a minute", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes > http.DefaultMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want a bound below the default", srv.MaxHeaderBytes)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Errorf("ReadTimeout = %v, WriteTimeout = %v, want neither: they would cut websockets and long transfers",
			srv.ReadTimeout, srv.WriteTimeout)
	}
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
