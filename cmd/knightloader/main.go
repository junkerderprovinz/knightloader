// Command knightloader runs the KnightLoader server: the download engine, the
// REST + WebSocket API, the embedded web UI, and the Click'n'Load listener, all
// in one process.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/bridge"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

func main() {
	// Bridge mode is a different program sharing one binary: it downloads
	// nothing, keeps no data, and exists only so a browser on this machine can
	// reach a KnightLoader that runs somewhere else. Click'n'Load is hard-wired
	// to 127.0.0.1 by every site that implements it, so a NAS install cannot be
	// reached any other way.
	remote := flag.String("bridge", "", "run as a Click'n'Load bridge to a remote KnightLoader (e.g. http://nas:8749)")
	remotePw := flag.String("bridge-password", "", "the remote instance's UI password, when it has one")
	flag.Parse()
	if *remote != "" {
		runBridge(*remote, *remotePw)
		return
	}

	dataDir := env("KL_DATA", defaultDataDir())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// Desktop first-run: provision a private headless JDownloader so the user
	// gets full hoster coverage out of the box (KL_PROVISION_JD=1). Blocking on
	// purpose — the jd backend is wired at app start from KL_JD.
	if envInt("KL_PROVISION_JD", 0) == 1 && os.Getenv("KL_JD") == "" {
		pv := provision.New(filepath.Join(dataDir, "jd"))
		// Held for the life of the process: dropping it would leave the JVM we
		// started running after we exit, and the next start would then find
		// port 3128 taken by an orphan it did not configure.
		defer func() { _ = pv.Stop() }()
		log.Printf("provisioning headless JDownloader (first run may take a few minutes)…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		if _, url, err := pv.Ensure(ctx); err != nil {
			log.Printf("JD provisioning failed (%v); continuing without JD", err)
		} else {
			_ = os.Setenv("KL_JD", url)
			log.Printf("headless JDownloader provisioned at %s", url)
		}
		cancel()
	}

	a, err := app.New(dataDir)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	defer a.Close()

	// Click'n'Load listener on the standard port 9666 (KL_CNL=0 disables, any
	// other value overrides the port). A taken port (e.g. a running JD) is not
	// fatal — CnL is simply unavailable then.
	if port := envInt("KL_CNL", 9666); port > 0 {
		c := cnl.New(a)
		if err := c.Start(port); err != nil {
			log.Printf("Click'n'Load not available on :%d (%v)", port, err)
		} else {
			defer c.Close()
			log.Printf("Click'n'Load listening on 127.0.0.1:%d", port)
		}
	}

	addr := env("KL_ADDR", ":8749")
	log.Printf("KnightLoader listening on %s (data: %s)", addr, dataDir)
	log.Fatal(http.ListenAndServe(addr, api.Handler(a)))
}

// runBridge serves Click'n'Load locally and forwards everything it receives to
// a remote instance. It blocks until interrupted.
func runBridge(remote, password string) {
	b, err := bridge.New(bridge.Options{Remote: remote, Password: password})
	if err != nil {
		log.Fatalf("bridge: %v", err)
	}
	// Fail loudly at start rather than silently swallowing links later: a bridge
	// that cannot reach its remote is worse than no bridge, because the site
	// still reports success.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = b.Check(ctx)
	cancel()
	if err != nil {
		log.Fatalf("bridge: %s is not reachable: %v", b.Remote(), err)
	}

	port := envInt("KL_CNL", 9666)
	if port <= 0 {
		port = 9666
	}
	c := cnl.New(b)
	if err := c.Start(port); err != nil {
		log.Fatalf("bridge: Click'n'Load port %d is taken (%v); is JDownloader or another bridge running?", port, err)
	}
	defer c.Close()
	log.Printf("Click'n'Load bridge on 127.0.0.1:%d, forwarding to %s", port, b.Remote())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("bridge stopped")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "KnightLoader")
	}
	return "kl-data"
}
