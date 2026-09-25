package ytdlp

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// A probe lists a source's tracks one by one. The video and audio rows offer
// them in two steps, a format and then a quality or bitrate within it, and
// store the pair as one key: "1080p60 webm vp9" for a video track, "opus 160k"
// for an audio track. The selector built from a key matches the track's
// attributes rather than its format id, so a pick means the same file to a host
// that renumbers its formats between the probe and the download.
//
// A host preset names a format before any probe has listed the link's tracks:
// "webm vp9 1080p" on a video row, a bare "opus" and a bitrate on an audio row.
// The probe turns such a wish into a track (ResolveVideoPick, ResolveAudioPick).
// One that reaches the download unresolved selects with a fallback, since a
// preset speaks for every link of a host and one link without the format is no
// reason to fail.

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

// preferredVideoCodecs is the order yt-dlp's own format sort ranks codecs in
// at an equal resolution and frame rate, which is how it picks between them
// when only a resolution is asked for.
var preferredVideoCodecs = []string{"av1", "vp9", "hevc", "avc1", "vp8"}

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
	// qualityToken is the first word of a video track's key: a resolution, and
	// the frame rate where it is above 30.
	qualityToken = regexp.MustCompile(`^([1-9][0-9]{1,4})p([1-9][0-9]{1,2})?$`)
	// capToken is the last word of a video wish: the resolution it may not
	// exceed.
	capToken = regexp.MustCompile(`^([1-9][0-9]{1,4})p$`)
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

// videoContainer is what the video row's format picker offers: a container
// and, where the host reports one, a codec family. Its id is "mp4 avc1", or
// "avi" for a host that names no codec.
type videoContainer struct {
	ext   string
	codec codecFamily
}

func (c videoContainer) id() string {
	if c.codec.name == "" {
		return c.ext
	}
	return c.ext + " " + c.codec.name
}

// parseContainer reads a container id from its one or two words.
func parseContainer(words []string) (videoContainer, bool) {
	if len(words) < 1 || len(words) > 2 || !keyToken.MatchString(words[0]) {
		return videoContainer{}, false
	}
	c := videoContainer{ext: words[0]}
	if len(words) == 2 {
		if !keyToken.MatchString(words[1]) {
			return videoContainer{}, false
		}
		c.codec = familyNamed(words[1], videoFamilies)
	}
	return c, true
}

// filter matches the container and the codec. The codec pattern is made
// case-insensitive for yt-dlp, which matches ~= against the codec as the host
// wrote it ("H264" as well as "avc1").
func (c videoContainer) filter() string {
	f := "[ext=" + c.ext + "]"
	if c.codec.name != "" {
		f += fmt.Sprintf("[vcodec~='(?i)%s']", c.codec.match)
	}
	return f
}

// companionAudio is the audio container that keeps a merge in this container,
// "" when mkv is the answer whatever the audio.
func (c videoContainer) companionAudio() string {
	switch c.ext {
	case "mp4":
		return "m4a"
	case "webm":
		return "webm"
	}
	return ""
}

// containerCodecs are the video codecs yt-dlp merges into mp4 and webm (its
// get_compatible_ext). Anything else goes into mkv, such as the vp9 YouTube
// serves in mp4 over HLS.
var containerCodecs = map[string][]string{
	"mp4":  {"avc1", "hevc", "av1"},
	"webm": {"vp9", "vp8", "av1"},
}

// keepsContainer reports whether a merge may stay in the track's own
// container. yt-dlp cannot embed a thumbnail into webm and fails the download
// when asked to, so with embedThumbnail a webm pick merges into mkv.
func (c videoContainer) keepsContainer(embedThumbnail bool) bool {
	if c.companionAudio() == "" || (embedThumbnail && c.ext == "webm") {
		return false
	}
	// yt-dlp goes by the extensions where the host names no codec.
	return c.codec.name == "" || slices.Contains(containerCodecs[c.ext], c.codec.name)
}

