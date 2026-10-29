package ytdlp

import (
	"slices"
	"testing"
)

func TestFFmpegOnPathAddsNoFlag(t *testing.T) {
	b := NewBackend("yt-dlp", t.TempDir(), nil)
	if got := b.ffmpegArgs(); got != nil {
		t.Errorf("args %q, want none", got)
	}
	if got := b.ffprobeBinary(); got != "ffprobe" {
		t.Errorf("ffprobe %q, want the one on PATH", got)
	}
}

func TestAFetchedFFmpegIsHandedToYtdlp(t *testing.T) {
	b := NewBackend("yt-dlp", t.TempDir(), nil)
	b.SetFFmpeg("/data/tools/ffmpeg", "/data/tools/ffprobe")
	if got, want := b.ffmpegArgs(), []string{"--ffmpeg-location", "/data/tools/ffmpeg"}; !slices.Equal(got, want) {
		t.Errorf("args %q, want %q", got, want)
	}
	if got := b.ffprobeBinary(); got != "/data/tools/ffprobe" {
		t.Errorf("ffprobe %q", got)
	}
}
