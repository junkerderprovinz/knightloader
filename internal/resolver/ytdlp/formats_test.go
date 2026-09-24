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
// under another id, with an estimate where the direct copy has an exact size.
var youtubeLike = []FormatEntry{
	{FormatID: "sb0", Ext: "mhtml", Vcodec: "none", Acodec: "none", Height: 90, Protocol: "mhtml"},
	{FormatID: "139", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.5", Abr: 48.8, Filesize: 1000, Protocol: "https"},
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129.478, Filesize: 3000, Protocol: "https"},
	{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 160.2, Filesize: 3500, Protocol: "https"},
	{FormatID: "250", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 70.1, Filesize: 1600, Protocol: "https"},
	{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, FPS: 30, Filesize: 8000, Protocol: "https"},
	{FormatID: "134", Ext: "mp4", Vcodec: "avc1.4d401e", Acodec: "none", Height: 360, FPS: 30, Filesize: 5000, Protocol: "https"},
	{FormatID: "243", Ext: "webm", Vcodec: "vp9", Acodec: "none", Height: 360, FPS: 30, Filesize: 4000, Protocol: "https"},
	{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 30, Filesize: 50000, Protocol: "https"},
	{FormatID: "hls-1080", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 30, FilesizeApprox: 52000, Protocol: "m3u8_native"},
	{FormatID: "248", Ext: "webm", Vcodec: "vp9", Acodec: "none", Height: 1080, FPS: 30, Filesize: 40000, Protocol: "https"},
	{FormatID: "399", Ext: "mp4", Vcodec: "av01.0.08M.08", Acodec: "none", Height: 1080, FPS: 30, Filesize: 30000, Protocol: "https"},
	{FormatID: "299", Ext: "mp4", Vcodec: "avc1.64002a", Acodec: "none", Height: 1080, FPS: 59.94, Filesize: 90000, Protocol: "https"},
	{FormatID: "315", Ext: "webm", Vcodec: "vp09.00.51.08", Acodec: "none", Height: 2160, FPS: 60, Filesize: 400000, Protocol: "https"},
}

// archiveLike is a host that reports no codecs at all, only a height per file,
// as archive.org does.
var archiveLike = []FormatEntry{
	{FormatID: "0", Ext: "ogv", Height: 300, Filesize: 46935223, Protocol: "https"},
	{FormatID: "1", Ext: "mp4", Height: 360, Filesize: 61878609, Protocol: "https"},
	{FormatID: "2", Ext: "avi", Height: 720, Filesize: 332243668, Protocol: "https"},
}

func TestVideoTracksListsEveryDistinctTrackTallestFirst(t *testing.T) {
	want := []string{
		"2160p60 webm vp9",
		"1080p60 mp4 avc1",
		"1080p mp4 avc1",
		"1080p mp4 av1",
		"1080p webm vp9",
		"360p mp4 avc1",
		"360p webm vp9",
	}
	if got := VideoTracks(youtubeLike); !reflect.DeepEqual(got, want) {
		t.Errorf("VideoTracks =\n  %v\nwant\n  %v", got, want)
	}
}

func TestVideoContainersListsEachContainerOnceAfterBest(t *testing.T) {
	want := []string{"best", "mp4 avc1", "mp4 av1", "webm vp9"}
	if got := VideoContainers(youtubeLike); !reflect.DeepEqual(got, want) {
		t.Errorf("VideoContainers = %v, want %v", got, want)
	}
}

// Each key must read back as a key, or the row would store a pick the backend
// then drops. A rate just over 30, as some hosts report, rounds to the 30 a
// key leaves unnamed.
func TestEveryListedVideoTrackIsAValidKey(t *testing.T) {
	formats := append(slices.Clone(youtubeLike),
		FormatEntry{FormatID: "720-30", Ext: "mp4", Vcodec: "avc1.64001F", Acodec: "none", Height: 720, FPS: 30.3})
	formats = append(formats, archiveLike...)
	for _, k := range VideoTracks(formats) {
		if !IsVideoTrack(k) {
			t.Errorf("VideoTracks listed %q, which IsVideoTrack refuses", k)
		}
	}
}