// mergeFormat is the --merge-output-format value: the track's own container
// where yt-dlp can put the chosen pair into it, mkv otherwise.
func (c videoContainer) mergeFormat(embedThumbnail bool) string {
	if c.keepsContainer(embedThumbnail) {
		return c.ext + "/mkv"
	}
	return "mkv"
}

// videoFormat is one distinct video track: a resolution (FormatEntry.Res), a
// frame rate and a container.
type videoFormat struct {
	res int
	// fps is the track's rate rounded to a whole number when that is above 30,
	// and 0 otherwise, which is how one resolution's 30 and 60 fps tracks tell
	// apart.
	fps int
	videoContainer
}

// quality is the first word of the key, the one the quality picker shows.
func (v videoFormat) quality() string {
	fps := ""
	if v.fps > 0 {
		fps = strconv.Itoa(v.fps)
	}
	return fmt.Sprintf("%dp%s", v.res, fps)
}

func (v videoFormat) key() string { return v.quality() + " " + v.id() }

func parseVideoFormat(key string) (videoFormat, bool) {
	words := strings.Split(key, " ")
	if len(words) < 2 {
		return videoFormat{}, false
	}
	m := qualityToken.FindStringSubmatch(words[0])
	if m == nil {
		return videoFormat{}, false
	}
	res, _ := strconv.Atoi(m[1])
	fps := 0
	if m[2] != "" {
		fps, _ = strconv.Atoi(m[2])
		if fps <= 30 {
			return videoFormat{}, false
		}
	}
	c, ok := parseContainer(words[1:])
	if !ok {
		return videoFormat{}, false
	}
	return videoFormat{res: res, fps: fps, videoContainer: c}, true
}

// videoFormatOf reads a format as a video track. A format that reports a
// height but no codec at all, as archive.org's files do, is a track of
// unknown codec; one whose vcodec is "none" is no video.
func videoFormatOf(f FormatEntry) (videoFormat, bool) {
	if f.Height <= 0 || f.Vcodec == "none" {
		return videoFormat{}, false
	}
	ext := strings.ToLower(f.Ext)
	if !keyToken.MatchString(ext) {
		return videoFormat{}, false
	}
	c := videoContainer{ext: ext}
	if carries(f.Vcodec) {
		codec, ok := familyOf(f.Vcodec, videoFamilies)
		if !ok {
			return videoFormat{}, false
		}
		c.codec = codec
	}
	fps := 0
	if r := int(math.Round(f.FPS)); r > 30 {
		fps = r
	}
	return videoFormat{res: f.Res(), fps: fps, videoContainer: c}, true
}

// filters match the track by its attributes, once for each way up it may
// stand, since its key names a resolution and not the side it is measured on.
func (v videoFormat) filters() []string {
	rest := v.videoContainer.filter()
	if v.fps > 0 {
		rest += fmt.Sprintf("[fps>%d][fps<%d]", v.fps-1, v.fps+1)
	} else {
		// Below 30.5, the rate that rounds to 31, so a 30.3 fps track keeps the
		// key videoFormatOf gave it. The ? keeps a track that reports no rate,
		// as most outside YouTube do.
		rest += "[fps<?30.5]"
	}
	out := atRes(v.res)
	for i := range out {
		out[i] += rest
	}
	return out
}

// selector asks for the track plus audio, the kind that keeps the track's own
// container first, and takes a track that already carries its audio as it is.
// Nothing falls back to another track: a pick that is gone fails with yt-dlp's
// own sentence rather than quietly fetching something else.
func (v videoFormat) selector() string {
	return mergeSelector(v.filters(), v.companionAudio())
}

// atRes matches a track of resolution r either way up: a portrait track by its
// width, any other by its height. A track that reports no width counts as
// landscape, as it does for FormatEntry.Res.
func atRes(r int) []string {
	return []string{
		fmt.Sprintf("[width=%d][height>%d]", r, r),
		fmt.Sprintf("[height=%d][width>=?%d]", r, r),
	}
}

