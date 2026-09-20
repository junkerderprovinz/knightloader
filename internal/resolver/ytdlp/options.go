package ytdlp

import (
	"strconv"
	"strings"
)

// Options is the user-configurable part of a yt-dlp invocation: the variant a
// task downloads, format selection, subtitles, output name and playlist
// handling. Backend.Options supplies it per task, so this package does not
// import internal/settings.
//
// The zero value reproduces the plain behaviour: no -f, no subtitle flags,
// %(title)s.%(ext)s and --no-playlist.
type Options struct {
	// Variant is which of the source's forms this task downloads; the zero
	// value is VariantVideo.
	Variant      Variant `json:"variant"`
	Quality      Quality `json:"quality"`
	CustomFormat string  `json:"customFormat"`
	// AudioFormat is the --audio-format value ("mp3", "m4a", "opus", or
	// "best" for no transcode), read only for VariantAudio.
	AudioFormat string `json:"audioFormat"`
	// AudioBitrate is the --audio-quality target in kbit/s ("192"), read only
	// for VariantAudio. Empty passes no flag; it has no effect on a "best"
	// extract, which copies the source stream.
	AudioBitrate string `json:"audioBitrate"`
	// SubtitleLangs is the --sub-langs value (codes, a pattern like "en.*",
	// or "all"); empty means DefaultSubtitleLangs. The subtitle row and
	// Embed.Subs on the video row share it, so both ask for the same
	// languages.
	SubtitleLangs string `json:"subtitleLangs"`
	// SubtitleAuto adds --write-auto-subs, so a site without a manual track
	// still yields auto-generated captions.
	SubtitleAuto bool `json:"subtitleAuto"`
	// SubtitleStrict fails a subtitle task that wrote no subtitle file. With
	// --no-warnings, yt-dlp's notice about a missing language is hidden and
	// the task would otherwise finish green over an empty folder.
	// AvailableSubtitleLangs keeps such languages off the menu; this covers
	// free text and a source that changed after the probe. It is off by
	// default so upgrades do not start failing existing tasks.
	SubtitleStrict bool `json:"subtitleStrict"`
	// AudioLang narrows the audio row to one spoken language through a
	// [language^=xx] filter in -f. Auto-dubbed YouTube videos offer several
	// tracks, and bestaudio does not reliably pick the original (see
	// AvailableAudioLangs). Empty applies no filter.
	AudioLang string `json:"audioLang"`
	// Playlist drops --no-playlist, so a playlist URL fetches every entry.
	Playlist bool `json:"playlist"`
	// OutputTemplate is yt-dlp's -o template, joined onto the task's
	// destination directory. Empty uses defaultOutputTemplate.
	OutputTemplate string `json:"outputTemplate"`
	// Cookies hands yt-dlp the cookies.txt stored for the download's host
	// (see cookies.go) as a 0600 temp file removed when the process exits. It
	// is a switch because rate limits and bans then land on the account.
	Cookies bool `json:"cookies"`
	// Music turns the audio row into a tagger: artist, album, title and track
	// metadata, an Artist/Album/NN-Title template and a cover.jpg (see
	// musicArgs). Libraries like Navidrome and Plex group by tags, not file
	// names.
	Music bool `json:"music"`
	// Embed is what gets written into the finished file.
	Embed Embed `json:"embed"`
	// Measure re-reads the finished file with ffprobe.
	Measure Measure `json:"measure"`
	// Live bounds a livestream recording.
	Live Live `json:"live"`
}

// Embed is what gets written into the finished file, or for the NFO next to
// it. It applies to the video and audio rows only; the thumbnail, subtitle and
// description rows are the separate-file form of the same wish.
type Embed struct {
	// Metadata is --embed-metadata: title, uploader, date and description in
	// the container's tags, which Jellyfin and Plex read without an NFO.
	Metadata bool `json:"metadata"`
	// Thumbnail is --embed-thumbnail: the cover as an attached picture.
	Thumbnail bool `json:"thumbnail"`
	// Chapters is --embed-chapters: the source's chapter marks as container
	// chapters.
	Chapters bool `json:"chapters"`
	// Subs is --embed-subs: subtitle tracks muxed into the video, in the
	// SubtitleLangs languages. Video row only.
	Subs bool `json:"subs"`
	// SplitChapters is --split-chapters: one extra file per chapter beside
	// the complete one.
	SplitChapters bool `json:"splitChapters"`
	// NFO writes a Kodi/Jellyfin/Plex <movie> NFO next to the finished file,
	// built from yt-dlp's info json (see nfo.go).
	NFO bool `json:"nfo"`
}

