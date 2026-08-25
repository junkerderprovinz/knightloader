// Command knightloader runs the KnightLoader server: the download engine, the
// REST + WebSocket API, the embedded web UI, and the Click'n'Load listener, all
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
	"sync"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/bridge"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// shutdownGrace bounds how long a graceful stop (Ctrl+C, SIGTERM, or the
// quit/restart API routes) waits for in-flight HTTP requests to finish
// before it stops waiting on them and moves on to a.Close's own drain -
// which has no grace period at all and abandons a running transfer
// outright; see a.Close's own doc comment for why that gap is deliberate.
// Generous enough that a backup download in progress at the same moment
// gets to finish rather than being cut off by the very shutdown it was
// unrelated to.
const shutdownGrace = 10 * time.Second

func main() {
	// Bridge mode is a different program sharing one binary: it downloads
	// nothing, keeps no data, and exists only so a browser on this machine can
	// reach a KnightLoader that runs somewhere else. Click'n'Load is hard-wired
	// to 127.0.0.1 by every site that implements it, so a NAS install cannot be
	// reached any other way.
	remote := flag.String("bridge", "", "run as a Click'n'Load bridge to a remote KnightLoader (e.g. http://nas:8749)")
	remotePw := flag.String("bridge-password", "", "the remote instance's UI password, when it has one")
	// Off by default, bridge-only, and a build-time opt-in on top of that: see
	// internal/bridge/clipboard.go's package comment for why this flag alone
	// does not put clipboard-reading code in the ordinary server binary.
	watchClipboard := flag.Bool("bridge-clipboard", false, "watch the OS clipboard for hoster links while bridging (build with -tags bridgeclipboard)")
	flag.Parse()
	if *remote != "" {
		runBridge(*remote, *remotePw, *watchClipboard)
		return
	}

	dataDir := env("KL_DATA", defaultDataDir())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// A restore validated and staged by a previous run (POST
	// /api/system/restore) is applied here, before app.New or anything else
	// below opens a single byte of the store or settings.json — see
	// internal/backup's own doc comment for why that ordering is what makes
	// this safe on every platform this ships for, Windows included, rather
	// than a hot-swap of a file this process is about to hold open.
	if applied, manifest, err := backup.ApplyPending(dataDir); err != nil {
		// Not fatal: ApplyPending's own doc comment is what makes that safe
		// here — by the time it can fail, either nothing live has been
		// touched yet, or the restore already fully landed and only its own
		// cleanup did not, which harms nothing this run needs.
		log.Printf("a staged restore could not be fully applied (%v); starting with the data already on disk", err)
	} else if applied {
		log.Printf("restored from a backup made by %s (%s build) on %s",
			manifest.Version, manifest.Deployment, manifest.CreatedAt.Format(time.RFC3339))
	}

	// Provision a private headless JDownloader so the user gets full hoster
	// coverage out of the box, with no separate JD sidecar to set up first -
	// on by default (KL_PROVISION_JD=0 opts out) for the same reason the
	// desktop build already does this unconditionally: an encrypted DLC or a
	// container-format link needs a JD backend to open at all, and requiring
	// a manual KL_JD beforehand meant that failed with a raw error on first
	// use rather than being something the app just handles (jdp: "es soll
	// einfach immer zuverlässig funktionieren ohne das man manuell was
	// machen muss"). Still skipped whenever KL_JD is already set - someone
	// pointing at their own existing JD (a shared instance, a sidecar
	// container) is never overridden by one this process starts itself.
	// Blocking on purpose — the jd backend is wired at app start from KL_JD.
	if envInt("KL_PROVISION_JD", 1) == 1 && os.Getenv("KL_JD") == "" {
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

	// Native hoster logins (internal/hosterauth): reconciles what is stored
	// in KL's own encrypted store into the headless-JD sidecar's account
	// config, on a loop rather than once at boot, so a JD container recreated
	// with an empty account list gets every login pushed back without a
	// restart. An optional subsystem with its own start, the same as
	// Click'n'Load below, not part of app.New's own lifecycle.
	a.StartHosterAuth()

	// Click'n'Load listener on the standard port 9666 (KL_CNL=0 disables, any
	// other value overrides the port). A taken port (e.g. a running JD) is not
	// fatal — CnL is simply unavailable then.
	//
	// Wrapped in start/stop closures rather than a bare defer c.Close() (jdp,
	// 2026-08-24: "wieso kann man es nicht dort direkt aktivieren/
	// deaktivieren?") so the Modules/Zugang tab's own switch can start and
	// stop the real listener at runtime without a restart - see app.App's
	// own CnLEnabled/CnLToggle doc comment for why this lives here rather
	// than as a field App manages itself, and for why it is deliberately
	// NOT persisted to settings.json. cnlPort is fixed at boot (0 disables
	// the feature outright - nothing to reach for a port to bind if
	// re-enabled later - anything else is remembered as the port a later
	// toggle-on should use, even while currently off).
	cnlPort := envInt("KL_CNL", 9666)
	// KL_CNL=0 means "do not auto-start" (autoStart below), never "there is
	// no port to bind if someone flips the switch on later" - the standard
	// 9666 is still what a runtime toggle-on reaches for, same as a fresh
	// install with no KL_CNL override at all would.
	bindPort := cnlPort
	if bindPort <= 0 {
		bindPort = 9666
	}
	var cnlMu sync.Mutex
	var cnlServer *cnl.Server
	startCnL := func() error {
		cnlMu.Lock()
		defer cnlMu.Unlock()
		if cnlServer != nil {
			return nil
		}
		c := cnl.New(a)
		if err := c.Start(bindPort); err != nil {
			return err
		}
		cnlServer = c
		return nil
	}
	stopCnL := func() {
		cnlMu.Lock()
		defer cnlMu.Unlock()
		if cnlServer == nil {
			return
		}
		cnlServer.Close()
		cnlServer = nil
	}
	if cnlPort > 0 {
		if err := startCnL(); err != nil {
			log.Printf("Click'n'Load not available on :%d (%v)", cnlPort, err)
		} else {
			log.Printf("Click'n'Load listening on 127.0.0.1:%d", cnlPort)
		}
	}
	defer stopCnL()
	a.CnLPort = func() int {
		cnlMu.Lock()
		defer cnlMu.Unlock()
		if cnlServer == nil {
			return 0
		}
		return bindPort
	}
	a.CnLToggle = func(on bool) error {
		if on {
			return startCnL()
		}
		stopCnL()
		return nil
	}

	// Wired before the server ever accepts a request, so a quit or restart
	// asked for in the first second after boot works exactly like one asked
	// for an hour in. See App.RequestExit's own doc comment for why this
	// lives on the App rather than as a second argument threaded through
	// api.Handler, which desktop/main.go also calls and does not set it.
	//
	// Buffered by one and sent to without blocking, the same shape
	// schedule.Runner's own wake channel uses for the same reason: two
	// requests arriving together (a double-click, or a stray retry) must
	// not stall the second caller behind the first, and one pending
	// shutdown is as good as two.
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
	// The listener's own resolved address answers "is this actually bound
	// wider than loopback", which the configured string alone cannot:
	// KL_ADDR's default (":8749") resolves an empty host to every
	// interface, the normal, correct default for a container regardless of
	// whether the host then forwards that port anywhere reachable - see
	// internal/api/routes_remote.go's own doc comment for why that string
	// was rejected as a signal there. Set once, before a single request is
	// served, the same way buildinfo.Deployment already is.
	if host, portStr, err := net.SplitHostPort(listener.Addr().String()); err == nil {
		ip := net.ParseIP(host)
		buildinfo.ListensWidely = host == "" || (ip != nil && !ip.IsLoopback())
		// The same resolved address also carries the port internal/discovery
		// has to announce, which nothing else can know before the first
		// request arrives.
		if n, err := strconv.Atoi(portStr); err == nil {
			buildinfo.ListenPort = n
		}
	}
	// Announce on the local network, so another instance finds this one
	// with nothing configured (internal/discovery).
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

	// SIGINT and SIGTERM are the two a container orchestrator or a plain
	// Ctrl+C ever sends — the CnL bridge path above (runBridge) already
	// listens for exactly these two, and this is that same shape rather
	// than a second one invented for the server path.
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
		// The listener stopped on its own, without Shutdown ever having
		// been called — a startup failure (the address is already in use)
		// is the ordinary way this happens, and there is nothing running
		// yet for the deferred CnL/App cleanup below to be worth waiting
		// on differently than it already is.
		if err != nil {
			log.Fatalf("serve: %v", err)
		}
		return
	}

	// Stop accepting new connections and let whatever is in flight -
	// including the very request that asked for this — finish, up to
	// shutdownGrace. Quit and restart are not told apart past this point,
	// deliberately: what runs next is the deferred chain above (the CnL
	// listener, then a.Close, which is where the actual drain happens — see
	// its own doc comment), identically either way. Under a supervised
	// deployment (Docker/Unraid) the process exiting is what a restart IS;
	// there is no separate "come back" step for this process to perform,
	// and the two routes exist as two names over one action for exactly
	// that reason — see App.RequestExit's own doc comment.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: not every in-flight request finished within %s: %v", shutdownGrace, err)
	}
	cancel()
}

// runBridge serves Click'n'Load locally and forwards everything it receives to
// a remote instance. It blocks until interrupted.
func runBridge(remote, password string, watchClipboard bool) {
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

	// Cancelled alongside the stop signal below, so a cancelled watcher is the
	// worst this leaves running past this function returning — there is no
	// store here for it to write into after the fact, unlike the App-owned
	// goroutines a.spawn tracks.
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