// underRes keeps the tracks whose resolution is at most r. yt-dlp filters by
// width or by height but not by the smaller of the two, so a portrait track
// taller than r but no wider is asked for first; the height filter alone would
// pass over it for a far smaller one. "<=?" keeps a track of unknown height in
// the running, where "<=" would drop it.
func underRes(r int) []string {
	return []string{
		fmt.Sprintf("[width<=%d][height>%d]", r, r),
		fmt.Sprintf("[height<=?%d]", r),
	}
}

// mergeSelector asks for a video-only format that one of filters matches,
// merged with audio of the companion container where there is one and then
// with any audio, and last for a matching format that carries its own audio.
// Each step tries the filters in order.
func mergeSelector(filters []string, companion string) string {
	var alts []string
	if companion != "" {
		for _, f := range filters {
			alts = append(alts, "bv"+f+"+ba[ext="+companion+"]")
		}
	}
	for _, f := range filters {
		alts = append(alts, "bv"+f+"+ba")
	}
	for _, f := range filters {
		alts = append(alts, "b"+f)
	}
	return strings.Join(alts, "/")
}

// videoWish is a host preset's video pick before a probe has resolved it: a
// container with a codec, and the resolution it may not exceed (0 for none).
type videoWish struct {
	videoContainer
	capRes int
}

func parseVideoWish(s string) (videoWish, bool) {
	words := strings.Split(s, " ")
	capRes := 0
	if len(words) == 3 {
		m := capToken.FindStringSubmatch(words[2])
		if m == nil {
			return videoWish{}, false
		}
		capRes, _ = strconv.Atoi(m[1])
		words = words[:2]
	}
	// Two words, so a quality cap or "best" alone is never a wish, and the
	// first may not be a quality, which would make it a track's key.
	if len(words) != 2 || qualityToken.MatchString(words[0]) {
		return videoWish{}, false
	}
	c, ok := parseContainer(words)
	if !ok {
		return videoWish{}, false
	}
	return videoWish{videoContainer: c, capRes: capRes}, true
}

// selector asks for the best track of the container under the cap, then for
// what the cap alone would give.
func (w videoWish) selector() string {
	if w.capRes == 0 {
		return mergeSelector([]string{w.filter()}, w.companionAudio()) + "/bv*+ba/b"
	}
	filters := underRes(w.capRes)
	for i := range filters {
		filters[i] = w.filter() + filters[i]
	}
	return mergeSelector(filters, w.companionAudio()) + "/" + capSelector(w.capRes)
}

// capSelector is the -f value for a resolution cap.
func capSelector(r int) string {
	return mergeSelector(underRes(r), "")
}

// pickedContainer is the container a video track or wish asks for.
func pickedContainer(pick string) (videoContainer, bool) {
	if v, ok := parseVideoFormat(pick); ok {
		return v.videoContainer, true
	}
	if w, ok := parseVideoWish(pick); ok {
		return w.videoContainer, true
	}
	return videoContainer{}, false
}

// IsVideoTrack reports whether s is a key VideoTracks produces.
func IsVideoTrack(s string) bool {
	_, ok := parseVideoFormat(s)
	return ok
}

// IsVideoPick reports whether s is a video track's key or a preset's wish,
// the two picks a video row stores beside the plain quality caps.
func IsVideoPick(s string) bool {
	_, ok := pickedContainer(s)
	return ok
}

// IsVideoContainer reports whether s names a container with its codec, the
// form a preset stores ("mp4 avc1").
func IsVideoContainer(s string) bool {
	words := strings.Split(s, " ")
	if len(words) != 2 || qualityToken.MatchString(words[0]) {
		return false
	}
	_, ok := parseContainer(words)
	return ok
}