// A host that names no codec still offers its files by container and height,
// and each is taken whole, since it carries its own audio.
func TestAHostWithoutCodecsStillOffersItsTracks(t *testing.T) {
	if got, want := VideoTracks(archiveLike), []string{"720p avi", "360p mp4", "300p ogv"}; !reflect.DeepEqual(got, want) {
		t.Errorf("VideoTracks = %v, want %v", got, want)
	}
	if got, want := VideoContainers(archiveLike), []string{"best", "avi", "mp4", "ogv"}; !reflect.DeepEqual(got, want) {
		t.Errorf("VideoContainers = %v, want %v", got, want)
	}
	sel, _ := valueAfter(buildArgs("d", Options{VideoPick: "720p avi"}), "-f")
	if want := "bv[height=720][ext=avi][fps<?30.5]+ba/b[height=720][ext=avi][fps<?30.5]"; sel != want {
		t.Errorf("-f = %q, want %q", sel, want)
	}
	if ext, size := VideoFile("720p avi", archiveLike, false); ext != "avi" || size != 332243668 {
		t.Errorf("VideoFile(720p avi) = %q, %d; want the avi file and its own size", ext, size)
	}
}

func TestAudioTracksListsEveryDistinctTrackHighestBitrateFirst(t *testing.T) {
	want := []string{"opus 160k", "m4a 129k", "opus 70k", "m4a 49k"}
	if got := AudioTracks(youtubeLike); !reflect.DeepEqual(got, want) {
		t.Errorf("AudioTracks = %v, want %v", got, want)
	}
}