// Measure re-reads the finished file with ffprobe, records runtime,
// resolution, codecs and audio tracks on the task, and warns when the runtime
// falls well short of the announced one. It catches a merge or fragmented
// download that died early while yt-dlp still exited 0.
type Measure struct {
	Enabled bool `json:"enabled"`
	// ShortPercent is the share of the announced runtime, in percent, below
	// which a file counts as short. Sanitize maps anything outside 1..100,
	// including 0, to DefaultShortPercent.
	ShortPercent int `json:"shortPercent"`
	// FailOnShort turns a short file into an error instead of a finished
	// download with a warning.
	FailOnShort bool `json:"failOnShort"`
}

// Live bounds a livestream recording, which otherwise never ends and shows a
// progress bar toward a total nobody knows.
type Live struct {
	// Enabled also gates detection: only then does the progress template
	// carry is_live (see liveProgressTemplate), so with it off the progress
	// format is unchanged.
	Enabled bool `json:"enabled"`
	// FromStart is --live-from-start: record from the stream's beginning.
	// yt-dlp supports it on YouTube only and ignores it for non-live media.
	FromStart bool `json:"fromStart"`
	// MaxMinutes stops the recording after this many minutes, 0 for no
	// limit. A duration rather than a time of day, so the limit means the
	// same whenever the link is pasted; time-of-day rules belong to
	// internal/schedule.
	MaxMinutes int `json:"maxMinutes"`
	// MaxMB stops the recording once this many mebibytes have arrived, 0 for
	// no limit. It counts yt-dlp's downloaded_bytes, since the temp fragments
	// on disk do not add up to that number.
	MaxMB int `json:"maxMB"`
}

// DefaultShortPercent is the share of the announced runtime a file must reach.
// Real downloads land slightly short (rounded durations, dropped trailing
// silence, a late live start), while a broken merge is off by half.
const DefaultShortPercent = 90

// Variant is which piece of a yt-dlp resource one task downloads. Like JD's
// "Variante" rows, one link yields video, audio, thumbnail, subtitles and
// description as separate, independently kept tasks.
type Variant string

const (
	// VariantVideo is the zero value, so a task without a variant downloads
	// the video.
	VariantVideo       Variant = "video"
	VariantAudio       Variant = "audio"
	VariantThumbnail   Variant = "thumbnail"
	VariantSubtitle    Variant = "subtitle"
	VariantDescription Variant = "description"
)

// Variants lists every variant in menu order, the order internal/app expands
// them in.
func Variants() []Variant {
	return []Variant{VariantVideo, VariantAudio, VariantThumbnail, VariantSubtitle, VariantDescription}
}

func validVariant(v Variant) bool {
	for _, x := range Variants() {
		if x == v {
			return true
		}
	}
	return false
}

// AudioFormats lists the --audio-format values offered on the audio row, in
// menu order. "best" keeps the source's codec without transcoding and is the
// default. AvailableAudioFormats narrows the list to what a probed source
// carries.
func AudioFormats() []string {
	return []string{"best", "aac", "alac", "flac", "m4a", "mp3", "opus", "vorbis", "wav"}
}

func validAudioFormat(f string) bool {
	for _, x := range AudioFormats() {
		if x == f {
			return true
		}
	}
	return false
}

// AudioBitrates lists the --audio-quality targets offered on the audio row, in
// menu order. "" leaves yt-dlp's own default.
func AudioBitrates() []string {
	return []string{"", "64", "96", "128", "160", "192", "256", "320"}
}

func validAudioBitrate(b string) bool {
	for _, x := range AudioBitrates() {
		if x == b {
			return true
		}
	}
	return false
}

// AvailableAudioBitrates drops bitrates above the best audio track the source
// carries, since those only promise more than the source has. maxAbr <= 0
// (nothing probed) returns every bitrate; "" is always kept.
func AvailableAudioBitrates(maxAbr float64) []string {
	all := AudioBitrates()
	if maxAbr <= 0 {
		return all
	}
	out := make([]string, 0, len(all))
	for _, b := range all {
		if b == "" {
			out = append(out, b)
			continue
		}
		n, err := strconv.Atoi(b)
		if err == nil && float64(n) <= maxAbr {
			out = append(out, b)
		}
	}
	return out
}

