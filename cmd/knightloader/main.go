// Command knightloader runs the KnightLoader server: the download engine, the
// REST and WebSocket API, the embedded web UI and the Click'n'Load listener, all
// in one process.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/bridge"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/fileowner"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// shutdownGrace bounds how long a graceful stop waits for in-flight HTTP
// requests, so a backup download running at that moment can still finish.
const shutdownGrace = 10 * time.Second

func main() {
	// Bridge mode downloads nothing and keeps no data. It exists because every
	// Click'n'Load site posts to 127.0.0.1, so a NAS install cannot be reached
	// from the browser any other way.
	remote := flag.String("bridge", "", "run as a Click'n'Load bridge to a remote KnightLoader (e.g. http://nas:8749)")
	remotePw := flag.String("bridge-password", "", "the remote instance's UI password, when it has one")
	// Also needs the bridgeclipboard build tag; see internal/bridge/clipboard.go.
	watchClipboard := flag.Bool("bridge-clipboard", false, "watch the OS clipboard for hoster links while bridging (build with -tags bridgeclipboard)")
	resetTwoFactor := flag.Bool("reset-2fa", false, "turn the second login factor off and exit; the password is untouched. For an operator who has lost both the phone and the recovery codes")
	flag.Parse()
	if *remote != "" {
		runBridge(*remote, *remotePw, *watchClipboard)
		return
	}

	dataDir := env("KL_DATA", defaultDataDir())

	if *resetTwoFactor {
		runResetTwoFactor(dataDir)
		return
	}

	// An unwritable data directory otherwise fails later as a bare "permission
	// denied" from SQLite. This names the owner, our uid and the chown to run,
	// and repairs nothing: chowning a mounted share on boot would rewrite the
	// ownership of somebody's library.
	if line, ok := fileowner.Advise(dataDir); ok {
		log.Print(line)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// A staged restore has to land before anything opens the store or
	// settings.json; see internal/backup.
	if applied, manifest, err := backup.ApplyPending(dataDir); err != nil {
		// By the time ApplyPending can fail, either nothing live was touched or
		// only its cleanup is left, so starting up is safe.
		log.Printf("a staged restore could not be fully applied (%v); starting with the data already on disk", err)
	} else if applied {
		log.Printf("restored from a backup made by %s (%s build) on %s",
			manifest.Version, manifest.Deployment, manifest.CreatedAt.Format(time.RFC3339))
	}

	// A private headless JDownloader gives full hoster coverage without a
	// sidecar, and DLC or container links cannot be opened without one.
	// KL_PROVISION_JD=0 opts out, and an existing KL_JD is never overridden.
	// This blocks because the JD backend is wired from KL_JD at app start.
	if envInt("KL_PROVISION_JD", 1) == 1 && os.Getenv("KL_JD") == "" {
		pv := provision.New(filepath.Join(dataDir, "jd"))
		// Stopping the JVM on exit keeps an orphan from holding port 3128 on the
		// next start.
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
	// app.New armed the optional log file. Records are written unbuffered, so
	// closing only releases the handle, which Windows needs before anything else
	// can open the file.
	defer func() { _ = logring.CloseFile() }()

	// Hoster logins are pushed into the JD sidecar on a loop, so a recreated JD
	// container gets them back without a restart. This and the account health
	// sweep below belong to the server binary rather than app.New, which every
	// test calls.
	a.StartHosterAuth()
	a.StartAccountHealthNow()

	a.CnL = cnl.Listen(a)
	defer a.CnL.Stop()

	// Buffered by one and sent without blocking, so a double-clicked quit does
	// not stall the second caller; one pending shutdown is as good as two.
	exit := make(chan bool, 1) // false = quit, true = restart
	a.RequestExit = func(restart bool) bool {
		select {
		case exit <- restart:
			return true
		default:
			return false // a shutdown is already pending
		}
	}

	addr := env("KL_ADDR", ":8749")
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	// Only the resolved address says whether we listen beyond loopback, since
	// the default ":8749" means every interface. It also carries the port that
	// internal/discovery announces.
	if host, portStr, err := net.SplitHostPort(listener.Addr().String()); err == nil {
		ip := net.ParseIP(host)
		buildinfo.ListensWidely = host == "" || (ip != nil && !ip.IsLoopback())
		if n, err := strconv.Atoi(portStr); err == nil {
			buildinfo.ListenPort = n
		}
	}
	buildinfo.DiscoveryEnabled = true
	srv := &http.Server{Handler: api.Handler(a)}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("KnightLoader listening on %s (data: %s)", addr, dataDir)
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	// The start report runs after net.Listen because statting a folder on a
	// dead mount blocks until the mount times out, and the HEALTHCHECK's ten
	// second start period would then restart the container in a loop.
	// KL_STARTUP_CHECK=0 records that the check was off, so an empty report is
	// not read as a clean one.
	if envInt("KL_STARTUP_CHECK", 1) == 1 {
		a.StartStartupCheck()
	} else {
		a.MarkStartupCheckOff()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("shutting down (%s)", sig)
	case restart := <-exit:
		if restart {
			log.Printf("restarting (requested over the API)")
		} else {
			log.Printf("shutting down (requested over the API)")
		}
	case err := <-serveErr:
		// The listener stopped without Shutdown, usually because the address
		// was already in use.
		if err != nil {
			log.Fatalf("serve: %v", err)
		}
		return
	}

	// Quit and restart are the same from here on: under Docker or Unraid the
	// supervisor brings the process back, so exiting is what a restart is.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: not every in-flight request finished within %s: %v", shutdownGrace, err)
	}
	cancel()
}

// runResetTwoFactor turns the second login factor off and exits, leaving the
// password untouched.
//
// There is one password and no second account to unlock anything, so an
// operator who lost both the authenticator and the recovery codes has no other
// way back short of deleting auth.json, which also drops the password and the
// session key. It grants nothing new: whoever can run this binary against the
// data directory can already delete that file. It opens nothing else, and since
// a running server also holds auth.json it asks for a restart.
func runResetTwoFactor(dataDir string) {
	g, err := auth.Open(dataDir)
	if err != nil {
		log.Fatalf("reset-2fa: %v", err)
	}
	if !g.TwoFactorEnabled() {
		log.Printf("reset-2fa: no second factor is set on %s; nothing to do", dataDir)
		return
	}
	if err := g.ClearTwoFactor(); err != nil {
		log.Fatalf("reset-2fa: %v", err)
	}
	log.Printf("reset-2fa: the second factor is off. The password is unchanged. "+
		"Restart KnightLoader if it is running, sign in with the password, and set the factor up again from Settings > Remote access (%s)", dataDir)
}

// runBridge serves Click'n'Load locally and forwards everything it receives to
// a remote instance. It blocks until interrupted.
func runBridge(remote, password string, watchClipboard bool) {
	b, err := bridge.New(bridge.Options{Remote: remote, Password: password})
	if err != nil {
		log.Fatalf("bridge: %v", err)
	}
	// A bridge that cannot reach its remote would swallow links while the site
	// still reports success, so fail at start instead.
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

	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if watchClipboard {
		go b.WatchClipboard(watchCtx)
	}

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