// VideoTracks lists the distinct video tracks in formats, highest resolution
// first, as the keys the video row stores.
func VideoTracks(formats []FormatEntry) []string {
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
		if a.res != b.res {
			return a.res > b.res
		}
		if a.fps != b.fps {
			return a.fps > b.fps
		}
		if a.ext != b.ext {
			return a.ext < b.ext
		}
		return containerRank(a.videoContainer) < containerRank(b.videoContainer)
	})
	out := make([]string, len(found))
	for i, v := range found {
		out[i] = v.key()
	}
	return out
}

// VideoContainers lists the containers of the video tracks in formats as the
// video row's format picker offers them: "best", then by container and codec.
func VideoContainers(formats []FormatEntry) []string {
	seen := map[string]bool{}
	var found []videoContainer
	for _, f := range formats {
		v, ok := videoFormatOf(f)
		if !ok || seen[v.id()] {
			continue
		}
		seen[v.id()] = true
		found = append(found, v.videoContainer)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].ext != found[j].ext {
			return found[i].ext < found[j].ext
		}
		return containerRank(found[i]) < containerRank(found[j])
	})
	out := make([]string, 0, len(found)+1)
	out = append(out, string(QualityBest))
	for _, c := range found {
		out = append(out, c.id())
	}
	return out
}

// containerRank orders one container's codecs as the family table lists
// them, a container without a codec after all of them.
func containerRank(c videoContainer) int {
	if c.codec.name == "" {
		return len(videoFamilies) + 1
	}
	return familyRank(c.codec, videoFamilies)
}

// ResolveVideoPick turns a preset's wish into the best track of its container
// under its cap, or, when the source has no track in that container, into the
// cap alone. A track's key that the source does not list may name a portrait
// track by its height, and takes that track's key. Any other pick comes back
// as it is.
func ResolveVideoPick(pick string, formats []FormatEntry) string {
	if v, ok := parseVideoFormat(pick); ok {
		if k, ok := v.byHeight(formats); ok {
			return k
		}
		return pick
	}
	w, ok := parseVideoWish(pick)
	if !ok {
		return pick
	}
	var best videoFormat
	found := false
	for _, f := range formats {
		v, ok := videoFormatOf(f)
		if !ok || v.id() != w.id() || (w.capRes > 0 && v.res > w.capRes) {
			continue
		}
		if !found || v.res > best.res || (v.res == best.res && v.fps > best.fps) {
			best, found = v, true
		}
	}
	if found {
		return best.key()
	}
	if q := Quality(fmt.Sprintf("%dp", w.capRes)); w.capRes > 0 && validQuality(q) {
		return string(q)
	}
	return string(QualityBest)
}

// byHeight reads v's number as a height and finds the key of that track: one
// of the same container and frame rate that is that tall. It reports false
// when the source lists v itself or has no such track.
func (v videoFormat) byHeight(formats []FormatEntry) (string, bool) {
	renamed := ""
	for _, f := range formats {
		got, ok := videoFormatOf(f)
		if !ok || got.fps != v.fps || got.id() != v.id() {
			continue
		}
		if got.res == v.res {
			return "", false
		}
		if f.Height == v.res && renamed == "" {
			renamed = got.key()
		}
	}
	return renamed, renamed != ""
}

