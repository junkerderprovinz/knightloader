package ytdlp

import "sort"

// The language menus are built from what the probe (ProbeTitle) reports a
// source offers, rather than from free text. With --no-warnings, asking for a
// subtitle language the source lacks would otherwise finish silently over an
// empty folder.

// SubtitleTracks lists the languages a source offers, sorted by code. Manual
// and automatic tracks stay apart, as yt-dlp's --write-subs and
// --write-auto-subs do, because their quality differs.
type SubtitleTracks struct {
	Manual []string
	Auto   []string
}

// Has reports whether lang is offered in either kind (see
// Options.SubtitleStrict for the check after the download).
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

// AvailableSubtitleLangs splits a probe's two subtitle maps into the picker's
// two sorted lists. A language in both maps stays in both, since YouTube often
// has a manual and an automatic track in the same language.
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

// AudioLang is one spoken language a source carries an audio track in.
type AudioLang struct {
	// Code is the language yt-dlp reports for the track ("en", "de-DE"),
	// never empty.
	Code string
	// Dubbed marks an automatic dub rather than the original audio. It is
	// read from language_preference, which yt-dlp's YouTube extractor ranks
	// below default for dubs, the same ranking bestaudio follows. An absent
	// value (0) counts as original, since most sites have a single track.
	Dubbed bool
}

// AvailableAudioLangs is the audio row's language menu: each distinct
// language of the audio-carrying formats (audio-only and combined), originals
// first, each group sorted by code. A code offered both as original and as a
// dub counts as original, since that is what a filter on it gets.
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
