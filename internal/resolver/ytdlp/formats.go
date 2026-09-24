package ytdlp

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A probe lists a source's tracks one by one, and the video and audio rows
// offer each distinct one by name: "1080p60 webm vp9" for a video track, "opus
// 160k" for an audio track. The name is also what the row stores. The selector
// built from it matches the track's attributes rather than its format id, so a
// pick means the same file to a host that renumbers its formats between the
// probe and the download.

// codecFamily is one codec under the several names extractors report it by:
// avc1.640028 on one site, h264 on another.
type codecFamily struct {
	name string
	// match is the family's pattern, valid both here and in a yt-dlp ~= filter.
	match *regexp.Regexp
	// ext is what -x writes when it copies an audio track of this codec
	// instead of converting it. Empty for video, and for a codec -x converts.
	ext string
}

var videoFamilies = []codecFamily{
	{name: "avc1", match: regexp.MustCompile(`^(avc|h264)`)},
	{name: "hevc", match: regexp.MustCompile(`^(hev|hvc|h265)`)},
	{name: "vp9", match: regexp.MustCompile(`^vp0?9`)},
	{name: "vp8", match: regexp.MustCompile(`^vp0?8`)},
	{name: "av1", match: regexp.MustCompile(`^av0?1`)},
}

var audioFamilies = []codecFamily{
	{name: "m4a", match: regexp.MustCompile(`^(mp4a|aac)`), ext: "m4a"},
	{name: "opus", match: regexp.MustCompile(`^opus`), ext: "opus"},
	{name: "vorbis", match: regexp.MustCompile(`^vorbis`), ext: "ogg"},
	{name: "mp3", match: regexp.MustCompile(`^mp3`), ext: "mp3"},
	{name: "flac", match: regexp.MustCompile(`^flac`), ext: "flac"},
	{name: "alac", match: regexp.MustCompile(`^alac`), ext: "m4a"},
}

// keyToken is one word of a key. Keys end up inside a -f selector, so nothing
// but letters and digits gets that far.
var keyToken = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

var (
	videoKey = regexp.MustCompile(`^([1-9][0-9]{1,4})p([1-9][0-9]{1,2})? ([a-z0-9]{1,10}) ([a-z0-9]{1,10})$`)
	audioKey = regexp.MustCompile(`^([a-z0-9]{1,10}) ([1-9][0-9]{0,3})k$`)
)

// familyOf names the family a codec string belongs to. A codec no family
// covers is named after its leading token ("theora" for "theora.3"), which
// still selects it by prefix.
func familyOf(codec string, families []codecFamily) (codecFamily, bool) {
	c := strings.ToLower(strings.TrimSpace(codec))
	if c == "" || c == "none" {
		return codecFamily{}, false
	}
	for _, f := range families {
		if f.match.MatchString(c) {
			return f, true
		}
	}
	tok := c
	if i := strings.IndexFunc(c, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }); i >= 0 {
		tok = c[:i]
	}
	if !keyToken.MatchString(tok) {
		return codecFamily{}, false
	}
	return familyNamed(tok, families), true
}

// familyNamed is the family a key names, which for an unlisted codec is the
// prefix match familyOf built it from.
func familyNamed(name string, families []codecFamily) codecFamily {
	for _, f := range families {
		if f.name == name {
			return f
		}
	}
	return codecFamily{name: name, match: regexp.MustCompile("^" + name)}
}

// familyRank orders families as their table lists them, unlisted ones last.
func familyRank(f codecFamily, families []codecFamily) int {
	for i, x := range families {
		if x.name == f.name {
			return i
		}
	}
	return len(families)
}

// videoFormat is one distinct video track: a height, a frame rate, a container
// and a codec family.
type videoFormat struct {
	height int
	// fps is the track's rate rounded to a whole number when that is above 30,
	// and 0 otherwise, which is how one height's 30 and 60 fps tracks tell
	// apart.
	fps   int
	ext   string
	codec codecFamily
}

func (v videoFormat) key() string {
	fps := ""
	if v.fps > 0 {
		fps = strconv.Itoa(v.fps)
	}
	return fmt.Sprintf("%dp%s %s %s", v.height, fps, v.ext, v.codec.name)
}

func parseVideoFormat(key string) (videoFormat, bool) {
	m := videoKey.FindStringSubmatch(key)
	if m == nil {
		return videoFormat{}, false
	}
	h, _ := strconv.Atoi(m[1])
	fps := 0
	if m[2] != "" {
		fps, _ = strconv.Atoi(m[2])
		if fps <= 30 {
			return videoFormat{}, false
		}
	}
	return videoFormat{height: h, fps: fps, ext: m[3], codec: familyNamed(m[4], videoFamilies)}, true
}

