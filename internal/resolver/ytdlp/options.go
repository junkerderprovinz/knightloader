package ytdlp

import (
	"strconv"
	"strings"
)

// Options is the user-configurable half of a yt-dlp invocation: which
// variant of the source this one task downloads, format selection,
// subtitles, the output filename, and whether a playlist URL fetches one
// video or the whole list - everything backend.go used to bake straight
// into args with no field anywhere to change it (see
// docs/jd-feature-census.md's "(per-plugin option list)" and "Variante"
// rows: this resolver is the only one with anything real to configure).
//
// It is this package's own type, read through Backend.Options - a closure,
// the same shape RateLimit and Dir already use - rather than an import of
// internal/settings here, so the resolver stays decoupled from the settings
// package the way every backend in this tree does.
//
// Every field's zero value reproduces exactly what run() did before any of
// them existed: no -f, no subtitle flags, the hardcoded %(title)s.%(ext)s
// name, --no-playlist. An install that never opens the settings page this
// wires into downloads exactly as it always has.
type Options struct {
	// Variant is which of the resource's own forms THIS task downloads -
	// see Variant's own doc comment. The zero value is VariantVideo, so an
	// Options built before this field existed (or a task with no variant
	// recorded on it) downloads exactly what it always did.
	Variant      Variant `json:"variant"`
	Quality      Quality `json:"quality"`
	CustomFormat string  `json:"customFormat"`
	// AudioFormat is yt-dlp's own --audio-format value (e.g. "mp3", "m4a",
	// "opus", or "best" for whatever the source itself already is) - read
	// only when Variant is VariantAudio.
	AudioFormat string `json:"audioFormat"`
	// AudioBitrate is yt-dlp's own --audio-quality target (e.g. "192" for
	// 192 kbit/s), read only when Variant is VariantAudio. Empty passes no
	// flag at all, leaving ffmpeg's own default bitrate for whatever
	// AudioFormat asks it to encode - meaningful only once AudioFormat names
	// an actual transcode target; a "best" extract copies the source's own
	// audio stream and has no bitrate of its own to retarget.
	AudioBitrate string `json:"audioBitrate"`
	// SubtitleLangs is yt-dlp's own --sub-langs value (comma-separated
	// codes, or a pattern like "en.*"; "all" is also yt-dlp's own keyword).
	// Empty falls back to DefaultSubtitleLangs. Read only when Variant is
	// VariantSubtitle - whether a subtitle row exists at all is that row's
	// own Enabled switch now, not a mode on this struct (a leftover
	// SubtitleMode on/off/embed field lived here before the "Variante" row
	// redesign and was removed with it: an "embed into the video" mode
	// cannot mean anything once fetching subtitles and fetching video are
	// two entirely separate tasks/invocations, and "off" duplicated what
	// the row's own Enabled already says).
	SubtitleLangs string `json:"subtitleLangs"`
	// SubtitleAuto adds --write-auto-subs, so a site with no manually
	// authored track still yields one from its auto-generated captions.
	SubtitleAuto bool `json:"subtitleAuto"`
	// Playlist, when true, drops --no-playlist: a playlist URL fetches
	// every entry instead of only the one the link happened to point at.
	Playlist bool `json:"playlist"`
	// OutputTemplate is yt-dlp's own -o template syntax, joined onto the
	// task's destination directory. Empty uses defaultOutputTemplate.
	OutputTemplate string `json:"outputTemplate"`
}

// Variant is which piece of a yt-dlp-resolved resource one task downloads -
// core.Task's own Variant field, in this package's own vocabulary. JD calls
// the same idea "Variante": pasting one YouTube link there lists a video
// track, an audio-only extraction, a thumbnail image, subtitles and a plain
// description as separate rows, each independently keepable, and a person
// picks which ones they actually want rather than always getting the one
// video KnightLoader used to hand-wire (jdp, 2026-08-25, after a first,
// narrower attempt at this same request: "ich glaub du hast nicht
// verstanden was ich mein... genau so soll es auch in KL sein" - a global
// on/off per file TYPE was not it; five independently keepable rows per
// link, the way JD shows them, was always the ask).
type Variant string

const (
	// VariantVideo is the zero value - a task with none recorded (every
	// task created before this field existed) downloads exactly what it
	// always did.
	VariantVideo       Variant = "video"
	VariantAudio       Variant = "audio"
	VariantThumbnail   Variant = "thumbnail"
	VariantSubtitle    Variant = "subtitle"
	VariantDescription Variant = "description"
)

// Variants lists every variant this build can offer, in the order JD's own
// list shows them and internal/app's variant-expansion iterates them.
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

// AudioFormats lists every --audio-format value this build offers on the
// audio variant's own quality picker, in menu order. "best" is yt-dlp's own
// keyword for "whatever the source's own audio codec already is, do not
// transcode" - the zero value, so an Options with no opinion re-encodes
// nothing.
func AudioFormats() []string {
	return []string{"best", "mp3", "m4a", "opus", "wav", "flac"}
}

