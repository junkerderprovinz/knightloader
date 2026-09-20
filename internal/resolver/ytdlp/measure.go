package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Measuring reads a finished file back with ffprobe, because yt-dlp exiting 0
// does not mean the video plays to the end. ffprobe comes with the ffmpeg
// package yt-dlp already needs and reads only local headers.
//
// It catches files shorter than the source announced: fragments that stopped
// arriving, a download muxed after ending early. It does not catch a mux
// killed mid-write, because ffmpeg writes the intended length into the
// matroska header up front; yt-dlp usually exits non-zero in that case anyway.
// Hence shortWarning says the file "may have been cut off".

// measureTimeout bounds one ffprobe call. Reading headers takes milliseconds;
// the minute only stops a hung ffprobe (say, on a vanished mount) from holding
// a finished download open.
const measureTimeout = time.Minute

// MediaInfo is what the finished file turns out to contain. Every field is
// best effort; a partial answer still allows the short check.
type MediaInfo struct {
	// Duration is the measured runtime in seconds, 0 when ffprobe could not
	// tell.
	Duration float64
	Width    int
	Height   int
	// VideoCodec and AudioCodec describe the first stream of each kind, the
	// one a player picks by default; AudioTracks counts the audio streams.
	VideoCodec  string
	AudioCodec  string
	AudioTracks int
}

// Summary is the line put on the task, built only from what was measured.
func (m MediaInfo) Summary() string {
	var parts []string
	if m.Duration > 0 {
		parts = append(parts, formatDuration(time.Duration(m.Duration*float64(time.Second))))
	}
	if m.Width > 0 && m.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", m.Width, m.Height))
	}
	codecs := strings.Trim(m.VideoCodec+"/"+m.AudioCodec, "/")
	if codecs != "" {
		parts = append(parts, codecs)
	}
	if m.AudioTracks > 1 {
		parts = append(parts, fmt.Sprintf("%d audio tracks", m.AudioTracks))
	}
	return strings.Join(parts, ", ")
}

// ffprobeJSON is ffprobe's -print_format json document, reduced to what
// MediaInfo needs. Durations arrive as strings ("3.023000"), and decoding them
// as numbers would fail the whole document. A matroska stream has no numeric
// duration, only a "DURATION" tag ("00:00:03.023000000"); mp4 streams have the
// number. Both are read, since merged videos are forced to mkv.
type ffprobeJSON struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
		Tags      struct {
			Duration string `json:"DURATION"`
		} `json:"tags"`
	} `json:"streams"`
}

// probeMedia runs ffprobe against path. -v quiet keeps warnings out of the
// JSON on stdout.
func probeMedia(ctx context.Context, bin, path string) (MediaInfo, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseFFprobe(out)
}

// parseFFprobe decodes one ffprobe document.
func parseFFprobe(out []byte) (MediaInfo, error) {
	var raw ffprobeJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe: unparseable output: %w", err)
	}
	var m MediaInfo
	m.Duration = parseSeconds(raw.Format.Duration)
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if m.VideoCodec == "" {
				m.VideoCodec, m.Width, m.Height = s.CodecName, s.Width, s.Height
			}
		case "audio":
			m.AudioTracks++
			if m.AudioCodec == "" {
				m.AudioCodec = s.CodecName
			}
		}
	}
	// Without a container duration, the longest stream counts, so one short
	// track beside a complete video does not raise a false alarm.
	if m.Duration == 0 {
		for _, s := range raw.Streams {
			for _, d := range []float64{parseSeconds(s.Duration), parseTimecode(s.Tags.Duration)} {
				if d > m.Duration {
					m.Duration = d
				}
			}
		}
	}
	return m, nil
}

func parseSeconds(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

// parseTimecode reads matroska's "HH:MM:SS.nnnnnnnnn" duration tag, returning
// 0 (unknown) for anything else rather than a partial guess.
func parseTimecode(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0
	}
	h, err1 := strconv.ParseFloat(parts[0], 64)
	m, err2 := strconv.ParseFloat(parts[1], 64)
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil || h < 0 || m < 0 || sec < 0 {
		return 0
	}
	return h*3600 + m*60 + sec
}

// gotPercent is how much of the announced runtime the file contains, in whole
// percent (truncated), matching how Measure.ShortPercent is written. ok is
// false when either duration is unknown, as for a live recording.
func gotPercent(announced, measured float64) (percent int, ok bool) {
	if announced <= 0 || measured <= 0 {
		return 0, false
	}
	return int(measured / announced * 100), true
}

// shortWarning is the sentence a short file carries, with both durations and
// the likely cause.
func shortWarning(announced, measured float64) string {
	return fmt.Sprintf("short file: %s of the announced %s, the merge or the last fragments may have been cut off",
		formatDuration(time.Duration(measured*float64(time.Second))),
		formatDuration(time.Duration(announced*float64(time.Second))))
}
