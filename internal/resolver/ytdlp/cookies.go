package ytdlp

import (
	"fmt"
	"os"
)

// writeCookieFile writes text to a 0600 temp file for yt-dlp's --cookies and
// returns its path and a cleanup that removes it. The cleanup may run more
// than once. Errors never mention the jar's content.
func writeCookieFile(dir, text string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "kl-cookies-*.txt")
	if err != nil {
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	name := f.Name()
	remove := func() { _ = os.Remove(name) }
	// Set explicitly before writing. Windows has no POSIX modes, so a failure
	// is ignored; its temp directory is per-user.
	_ = f.Chmod(0o600)
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		remove()
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	if err := f.Close(); err != nil {
		remove()
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	return name, remove, nil
}
