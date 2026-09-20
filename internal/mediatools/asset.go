package mediatools

import "runtime"

// candidates lists the release assets to try for this platform, best first.
//
// The fallback is not optional. The container is Alpine, which is musl libc,
// and yt-dlp's `yt-dlp_linux` asset is a PyInstaller bundle built on Ubuntu
// against glibc: on musl the kernel refuses to start it with ENOENT, which a
// shell and Go both report as "no such file or directory" for a file that is
// visibly there.
//
// The asset that works there is the plain `yt-dlp` python zipapp, which needs
// a python3 on PATH. The container has one, because Alpine's own yt-dlp
// package, which the image installs, depends on python3.
//
// Hence this list together with the smoke test in fetch.go: a libc guess is
// not trusted, the file is run and has to print the release tag before
// anything on disk is replaced.
//
// An empty list means this build runs on a platform yt-dlp publishes no binary
// for, which Install reports rather than guessing at.
func candidates() []string {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return []string{"yt-dlp_linux", "yt-dlp"}
		case "arm64":
			return []string{"yt-dlp_linux_aarch64", "yt-dlp"}
		case "arm":
			return []string{"yt-dlp_linux_armv7l", "yt-dlp"}
		default:
			// Every other Linux architecture: the zipapp is the only thing
			// published that could run, and it will if python3 is there.
			// Offering it and letting the smoke test decide beats refusing
			// before trying.
			return []string{"yt-dlp"}
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return []string{"yt-dlp.exe"}
		}
		// No zipapp fallback on Windows: the bare `yt-dlp` asset has no .exe
		// extension and Windows will not execute it as a program, so offering
		// it would only produce a confusing second failure after the first.
		return nil
	case "darwin":
		// One universal build for both arches, the shape internal/update's
		// platformSlug deals with for this app's macOS bundle.
		return []string{"yt-dlp_macos"}
	}
	return nil
}