// VideoFile predicts what a video row's pick downloads from formats: the
// file's extension, "" where it cannot be told in advance, and its size, the
// video and the audio merged with it together, 0 where the host gave neither a
// size nor a bitrate. embedThumbnail is Embed.Thumbnail, which decides the
// container of a webm pick.
func VideoFile(pick string, formats []FormatEntry, embedThumbnail bool) (ext string, size int64) {
	pick = ResolveVideoPick(pick, formats)
	if v, ok := parseVideoFormat(pick); ok {
		return v.file(formats, embedThumbnail)
	}
	if r, ok := ResCap(Quality(pick)); ok {
		// capSelector: a video-only track and audio, else one that has both.
		if v := bestVideo(formats, r, isVideoOnly); v != nil {
			if a := bestAudio(formats, ""); a != nil {
				return "mkv", mergedSize(v, a)
			}
		}
		if v := bestVideo(formats, r, carriesBoth); v != nil {
			return v.Ext, v.Size()
		}
		return "", 0
	}
	// Best and custom pass no selector of their own, so yt-dlp's default
	// choice, which the probe answered with, is what downloads.
	var picked []*FormatEntry
	for i := range formats {
		if formats[i].Default {
			picked = append(picked, &formats[i])
		}
	}
	switch len(picked) {
	case 1:
		return picked[0].Ext, picked[0].Size()
	case 2:
		v, a := picked[0], picked[1]
		if !carries(v.Vcodec) {
			v, a = a, v
		}
		return "mkv", mergedSize(v, a)
	}
	v := bestVideo(formats, 0, func(FormatEntry) bool { return true })
	if v == nil {
		return "", 0
	}
	if isVideoOnly(*v) {
		if a := bestAudio(formats, ""); a != nil {
			return "mkv", mergedSize(v, a)
		}
		return "", v.Size()
	}
	// Without a pair to merge, --merge-output-format has nothing to name, and
	// which single file yt-dlp takes is left to its own sort.
	return "", v.Size()
}

// file is VideoFile for a track. The selector asks for a video-only track
// first and takes one that carries its own audio only when there is none, so
// the two are kept apart.
func (v videoFormat) file(formats []FormatEntry, embedThumbnail bool) (string, int64) {
	var videoOnly, combined *FormatEntry
	for i, f := range formats {
		got, match := videoFormatOf(f)
		if !match || got.key() != v.key() {
			continue
		}
		slot := &combined
		if isVideoOnly(f) {
			slot = &videoOnly
		}
		if *slot == nil || preferCopy(f, **slot) {
			*slot = &formats[i]
		}
	}
	if videoOnly != nil {
		var audio *FormatEntry
		own := false
		if a := v.companionAudio(); a != "" {
			audio = bestAudio(formats, a)
			own = audio != nil
		}
		if audio == nil {
			audio = bestAudio(formats, "")
		}
		if audio != nil {
			ext := "mkv"
			if own && v.keepsContainer(embedThumbnail) {
				ext = v.ext
			}
			return ext, mergedSize(videoOnly, audio)
		}
	}
	if combined != nil {
		return combined.Ext, combined.Size()
	}
	return "", 0
}

// isVideoOnly reports a format that needs audio merged in: yt-dlp's
// bestvideo, which a format with an unreported acodec is not.
func isVideoOnly(f FormatEntry) bool { return carries(f.Vcodec) && f.Acodec == "none" }

// carriesBoth reports a format yt-dlp's best takes: one whose video and audio
// are not both known to be absent.
func carriesBoth(f FormatEntry) bool { return f.Vcodec != "none" && f.Acodec != "none" }

// bestVideo is the video format yt-dlp's sort puts first among those that
// want accepts and whose resolution is at most capRes (0 for no cap): the
// highest resolution, then the highest frame rate, then the codec it prefers.
func bestVideo(formats []FormatEntry, capRes int, want func(FormatEntry) bool) *FormatEntry {
	var best *FormatEntry
	for i, f := range formats {
		if f.Vcodec == "none" || !want(f) || (capRes > 0 && f.Res() > capRes) {
			continue
		}
		if best == nil || preferVideo(f, *best) {
			best = &formats[i]
		}
	}
	return best
}

func preferVideo(a, b FormatEntry) bool {
	if ra, rb := a.Res(), b.Res(); ra != rb {
		return ra > rb
	}
	if ra, rb := math.Round(a.FPS), math.Round(b.FPS); ra != rb {
		return ra > rb
	}
	if ca, cb := videoCodecPreference(a.Vcodec), videoCodecPreference(b.Vcodec); ca != cb {
		return ca < cb
	}
	return preferCopy(a, b)
}

func videoCodecPreference(vcodec string) int {
	if f, ok := familyOf(vcodec, videoFamilies); ok {
		for i, name := range preferredVideoCodecs {
			if f.name == name {
				return i
			}
		}
	}
	return len(preferredVideoCodecs)
}

