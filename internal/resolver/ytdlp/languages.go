package ytdlp

// languages.go: the two language menus, built from what a source actually
// offers instead of from a free-text box.
//
// The free-text box is the bug. A subtitle row ships with "en" in it, the
// invocation carries --no-warnings (buildArgs), and "There are no subtitles
// for the requested languages" is a warning - so asking a German film for
// English subtitles exits 0, writes nothing, and settles the task green over
// an empty folder. Nothing anywhere told the person the language was not on
// offer, because the one process that knew said so through a channel this
// backend had muted.
//
// The information was already on hand: the same -j probe that fills in a
// task's title (ProbeTitle, backend.go) returns the source's own "subtitles"
// and "automatic_captions" maps and, per audio format, the track's own
// language. These functions turn that into the two lists a picker needs, in
// the same shape AvailableQualities and AvailableAudioFormats already answer
// in - a subset of what could be offered, narrowed to what this source
// genuinely has, with the "no opinion yet" case (nothing probed) returning
// nothing rather than a guess.

import "sort"

// SubtitleTracks is what a source offers in one of the two kinds, sorted by
// language code. Manual and Auto are kept apart rather than merged because
// they are not the same product: a hand-written track is a translation
// somebody made, an automatic one is a speech-to-text pass whose quality
// varies from usable to comic, and a person choosing between them is making a
// real choice - which is exactly what --write-subs and --write-auto-subs are
// two separate flags for.
type SubtitleTracks struct {
	Manual []string
	Auto   []string
}

// Has reports whether lang is offered at all, in either kind. Used to answer
// "the language this row asks for is not on this source" BEFORE a download
// spends a process finding out (see Options.SubtitleStrict for the backstop
// that catches it afterwards).
func (t SubtitleTracks) Has(lang string) bool {
	return contains(t.Manual, lang) || contains(t.Auto, lang)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// AvailableSubtitleLangs splits a probe's own two subtitle maps into the
// picker's two lists.
//
// A language present in BOTH maps stays in both, deliberately: YouTube
// commonly carries a hand-written English track and an automatic English one
// for the very same video, and collapsing them would hide the choice this
// split exists to offer. Order is the map's iteration order in Go, i.e. none,
// so both lists are sorted here rather than at every call site that wants to
// show them.
func AvailableSubtitleLangs(res ProbeResult) SubtitleTracks {
	return SubtitleTracks{Manual: sortedKeys(res.Subtitles), Auto: sortedKeys(res.AutoCaptions)}
}

func sortedKeys(langs []string) []string {
	if len(langs) == 0 {
		return nil
	}
	out := make([]string, 0, len(langs))
	seen := make(map[string]bool, len(langs))
	for _, l := range langs {
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// AudioLang is one spoken language a source carries an audio track in, and
// whether that track is the one actually spoken or a machine translation of
// it.
type AudioLang struct {
	// Code is yt-dlp's own reported language for the track ("en", "de-DE",
	// "pt-BR"). Never empty - a format that reports no language at all
	// contributes nothing to the list, because an unnamed track cannot be
	// asked for by name either.
	Code string
	// Dubbed marks an automatic dub rather than the original audio.
	//
	// Read off yt-dlp's own language_preference, which its YouTube extractor
	// sets ABOVE the default for the track the video was recorded in and
	// leaves below it for every auto-dubbed alternative - that ranking is
	// what makes yt-dlp's own "bestaudio" prefer the original, and reading
	// the same number is how this list agrees with what a download without
	// an AudioLang filter would actually have picked.
	//
	// A source that says nothing (the field absent, so 0 here) is reported
	// as NOT dubbed. That is the honest reading: every site that is not
	// YouTube ships one audio track and never had an opinion to state, and
	// calling those "possibly a dub" would put a warning on every ordinary
	// video in order to describe one site's feature.
	Dubbed bool
}

// AvailableAudioLangs is the audio row's own language menu: every distinct
// language the source's audio-carrying formats report, original tracks first
// and each kind sorted by code.
//
// Originals first because that is the answer somebody wants by default and a
// list that buries it under six machine dubs makes the right choice the
// hardest one to find. Audio-only formats AND progressive ones (a format with
// both a real vcodec and a real acodec) both count - a source whose only
// German audio sits inside a combined 360p stream still genuinely offers
// German, and leaving it out would say otherwise.
//
// One entry per language, not per format: a language shows up once for every
// bitrate it is offered in, and a menu repeating "de" five times is not a
// menu. When the same code appears both as an original and as a dub - which
// happens on a video whose original language also got a dubbed track for a
// regional variant - original wins, because that is the track a filter on
// that code will actually get.
func AvailableAudioLangs(formats []FormatEntry) []AudioLang {
	best := map[string]bool{} // code -> dubbed
	var order []string
	for _, f := range formats {
		if f.Acodec == "" || f.Acodec == "none" || f.Language == "" {
			continue
		}
		dubbed := f.LanguagePreference < 0
		if prev, seen := best[f.Language]; seen {
			if prev && !dubbed {
				best[f.Language] = false
			}
			continue
		}
		best[f.Language] = dubbed
		order = append(order, f.Language)
	}
	if len(order) == 0 {
		return nil
	}
	sort.Strings(order)
	out := make([]AudioLang, 0, len(order))
	for _, code := range order {
		if !best[code] {
			out = append(out, AudioLang{Code: code})
		}
	}
	for _, code := range order {
		if best[code] {
			out = append(out, AudioLang{Code: code, Dubbed: true})
		}
	}
	return out
}
