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
	// Empty falls back to DefaultSubtitleLangs. Read on the subtitle row -
	// whether that row exists at all is its own Enabled switch now, not a
	// mode on this struct (a leftover SubtitleMode on/off/embed field lived
	// here before the "Variante" row redesign and was removed with it: an
	// "embed into the video" mode cannot mean anything once fetching
	// subtitles and fetching video are two entirely separate tasks) - AND,
	// since Embed.Subs exists, on the video row too, where it says which
	// languages get muxed INTO the container. One list rather than a second
	// field spelling the same codes twice: a person who wants German
	// subtitles wants them in both places, and two lists that can disagree
	// is a support question waiting to happen.
	SubtitleLangs string `json:"subtitleLangs"`
	// SubtitleAuto adds --write-auto-subs, so a site with no manually
	// authored track still yields one from its auto-generated captions.
	// Read alongside SubtitleLangs in both of the places above.
	SubtitleAuto bool `json:"subtitleAuto"`
	// SubtitleStrict turns "yt-dlp wrote no subtitle file at all" from a
	// task that reports Done with nothing on disk into a real error naming
	// what was asked for.
	//
	// The silence is the bug: --no-warnings is in every invocation this
	// backend builds (buildArgs), and "There are no subtitles for the
	// requested languages" is a WARNING, so a subtitle row for a language
	// the source does not carry exits 0, prints nothing this backend reads,
	// and settles green over an empty folder. AvailableSubtitleLangs
	// (languages.go) is the half that stops that being pickable in the
	// first place; this is the backstop for the free-text field and for a
	// source whose offer changed between the probe and the download.
	//
	// Off by default, because turning it on retroactively fails tasks an
	// existing install has been settling green for months, and a settings
	// switch is the honest place to make that choice rather than a silent
	// behaviour change on upgrade.
	SubtitleStrict bool `json:"subtitleStrict"`
	// AudioLang narrows the audio row to ONE spoken language, as a
	// `[language^=xx]` filter on yt-dlp's own format selector rather than a
	// flag of its own (there is no --audio-lang; language is a format
	// property, so it belongs in -f).
	//
	// It exists because a YouTube video with auto-dubbing offers several
	// audio tracks and "bestaudio" picks whichever sorts highest, which is
	// not reliably the one that was actually spoken - see
	// AvailableAudioLangs (languages.go) for how the original is told apart
	// from the machine dubs. Empty is the zero value and passes no filter at
	// all, i.e. exactly what this row did before the field existed.
	AudioLang string `json:"audioLang"`
	// Playlist, when true, drops --no-playlist: a playlist URL fetches
	// every entry instead of only the one the link happened to point at.
	Playlist bool `json:"playlist"`
	// OutputTemplate is yt-dlp's own -o template syntax, joined onto the
	// task's destination directory. Empty uses defaultOutputTemplate.
	OutputTemplate string `json:"outputTemplate"`
	// Cookies lets a download use the cookies.txt stored for its host in the
	// encrypted credential store (cookies.go), handed to yt-dlp as a 0600
	// temp file that is deleted the moment the process exits.
	//
	// A switch rather than "use them whenever one is stored", because
	// sending a logged-in session to a site is a decision with consequences
	// (rate limits and bans land on the ACCOUNT, not on the address) and
	// nothing should start doing it because a cookie jar was pasted in once.
	Cookies bool `json:"cookies"`
	// Music turns the audio row into a tagger: --parse-metadata for artist/
	// album/title/track number, an Artist/Album/NN-Title output template and
	// a cover.jpg beside the tracks. See musicArgs (backend.go) for what
	// each mapping actually is and why the fallback chains are ordered the
	// way they are.
	//
	// Twelve mp3 files with no tags are twelve unrelated singles by
	// "Unknown Artist" in Navidrome/Plex, which is the state this switch
	// exists to avoid - the tags are what a library reads, the filename is
	// only what a file manager shows.
	Music bool `json:"music"`
	// Embed is the container-level extras: metadata, cover art, chapters,
	// subtitle tracks, chapter splitting and a Kodi NFO - see Embed's own
	// doc comment.
	Embed Embed `json:"embed"`
	// Measure re-reads the finished file with ffprobe and says so - see
	// Measure's own doc comment.
	Measure Measure `json:"measure"`
	// Live bounds a livestream recording, which otherwise has no end - see
	// Live's own doc comment.
	Live Live `json:"live"`
}

