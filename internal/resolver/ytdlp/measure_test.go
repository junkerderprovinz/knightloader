package ytdlp

import (
	"strings"
	"testing"
)

// TestParseFFprobeReadsTheDurationOutOfAStringField is the trap that would
// have silently disabled the whole check: ffprobe's json writer emits
// durations as STRINGS ("2718.041"), so decoding them as a number fails the
// entire document and every finished file would come back unmeasured.
func TestParseFFprobeReadsTheDurationOutOfAStringField(t *testing.T) {
	m, err := parseFFprobe([]byte(`{"format":{"duration":"2718.041"},"streams":[
		{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},
		{"codec_type":"audio","codec_name":"aac"},
		{"codec_type":"audio","codec_name":"opus"}]}`))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if m.Duration != 2718.041 {
		t.Errorf("Duration = %v, want 2718.041", m.Duration)
	}
	if m.Width != 1920 || m.Height != 1080 || m.VideoCodec != "h264" {
		t.Errorf("video = %dx%d %s, want 1920x1080 h264", m.Width, m.Height, m.VideoCodec)
	}
	if m.AudioCodec != "aac" || m.AudioTracks != 2 {
		t.Errorf("audio = %s across %d tracks, want aac across 2", m.AudioCodec, m.AudioTracks)
	}
}

// TestParseFFprobeFallsBackToTheLongestStream: a container that reports no
// duration of its own can still have one per stream, and the LONGEST is the
// right one - a truncated audio track beside a complete video one would
// otherwise make a good file look short.
func TestParseFFprobeFallsBackToTheLongestStream(t *testing.T) {
	m, err := parseFFprobe([]byte(`{"format":{},"streams":[
		{"codec_type":"audio","codec_name":"aac","duration":"12.0"},
		{"codec_type":"video","codec_name":"h264","width":640,"height":360,"duration":"600.0"}]}`))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if m.Duration != 600 {
		t.Errorf("Duration = %v, want the longest stream's 600", m.Duration)
	}
}

// TestParseFFprobeReadsAMatroskaTrackDurationTag is the shape a real ffprobe
// 9.0 answers for an mkv, transcribed off one rather than imagined: a
// matroska stream has NO numeric "duration" field at all, only a
// "DURATION": "00:00:03.023000000" tag the muxer writes when the mux
// finishes. mkv is the container this backend forces for every merged video,
// so a fallback that only understood mp4's numeric field would have missed
// exactly the files it was written for.
func TestParseFFprobeReadsAMatroskaTrackDurationTag(t *testing.T) {
	m, err := parseFFprobe([]byte(`{"format":{},"streams":[
		{"codec_type":"video","codec_name":"h264","width":320,"height":240,
		 "tags":{"ENCODER":"Lavc63.1.100 libx264","DURATION":"00:00:03.000000000"}},
		{"codec_type":"audio","codec_name":"aac",
		 "tags":{"ENCODER":"Lavc63.1.100 aac","DURATION":"01:02:03.500000000"}}]}`))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if m.Duration != 3723.5 {
		t.Errorf("Duration = %v, want the longest track tag's 3723.5", m.Duration)
	}
}

func TestParseTimecodeRefusesAnythingItCannotFullyRead(t *testing.T) {
	if got := parseTimecode("00:00:03.023000000"); got != 3.023 {
		t.Errorf("parseTimecode = %v, want 3.023", got)
	}
	// 0 means "could not measure", which the caller reads as "say nothing" -
	// a half-parsed timecode would be a wrong number presented as a fact.
	for _, in := range []string{"", "3.023", "00:03.023", "a:b:c", "N/A"} {
		if got := parseTimecode(in); got != 0 {
			t.Errorf("parseTimecode(%q) = %v, want 0", in, got)
		}
	}
}

func TestParseFFprobeRejectsWhatIsNotAnFFprobeAnswer(t *testing.T) {
	if _, err := parseFFprobe([]byte("ffprobe version 6.1\n")); err == nil {
		t.Fatalf("parseFFprobe accepted non-JSON output")
	}
}

