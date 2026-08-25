// Command knightloader-relay is the self-hosted message relay KnightLoader
// instances use to find each other when neither of them can accept an inbound
// connection - two desktop installs on different networks, say. Both dial out
// to one relay; the relay forwards frames between whichever connections
// present the same relay key.
//
// It is deliberately its own binary and not a mode of the main server: it
// downloads nothing, stores nothing and never sees a file byte, so the thing
// exposed to the public internet has almost no surface. Run one yourself, the
// same way you run KnightLoader itself - see docs/superpowers/specs for why
// this project does not operate one on anyone's behalf. Put it behind a
// reverse proxy for TLS: the relay key is a credential, and WSS is what keeps
// it off the wire in the clear.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// shutdownGrace bounds how long a graceful stop waits for connections to end
// before it stops waiting. Shorter than the main server's own grace period:
// nothing here is mid-transfer, and a client whose socket is cut simply
// reconnects and re-announces.
const shutdownGrace = 5 * time.Second

func main() {
	// 8760 is clear of both ports a machine running KnightLoader already has
	// taken (:8749 for the app, :9666 for Click'n'Load), so the relay can be
	// tried out on the same box before it moves to its own container.
	addr := env("KL_RELAY_ADDR", ":8760")

	r := relay.New()
	mux := http.NewServeMux()
	// Same shape as the app's own GET /api/health, so one orchestrator health
	// check works against either process without a second parser.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": buildinfo.Version})
	})
	mux.Handle("GET /relay/connect", r)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("KnightLoader relay listening on %s (ws %s/relay/connect)", addr, addr)
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("shutting down (%s)", sig)
	case err := <-serveErr:
		// The listener stopped on its own, without Shutdown having been
		// called - a startup failure (the address is already in use) is the
		// ordinary way that happens.
		if err != nil {
			log.Fatalf("serve: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: not every connection closed within %s: %v", shutdownGrace, err)
	}
	cancel()
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