// audioFormatsForCodec maps a yt-dlp audio codec id (FormatEntry.Acodec, e.g.
// "opus", "mp4a.40.2") onto the AudioFormats entries that keep it without
// re-encoding. AAC is both "m4a" (its container) and "aac" (the raw stream).
// An unknown codec yields nil.
func audioFormatsForCodec(acodec string) []string {
	switch {
	case strings.HasPrefix(acodec, "mp4a"), strings.HasPrefix(acodec, "aac"):
		return []string{"m4a", "aac"}
	case strings.HasPrefix(acodec, "opus"):
		return []string{"opus"}
	case strings.HasPrefix(acodec, "mp3"):
		return []string{"mp3"}
	case strings.HasPrefix(acodec, "flac"):
		return []string{"flac"}
	case strings.HasPrefix(acodec, "vorbis"):
		return []string{"vorbis"}
	case strings.HasPrefix(acodec, "alac"):
		return []string{"alac"}
	default:
		return nil
	}
}

// AvailableAudioFormats is the subset of AudioFormats matching the codecs a
// source's audio tracks report, in menu order, with "best" always kept.
// codecs may repeat and come in any order.
func AvailableAudioFormats(codecs []string) []string {
	present := make(map[string]bool, len(codecs))
	for _, c := range codecs {
		for _, f := range audioFormatsForCodec(c) {
			present[f] = true
		}
	}
	out := make([]string, 0, len(present)+1)
	for _, f := range AudioFormats() {
		if f == "best" || present[f] {
			out = append(out, f)
		}
	}
	return out
}

// HosterPreset is what a person configures once per site (a host string such
// as "youtube.com"): which variants are staged by default, and the default
// quality and audio format.
type HosterPreset struct {
	// Variants lists which of Variants() are staged enabled by default.
	Variants    []Variant `json:"variants"`
	Quality     Quality   `json:"quality"`
	AudioFormat string    `json:"audioFormat"`
}

// DefaultHosterPreset enables all variants for an unconfigured host, as JD
// shows every row it finds; staging only the video would hide that the other
// rows exist.
func DefaultHosterPreset() HosterPreset {
	return HosterPreset{Variants: Variants(), Quality: QualityBest, AudioFormat: "best"}
}

// Sanitize repairs a HosterPreset like Options.Sanitize does, without ever
// failing.
func (p HosterPreset) Sanitize() HosterPreset {
	kept := make([]Variant, 0, len(p.Variants))
	seen := map[Variant]bool{}
	for _, v := range p.Variants {
		if validVariant(v) && !seen[v] {
			kept = append(kept, v)
			seen[v] = true
		}
	}
	p.Variants = kept
	if !validQuality(p.Quality) {
		p.Quality = QualityBest
	}
	if !validAudioFormat(p.AudioFormat) {
		p.AudioFormat = "best"
	}
	return p
}

// HasVariant reports whether v is one of this preset's enabled variants.
func (p HosterPreset) HasVariant(v Variant) bool {
	for _, x := range p.Variants {
		if x == v {
			return true
		}
	}
	return false
}

// Quality is a format-selector preset offered on the settings page.
type Quality string

const (
	// QualityBest passes no -f, leaving yt-dlp's own selection.
	QualityBest Quality = "best"
	// The ladder covers every height the supported sites publish;
	// AvailableQualities trims what a source lacks.
	Quality4320p Quality = "4320p"
	Quality2160p Quality = "2160p"
	Quality1440p Quality = "1440p"
	Quality1080p Quality = "1080p"
	Quality720p  Quality = "720p"
	Quality480p  Quality = "480p"
	Quality360p  Quality = "360p"
	Quality240p  Quality = "240p"
	Quality144p  Quality = "144p"
	// QualityAudioOnly is no longer offered; the audio variant replaced it.
	// Sanitize folds a stored value onto QualityBest.
	QualityAudioOnly Quality = "audioOnly"
	// QualityCustom hands CustomFormat to -f verbatim; a value yt-dlp
	// rejects fails like any other yt-dlp error.
	QualityCustom Quality = "custom"
)

// Qualities lists every offered quality in menu order; the menu and the
// validity check share it.
func Qualities() []Quality {
	return []Quality{
		QualityBest, Quality4320p, Quality2160p, Quality1440p, Quality1080p,
		Quality720p, Quality480p, Quality360p, Quality240p, Quality144p, QualityCustom,
	}
}