func videoFormatOf(f FormatEntry) (videoFormat, bool) {
	if f.Height <= 0 {
		return videoFormat{}, false
	}
	codec, ok := familyOf(f.Vcodec, videoFamilies)
	if !ok {
		return videoFormat{}, false
	}
	ext := strings.ToLower(f.Ext)
	if !keyToken.MatchString(ext) {
		return videoFormat{}, false
	}
	fps := 0
	if r := int(math.Round(f.FPS)); r > 30 {
		fps = r
	}
	return videoFormat{height: f.Height, fps: fps, ext: ext, codec: codec}, true
}

// filter matches the track by its attributes. The codec pattern is made
// case-insensitive for yt-dlp, which matches ~= against the codec as the host
// wrote it ("H264" as well as "avc1").
func (v videoFormat) filter() string {
	f := fmt.Sprintf("[height=%d][ext=%s][vcodec~='(?i)%s']", v.height, v.ext, v.codec.match)
	if v.fps > 0 {
		return f + fmt.Sprintf("[fps>%d][fps<%d]", v.fps-1, v.fps+1)
	}
	// Below 30.5, the rate that rounds to 31, so a 30.3 fps track keeps the key
	// videoFormatOf gave it. The ? keeps a track that reports no rate, as most
	// outside YouTube do.
	return f + "[fps<?30.5]"
}

// companionAudio is the audio container that keeps a merge in this track's
// own container, "" when mkv is the answer whatever the audio.
func (v videoFormat) companionAudio() string {
	switch v.ext {
	case "mp4":
		return "m4a"
	case "webm":
		return "webm"
	}
	return ""
}

// selector asks for the track plus audio, the kind that keeps the track's own
// container first, and takes a track that already carries its audio as it is.
// Nothing falls back to another track: a pick that is gone fails with yt-dlp's
// own sentence rather than quietly fetching something else.
func (v videoFormat) selector() string {
	f := v.filter()
	s := "bv" + f + "+ba/b" + f
	if a := v.companionAudio(); a != "" {
		s = "bv" + f + "+ba[ext=" + a + "]/" + s
	}
	return s
}

// keepsContainer reports whether a merge may stay in the track's own
// container. yt-dlp cannot embed a thumbnail into webm and fails the download
// when asked to, so with embedThumbnail a webm pick merges into mkv.
func (v videoFormat) keepsContainer(embedThumbnail bool) bool {
	return v.companionAudio() != "" && !(embedThumbnail && v.ext == "webm")
}

// mergeFormat is the --merge-output-format value: the track's own container
// where yt-dlp can put the chosen pair into it, mkv otherwise.
func (v videoFormat) mergeFormat(embedThumbnail bool) string {
	if v.keepsContainer(embedThumbnail) {
		return v.ext + "/mkv"
	}
	return "mkv"
}

// VideoFormats lists the distinct video tracks in formats, tallest first, as
// the keys the video row offers and stores.
func VideoFormats(formats []FormatEntry) []string {
	seen := map[string]bool{}
	var found []videoFormat
	for _, f := range formats {
		v, ok := videoFormatOf(f)
		if !ok || seen[v.key()] {
			continue
		}
		seen[v.key()] = true
		found = append(found, v)
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.height != b.height {
			return a.height > b.height
		}
		if a.fps != b.fps {
			return a.fps > b.fps
		}
		if a.ext != b.ext {
			return a.ext < b.ext
		}
		return familyRank(a.codec, videoFamilies) < familyRank(b.codec, videoFamilies)
	})
	out := make([]string, len(found))
	for i, v := range found {
		out[i] = v.key()
	}
	return out
}

// IsVideoFormat reports whether s is a key VideoFormats produces.
func IsVideoFormat(s string) bool {
	_, ok := parseVideoFormat(s)
	return ok
}

// VideoFormatFile predicts what a video key downloads from formats: the
// file's extension and the matched track's size, 0 when the host gave none.
// embedThumbnail is Embed.Thumbnail, which decides the container of a webm
// pick. ok is false when no format matches the key.
func VideoFormatFile(key string, formats []FormatEntry, embedThumbnail bool) (ext string, size int64, ok bool) {
	v, valid := parseVideoFormat(key)
	if !valid {
		return "", 0, false
	}
	// Kept apart because selector asks for a video-only track first and takes
	// one that carries its own audio only when there is none.
	var videoOnly, combined *FormatEntry
	for i, f := range formats {
		got, match := videoFormatOf(f)
		if !match || got.key() != v.key() {
			continue
		}
		slot := &combined
		if !carries(f.Acodec) {
			slot = &videoOnly
		}
		if *slot == nil || f.Size() > (*slot).Size() {
			*slot = &formats[i]
		}
	}
	switch {
	case videoOnly != nil:
		ext = "mkv"
		if v.keepsContainer(embedThumbnail) {
			for _, f := range formats {
				if !carries(f.Vcodec) && carries(f.Acodec) && f.Ext == v.companionAudio() {
					ext = v.ext
					break
				}
			}
		}
		return ext, videoOnly.Size(), true
	case combined != nil:
		return combined.Ext, combined.Size(), true
	}
	return "", 0, false
}

