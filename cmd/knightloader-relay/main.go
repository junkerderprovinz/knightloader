// Command knightloader-relay is the message relay KnightLoader instances use
// when neither side can accept an inbound connection, such as two desktop
// installs on different networks. Both dial out to the relay, which forwards
// frames between connections that present the same relay key.
//
// It is a separate binary so that what faces the public internet downloads
// nothing, stores nothing and never sees a file byte.
//
// # TLS
//
// With KL_RELAY_DOMAIN set it terminates TLS on :443 and manages its own Let's
// Encrypt certificate through tls-alpn-01, so port 80 can stay closed. Without
// it, it serves plain HTTP on KL_RELAY_ADDR for a local test or a proxy in
// front. The relay key is a credential, so anything reachable from outside a
// trusted network needs one of the two.
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
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// shutdownGrace bounds how long a graceful stop waits for connections. Nothing
// here is mid-transfer, and a cut client reconnects and announces again.
const shutdownGrace = 5 * time.Second

func main() {
	// The privacy policy promises no record of who connects, but net/http logs
	// the client address on failed TLS handshakes, some HTTP/2 errors and
	// panics. Both the standard logger and the server's ErrorLog are redacted.
	logOut := relay.RedactAddrs(os.Stderr)
	log.SetOutput(logOut)

	// Clear of :8749 and :9666, so the relay can run beside KnightLoader.
	addr := env("KL_RELAY_ADDR", ":8760")

	r := relay.New()
	// The request path sweeps too, but only while traffic arrives. The timer
	// keeps the 61 minute retention promised in extension/PRIVACY.md on a
	// quiet relay.
	go func() {
		for range time.Tick(time.Minute) {
			r.SweepLimiter()
		}
	}()
	mux := http.NewServeMux()
	// Same shape as the app's GET /api/health, so one health check fits both.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": buildinfo.Version})
	})
	mux.Handle("GET /relay/connect", r)

	// KL_RELAY_DOMAIN is a comma-separated list because the relay address is
	// compiled into released builds, so after a move the old name still needs
	// a certificate until nothing dials it.
	domains := splitNames(os.Getenv("KL_RELAY_DOMAIN"))
	domain := ""
	if len(domains) > 0 {
		domain = domains[0]
		addr = env("KL_RELAY_ADDR", ":443")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ErrorLog: log.New(logOut, "", log.LstdFlags)}

	if domain != "" {
		m := &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			// Without a whitelist autocert requests a certificate for any
			// name a scanner puts in its handshake, and burns the rate limit.
			HostPolicy: autocert.HostWhitelist(domains...),
			Cache:      autocert.DirCache(env("KL_RELAY_CERT_DIR", "/var/lib/knightloader-relay/certs")),
		}
		tlsCfg := m.TLSConfig()
		// m.TLSConfig already lists acme.ALPNProto. Spelling it out keeps an
		// edit from dropping it, which would only surface months later when
		// renewal fails without port 80.
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
		// The listener stopped without Shutdown, usually because the address
		// was already in use.
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

// splitNames turns the comma-separated KL_RELAY_DOMAIN into the names the
// certificate may cover. Empty entries are dropped, since an empty name in the
// whitelist would admit handshakes without a server name.
func splitNames(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// hostOr returns the address the startup line shows: the domain when TLS is
// on, otherwise the bind address.
func hostOr(domain, addr string) string {
	if domain != "" {
		return domain
	}
	return addr
}