func validQuality(q Quality) bool {
	for _, x := range Qualities() {
		if x == q {
			return true
		}
	}
	return false
}

// heightCaps maps a resolution preset onto yt-dlp's filter syntax. "<=?"
// keeps formats with an unknown height in the running, where "<=" would drop
// them.
var heightCaps = map[Quality]string{
	Quality4320p: "4320", Quality2160p: "2160", Quality1440p: "1440", Quality1080p: "1080",
	Quality720p: "720", Quality480p: "480", Quality360p: "360", Quality240p: "240", Quality144p: "144",
}

// HeightCap returns the pixel height a preset caps at, or false for presets
// without one (QualityBest, QualityCustom).
func HeightCap(q Quality) (int, bool) {
	h, ok := heightCaps[q]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(h)
	if err != nil {
		return 0, false
	}
	return n, true
}

// AvailableQualities drops the presets above the tallest video stream a
// source offers. Those would still work, since "<=?" falls back to the best
// available height, but they only duplicate the top entry. maxHeight <= 0
// (nothing probed) returns every quality. QualityBest and QualityCustom are
// always kept.
func AvailableQualities(maxHeight int) []Quality {
	all := Qualities()
	if maxHeight <= 0 {
		return all
	}
	out := make([]Quality, 0, len(all))
	for _, q := range all {
		if q == QualityBest || q == QualityCustom {
			out = append(out, q)
			continue
		}
		if capH, ok := HeightCap(q); ok && capH <= maxHeight {
			out = append(out, q)
		}
	}
	return out
}

// DefaultSubtitleLangs is the --sub-langs value when no languages are set.
const DefaultSubtitleLangs = "en"

// defaultOutputTemplate is the -o template when none is set.
const defaultOutputTemplate = "%(title)s.%(ext)s"

// maxFieldLen bounds the free-text fields against a pathological paste. It is
// a sanity limit, not a security boundary.
const maxFieldLen = 512

// Defaults returns the settings of a fresh install.
func Defaults() Options {
	return Options{}
}

// Sanitize repairs invalid fields instead of refusing them, so one bad field
// never fails a settings save.
func (o Options) Sanitize() Options {
	if !validVariant(o.Variant) {
		o.Variant = VariantVideo
	}
	if !validQuality(o.Quality) {
		o.Quality = QualityBest
	}
	if !validAudioFormat(o.AudioFormat) {
		o.AudioFormat = "best"
	}
	if !validAudioBitrate(o.AudioBitrate) {
		o.AudioBitrate = ""
	}
	o.CustomFormat = clip(strings.TrimSpace(o.CustomFormat))
	o.SubtitleLangs = clip(strings.TrimSpace(o.SubtitleLangs))
	o.AudioLang = sanitizeLangCode(o.AudioLang)
	o.OutputTemplate = sanitizeTemplate(o.OutputTemplate)
	o.Measure = o.Measure.sanitize()
	o.Live = o.Live.sanitize()
	return o
}

// sanitize maps an out-of-range ShortPercent onto the default rather than the
// nearest edge, since 500 is a typo and not a request that nothing ever pass.
func (m Measure) sanitize() Measure {
	if m.ShortPercent < 1 || m.ShortPercent > 100 {
		m.ShortPercent = DefaultShortPercent
	}
	return m
}

// sanitize floors Live's caps at zero (no limit). A negative cap would stop a
// recording on its first progress line.
func (l Live) sanitize() Live {
	if l.MaxMinutes < 0 {
		l.MaxMinutes = 0
	}
	if l.MaxMB < 0 {
		l.MaxMB = 0
	}
	return l
}

// sanitizeLangCode keeps only letters, digits and hyphens, which covers BCP 47
// tags like "de", "pt-BR" and "zh-Hans". Anything else, such as a bracket or
// comma, would break the [language^=...] format filter and fail the whole
// yt-dlp call.
func sanitizeLangCode(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	return clip(b.String())
}

func clip(s string) string {
	if len(s) > maxFieldLen {
		return s[:maxFieldLen]
	}
	return s
}

// sanitizeTemplate removes ".." components and leading separators, which
// would let the template escape the task's directory once filepath.Join
// cleans it. Subfolders and the rest of yt-dlp's template syntax are left
// alone.
func sanitizeTemplate(s string) string {
	s = clip(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' })
	kept := parts[:0]
	for _, p := range parts {
		if p == ".." || p == "" {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "/")
}