// bestAudio is the audio-only format the audio half of a merge takes, limited
// to one container when ext is set: yt-dlp's own default choice where the
// probe marked one, else the highest bitrate.
func bestAudio(formats []FormatEntry, ext string) *FormatEntry {
	var best *FormatEntry
	for i, f := range formats {
		if carries(f.Vcodec) || !carries(f.Acodec) || (ext != "" && f.Ext != ext) {
			continue
		}
		if best == nil || preferAudio(f, *best) {
			best = &formats[i]
		}
	}
	return best
}

func preferAudio(a, b FormatEntry) bool {
	if a.Default != b.Default {
		return a.Default
	}
	if a.Abr != b.Abr {
		return a.Abr > b.Abr
	}
	return preferCopy(a, b)
}

// preferCopy chooses between two copies of what the picker shows as one
// track: a direct download over a streaming manifest, as yt-dlp does, then an
// exact size over an estimate, then the larger file.
func preferCopy(a, b FormatEntry) bool {
	if ma, mb := isManifest(a.Protocol), isManifest(b.Protocol); ma != mb {
		return !ma
	}
	if ea, eb := a.Filesize > 0, b.Filesize > 0; ea != eb {
		return ea
	}
	return a.Size() > b.Size()
}

func isManifest(protocol string) bool {
	return strings.HasPrefix(protocol, "m3u8") || strings.Contains(protocol, "dash") ||
		protocol == "f4m" || protocol == "ism"
}

