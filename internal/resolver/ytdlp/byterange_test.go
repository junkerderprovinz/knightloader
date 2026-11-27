package ytdlp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testdata/byterange has the shape arte.tv serves: CMAF HLS in which the init
// segment and every fragment are byte ranges of one mp4, and the server
// answers each range with the length of the whole file in Content-Range.
// This test runs the real yt-dlp and skips where it is not installed.
func TestAByteRangeHLSStreamDownloads(t *testing.T) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is not on PATH")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not on PATH")
	}

	files := http.FileServer(http.Dir(filepath.Join("testdata", "byterange")))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		}
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	args := append(buildArgs(dir, Options{}), srv.URL+"/clip.m3u8")
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("yt-dlp failed: %v\n%s", err, tail(string(out), 20))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return
		}
	}
	t.Fatalf("yt-dlp left no file in %s\n%s", dir, tail(string(out), 20))
}

// tail is the last n lines of yt-dlp's output, without the progress lines.
func tail(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if !strings.HasPrefix(l, "KLP:") {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
