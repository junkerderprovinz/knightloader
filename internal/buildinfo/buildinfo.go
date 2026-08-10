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
