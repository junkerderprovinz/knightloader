// Command knightloader runs the KnightLoader server: the download engine, the
// REST + WebSocket API, and the embedded web UI, all in one process.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

func main() {
	dataDir := env("KL_DATA", defaultDataDir())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	a, err := app.New(dataDir)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	defer a.Close()

	addr := env("KL_ADDR", ":8749")
	log.Printf("KnightLoader listening on %s (data: %s)", addr, dataDir)
	log.Fatal(http.ListenAndServe(addr, api.Handler(a)))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "KnightLoader")
	}
	return "kl-data"
}
