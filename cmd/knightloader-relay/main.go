// Command knightloader-relay is the self-hosted message relay KnightLoader
// instances use to find each other when neither of them can accept an inbound
// connection - two desktop installs on different networks, say. Both dial out
// to one relay; the relay forwards frames between whichever connections
// present the same relay key.
//
// It is deliberately its own binary and not a mode of the main server: it
// downloads nothing, stores nothing and never sees a file byte, so the thing
// exposed to the public internet has almost no surface. Run one yourself, the
// same way you run KnightLoader itself - or use the one this project operates
// (see docs/superpowers/specs/2026-08-27-public-relay-seed-phrase-design.md).
//
// # TLS
//
// Set KL_RELAY_DOMAIN and this binary terminates TLS itself on :443, getting
// and renewing its own Let's Encrypt certificate. No reverse proxy, no
// certbot, no cron entry, and - the reason it is done this way rather than
// with the usual HTTP-01 challenge - no port 80. The firewall in front of
// this only opens 443, and TLS-ALPN-01 completes the challenge inside a TLS
// handshake on that same port, so the narrower firewall costs nothing.
//
// Leave KL_RELAY_DOMAIN unset and it serves plain HTTP on KL_RELAY_ADDR, for
// a local test or for somebody who does want their own proxy in front. The
// relay key is a credential, so anything reachable from outside a trusted
// network needs one of the two.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

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

	domain := os.Getenv("KL_RELAY_DOMAIN")
	if domain != "" {
		addr = env("KL_RELAY_ADDR", ":443")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}

	if domain != "" {
		m := &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			// Pinned to the one name this relay answers on. Without it,
			// autocert would ask Let's Encrypt for a certificate for
			// whatever name any caller put in its handshake, which is a
			// rate limit waiting to be hit by the first scanner that finds
			// the port.
			HostPolicy: autocert.HostWhitelist(domain),
			Cache:      autocert.DirCache(env("KL_RELAY_CERT_DIR", "/var/lib/knightloader-relay/certs")),
		}
		tlsCfg := m.TLSConfig()
		// TLS-ALPN-01 only. m.TLSConfig() already lists it, but being
		// explicit is the point: this is what lets the challenge complete
		// without port 80, and a future edit that drops it would otherwise
		// fail at renewal time - months later, on a certificate nobody was
		// watching.
		tlsCfg.NextProtos = []string{"h2", "http/1.1", acme.ALPNProto}
		tlsCfg.MinVersion = tls.VersionTLS12
		srv.TLSConfig = tlsCfg
		listener = tls.NewListener(listener, tlsCfg)
	}

	serveErr := make(chan error, 1)
	go func() {
		scheme, ws := "http", "ws"
		if domain != "" {
			scheme, ws = "https", "wss"
		}
		log.Printf("KnightLoader relay listening on %s (%s, %s://%s/relay/connect)", addr, scheme, ws, hostOr(domain, addr))
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

// hostOr picks what the startup line should show as the reachable address:
// the domain when TLS is on (the name clients actually dial, and the only
// one the certificate is valid for), otherwise the bind address, which is
// all a plain-HTTP run has.
func hostOr(domain, addr string) string {
	if domain != "" {
		return domain
	}
	return addr
}
