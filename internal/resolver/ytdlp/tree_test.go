package ytdlp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kill ends yt-dlp and the ffmpeg it started, but cmd.Wait reaps only
// yt-dlp, and the orphaned ffmpeg goes to the container's PID 1. The image
// therefore starts the app under an init that reaps it, or every removal of a
// running download leaves a zombie behind.
func TestTheImageRunsUnderAnInitThatReapsTheFfmpegAKillOrphans(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("the image's Dockerfile could not be read (%v)", err)
	}
	var entrypoint []string
	installed := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "RUN apk add") && strings.Contains(line, " tini") {
			installed = true
		}
		if rest, ok := strings.CutPrefix(line, "ENTRYPOINT "); ok {
			if err := json.Unmarshal([]byte(rest), &entrypoint); err != nil {
				t.Fatalf("ENTRYPOINT is not in exec form: %s", line)
			}
		}
	}
	if len(entrypoint) == 0 || entrypoint[0] != "/sbin/tini" || !installed {
		t.Errorf("the image runs %v as PID 1 (tini installed: %v), which reaps no orphaned ffmpeg", entrypoint, installed)
	}
}