func validAudioFormat(f string) bool {
	for _, x := range AudioFormats() {
		if x == f {
			return true
		}
	}
	return false
}

// AudioBitrates lists every --audio-quality target this build offers on the
// audio variant's own bitrate picker, in menu order. "" is "no opinion" -
// ffmpeg's own default (yt-dlp's own default is 5, an ~128kbit/s-equivalent
// VBR setting) - the zero value, so an Options with no bitrate chosen
// behaves exactly as it always did before this field existed.
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

// AvailableAudioBitrates is AvailableQualities' own reasoning, applied to
// the audio row's bitrate picker instead (jdp, 2026-08-26: "alle formate
// immer auf hosterangebot begrenzen. auch die audioqualitäten! bei allen
// hostern!"): a bitrate above what the source's own best audio track
// actually carries is not wrong to pick - --audio-quality only sets a
// ceiling ffmpeg encodes up to, never a guarantee of that many real bits -
// but it is a menu entry that promises more than the source has to give,
// the same discoverability wart AvailableQualities was already trimmed
// for. maxAbr <= 0 (nothing probed yet, or no audio track reported one)
// returns every bitrate unfiltered; "" (Auto) is always kept, since it
// names no specific target to compare against.
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

// audioFormatForCodec maps one of yt-dlp's own reported audio codec ids
// (FormatEntry.Acodec, e.g. "opus", "mp4a.40.2") onto the matching entry in
// AudioFormats()'s own menu - the source's own NATIVE container, not a
// generic transcode target ffmpeg could also reach from it (see
// AvailableAudioFormats' own doc comment for why the distinction matters).
// "" means the codec has no native match on that menu at all (vorbis, ac-3,
// eac3, alac...) - not an error, simply nothing to offer for it.
func audioFormatForCodec(acodec string) string {
	switch {
	case strings.HasPrefix(acodec, "mp4a"):
		return "m4a"
	case strings.HasPrefix(acodec, "opus"):
		return "opus"
	case strings.HasPrefix(acodec, "mp3"):
		return "mp3"
	case strings.HasPrefix(acodec, "flac"):
		return "flac"
	default:
		return ""
	}
}

