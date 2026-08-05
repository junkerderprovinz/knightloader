// Package buildinfo carries the build version, stamped at release time via
// -ldflags "-X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=vX.Y.Z".
package buildinfo

// Version is the running build; "dev" for untagged local/main builds.
var Version = "dev"
