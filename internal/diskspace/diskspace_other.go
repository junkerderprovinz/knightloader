//go:build !linux && !darwin && !freebsd && !windows

package diskspace

const supported = false

// free has no implementation on this platform (NetBSD, OpenBSD, solaris,
// illumos, aix, plan9, js/wasm). It reports false rather than a made-up
// figure, so callers fail open.
func free(string) (uint64, bool) { return 0, false }

func usage(string) (Space, bool) { return Space{}, false }