// carries reports whether a vcodec or acodec value names a stream rather than
// its absence.
func carries(codec string) bool { return codec != "" && codec != "none" }

// audioTrack is one distinct audio-only track: a codec family and a bitrate.
type audioTrack struct {
	codec codecFamily
	kbps  int
}

func (a audioTrack) key() string { return fmt.Sprintf("%s %dk", a.codec.name, a.kbps) }

func parseAudioTrack(key string) (audioTrack, bool) {
	m := audioKey.FindStringSubmatch(key)
	if m == nil {
		return audioTrack{}, false
	}
	kbps, _ := strconv.Atoi(m[2])
	return audioTrack{codec: familyNamed(m[1], audioFamilies), kbps: kbps}, true
}

// audioTrackOf reads an audio-only format as a track. One without a bitrate
// is left out: its name would be the bare codec, which is already on the menu
// as a conversion target.
func audioTrackOf(f FormatEntry) (audioTrack, bool) {
	if carries(f.Vcodec) {
		return audioTrack{}, false
	}
	codec, ok := familyOf(f.Acodec, audioFamilies)
	if !ok || f.Abr <= 0 {
		return audioTrack{}, false
	}
	kbps := int(math.Round(f.Abr))
	if kbps < 1 || kbps > 9999 {
		return audioTrack{}, false
	}
	return audioTrack{codec: codec, kbps: kbps}, true
}

// selector matches the track's codec, ignoring case as videoFormat.filter does,
// and its bitrate as the key rounded it, with the language filter first when
// one is set.
func (a audioTrack) selector(lang string) string {
	f := fmt.Sprintf("[acodec~='(?i)%s'][abr>=%s][abr<%s]", a.codec.match,
		strconv.FormatFloat(float64(a.kbps)-0.5, 'f', 1, 64),
		strconv.FormatFloat(float64(a.kbps)+0.5, 'f', 1, 64))
	if lang == "" {
		return "ba" + f
	}
	return "ba[language^=" + lang + "]" + f + "/ba" + f
}

// AudioTracks lists the distinct audio-only tracks in formats, highest
// bitrate first, as the keys the audio row offers and stores.
func AudioTracks(formats []FormatEntry) []string {
	seen := map[string]bool{}
	var found []audioTrack
	for _, f := range formats {
		a, ok := audioTrackOf(f)
		if !ok || seen[a.key()] {
			continue
		}
		seen[a.key()] = true
		found = append(found, a)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].kbps != found[j].kbps {
			return found[i].kbps > found[j].kbps
		}
		return familyRank(found[i].codec, audioFamilies) < familyRank(found[j].codec, audioFamilies)
	})
	out := make([]string, len(found))
	for i, a := range found {
		out[i] = a.key()
	}
	return out
}

// IsAudioTrack reports whether s is a key AudioTracks produces.
func IsAudioTrack(s string) bool {
	_, ok := parseAudioTrack(s)
	return ok
}

// AudioTrackExt is the extension a picked track is written with, "" when its
// codec has no fixed one.
func AudioTrackExt(key string) string {
	a, ok := parseAudioTrack(key)
	if !ok {
		return ""
	}
	return a.codec.ext
}

// ExtractedExt is the extension -x writes when it keeps f's audio as it is:
// the codec's own for the codecs it copies (opus out of a webm is .opus), the
// format's extension otherwise.
func ExtractedExt(f FormatEntry) string {
	if c, ok := familyOf(f.Acodec, audioFamilies); ok && c.ext != "" {
		return c.ext
	}
	return f.Ext
}

// AudioTrackSize is the size of the track a key picks from formats, 0 when
// none matches or the host gave no size.
func AudioTrackSize(key string, formats []FormatEntry) int64 {
	var size int64
	for _, f := range formats {
		if a, ok := audioTrackOf(f); ok && a.key() == key && f.Size() > size {
			size = f.Size()
		}
	}
	return size
}

// Size is a format's best known byte count: exact when the host reports one,
// yt-dlp's estimate otherwise, 0 when neither is known.
func (f FormatEntry) Size() int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}
