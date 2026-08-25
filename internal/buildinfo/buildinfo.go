// Package buildinfo carries the build version, stamped at release time via
// -ldflags "-X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=vX.Y.Z".
package buildinfo

// Version is the running build; "dev" for untagged local/main builds.
var Version = "dev"

// Deployment says which binary this build is: "container" for
// cmd/knightloader (the default a fresh import of this package always
// reads as), or "desktop" for the Wails-wrapped native app.
//
// Nothing in api.Handler can tell the two apart on its own — it is reused
// byte-for-byte by both main packages — so whichever one constructs the App
// sets this before serving a single request, the same way each already
// carries its own KL_* environment reading. It is read, never inferred: a
// guess (probing for a display, sniffing a container cgroup) would be wrong
// for exactly the deployments self-hosting cares about, such as a desktop
// build launched with no display from a script, or a server binary run
// directly on bare metal with no container at all.
var Deployment = "container"

// ListensWidely reports whether this process's own HTTP listener is bound
// to more than loopback - set once, before the server starts accepting
// connections, the same way Deployment is. False for the desktop build,
// which opens no TCP listener at all (see Deployment's own doc comment).
//
// Read from the real, resolved net.Listener address, never guessed from
// the configured address string: KL_ADDR's default (":8749", empty host)
// resolves to "every interface", which is the normal, correct default for
// a container regardless of whether the host then forwards that port
// anywhere reachable - internal/api/routes_remote.go's own doc comment
// explains in full why that string alone was rejected as a signal for "a
// request just arrived from outside this machine". This is a different
// question this var answers: not "did something external just prove it can
// reach this instance", but "could it, in principle" - the one thing an
// admin looking at their own Access page from 127.0.0.1 has no way to see
// for themselves, because every request they make is by definition local.
var ListensWidely bool

// ListenPort is the port this process's own HTTP listener actually resolved
// to, or 0 for a build that opens none (the desktop).
//
// Read from the real listener rather than parsed out of KL_ADDR for the same
// reason ListensWidely is: ":8749" and an ephemeral ":0" are both legal, and
// only the resolved address knows what the second one became.
//
// It exists for internal/discovery, which has to put a reachable address in
// the announce it sends before any request has ever arrived - so the usual
// trick of reading it off r.Host (routes_remote.go) is not available there.
var ListenPort int

// DiscoveryEnabled turns on internal/discovery's multicast announce and
// listener. Set by whichever main package is actually serving, exactly like
// Deployment and ListensWidely.
//
// Off by default, and therefore off in tests: api.Handler is constructed
// hundreds of times across the test suite, and each one starting a real
// multicast socket and two goroutines would leak both for the life of the
// test binary. A main that serves opts in; nothing else needs to.
var DiscoveryEnabled bool
