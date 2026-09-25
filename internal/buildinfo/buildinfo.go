// Package buildinfo carries the build version, stamped at release time via
// -ldflags "-X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=vX.Y.Z".
package buildinfo

import "runtime/debug"

// Version is the running build; "dev" for untagged local/main builds.
var Version = "dev"

// Commit is the source revision, stamped like Version. Read it through
// Revision.
var Commit = ""

// Revision is the commit this build came from, or "" when nothing knows it.
//
// The ldflags stamp comes first because the container build excludes .git
// and so gets no VCS stamp from the toolchain; every other build falls back
// to vcs.revision. It never guesses: a plausible wrong identifier is worse
// than none when checking for a stale deploy.
func Revision() string {
	if Commit != "" {
		return Commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

// Deployment says which binary this build is: "container" for
// cmd/knightloader or "desktop" for the Wails app. Both share api.Handler, so
// the main package sets it before serving rather than anything inferring it.
var Deployment = "container"

// ListensWidely reports whether the HTTP listener is bound to more than
// loopback, taken from the resolved listener address and set before serving.
// It tells an admin browsing from 127.0.0.1 whether the instance could be
// reached from elsewhere. False for the desktop build, which opens no TCP
// listener.
var ListensWidely bool

// ListenPort is the port the HTTP listener resolved to, or 0 when there is
// none. internal/discovery needs it before any request has arrived, and only
// the resolved address knows what ":0" became.
var ListenPort int

// BasePath is the path a reverse proxy mounts the server under, "/kl" for
// https://example.com/kl/, or "" at the root. The main package sets it from
// KL_BASE_PATH; the desktop build has no proxy and leaves it empty.
var BasePath string

// DiscoveryEnabled turns on internal/discovery's multicast announce and
// listener. It is off by default so the many api.Handler instances in tests
// do not each open a multicast socket; a serving main package opts in.
var DiscoveryEnabled bool
