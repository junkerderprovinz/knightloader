package ytdlp

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// youtubeLike is the shape a YouTube probe answers with: the same heights in
// three codecs across two containers, a 60 fps ladder on top, a pre-muxed
// fallback, and audio in two codecs. The HLS entry repeats 1080p mp4 avc1
// under another id.
var youtubeLike = []FormatEntry{
	{FormatID: "sb0", Ext: "mhtml", Vcodec: "none", Acodec: "none"},
	{FormatID: "139", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.5", Abr: 48.8, Filesize: 1000},
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129.478, Filesize: 3000},
	{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 160.2, Filesize: 3500},
	{FormatID: "250", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 70.1, Filesize: 1600},
	{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, FPS: 30, Filesize: 8000},
	{FormatID: "134", Ext: "mp4", Vcodec: "avc1.4d401e", Acodec: "none", Height: 360, FPS: 30, Filesize: 5000},
	{FormatID: "243", Ext: "webm", Vcodec: "vp9", Acodec: "none", Height: 360, FPS: 30, Filesize: 4000},
	{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 30, Filesize: 50000},
	{FormatID: "hls-1080", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 30, FilesizeApprox: 52000},
	{FormatID: "248", Ext: "webm", Vcodec: "vp9", Acodec: "none", Height: 1080, FPS: 30, Filesize: 40000},
	{FormatID: "399", Ext: "mp4", Vcodec: "av01.0.08M.08", Acodec: "none", Height: 1080, FPS: 30, Filesize: 30000},
	{FormatID: "299", Ext: "mp4", Vcodec: "avc1.64002a", Acodec: "none", Height: 1080, FPS: 59.94, Filesize: 90000},
	{FormatID: "315", Ext: "webm", Vcodec: "vp09.00.51.08", Acodec: "none", Height: 2160, FPS: 60, Filesize: 400000},
}

func TestVideoFormatsListsEveryDistinctTrackTallestFirst(t *testing.T) {
	want := []string{
		"2160p60 webm vp9",
		"1080p60 mp4 avc1",
		"1080p mp4 avc1",
		"1080p mp4 av1",
		"1080p webm vp9",
		"360p mp4 avc1",
		"360p webm vp9",
	}
	if got := VideoFormats(youtubeLike); !reflect.DeepEqual(got, want) {
		t.Errorf("VideoFormats =\n  %v\nwant\n  %v", got, want)
	}
}

// Each key must read back as a key, or the row would store a pick the backend
// then drops. A rate just over 30, as some hosts report, rounds to the 30 a
// key leaves unnamed.
func TestEveryListedVideoFormatIsAValidKey(t *testing.T) {
	formats := append(slices.Clone(youtubeLike),
		FormatEntry{FormatID: "720-30", Ext: "mp4", Vcodec: "avc1.64001F", Acodec: "none", Height: 720, FPS: 30.3})
	for _, k := range VideoFormats(formats) {
		if !IsVideoFormat(k) {
			t.Errorf("VideoFormats listed %q, which IsVideoFormat refuses", k)
		}
	}
}

func TestAudioTracksListsEveryDistinctTrackHighestBitrateFirst(t *testing.T) {
	want := []string{"opus 160k", "m4a 129k", "opus 70k", "m4a 49k"}
	if got := AudioTracks(youtubeLike); !reflect.DeepEqual(got, want) {
		t.Errorf("AudioTracks = %v, want %v", got, want)
	}
}

