package mediatools

import (
	"path/filepath"
	"runtime"
)

// candidates lists the release assets to try for this platform, best first.
//
// Linux needs two lists. The container is Alpine, which is musl libc, and
// yt-dlp's `yt-dlp_linux` asset is a PyInstaller bundle built on Ubuntu
// against glibc: on musl the kernel refuses to start it with ENOENT, which a
// shell and Go both report as "no such file or directory" for a file that is
// visibly there. yt-dlp publishes musl builds for amd64 and arm64, which is
// what the image itself ships.
//
// The last resort everywhere on Linux is the plain `yt-dlp` python zipapp,
// which runs wherever a python3 is on PATH. The image has none, so there it
// only matters on an architecture with no build of its own.
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
		return linuxCandidates(runtime.GOARCH, onMusl())
	case "windows":
		// No zipapp fallback on Windows: the bare `yt-dlp` asset has no .exe
		// extension and Windows will not execute it as a program, so offering
		// it would only produce a confusing second failure after the first.
		switch runtime.GOARCH {
		case "amd64":
			return []string{"yt-dlp.exe"}
		case "arm64":
			return []string{"yt-dlp_arm64.exe"}
		}
		return nil
	case "darwin":
		// One universal build for both arches, the shape internal/update's
		// platformSlug deals with for this app's macOS bundle.
		return []string{"yt-dlp_macos"}
	}
	return nil
}

func linuxCandidates(arch string, musl bool) []string {
	if musl {
		switch arch {
		case "amd64":
			return []string{"yt-dlp_musllinux", "yt-dlp"}
		case "arm64":
			return []string{"yt-dlp_musllinux_aarch64", "yt-dlp"}
		}
		return []string{"yt-dlp"}
	}
	switch arch {
	case "amd64":
		return []string{"yt-dlp_linux", "yt-dlp"}
	case "arm64":
		return []string{"yt-dlp_linux_aarch64", "yt-dlp"}
	case "arm":
		return []string{"yt-dlp_linux_armv7l", "yt-dlp"}
	}
	// Every other Linux architecture: the zipapp is the only thing published
	// that could run, and it will if python3 is there. Offering it and letting
	// the smoke test decide beats refusing before trying.
	return []string{"yt-dlp"}
}

// onMusl reports whether this Linux system's C library is musl, going by the
// dynamic loader musl installs and glibc never does.
func onMusl() bool {
	found, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(found) > 0
}
