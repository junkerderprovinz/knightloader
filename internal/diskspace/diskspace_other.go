//go:build !linux && !darwin && !freebsd && !windows

package diskspace

// supported says this build has NO implementation behind free or usage - see
// the unix file's own copy of this constant.
const supported = false

// free is the honest silence for a platform this package has no call for:
// NetBSD and OpenBSD (statvfs, and differently spelled fields), solaris,
// illumos, aix, plan9, js/wasm.
//
// It answers false rather than a large number, and the difference matters in
// exactly one direction. False makes every caller in this tree fail OPEN, so
// the app on such a platform behaves precisely as it did before this package
// existed. Returning a made-up "plenty" would look identical today and would
// silently become a lie the moment somebody wired a caller that treats a big
// number as permission.
func free(string) (uint64, bool) { return 0, false }

// usage is the same silence, and the zero Space that comes with it means
// nothing at all rather than an empty disk. A readout handed this must print
// that it cannot measure; drawing a bar from three zeroes would show a volume
// with no files on it and no room left, which is not a state any disk is in.
func usage(string) (Space, bool) { return Space{}, false }