// A track without a bitrate would be named after its codec alone, which the
// menu already offers as a conversion target.
func TestAudioTracksLeavesOutATrackWithoutABitrate(t *testing.T) {
	got := AudioTracks([]FormatEntry{{Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2"}})
	if len(got) != 0 {
		t.Errorf("AudioTracks = %v, want nothing for a track that reports no bitrate", got)
	}
}

func TestKeysRefuseAnythingTheyCouldNotHaveProduced(t *testing.T) {
	for _, bad := range []string{
		"", "best", "1080p", "1080p mp4", "1080p25 mp4 avc1", "1080p mp4 avc1'", "1080p mp4 avc1]+ba",
		"1080p mp4 avc1 extra", "0p mp4 avc1",
	} {
		if IsVideoFormat(bad) {
			t.Errorf("IsVideoFormat(%q) = true, want false", bad)
		}
	}
	for _, bad := range []string{"", "opus", "best", "opus 0k", "opus 160", "opus 160k]", "op us 160k"} {
		if IsAudioTrack(bad) {
			t.Errorf("IsAudioTrack(%q) = true, want false", bad)
		}
	}
}

func TestAPickedVideoTrackSelectsByItsAttributesAndKeepsItsContainer(t *testing.T) {
	args := buildArgs("d", Options{VideoFormat: "1080p mp4 avc1"})
	got, _ := valueAfter(args, "-f")
	filter := "[height=1080][ext=mp4][vcodec~='(?i)^(avc|h264)'][fps<?30.5]"
	want := "bv" + filter + "+ba[ext=m4a]/bv" + filter + "+ba/b" + filter
	if got != want {
		t.Errorf("-f =\n  %q\nwant\n  %q", got, want)
	}
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "mp4/mkv" {
		t.Errorf("--merge-output-format = %q, want mp4/mkv", merge)
	}
}

// 59.94 rounds to 60 in the key, and the selector has to take it back.
func TestAHighFrameRateTrackSelectsAroundItsRoundedRate(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{VideoFormat: "1080p60 mp4 avc1"}), "-f")
	if !strings.Contains(got, "[fps>59][fps<61]") {
		t.Errorf("-f = %q, want the 59..61 band a 59.94 fps track falls in", got)
	}
}

func TestAPickedVideoTrackOutranksTheQualityCap(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{Quality: Quality720p, VideoFormat: "1080p webm vp9"}), "-f")
	if !strings.Contains(got, "[height=1080][ext=webm][vcodec~='(?i)^vp0?9']") {
		t.Errorf("-f = %q, want the picked 1080p webm track, not the 720p cap", got)
	}
}

// yt-dlp refuses to embed a thumbnail into webm and fails the download, so a
// webm pick merges into mkv while the thumbnail is embedded.
func TestAWebmPickMergesIntoMkvWhenTheThumbnailIsEmbedded(t *testing.T) {
	args := buildArgs("d", Options{VideoFormat: "1080p webm vp9", Embed: Embed{Thumbnail: true}})
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "mkv" {
		t.Errorf("--merge-output-format = %q, want mkv", merge)
	}
	if ext, _, _ := VideoFormatFile("1080p webm vp9", youtubeLike, true); ext != "mkv" {
		t.Errorf("VideoFormatFile predicts %q, want the mkv the merge writes", ext)
	}
}

// yt-dlp matches ~= against the codec as the host wrote it, and some hosts
// write it in capitals.
func TestACodecFilterMatchesTheCodecInAnyCase(t *testing.T) {
	formats := []FormatEntry{
		{Ext: "mp4", Vcodec: "H264", Acodec: "none", Height: 720},
		{Ext: "m4a", Vcodec: "none", Acodec: "AAC", Abr: 128},
	}
	video, audio := VideoFormats(formats), AudioTracks(formats)
	if len(video) != 1 || len(audio) != 1 {
		t.Fatalf("VideoFormats = %v, AudioTracks = %v; want one of each", video, audio)
	}
	vsel, _ := valueAfter(buildArgs("d", Options{VideoFormat: video[0]}), "-f")
	asel, _ := valueAfter(buildArgs("d", Options{Variant: VariantAudio, AudioTrack: audio[0]}), "-f")
	pattern := regexp.MustCompile(`codec~='([^']*)'`)
	for sel, codec := range map[string]string{vsel: "H264", asel: "AAC"} {
		m := pattern.FindStringSubmatch(sel)
		if m == nil {
			t.Fatalf("-f %q has no codec filter", sel)
		}
		if !regexp.MustCompile(m[1]).MatchString(codec) {
			t.Errorf("codec filter %q does not match %q, the codec the host reported", m[1], codec)
		}
	}
}

// A container other than mp4 and webm has no audio of its own to pair with.
func TestAnUncommonContainerMergesIntoMkv(t *testing.T) {
	args := buildArgs("d", Options{VideoFormat: "720p flv avc1"})
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "mkv" {
		t.Errorf("--merge-output-format = %q, want mkv", merge)
	}
	if got, _ := valueAfter(args, "-f"); strings.Contains(got, "+ba[ext=") {
		t.Errorf("-f = %q, want no preferred audio container for flv", got)
	}
}

