package ytdlp

import "strings"

// Options is the user-configurable half of a yt-dlp invocation: format
// selection, subtitles, the output filename, and whether a playlist URL
// fetches one video or the whole list - everything backend.go used to bake
// straight into args with no field anywhere to change it (see
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
	Quality      Quality      `json:"quality"`
	CustomFormat string       `json:"customFormat"`
	Subtitles    SubtitleMode `json:"subtitles"`
	// SubtitleLangs is yt-dlp's own --sub-langs value (comma-separated
	// codes, or a pattern like "en.*"; "all" is also yt-dlp's own keyword).
	// Empty falls back to DefaultSubtitleLangs whenever Subtitles is not
	// SubtitlesOff - yt-dlp requires some language list to fetch, and this
	// package cannot guess a person's language for them.
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

// Quality is a format-selector preset offered on the settings page.
type Quality string

const (
	// QualityBest is the zero value: no -f at all, i.e. yt-dlp's own
	// default selection - named so the page has something to show selected
	// on a fresh install, not so this package ever compares against it
	// before Sanitize has run.
	QualityBest      Quality = "best"
	Quality2160p     Quality = "2160p"
	Quality1440p     Quality = "1440p"
	Quality1080p     Quality = "1080p"
	Quality720p      Quality = "720p"
	Quality480p      Quality = "480p"
	Quality360p      Quality = "360p"
	QualityAudioOnly Quality = "audioOnly"
	// QualityCustom hands CustomFormat to -f verbatim, unexamined - yt-dlp's
	// own format-selector grammar is not reimplemented here, and a value it
	// rejects surfaces through run()'s existing stderr-tail error path
	// exactly like any other yt-dlp failure.
	QualityCustom Quality = "custom"
)

// Qualities lists every quality this build can offer, in menu order - the
// same "the menu and the validity check read one list" shape
// internal/idleaction.Actions already uses for Config.Action.
func Qualities() []Quality {
	return []Quality{
		QualityBest, Quality2160p, Quality1440p, Quality1080p,
		Quality720p, Quality480p, Quality360p, QualityAudioOnly, QualityCustom,
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

// SubtitleMode is what a downloaded video does with subtitle tracks.
type SubtitleMode string

const (
	// SubtitlesOff is the zero value and today's only behaviour: no
	// subtitle flags at all.
	SubtitlesOff SubtitleMode = "off"
	// SubtitlesFile writes subtitles beside the video (--write-subs).
	SubtitlesFile SubtitleMode = "file"
	// SubtitlesEmbed writes AND muxes them into the video container
	// (--write-subs --embed-subs); yt-dlp remuxes into a container that can
	// hold them when the chosen format's own container cannot.
	SubtitlesEmbed SubtitleMode = "embed"
)

// SubtitleModes lists every mode this build can offer, in menu order.
func SubtitleModes() []SubtitleMode {
	return []SubtitleMode{SubtitlesOff, SubtitlesFile, SubtitlesEmbed}
}

func validSubtitleMode(m SubtitleMode) bool {
	for _, x := range SubtitleModes() {
		if x == m {
			return true
		}
	}
	return false
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
	if !validQuality(o.Quality) {
		o.Quality = QualityBest
	}
	if !validSubtitleMode(o.Subtitles) {
		o.Subtitles = SubtitlesOff
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