// mergedSize is the size of a video-only track and the audio merged with it,
// 0 when the video's own is unknown, since the audio alone would read as a
// complete answer.
func mergedSize(video, audio *FormatEntry) int64 {
	if video.Size() == 0 {
		return 0
	}
	return video.Size() + audio.Size()
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
// is left out, since the bitrate is what tells it apart in the picker.
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

// selector matches the track's codec, ignoring case as videoContainer.filter
// does, and its bitrate as the key rounded it, with the language filter first
// when one is set.
func (a audioTrack) selector(lang string) string {
	f := fmt.Sprintf("[acodec~='(?i)%s'][abr>=%s][abr<%s]", a.codec.match,
		strconv.FormatFloat(float64(a.kbps)-0.5, 'f', 1, 64),
		strconv.FormatFloat(float64(a.kbps)+0.5, 'f', 1, 64))
	if lang == "" {
		return "ba" + f
	}
	return "ba[language^=" + lang + "]" + f + "/ba" + f
}

// audioFamilySelector asks for the best track of one codec family, then for
// the best track of any, so a format the source lacks is converted to rather
// than failing.
func audioFamilySelector(fam codecFamily, lang string) string {
	f := fmt.Sprintf("[acodec~='(?i)%s']", fam.match)
	if lang == "" {
		return "ba" + f + "/" + audioSelector("")
	}
	return "ba[language^=" + lang + "]" + f + "/ba" + f + "/" + audioSelector(lang)
}

// listedAudioFamily reports whether f is one of the families the format picker
// names. A track of another codec is still downloaded by best.
func listedAudioFamily(f codecFamily) bool {
	return familyRank(f, audioFamilies) < len(audioFamilies)
}

// AudioTracks lists the distinct audio-only tracks in formats, highest
// bitrate first, as the keys the audio row stores.
func AudioTracks(formats []FormatEntry) []string {
	seen := map[string]bool{}
	var found []audioTrack
	for _, f := range formats {
		a, ok := audioTrackOf(f)
		if !ok || !listedAudioFamily(a.codec) || seen[a.key()] {
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

// AudioFormatsOf lists the audio formats a source carries as the audio row's
// format picker offers them: "best", then each codec family with an audio-only
// track of its own, in the family table's order.
func AudioFormatsOf(formats []FormatEntry) []string {
	present := map[string]bool{}
	for _, f := range formats {
		if carries(f.Vcodec) {
			continue
		}
		if fam, ok := familyOf(f.Acodec, audioFamilies); ok {
			present[fam.name] = true
		}
	}
	out := []string{"best"}
	for _, fam := range audioFamilies {
		if present[fam.name] {
			out = append(out, fam.name)
		}
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

// AudioFormatExt is the extension an audio row asking for format is written
// with, whether -x copies a track in it or converts to it. "" for best, which
// depends on the source.
func AudioFormatExt(format string) string {
	switch format = foldAudioFormat(format); format {
	case "", "best":
		return ""
	case "wav":
		return "wav"
	}
	return familyNamed(format, audioFamilies).ext
}

// foldAudioFormat reads "aac" as "m4a". yt-dlp writes both into an .m4a file,
// the raw stream only where asked for "aac", so the two are one choice.
func foldAudioFormat(format string) string {
	if format == "aac" {
		return "m4a"
	}
	return format
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

// ResolveAudioPick turns a preset's format and bitrate into the track of that
// format whose bitrate is nearest, the higher one on a tie. It returns the pick
// and the bitrate the row keeps: none once a track is named, since the track
// carries its own. A format the source has no track of stays a conversion, and
// any other pick comes back as it is.
func ResolveAudioPick(pick, bitrate string, formats []FormatEntry) (string, string) {
	pick = foldAudioFormat(pick)
	want, err := strconv.Atoi(bitrate)
	if pick == "" || pick == "best" || IsAudioTrack(pick) || err != nil {
		return pick, bitrate
	}
	var best audioTrack
	found := false
	for _, f := range formats {
		a, ok := audioTrackOf(f)
		if !ok || a.codec.name != pick {
			continue
		}
		if !found || nearer(a.kbps, best.kbps, want) {
			best, found = a, true
		}
	}
	if !found {
		return pick, bitrate
	}
	return best.key(), ""
}

func nearer(a, b, want int) bool {
	da, db := a-want, b-want
	if da < 0 {
		da = -da
	}
	if db < 0 {
		db = -db
	}
	return da < db || (da == db && a > b)
}

// AudioFile predicts what an audio row's pick downloads from formats: the
// extension and the size of the track -x copies. A conversion has a known
// extension and no size, since the encoder decides it.
func AudioFile(pick string, formats []FormatEntry) (ext string, size int64) {
	pick = foldAudioFormat(pick)
	if t, ok := parseAudioTrack(pick); ok {
		var best *FormatEntry
		for i, f := range formats {
			if a, ok := audioTrackOf(f); ok && a.key() == t.key() && (best == nil || preferCopy(f, *best)) {
				best = &formats[i]
			}
		}
		if best == nil {
			return t.codec.ext, 0
		}
		return ExtractedExt(*best), best.Size()
	}
	if pick != "" && pick != "best" {
		fam := familyNamed(pick, audioFamilies)
		var best *FormatEntry
		for i, f := range formats {
			if carries(f.Vcodec) {
				continue
			}
			if c, ok := familyOf(f.Acodec, audioFamilies); ok && c.name == fam.name && (best == nil || preferAudio(f, *best)) {
				best = &formats[i]
			}
		}
		if best == nil {
			return AudioFormatExt(pick), 0
		}
		return AudioFormatExt(pick), best.Size()
	}
	if a := bestAudio(formats, ""); a != nil {
		return ExtractedExt(*a), a.Size()
	}
	return "", 0
}

// Res is the resolution a quality such as 1080p names, as yt-dlp's format
// sort reads it: the smaller of width and height, so a portrait 1080x1920
// track is 1080p like a landscape 1920x1080 one. A format that reports no
// width is taken by its height.
func (f FormatEntry) Res() int {
	if f.Width > 0 && f.Width < f.Height {
		return f.Width
	}
	return f.Height
}

// Size is a format's best known byte count: exact when the host reports one,
// an estimate otherwise (see ProbeTitle), 0 when neither is known.
func (f FormatEntry) Size() int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}