// The bitrate is what tells two tracks of one format apart in the picker.
func TestAudioTracksLeavesOutATrackWithoutABitrate(t *testing.T) {
	got := AudioTracks([]FormatEntry{{Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2"}})
	if len(got) != 0 {
		t.Errorf("AudioTracks = %v, want nothing for a track that reports no bitrate", got)
	}
}

func TestAudioFormatsOfListsEachCodecTheSourceCarriesAfterBest(t *testing.T) {
	if got, want := AudioFormatsOf(youtubeLike), []string{"best", "m4a", "opus"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AudioFormatsOf = %v, want %v", got, want)
	}
	// A track without a bitrate still makes its format available, and a codec
	// the picker has no name for is left to best.
	formats := []FormatEntry{
		{Ext: "ogg", Vcodec: "none", Acodec: "vorbis"},
		{Ext: "mp4", Vcodec: "none", Acodec: "ec-3", Abr: 384},
	}
	if got, want := AudioFormatsOf(formats), []string{"best", "vorbis"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AudioFormatsOf = %v, want %v", got, want)
	}
}

func TestKeysRefuseAnythingTheyCouldNotHaveProduced(t *testing.T) {
	for _, bad := range []string{
		"", "best", "1080p", "mp4", "1080p25 mp4 avc1", "1080p mp4 avc1'", "1080p mp4 avc1]+ba",
		"1080p mp4 avc1 extra", "0p mp4 avc1",
	} {
		if IsVideoTrack(bad) {
			t.Errorf("IsVideoTrack(%q) = true, want false", bad)
		}
	}
	for _, bad := range []string{"", "best", "custom", "1080p", "mp4", "mp4 avc1 1080", "mp4 avc1 1080p60", "1080p mp4"} {
		if _, ok := parseVideoWish(bad); ok {
			t.Errorf("parseVideoWish(%q) accepted it, want it refused", bad)
		}
	}
	for _, bad := range []string{"", "opus", "best", "opus 0k", "opus 160", "opus 160k]", "op us 160k"} {
		if IsAudioTrack(bad) {
			t.Errorf("IsAudioTrack(%q) = true, want false", bad)
		}
	}
}

func TestAPickedVideoTrackSelectsByItsAttributesAndKeepsItsContainer(t *testing.T) {
	args := buildArgs("d", Options{VideoPick: "1080p mp4 avc1"})
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
	got, _ := valueAfter(buildArgs("d", Options{VideoPick: "1080p60 mp4 avc1"}), "-f")
	if !strings.Contains(got, "[fps>59][fps<61]") {
		t.Errorf("-f = %q, want the 59..61 band a 59.94 fps track falls in", got)
	}
}

func TestAPickedVideoTrackOutranksTheQualityCap(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{Quality: Quality720p, VideoPick: "1080p webm vp9"}), "-f")
	if !strings.Contains(got, "[height=1080][ext=webm][vcodec~='(?i)^vp0?9']") {
		t.Errorf("-f = %q, want the picked 1080p webm track, not the 720p cap", got)
	}
}

// yt-dlp refuses to embed a thumbnail into webm and fails the download, so a
// webm pick merges into mkv while the thumbnail is embedded.
func TestAWebmPickMergesIntoMkvWhenTheThumbnailIsEmbedded(t *testing.T) {
	args := buildArgs("d", Options{VideoPick: "1080p webm vp9", Embed: Embed{Thumbnail: true}})
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "mkv" {
		t.Errorf("--merge-output-format = %q, want mkv", merge)
	}
	if ext, _ := VideoFile("1080p webm vp9", youtubeLike, true); ext != "mkv" {
		t.Errorf("VideoFile predicts %q, want the mkv the merge writes", ext)
	}
}

// yt-dlp matches ~= against the codec as the host wrote it, and some hosts
// write it in capitals.
func TestACodecFilterMatchesTheCodecInAnyCase(t *testing.T) {
	formats := []FormatEntry{
		{Ext: "mp4", Vcodec: "H264", Acodec: "none", Height: 720},
		{Ext: "m4a", Vcodec: "none", Acodec: "AAC", Abr: 128},
	}
	video, audio := VideoTracks(formats), AudioTracks(formats)
	if len(video) != 1 || len(audio) != 1 {
		t.Fatalf("VideoTracks = %v, AudioTracks = %v; want one of each", video, audio)
	}
	vsel, _ := valueAfter(buildArgs("d", Options{VideoPick: video[0]}), "-f")
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
	args := buildArgs("d", Options{VideoPick: "720p flv avc1"})
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "mkv" {
		t.Errorf("--merge-output-format = %q, want mkv", merge)
	}
	if got, _ := valueAfter(args, "-f"); strings.Contains(got, "+ba[ext=") {
		t.Errorf("-f = %q, want no preferred audio container for flv", got)
	}
}

func TestAPresetWishResolvesToTheBestTrackOfItsFormatUnderTheCap(t *testing.T) {
	cases := map[string]string{
		"webm vp9 1080p": "1080p webm vp9",
		"mp4 avc1":       "1080p60 mp4 avc1",
		"mp4 avc1 720p":  "360p mp4 avc1",
		"webm vp9":       "2160p60 webm vp9",
		// No track in the format: the cap alone, or best without one.
		"mp4 hevc 1080p": "1080p",
		"mp4 hevc":       "best",
		// Anything that is not a wish is left alone.
		"1080p webm vp9": "1080p webm vp9",
		"720p":           "720p",
		"best":           "best",
	}
	for pick, want := range cases {
		if got := ResolveVideoPick(pick, youtubeLike); got != want {
			t.Errorf("ResolveVideoPick(%q) = %q, want %q", pick, got, want)
		}
	}
}

// Before a probe answers, a preset's format is asked for directly, and a link
// without it downloads what the cap alone would give instead of failing.
func TestAnUnresolvedWishFallsBackToTheCap(t *testing.T) {
	args := buildArgs("d", Options{VideoPick: "webm vp9 1080p"})
	got, _ := valueAfter(args, "-f")
	g := "[ext=webm][vcodec~='(?i)^vp0?9'][height<=?1080]"
	want := "bv" + g + "+ba[ext=webm]/bv" + g + "+ba/b" + g + "/bestvideo[height<=?1080]+bestaudio/best[height<=?1080]"
	if got != want {
		t.Errorf("-f =\n  %q\nwant\n  %q", got, want)
	}
	if merge, _ := valueAfter(args, "--merge-output-format"); merge != "webm/mkv" {
		t.Errorf("--merge-output-format = %q, want webm/mkv", merge)
	}
	uncapped, _ := valueAfter(buildArgs("d", Options{VideoPick: "mp4 avc1"}), "-f")
	if !strings.HasSuffix(uncapped, "/bv*+ba/b") {
		t.Errorf("-f = %q, want yt-dlp's own default as the last resort", uncapped)
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

// Asked for m4a, a source with an AAC track has it copied; only a source
// without one is converted, which is what --audio-format does on its own.
func TestAnAudioFormatTakesTheSourcesOwnTrackBeforeConverting(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioFormat: "m4a"})
	got, _ := valueAfter(args, "-f")
	if want := "ba[acodec~='(?i)^(mp4a|aac)']/bestaudio/best"; got != want {
		t.Errorf("-f = %q, want %q", got, want)
	}
	if got, _ := valueAfter(args, "--audio-format"); got != "m4a" {
		t.Errorf("--audio-format = %q, want m4a", got)
	}
	withLang, _ := valueAfter(buildArgs("d", Options{Variant: VariantAudio, AudioFormat: "opus", AudioLang: "de"}), "-f")
	if want := "ba[language^=de][acodec~='(?i)^opus']/ba[acodec~='(?i)^opus']/bestaudio[language^=de]/bestaudio/best"; withLang != want {
		t.Errorf("-f = %q, want %q", withLang, want)
	}
}

// yt-dlp writes both into an .m4a file, so they are one choice.
func TestAacIsReadAsM4a(t *testing.T) {
	if got := (Options{AudioFormat: "aac"}).Sanitize().AudioFormat; got != "m4a" {
		t.Errorf("Sanitize kept AudioFormat %q, want m4a", got)
	}
	if got := (HosterPreset{AudioFormat: "aac"}).Sanitize().AudioFormat; got != "m4a" {
		t.Errorf("a preset kept AudioFormat %q, want m4a", got)
	}
	if got, _ := ResolveAudioPick("aac", "", youtubeLike); got != "m4a" {
		t.Errorf("ResolveAudioPick(aac) = %q, want m4a", got)
	}
}

func TestAPresetBitrateResolvesToTheNearestTrackOfItsFormat(t *testing.T) {
	cases := []struct {
		pick, bitrate         string
		wantPick, wantBitrate string
	}{
		{"opus", "128", "opus 160k", ""},
		{"m4a", "64", "m4a 49k", ""},
		// Equally far from both: the higher one.
		{"opus", "115", "opus 160k", ""},
		// Nothing asked for: the best track of the format, at download time.
		{"opus", "", "opus", ""},
		// A format the source lacks stays a conversion at that bitrate.
		{"mp3", "192", "mp3", "192"},
		{"best", "128", "best", "128"},
		{"m4a 49k", "", "m4a 49k", ""},
	}
	for _, c := range cases {
		gotPick, gotBitrate := ResolveAudioPick(c.pick, c.bitrate, youtubeLike)
		if gotPick != c.wantPick || gotBitrate != c.wantBitrate {
			t.Errorf("ResolveAudioPick(%q, %q) = %q, %q; want %q, %q",
				c.pick, c.bitrate, gotPick, gotBitrate, c.wantPick, c.wantBitrate)
		}
	}
}

// A hand-edited task could carry anything; Sanitize drops what is not a key.
func TestSanitizeDropsAPickThatIsNotAKey(t *testing.T) {
	o := Options{VideoPick: "1080p mp4 avc1]+ba", AudioTrack: "opus"}.Sanitize()
	if o.VideoPick != "" || o.AudioTrack != "" {
		t.Errorf("Sanitize kept VideoPick %q and AudioTrack %q, want both dropped", o.VideoPick, o.AudioTrack)
	}
	if o := (Options{VideoPick: "webm vp9 1080p"}).Sanitize(); o.VideoPick != "webm vp9 1080p" {
		t.Errorf("Sanitize dropped the wish %q", "webm vp9 1080p")
	}
}

// A merge downloads the video and the audio, so the size is both.
func TestVideoFilePredictsTheContainerAndTheMergedSize(t *testing.T) {
	cases := []struct {
		pick string
		ext  string
		size int64
	}{
		// The direct copy over the HLS one, with the m4a track that keeps
		// the merge in mp4.
		{"1080p mp4 avc1", "mp4", 50000 + 3000},
		{"1080p webm vp9", "webm", 40000 + 3500},
		{"2160p60 webm vp9", "webm", 400000 + 3500},
		// 360p mp4 avc1 exists as video-only (134) and pre-muxed (18); the
		// selector asks for the video-only one first.
		{"360p mp4 avc1", "mp4", 5000 + 3000},
		// A wish is measured as the track it resolves to.
		{"webm vp9 1080p", "webm", 40000 + 3500},
		// A cap takes the tallest video-only track under it, the 60 fps one
		// first, and the best audio.
		{"1080p", "mkv", 90000 + 3500},
		{"best", "mkv", 400000 + 3500},
	}
	for _, c := range cases {
		ext, size := VideoFile(c.pick, youtubeLike, false)
		if ext != c.ext || size != c.size {
			t.Errorf("VideoFile(%q) = %q, %d; want %q, %d", c.pick, ext, size, c.ext, c.size)
		}
	}
	if ext, size := VideoFile("720p mp4 avc1", youtubeLike, false); ext != "" || size != 0 {
		t.Errorf("VideoFile found %q, %d for a 720p track in a list that has none", ext, size)
	}
}

// Best passes no selector, so yt-dlp's own default choice, which the probe
// reports, is what downloads, whatever the heuristics would have guessed.
func TestTheBestVideoIsWhatTheProbeSaidYtdlpPicks(t *testing.T) {
	formats := slices.Clone(youtubeLike)
	for i := range formats {
		formats[i].Default = formats[i].FormatID == "399" || formats[i].FormatID == "140"
	}
	if ext, size := VideoFile("best", formats, false); ext != "mkv" || size != 30000+3000 {
		t.Errorf("VideoFile(best) = %q, %d; want mkv and 33000, the two formats yt-dlp picked", ext, size)
	}
	if ext, size := AudioFile("best", formats); ext != "m4a" || size != 3000 {
		t.Errorf("AudioFile(best) = %q, %d; want m4a and 3000, the audio yt-dlp picked", ext, size)
	}
}

func TestVideoFileMergesIntoMkvWithoutAudioOfTheSameContainer(t *testing.T) {
	onlyOpus := []FormatEntry{
		{Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 160},
		{Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080},
	}
	if ext, _ := VideoFile("1080p mp4 avc1", onlyOpus, false); ext != "mkv" {
		t.Errorf("ext = %q, want mkv; mp4 cannot hold the only audio there is", ext)
	}
}

func TestAPreMuxedTrackKeepsItsOwnExtension(t *testing.T) {
	premuxed := []FormatEntry{{Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, Filesize: 8000}}
	if ext, size := VideoFile("360p mp4 avc1", premuxed, false); ext != "mp4" || size != 8000 {
		t.Errorf("VideoFile = %q, %d; want mp4, 8000", ext, size)
	}
}

// Without the video's own size, the audio's alone would pass for the whole.
func TestAMergeWhoseVideoHasNoSizeHasNone(t *testing.T) {
	formats := []FormatEntry{
		{Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 128, Filesize: 3000},
		{Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080},
	}
	if _, size := VideoFile("1080p mp4 avc1", formats, false); size != 0 {
		t.Errorf("size = %d, want 0 while the video's own size is unknown", size)
	}
}

func TestAudioFilePredictsTheExtensionAndTheCopiedTracksSize(t *testing.T) {
	cases := []struct {
		pick string
		ext  string
		size int64
	}{
		{"m4a 129k", "m4a", 3000},
		{"opus 70k", "opus", 1600},
		// A format alone is its best track.
		{"opus", "opus", 3500},
		{"m4a", "m4a", 3000},
		// Without a probe mark, best is the highest bitrate.
		{"best", "opus", 3500},
		// A conversion's size is the encoder's to decide.
		{"mp3", "mp3", 0},
		{"vorbis", "ogg", 0},
	}
	for _, c := range cases {
		ext, size := AudioFile(c.pick, youtubeLike)
		if ext != c.ext || size != c.size {
			t.Errorf("AudioFile(%q) = %q, %d; want %q, %d", c.pick, ext, size, c.ext, c.size)
		}
	}
}

func TestAPresetStartsTheVideoRowWithItsFormatCappedAtItsQuality(t *testing.T) {
	cases := []struct {
		preset HosterPreset
		want   string
	}{
		{HosterPreset{VideoFormat: "webm vp9", Quality: Quality1080p}, "webm vp9 1080p"},
		{HosterPreset{VideoFormat: "mp4 avc1", Quality: QualityBest}, "mp4 avc1"},
		{HosterPreset{VideoFormat: "best", Quality: Quality720p}, "720p"},
		{HosterPreset{VideoFormat: "best", Quality: QualityCustom}, "custom"},
	}
	for _, c := range cases {
		if got := c.preset.Sanitize().VideoPick(); got != c.want {
			t.Errorf("%+v.VideoPick() = %q, want %q", c.preset, got, c.want)
		}
	}
}

func TestSanitizeRepairsAPresetsFormats(t *testing.T) {
	p := HosterPreset{VideoFormat: "mp4 avc1]", Quality: QualityCustom, AudioFormat: "best", AudioBitrate: "192"}.Sanitize()
	if p.VideoFormat != "best" || p.AudioBitrate != "" {
		t.Errorf("Sanitize = %+v, want an unreadable format folded to best and no bitrate beside best audio", p)
	}
	p = HosterPreset{VideoFormat: "webm vp9", Quality: QualityCustom, AudioFormat: "opus", AudioBitrate: "1337"}.Sanitize()
	if p.Quality != QualityBest || p.AudioBitrate != "" {
		t.Errorf("Sanitize = %+v, want custom dropped beside a container and an unknown bitrate cleared", p)
	}
	if p := (HosterPreset{}).Sanitize(); p.VideoFormat != "best" || p.AudioFormat != "best" {
		t.Errorf("Sanitize of a preset saved before formats existed = %+v, want best for both", p)
	}
}