// TestGotPercentRefusesAComparisonItCannotMake: a live recording announces no
// duration and an unreadable container measures none. Both must say nothing
// rather than raise an alarm on every single download.
func TestGotPercentRefusesAComparisonItCannotMake(t *testing.T) {
	for _, c := range [][2]float64{{0, 100}, {100, 0}, {-1, 100}, {0, 0}} {
		if _, ok := gotPercent(c[0], c[1]); ok {
			t.Errorf("gotPercent(%v, %v) claimed a comparison it cannot make", c[0], c[1])
		}
	}
}

func TestGotPercentAnswersHowMuchIsThere(t *testing.T) {
	cases := []struct {
		announced, measured float64
		want                int
	}{
		{2718, 2718, 100},
		{2718, 1359, 50},
		{2718, 2700, 99},
		// A file slightly longer than announced is not short. A rounded
		// announced duration and a container that counts a trailing frame
		// make this an ordinary outcome, not an anomaly.
		{100, 101, 101},
	}
	for _, c := range cases {
		got, ok := gotPercent(c.announced, c.measured)
		if !ok || got != c.want {
			t.Errorf("gotPercent(%v, %v) = %d (ok=%v), want %d", c.announced, c.measured, got, ok, c.want)
		}
	}
}

// TestDefaultShortPercentLeavesRoomForAnHonestDownload: a threshold that
// fires on a file legitimately a few seconds under the announced number
// trains people to ignore it, and then it catches nothing at all.
func TestDefaultShortPercentLeavesRoomForAnHonestDownload(t *testing.T) {
	// Two seconds missing off a 45 minute video: a rounded announced
	// duration, or a trailing silence the merge dropped.
	got, ok := gotPercent(2718, 2716)
	if !ok || got < DefaultShortPercent {
		t.Errorf("gotPercent = %d, want it at or above the %d%% default", got, DefaultShortPercent)
	}
	// Half the film missing: an interrupted merge, which has to be caught.
	got, ok = gotPercent(2718, 1359)
	if !ok || got >= DefaultShortPercent {
		t.Errorf("gotPercent = %d, want it below the %d%% default", got, DefaultShortPercent)
	}
}

func TestSanitizeFoldsAnImpossibleShortPercentOntoTheDefault(t *testing.T) {
	for _, in := range []int{0, -1, 101, 500} {
		o := Options{Measure: Measure{Enabled: true, ShortPercent: in}}
		if got := o.Sanitize().Measure.ShortPercent; got != DefaultShortPercent {
			t.Errorf("Sanitize(ShortPercent %d) = %d, want the default %d", in, got, DefaultShortPercent)
		}
	}
	o := Options{Measure: Measure{Enabled: true, ShortPercent: 50}}
	if got := o.Sanitize().Measure.ShortPercent; got != 50 {
		t.Errorf("Sanitize(ShortPercent 50) = %d, want it left alone", got)
	}
}

// TestMediaInfoSummarySkipsWhatWasNotMeasured: a container ffprobe could only
// time must not be described as "0x0, /" on the task row.
func TestMediaInfoSummarySkipsWhatWasNotMeasured(t *testing.T) {
	if got := (MediaInfo{Duration: 90}).Summary(); got != "1:30" {
		t.Errorf("Summary = %q, want just the runtime", got)
	}
	if got := (MediaInfo{}).Summary(); got != "" {
		t.Errorf("Summary of nothing = %q, want empty", got)
	}
	got := MediaInfo{Duration: 2718, Width: 1920, Height: 1080, VideoCodec: "h264", AudioCodec: "aac", AudioTracks: 1}.Summary()
	if got != "45:18, 1920x1080, h264/aac" {
		t.Errorf("Summary = %q", got)
	}
	// One audio track is the normal case and saying so adds nothing.
	if strings.Contains(got, "audio tracks") {
		t.Errorf("Summary = %q, want no track count for a single track", got)
	}
}

func TestShortWarningNamesBothRuntimes(t *testing.T) {
	got := shortWarning(2718, 1359)
	if !strings.Contains(got, "22:39") || !strings.Contains(got, "45:18") {
		t.Errorf("shortWarning = %q, want both the measured and the announced runtime", got)
	}
}