func TestAPickedAudioTrackIsCopiedNotConverted(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioTrack: "opus 160k", AudioFormat: "mp3"})
	got, _ := valueAfter(args, "-f")
	if want := "ba[acodec~='(?i)^opus'][abr>=159.5][abr<160.5]"; got != want {
		t.Errorf("-f = %q, want %q", got, want)
	}
	if hasArg(args, "--audio-format") {
		t.Errorf("a picked track passed --audio-format, want it copied as it is: %v", args)
	}
	if !hasArg(args, "-x") {
		t.Errorf("a picked track did not pass -x: %v", args)
	}
}

func TestAPickedAudioTrackStillHonoursTheLanguage(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{Variant: VariantAudio, AudioTrack: "m4a 129k", AudioLang: "de"}), "-f")
	band := "[acodec~='(?i)^(mp4a|aac)'][abr>=128.5][abr<129.5]"
	if want := "ba[language^=de]" + band + "/ba" + band; got != want {
		t.Errorf("-f = %q, want %q", got, want)
	}
}

// A hand-edited task could carry anything; Sanitize drops what is not a key.
func TestSanitizeDropsAPickThatIsNotAKey(t *testing.T) {
	o := Options{VideoFormat: "1080p mp4 avc1]+ba", AudioTrack: "opus"}.Sanitize()
	if o.VideoFormat != "" || o.AudioTrack != "" {
		t.Errorf("Sanitize kept VideoFormat %q and AudioTrack %q, want both dropped", o.VideoFormat, o.AudioTrack)
	}
}

func TestVideoFormatFilePredictsTheContainerAndTheTrackSize(t *testing.T) {
	cases := []struct {
		key  string
		ext  string
		size int64
	}{
		// m4a audio exists, so the merge stays mp4; the HLS copy's estimate is
		// the larger of the two.
		{"1080p mp4 avc1", "mp4", 52000},
		{"1080p webm vp9", "webm", 40000},
		{"2160p60 webm vp9", "webm", 400000},
		// 360p mp4 avc1 exists as video-only (134) and pre-muxed (18); the
		// selector asks for the video-only one first.
		{"360p mp4 avc1", "mp4", 5000},
	}
	for _, c := range cases {
		ext, size, ok := VideoFormatFile(c.key, youtubeLike, false)
		if !ok || ext != c.ext || size != c.size {
			t.Errorf("VideoFormatFile(%q) = %q, %d, %v; want %q, %d, true", c.key, ext, size, ok, c.ext, c.size)
		}
	}
	if _, _, ok := VideoFormatFile("720p mp4 avc1", youtubeLike, false); ok {
		t.Error("VideoFormatFile found a 720p track in a list that has none")
	}
}

func TestVideoFormatFileMergesIntoMkvWithoutAudioOfTheSameContainer(t *testing.T) {
	onlyOpus := []FormatEntry{
		{Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 160},
		{Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080},
	}
	if ext, _, _ := VideoFormatFile("1080p mp4 avc1", onlyOpus, false); ext != "mkv" {
		t.Errorf("ext = %q, want mkv; mp4 cannot hold the only audio there is", ext)
	}
}

func TestAPreMuxedTrackKeepsItsOwnExtension(t *testing.T) {
	premuxed := []FormatEntry{{Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, Filesize: 8000}}
	ext, size, ok := VideoFormatFile("360p mp4 avc1", premuxed, false)
	if !ok || ext != "mp4" || size != 8000 {
		t.Errorf("VideoFormatFile = %q, %d, %v; want mp4, 8000, true", ext, size, ok)
	}
}

func TestAudioTrackExtAndSize(t *testing.T) {
	if got := AudioTrackExt("opus 160k"); got != "opus" {
		t.Errorf("AudioTrackExt(opus 160k) = %q, want opus", got)
	}
	if got := AudioTrackExt("m4a 129k"); got != "m4a" {
		t.Errorf("AudioTrackExt(m4a 129k) = %q, want m4a", got)
	}
	if got := AudioTrackSize("m4a 129k", youtubeLike); got != 3000 {
		t.Errorf("AudioTrackSize(m4a 129k) = %d, want 3000", got)
	}
}
