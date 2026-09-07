package ytdlp

// measure.go: reading the finished file back, because "yt-dlp exited 0" is
// not the same statement as "the video plays to the end".
//
// The failure being caught is a merge that died between the video half and
// the audio half, or a fragmented download that gave up on a server hiccup
// after most of the fragments were in. Every signal this backend has says the
// download worked: the exit code is 0, the file is there, the size is in the
// right neighbourhood. The first anybody hears otherwise is the picture
// stopping twenty minutes into a fifty minute film, days later, usually while
// somebody else is watching.
//
// ffprobe answers it in one short process, and ffprobe is already installed:
// the Dockerfile pulls in ffmpeg because yt-dlp needs it to mux, and ffprobe
// ships in that same package. So this costs no new dependency and no network
// round trip - it reads the container's own headers off local disk.
//
// WHAT THIS CATCHES AND WHAT IT DOES NOT, measured against ffprobe 9.0 on
// real files rather than reasoned about:
//
//   - A file that is genuinely shorter than the source announced - fragments
//     that stopped arriving, a download that ended early and was muxed anyway
//     - is caught. The container's own duration is then honest and short, and
//     comparing it against the announced one is the whole check.
//   - A mux that was KILLED mid-write is NOT caught by the duration, and that
//     is worth stating plainly because it is the failure people expect it to
//     find. ffmpeg writes the intended length into the matroska header up
//     front: an encode asked for 600 seconds and killed after 6 leaves a file
//     whose format.duration still reads "600.000000". The only honest way to
//     see through that is to read every packet in the file, which is a full
//     pass over gigabytes for a check meant to cost milliseconds.
//   - That same killed mux is usually not a silent success anyway: yt-dlp's
//     merge failing makes yt-dlp itself exit non-zero, and the task goes red
//     through the ordinary error path long before this code runs.
//
// So this is a genuine second opinion on the common case, not a guarantee
// against every possible half-written file, and the wording of what it
// reports (shortWarning) says "may have been cut off" for exactly that
// reason.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// measureTimeout bounds one ffprobe call. Reading a container's header is
// milliseconds of work; a minute is not a performance budget, it is the point
// past which the only explanation left is a process that will never return
// (a file on a network mount that has gone away mid-write is the realistic
// one), and a stuck ffprobe must not hold a finished download open forever.
const measureTimeout = time.Minute

// MediaInfo is what the finished file turns out to contain. Every field is
// best effort: a container ffprobe cannot fully describe still reports what it
// could read, because a partial answer is what makes the SHORT check possible
// and that check is the reason this runs at all.
type MediaInfo struct {
	// Duration is the runtime ffprobe measured, in seconds. 0 means it could
	// not say - which is itself a finding for a video file, and the reason
	// the short check treats 0 as "no comparison possible" rather than as
	// "zero seconds long".
	Duration float64
	Width    int
	Height   int
	// VideoCodec and AudioCodec are the FIRST stream of each kind, which is
	// the one a player picks by default. A file with three audio tracks is
	// described by AudioTracks, not by three codec names nobody reads.
	VideoCodec  string
	AudioCodec  string
	AudioTracks int
}

// Summary is the one line this ends up putting on the task. Built from
// whatever was actually measured, skipping what was not: a file ffprobe could
// only read the duration of says the duration, rather than "0x0, , ".
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

// ffprobeJSON is ffprobe's own -print_format json document, reduced to what
// MediaInfo needs.
//
// Every shape here was read off a real ffprobe (9.0) against real files
// rather than assumed:
//
//   - Durations come back as STRINGS ("3.023000"), not numbers. Decoding them
//     as float64 fails the WHOLE document, which would have left every
//     finished file silently unmeasured while the feature looked switched on.
//   - A matroska stream carries no numeric "duration" of its own at all - its
//     length is a tag, "DURATION": "00:00:03.023000000", written by the muxer
//     when the mux finishes. An mp4 stream does carry the numeric field. Both
//     are read below, because mkv is the container this backend forces for
//     every merged video (--merge-output-format, buildArgs) and a fallback
//     that only understood mp4 would miss exactly those.
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

// probeMedia runs ffprobe against path.
//
// -v quiet with -print_format json is the machine-readable pairing ffprobe
// documents: everything human goes away and stdout carries one JSON document
// and nothing else, so a warning about an unusual container cannot end up
// prepended to the answer.
func probeMedia(ctx context.Context, bin, path string) (MediaInfo, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseFFprobe(out)
}

// parseFFprobe decodes one ffprobe document. Split out for the same reason
// buildArgs and parsePlaylist are: every decision about what the answer means
// is testable without a binary anywhere near it.
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
	// A container that reports no duration of its own can still have one per
	// stream. The LONGEST stream, not the first: a truncated audio track
	// beside a complete video one would otherwise make a good file look short
	// and turn the warning into a cried wolf.
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

// parseTimecode reads matroska's own "HH:MM:SS.nnnnnnnnn" track duration tag.
// Anything that is not exactly three colon-separated parts answers 0 rather
// than a partial guess - the caller treats 0 as "could not measure", which is
// the safe reading, and a half-parsed timecode would be a wrong number
// presented as a measurement.
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

// gotPercent is how much of the announced runtime the file actually
// contains, in whole percent, and whether the comparison could be made at
// all.
//
// It answers how much is THERE rather than how much is missing, because the
// threshold it feeds (Measure.ShortPercent) is written the same way round -
// "at least 90% of what was announced" - and a function that returned the
// shortfall would make the one comparison in finish() read backwards. Whole
// percent, truncated: a comparison against a setting somebody typed as a
// round number has no business being decided by a fractional second.
//
// ok=false means "no comparison": a source that announced no duration (a live
// recording, an extractor that does not report one) and a file ffprobe could
// not time are both cases where saying nothing is right, and where a naive
// zero would raise a false alarm on every single download.
func gotPercent(announced, measured float64) (percent int, ok bool) {
	if announced <= 0 || measured <= 0 {
		return 0, false
	}
	return int(measured / announced * 100), true
}

// shortWarning is the sentence a short file carries. It names both numbers,
// because "this file is short" is a claim somebody is going to want to check
// against the player they are about to open, and the likely cause, because a
// person reading it at 23:00 should not have to work out what makes a file
// end early.
func shortWarning(announced, measured float64) string {
	return fmt.Sprintf("short file: %s of the announced %s, the merge or the last fragments may have been cut off",
		formatDuration(time.Duration(measured*float64(time.Second))),
		formatDuration(time.Duration(announced*float64(time.Second))))
}
