//go:build !linux && !darwin && !freebsd && !windows

package diskspace

// supported says this build has NO implementation behind free - see the unix
// file's own copy of this constant.
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