// Embed is what gets written INTO the finished file (and, for the NFO, right
// next to it) rather than fetched as a separate task of its own. Every field
// is off at the zero value, so a download built from Options{} carries
// exactly the flags it always did.
//
// Read on the video and audio rows only. The thumbnail, subtitle and
// description rows ARE the separate-file answer to the same wish (jdp,
// 2026-08-25's five "Variante" rows), and passing --embed-thumbnail to a task
// whose whole job is writing that thumbnail out as its own file would be a
// contradiction rather than a feature.
type Embed struct {
	// Metadata is --embed-metadata: title, uploader, date and description
	// into the container's own tag block. This is the field Jellyfin and
	// Plex read first when there is no NFO beside the file.
	Metadata bool `json:"metadata"`
	// Thumbnail is --embed-thumbnail: the source's own cover image as an
	// attached picture, so a library that scrapes nothing still shows art.
	Thumbnail bool `json:"thumbnail"`
	// Chapters is --embed-chapters: the source's own chapter marks as real
	// container chapters, which is what makes a player's skip button jump to
	// the next section instead of a fixed number of seconds.
	Chapters bool `json:"chapters"`
	// Subs is --embed-subs: subtitle tracks muxed into the video rather than
	// left as sidecar files. Video row only - an extracted audio file has no
	// subtitle stream to carry, and yt-dlp answers a request to put one
	// there with a warning --no-warnings then swallows.
	//
	// Which languages is SubtitleLangs, the same list the subtitle row uses;
	// SubtitleAuto widens it to auto-generated captions the same way.
	Subs bool `json:"subs"`
	// SplitChapters is --split-chapters: one file per chapter beside the
	// whole one. yt-dlp writes the split files through its own chapter
	// output template and keeps the complete file as well, so this adds
	// files rather than replacing the download.
	SplitChapters bool `json:"splitChapters"`
	// NFO writes a Kodi/Jellyfin/Plex <movie> NFO next to the finished file,
	// built from the info dict yt-dlp already assembled during extraction
	// (see nfo.go). Not a yt-dlp flag: yt-dlp writes --write-info-json and
	// nothing else, so the conversion is this package's own.
	NFO bool `json:"nfo"`
}

// Measure re-reads the finished file with ffprobe and records what is
// actually in it - runtime, resolution, codecs, how many audio tracks - on
// the task, and says so loudly when that runtime falls well short of the one
// the source announced.
//
// The failure this catches is a merge that died between the video and audio
// halves, or a fragmented download that stopped early on a server hiccup:
// yt-dlp exits 0, the file is there, the size looks plausible, and the first
// anybody knows about it is the picture stopping twenty minutes in. ffprobe
// is already in the image (the Dockerfile installs ffmpeg for yt-dlp's own
// muxing), so this costs one short process per finished download and no new
// dependency.
type Measure struct {
	// Enabled is the switch. Off at the zero value, like every field added
	// in this wave.
	Enabled bool `json:"enabled"`
	// ShortPercent is how much of the announced runtime the file has to
	// actually contain before the result counts as short, in percent. 0 uses
	// DefaultShortPercent. Values are clamped to 1..100 by Sanitize - 0 as
	// "never complain" is deliberately NOT expressible, because the whole
	// point of switching this on is being told.
	ShortPercent int `json:"shortPercent"`
	// FailOnShort settles a short file as an error instead of a finished
	// download carrying a warning. Off by default: the bytes on disk are
	// real and often still worth keeping, and a person who would rather have
	// the task go red and be re-runnable can say so here.
	FailOnShort bool `json:"failOnShort"`
}

// Live bounds a livestream recording. is_live is in the info dict every probe
// already reads and was until now simply discarded, so a link to a stream
// that has been running since Friday looked like an ordinary download with an
// ordinary progress bar - a bar computed from a total nobody knows, which
// creeps toward a finish that does not exist while the disk fills.
type Live struct {
	// Enabled is the switch, and it gates the DETECTION as much as the
	// limits: only with it on does buildArgs ask yt-dlp's progress template
	// to carry is_live at all (see liveProgressTemplate, live.go), so an
	// install that leaves this off runs the exact progress-line format, and
	// the exact parse, that it always did.
	Enabled bool `json:"enabled"`
	// FromStart is --live-from-start: begin at the stream's own beginning
	// instead of at the live edge. yt-dlp documents it as YouTube-only and
	// experimental, and it is ignored outright for anything that is not
	// live, which is why it is safe to pass on every spawn once the switch
	// is on.
	FromStart bool `json:"fromStart"`
	// MaxMinutes stops the recording after this many minutes of wall clock,
	// 0 for no limit.
	//
	// Minutes of recording rather than a time of day, and that IS the
	// deliberate reading of "limit the clock": a stream started at 22:00
	// with "stop at 23:00" is a one-hour cap that silently becomes a
	// twenty-three-hour one when the same link is pasted at midnight,
	// whereas a duration means the same thing whenever it starts. Rules that
	// genuinely are about the time of day already have a home in this app -
	// internal/schedule's timetable - and this is not a second, weaker copy
	// of it.
	MaxMinutes int `json:"maxMinutes"`
	// MaxMB stops the recording once this many mebibytes have arrived, 0 for
	// no limit. Counted from yt-dlp's own downloaded_bytes rather than by
	// stat-ing the file, because a fragmented live download writes through
	// temp parts whose combined size on disk is not the number a person set
	// this limit against.
	MaxMB int `json:"maxMB"`
}

