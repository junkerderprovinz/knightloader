// Package buildinfo carries the build version, stamped at release time via
// -ldflags "-X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=vX.Y.Z".
package buildinfo

import (
	"os"
	"runtime/debug"
)

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

// Describe is the line a -version flag prints: the program, Version and, when
// it is known, the Revision.
func Describe(program string) string {
	s := program + " " + Version
	if r := Revision(); r != "" {
		s += " (commit " + r + ")"
	}
	return s
}

// InContainer reports whether this process runs in a container, going by the
// marker files Docker and Podman leave and the variable Kubernetes sets. A
// variable, so a test can stand in either place.
var InContainer = func() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
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

// Module is a library compiled into this binary, at the version the toolchain
// recorded for it.
type Module struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	// Fork is set where go.mod replaces the module with a fork, and Version is
	// then the fork's.
	Fork bool `json:"fork,omitempty"`
}

// Modules reports which of paths this binary carries, in the order of paths.
func Modules(paths ...string) []Module {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return modulesIn(info.Deps, paths)
}

func modulesIn(deps []*debug.Module, paths []string) []Module {
	byPath := make(map[string]*debug.Module, len(deps))
	for _, d := range deps {
		byPath[d.Path] = d
	}
	var out []Module
	for _, p := range paths {
		d, ok := byPath[p]
		if !ok {
			continue
		}
		m := Module{Path: p, Version: d.Version}
		if d.Replace != nil {
			m.Fork = true
			// A fork in a local directory has no version of its own.
			if d.Replace.Version != "" {
				m.Version = d.Replace.Version
			}
		}
		out = append(out, m)
	}
	return out
}