// AvailableAudioFormats is the subset of AudioFormats() worth offering for a
// source whose own audio-only tracks report codecs - built from the DISTINCT
// native formats acodecs maps onto, in AudioFormats()'s own menu order, with
// "best" always kept (it names no specific codec to compare against). A
// codec audioFormatForCodec does not recognise contributes nothing rather
// than a guess. codecs may repeat or come in any order - every source track
// naturally offers more than one bitrate of the same codec.
func AvailableAudioFormats(codecs []string) []string {
	present := make(map[string]bool, len(codecs))
	for _, c := range codecs {
		if f := audioFormatForCodec(c); f != "" {
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

// HosterPreset is what a person configures once per site (a host string,
// e.g. "youtube.com") for every future link from it: which of the five
// variants land in the collector by default, and - for the two with a
// quality choice - the default they land with. Reached from the
// collector's own package row (a gear badge, jdp 2026-08-25: "auf dem
// link-ordner soll ein zahnrad-badge sein der mich zu den voreinstellungen
// des Hosters führt wo ich einstellen kann welche variante es
// standardmäßig in den sammler packen soll und wo ich die einzelnen
// formata an und abhaken kann").
type HosterPreset struct {
	// Variants lists which of Variants() are staged enabled by default. A
	// host with no preset saved yet gets DefaultHosterPreset's own answer
	// (every one on) - see that function's own doc comment for why.
	Variants    []Variant `json:"variants"`
	Quality     Quality   `json:"quality"`
	AudioFormat string    `json:"audioFormat"`
}

// DefaultHosterPreset is what a host nobody has configured gets: all five
// variants enabled. JD's own list shows every row it found and leaves
// unticking any of them to the person looking at it - starting from
// "nothing" here would mean a first-ever YouTube paste quietly stages only
// a lone video task with no visible sign four more rows were even
// possible, which is a worse first impression than five rows to glance at
// and switch off.
func DefaultHosterPreset() HosterPreset {
	return HosterPreset{Variants: Variants(), Quality: QualityBest, AudioFormat: "best"}
}

// Sanitize repairs a HosterPreset the same way Options.Sanitize does -
// always succeeds, never refuses a whole settings save over one bad field.
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

// HasVariant reports whether v is one of this preset's own enabled
// variants.
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
	// QualityBest is the zero value: no -f at all, i.e. yt-dlp's own
	// default selection - named so the page has something to show selected
	// on a fresh install, not so this package ever compares against it
	// before Sanitize has run.
	QualityBest  Quality = "best"
	Quality2160p Quality = "2160p"
	Quality1440p Quality = "1440p"
	Quality1080p Quality = "1080p"
	Quality720p  Quality = "720p"
	Quality480p  Quality = "480p"
	Quality360p  Quality = "360p"
	// QualityAudioOnly is no longer offered on Qualities()'s own menu -
	// superseded by the dedicated VariantAudio row (jdp, 2026-08-25: five
	// independently keepable rows per link, JD-style, not a video-quality
	// preset standing in for "no video at all"). The constant stays
	// declared only so old code/tests that still name it compile; an
	// install with one already saved on its video quality is folded onto
	// QualityBest by Sanitize the same as any other value Qualities() no
	// longer lists - the right way to get audio-only now is enabling the
	// Audio row and leaving Video off, not a video-quality preset that
	// secretly downloads no video at all.
	QualityAudioOnly Quality = "audioOnly"
	// QualityCustom hands CustomFormat to -f verbatim, unexamined - yt-dlp's
	// own format-selector grammar is not reimplemented here, and a value it
	// rejects surfaces through run()'s existing stderr-tail error path
	// exactly like any other yt-dlp failure.
	QualityCustom Quality = "custom"
)

// Qualities lists every quality this build can offer, in menu order - the
// same "the menu and the validity check read one list" shape
// internal/idleaction.Actions already uses for Config.Action. See
// QualityAudioOnly's own doc comment for why it is declared above but not
// listed here.
func Qualities() []Quality {
	return []Quality{
		QualityBest, Quality2160p, Quality1440p, Quality1080p,
		Quality720p, Quality480p, Quality360p, QualityCustom,
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

// heightCaps maps a resolution preset onto yt-dlp's own filter syntax. The
// `?` in `<=?` matters: a plain `<=` excludes any format whose height yt-dlp
// could not read, which real-world formats sometimes lack, while `<=?`
// keeps it in the running - see yt-dlp's own format-selection docs on
// "eager" comparisons.
var heightCaps = map[Quality]string{
	Quality2160p: "2160", Quality1440p: "1440", Quality1080p: "1080",
	Quality720p: "720", Quality480p: "480", Quality360p: "360",
}

// HeightCap is heightCaps's own mapping, parsed and exported - what a probed
// source's own available heights need comparing against (AvailableQualities
// below) and what a video row's own currently-picked Quality caps out at
// (a probe's best-effort Size estimate, app_ytdlp_variants.go). false for
// QualityBest/QualityCustom/anything else with no fixed cap of its own.
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

// AvailableQualities is the subset of Qualities() worth offering for a
// source whose own real video streams top out at maxHeight (jdp,
// 2026-08-25: "man soll nur die varianten auswählen können die wirklich
// verfügbar sind" - an old or low-resolution source may genuinely not have
// a 1080p/4K stream at all). Anything above that ceiling is not wrong to
// pick - formatSelector's own "<=?H" selector already falls back to the
// best height actually available rather than erroring - it is only ever a
// menu entry that quietly resolves to the exact same thing the ceiling
// itself would have, which is a discoverability wart worth trimming, not a
// behaviour worth warning about. maxHeight <= 0 (nothing probed yet, or the
// probe found no real video track at all) returns every quality unfiltered
// - the same "no opinion yet" default every other optional signal in this
// feature already falls back to.
//
// QualityBest and QualityCustom are always kept: "best" has no ceiling of
// its own to compare against, and "custom" is a verbatim -f passthrough
// this function has no way to evaluate.
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

// DefaultSubtitleLangs is what --sub-langs gets when subtitles are switched
// on with no language list of their own - yt-dlp requires some value, and
// English is the one default this package can pick without guessing at a
// person's actual language.
const DefaultSubtitleLangs = "en"

// defaultOutputTemplate is exactly the literal backend.go hardcoded before
// OutputTemplate existed. Named once so buildArgs and this file's doc
// comments cannot drift apart.
const defaultOutputTemplate = "%(title)s.%(ext)s"

// maxFieldLen bounds the free-text fields against a pathological paste. Not
// a security boundary - exec.CommandContext never invokes a shell, so
// nothing here is an injection vector regardless of content (see
// buildArgs in backend.go) - just a sanity limit on what a settings
// document is allowed to carry.
const maxFieldLen = 512

// Defaults is what a fresh install has: the zero value, spelled out once
// rather than left implicit, matching every other settings sub-package in
// this tree exposing its own Defaults().
func Defaults() Options {
	return Options{}
}

// Sanitize repairs what a caller should never be refused a whole settings
// save over - the same contract internal/idleaction.Config.Sanitize
// documents: it always succeeds, because the one path that calls it
// (settings.sanitize) never fails an entire save because of one field.
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
	o.OutputTemplate = sanitizeTemplate(o.OutputTemplate)
	return o
}

func clip(s string) string {
	if len(s) > maxFieldLen {
		return s[:maxFieldLen]
	}
	return s
}

// sanitizeTemplate strips what would let a template escape the task's own
// destination directory once handed to filepath.Join in buildArgs: a ".."
// component resolves lexically there, so
// "%(title)s/../../../etc/passwd" and "/etc/passwd" join onto the very same
// absolute path once filepath.Join's Clean runs. Everything else yt-dlp's
// own template language accepts - subfolders via %(x)s/%(y)s, sorting into
// per-uploader or per-playlist folders - is left alone: this is a
// containment check, not a template-syntax validator.
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