// DefaultShortPercent is the share of the announced runtime a finished file
// has to reach before Measure stops calling it short.
//
// Ninety, not ninety-nine: a real download legitimately lands a little under
// the announced number (a source that counts a trailing silence yt-dlp's
// merge drops, an announced duration rounded to whole seconds, a live-edge
// recording that starts a moment late), and a threshold that fires on those
// trains people to ignore it. A truncated merge is not off by five percent,
// it is off by half.
const DefaultShortPercent = 90

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
// Every one of these is a value yt-dlp's own --audio-format accepts. It used
// to be six, and the three that were missing are exactly the ones a person
// coming from JDownloader looks for first: JD lists AAC by name, and a source
// carrying a Vorbis or ALAC track had nothing on this menu that matched it
// (jdp, 2026-09-06: "bei youtube links gibt es zb das audioformat aac nicht im
// dropdown als auswahl obwohl es JD anbietet"). AvailableAudioFormats still
// narrows this to what a probed source genuinely carries, so a longer menu
// costs a real link nothing.
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
// It returns a LIST, because one codec can have more than one native reading
// on that menu and picking a favourite would hide the other: an AAC stream is
// "m4a" when you mean the container it already sits in and "aac" when you mean
// the raw stream, and neither one re-encodes anything. JDownloader names that
// row AAC, which is why the missing half was the half people looked for.
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
	QualityBest Quality = "best"
	// The ladder covers what the sites this resolver reaches actually
	// publish, top and bottom (jdp, 2026-09-06: "Es soll bei solchen links
	// immer generell alle dateiformate anbieten die der hoster ausgibt bzw
	// anbietet. ebenso die qualitäten"). It used to stop at 2160p and at
	// 360p, so a source with an 8K track offered nothing above 4K and one
	// watched on a phone connection could not be asked for 240p at all -
	// both are real YouTube heights, and AvailableQualities trims whatever
	// a given source does not have anyway.
	Quality4320p Quality = "4320p"
	Quality2160p Quality = "2160p"
	Quality1440p Quality = "1440p"
	Quality1080p Quality = "1080p"
	Quality720p  Quality = "720p"
	Quality480p  Quality = "480p"
	Quality360p  Quality = "360p"
	Quality240p  Quality = "240p"
	Quality144p  Quality = "144p"
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

// heightCaps maps a resolution preset onto yt-dlp's own filter syntax. The
// `?` in `<=?` matters: a plain `<=` excludes any format whose height yt-dlp
// could not read, which real-world formats sometimes lack, while `<=?`
// keeps it in the running - see yt-dlp's own format-selection docs on
// "eager" comparisons.
var heightCaps = map[Quality]string{
	Quality4320p: "4320", Quality2160p: "2160", Quality1440p: "1440", Quality1080p: "1080",
	Quality720p: "720", Quality480p: "480", Quality360p: "360", Quality240p: "240", Quality144p: "144",
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
	o.AudioLang = sanitizeLangCode(o.AudioLang)
	o.OutputTemplate = sanitizeTemplate(o.OutputTemplate)
	o.Measure = o.Measure.sanitize()
	o.Live = o.Live.sanitize()
	return o
}

// sanitize keeps Measure's own numbers inside the range the comparison can
// actually mean something in. Out of range folds onto the default rather than
// onto the nearest edge: somebody who typed 500 did not mean "the file has to
// be five times as long as announced", they mistyped, and a threshold nothing
// can ever satisfy would fail every download this feature was switched on to
// watch over.
func (m Measure) sanitize() Measure {
	if m.ShortPercent < 1 || m.ShortPercent > 100 {
		m.ShortPercent = DefaultShortPercent
	}
	return m
}

// sanitize floors Live's two caps at zero, which is this struct's own "no
// limit" (see the fields' own doc comments). A negative number reaching the
// guard would compare true on the very first progress line and stop a
// recording the instant it started.
func (l Live) sanitize() Live {
	if l.MaxMinutes < 0 {
		l.MaxMinutes = 0
	}
	if l.MaxMB < 0 {
		l.MaxMB = 0
	}
	return l
}

// sanitizeLangCode keeps AudioLang to something that can be dropped into a
// yt-dlp format filter without changing that filter's SHAPE. `[language^=de]`
// is a bracket expression with its own grammar, and a value carrying `]`,
// `[`, a comma or a quote would not be a wrong language, it would be a
// different, malformed selector - yt-dlp then refuses the whole invocation
// over a settings field that was only ever meant to name a language.
//
// Not a shell-escaping concern (exec.CommandContext never invokes a shell,
// see buildArgs), and deliberately not a list of valid ISO codes either: BCP
// 47 tags in the wild are "de", "de-DE", "pt-BR", "zh-Hans", and a whitelist
// here would be a second, always-stale copy of what the source itself
// reports. Letters, digits and a hyphen cover every one of those; anything
// else is dropped rather than the whole field refused, matching this file's
// "Sanitize always succeeds" contract.
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
