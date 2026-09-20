package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// Variant rows: one pasted yt-dlp link becomes up to five sibling tasks
// (video, audio, thumbnail, subtitle, description) sharing the URL and
// package, each with its own Enabled switch and, for video and audio, its own
// quality.

// variantEncode and variantDecode are core.Task.Variant's encoding: "<kind>"
// or "<kind>:<quality-or-format>", one string so no store migration is needed.
func variantEncode(kind ytdlp.Variant, sub string) string {
	if sub == "" {
		return string(kind)
	}
	return string(kind) + ":" + sub
}

func variantDecode(v string) (kind ytdlp.Variant, sub string) {
	k, s, _ := strings.Cut(v, ":")
	return ytdlp.Variant(k), s
}

// ytdlpOptionsForTask is the per-task options closure wired in rewireBackends:
// the instance-wide defaults with Variant, Quality and AudioFormat taken from
// the task's own Variant.
func (a *App) ytdlpOptionsForTask(taskID string) ytdlp.Options {
	base := a.Settings.Get().Ytdlp
	a.mu.Lock()
	t := a.tasks[taskID]
	a.mu.Unlock()
	if t == nil {
		return base
	}
	// A playlist entry or crawled link is one item from a list that has already
	// been expanded. Without --no-playlist, an entry URL that still carries
	// "&list=" would download the whole playlist again for every row.
	if t.Origin == OriginCrawl {
		base.Playlist = false
	}
	kind, sub := variantDecode(t.Variant)
	if kind == "" {
		kind = ytdlp.VariantVideo
	}
	base.Variant = kind
	switch kind {
	case ytdlp.VariantVideo:
		if sub != "" {
			base.Quality = ytdlp.Quality(sub)
		}
	case ytdlp.VariantAudio:
		if sub != "" {
			base.AudioFormat = sub
		}
		if t.AudioBitrate != "" {
			base.AudioBitrate = t.AudioBitrate
		}
	}
	return base
}

// HosterPresetFor is the preset a host's links stage with: the saved one, or
// ytdlp.DefaultHosterPreset.
func (a *App) HosterPresetFor(host string) ytdlp.HosterPreset {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ytdlp.DefaultHosterPreset()
	}
	if p, ok := a.Settings.Get().YtdlpPresets[host]; ok {
		return p.Sanitize()
	}
	return ytdlp.DefaultHosterPreset()
}

// SetHosterPreset saves a host's preset. host is lower-cased and stripped of
// "www." as task hosts are, so a preset matches every link from the site.
//
// It goes through PatchSettings so a concurrent save of another field is not
// clobbered. The presets map itself is still read then written, which only
// matters if two hosts' presets are saved at the same instant.
func (a *App) SetHosterPreset(host string, p ytdlp.HosterPreset) error {
	host = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(host, "www.")))
	if host == "" {
		return nil
	}
	cur := a.Settings.Get().YtdlpPresets
	presets := make(map[string]ytdlp.HosterPreset, len(cur)+1)
	for k, v := range cur {
		presets[k] = v
	}
	presets[host] = p.Sanitize()
	raw, err := json.Marshal(presets)
	if err != nil {
		return err
	}
	_, err = a.PatchSettings(map[string]json.RawMessage{"ytdlpPresets": raw})
	return err
}

// expandYtdlpVariants turns the task stage created into a full family: it
// becomes the video row, and the other four variants are added as rows
// regardless of the preset, enabled as the host's preset says. It runs once,
// right after staging.
func (a *App) expandYtdlpVariants(primary *core.Task) {
	preset := a.HosterPresetFor(primary.Host)

	a.mu.Lock()
	live := a.tasks[primary.ID]
	if live == nil {
		a.mu.Unlock()
		return
	}
	live.Variant = variantEncode(ytdlp.VariantVideo, string(preset.Quality))
	pc := *live
	a.mu.Unlock()
	_ = a.Store.Save(&pc)
	a.Hub.Broadcast("task", &pc)

	for _, v := range ytdlp.Variants() {
		if v == ytdlp.VariantVideo {
			continue // the primary itself
		}
		sub := ""
		if v == ytdlp.VariantAudio {
			sub = preset.AudioFormat
		}
		a.insertVariantSibling(&core.Task{
			URL: pc.URL,
			// pc.Name rather than the URL: a playlist entry is already named by
			// the listing, and setTaskName does not rename siblings of a primary
			// that already has a name.
			Name:      pc.Name,
			Package:   pc.Package,
			Status:    core.StatusCollected,
			Enabled:   preset.HasVariant(v),
			Source:    pc.Source,
			Origin:    pc.Origin,
			Host:      pc.Host,
			Resolver:  pc.Resolver,
			Variant:   variantEncode(v, sub),
			Ext:       fixedVariantExt(v, sub),
			CreatedAt: pc.CreatedAt,
		})
	}
}

// fixedVariantExt is the extension a variant row knows without asking the
// source. buildArgs pins thumbnails to jpg and subtitles to srt, a description
// is a text file, and audio with an explicit format has that extension. Best
// audio and video depend on the source and wait for the probe. Setting these at
// creation means a probe that never answers still leaves an extension.
func fixedVariantExt(v ytdlp.Variant, sub string) string {
	switch v {
	case ytdlp.VariantThumbnail:
		return "jpg"
	case ytdlp.VariantSubtitle:
		return "srt"
	case ytdlp.VariantDescription:
		return "description"
	case ytdlp.VariantAudio:
		if sub != "" && sub != "best" {
			return sub
		}
	}
	return ""
}

// insertVariantSibling does what put does without the dedupe check: every row
// in the family shares the primary's URL, which put would refuse as a second
// paste of the same link.
func (a *App) insertVariantSibling(t *core.Task) {
	a.mu.Lock()
	t.ID = a.freshIDLocked()
	a.tasks[t.ID] = t
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// applyProbeFormats applies a completed probe's format list to every row of
// the family, and marks them online, since a probe that answered at all proves
// the source is there. A failed probe never gets here and is not read as
// offline: a timeout or an age gate is not the host saying the file is gone.
//
// Per kind, matching buildArgs (backend.go):
//   - description: Ext is always "description".
//   - audio with a fixed format: Ext is that format; Size is left alone since
//     transcoding changes it. AvailableAudioFormats narrows to what the
//     source's audio-only tracks carry.
//   - audio "best": a straight extract, so Ext and Size come from the best
//     audio-only track.
//   - video: AvailableQualities from the track heights, Size from the best
//     track under the picked cap, and Ext "mkv" only when a video-only and
//     audio-only pair will be merged.
//   - thumbnail and subtitle: jpg and srt, from the forced conversions.
func (a *App) applyProbeFormats(rawurl string, formats []ytdlp.FormatEntry) {
	var maxVideoHeight int
	var maxAudioAbr float64
	var bestAudio ytdlp.FormatEntry
	hasBestAudio := false
	hasVideoOnly := false
	audioCodecs := make([]string, 0, len(formats))
	for _, f := range formats {
		isVideo := f.Vcodec != "" && f.Vcodec != "none"
		isAudio := f.Acodec != "" && f.Acodec != "none"
		if isVideo && f.Height > maxVideoHeight {
			maxVideoHeight = f.Height
		}
		if isVideo && !isAudio {
			hasVideoOnly = true
		}
		if isAudio && !isVideo {
			audioCodecs = append(audioCodecs, f.Acodec)
			if f.Abr > maxAudioAbr {
				maxAudioAbr = f.Abr
			}
			if !hasBestAudio || formatSize(f) > formatSize(bestAudio) {
				bestAudio = f
				hasBestAudio = true
			}
		}
	}
	qualities := ytdlp.AvailableQualities(maxVideoHeight)
	availableQualities := make([]string, len(qualities))
	for i, q := range qualities {
		availableQualities[i] = string(q)
	}
	availableAudioFormats := ytdlp.AvailableAudioFormats(audioCodecs)
	availableAudioBitrates := ytdlp.AvailableAudioBitrates(maxAudioAbr)

	a.mu.Lock()
	var touched []core.Task
	for _, t := range a.tasks {
		if t.URL != rawurl {
			continue
		}
		changed := false
		if t.Online != core.AvailOnline {
			t.Online = core.AvailOnline
			if t.Status != core.StatusError {
				t.Error = ""
				t.Reason = ""
			}
			changed = true
		}
		kind, sub := variantDecode(t.Variant)
		switch kind {
		case ytdlp.VariantDescription:
			if t.Ext != "description" {
				t.Ext = "description"
				changed = true
			}
		case ytdlp.VariantAudio:
			if !stringSlicesEqual(t.AvailableAudioFormats, availableAudioFormats) {
				t.AvailableAudioFormats = availableAudioFormats
				changed = true
			}
			if !stringSlicesEqual(t.AvailableAudioBitrates, availableAudioBitrates) {
				t.AvailableAudioBitrates = availableAudioBitrates
				changed = true
			}
			if sub != "" && sub != "best" {
				if t.Ext != sub {
					t.Ext = sub
					changed = true
				}
			} else if hasBestAudio {
				// -x without --audio-format copies the track as is, so the
				// format's own extension is what yt-dlp writes.
				if bestAudio.Ext != "" && t.Ext != bestAudio.Ext {
					t.Ext = bestAudio.Ext
					changed = true
				}
				if sz := formatSize(bestAudio); sz > 0 && t.Size != sz {
					t.Size = sz
					changed = true
				}
			}
		case ytdlp.VariantVideo:
			if !stringSlicesEqual(t.AvailableQualities, availableQualities) {
				t.AvailableQualities = availableQualities
				changed = true
			}
			// --merge-output-format mkv only applies to a real merge; a source
			// without a video-only and audio-only pair falls back to one
			// pre-muxed stream.
			if hasVideoOnly && hasBestAudio && t.Ext != "mkv" {
				t.Ext = "mkv"
				changed = true
			}
			capHeight := maxVideoHeight
			if sub != "" && sub != string(ytdlp.QualityBest) && sub != string(ytdlp.QualityCustom) {
				if h, ok := ytdlp.HeightCap(ytdlp.Quality(sub)); ok {
					capHeight = h
				}
			}
			if best := bestVideoAtOrUnder(formats, capHeight); best != nil {
				if sz := formatSize(*best); sz > 0 && t.Size != sz {
					t.Size = sz
					changed = true
				}
			}
		case ytdlp.VariantThumbnail:
			// --convert-thumbnails jpg (backend.go).
			if t.Ext != "jpg" {
				t.Ext = "jpg"
				changed = true
			}
		case ytdlp.VariantSubtitle:
			// --sub-format srt (backend.go).
			if t.Ext != "srt" {
				t.Ext = "srt"
				changed = true
			}
		}
		if changed {
			touched = append(touched, *t)
		}
	}
	a.mu.Unlock()
	for i := range touched {
		_ = a.Store.Save(&touched[i])
		a.Hub.Broadcast("task", &touched[i])
	}
}

// backfillYtdlpProbes probes collector rows whose quality or format menus have
// nothing to narrow them, because they were staged before probing existed or
// their probe never answered. Empty means no opinion, so the picker would offer
// the full static menu (flac for a source that has no lossless track).
//
// It does not check the row's current resolver: routing is decided per attempt,
// but the question is what the source offers, which only yt-dlp can answer.
// It probes one distinct URL at a time, since applyProbeFormats updates every
// sibling and forty yt-dlp processes at boot would look like a hang.
func (a *App) backfillYtdlpProbes() {
	// The fixed extensions first and without a network call, so a box without
	// yt-dlp still shows them.
	a.applyFixedVariantExts()
	tp, ok := a.ytdlpTitleProber()
	if !ok {
		return
	}
	a.mu.Lock()
	var urls []string
	seen := make(map[string]bool)
	for _, t := range a.tasks {
		if t.Status != core.StatusCollected || t.URL == "" || seen[t.URL] {
			continue
		}
		kind, _ := variantDecode(t.Variant)
		switch kind {
		case ytdlp.VariantVideo:
			if len(t.AvailableQualities) > 0 {
				continue
			}
		case ytdlp.VariantAudio:
			if len(t.AvailableAudioFormats) > 0 {
				continue
			}
		default:
			// Not a family member, or a fixed-extension kind with nothing to
			// probe for.
			continue
		}
		seen[t.URL] = true
		urls = append(urls, t.URL)
	}
	a.mu.Unlock()

	for _, u := range urls {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		ctx, cancel := context.WithTimeout(a.ctx, ytdlpProbeTimeout)
		res, err := tp.ProbeTitle(ctx, u)
		cancel()
		if err != nil {
			// Unreachable now; the menu stays as wide as it was.
			continue
		}
		a.applyProbeFormats(u, res.Formats)
	}
}

// applyFixedVariantExts gives existing variant rows the extension
// fixedVariantExt knows for their kind. It only fills blanks, since a probed
// extension (an audio row resolved to m4a) is more accurate than the table.
func (a *App) applyFixedVariantExts() {
	a.mu.Lock()
	var touched []core.Task
	for _, t := range a.tasks {
		if t.Ext != "" || t.Variant == "" {
			continue
		}
		kind, sub := variantDecode(t.Variant)
		ext := fixedVariantExt(kind, sub)
		if ext == "" {
			continue
		}
		t.Ext = ext
		touched = append(touched, *t)
	}
	a.mu.Unlock()
	for i := range touched {
		_ = a.Store.Save(&touched[i])
		a.Hub.Broadcast("task", &touched[i])
	}
}

// formatSize is a format's best known byte count: exact when the host reports
// one, yt-dlp's estimate otherwise, 0 when neither is known.
func formatSize(f ytdlp.FormatEntry) int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}

// bestVideoAtOrUnder is the tallest video track at or under capHeight, the
// choice formatSelector's "<=?H" makes, so the size estimate matches the real
// download. capHeight <= 0 means no cap.
func bestVideoAtOrUnder(formats []ytdlp.FormatEntry, capHeight int) *ytdlp.FormatEntry {
	var best *ytdlp.FormatEntry
	for i, f := range formats {
		if f.Vcodec == "" || f.Vcodec == "none" {
			continue
		}
		if capHeight > 0 && f.Height > capHeight {
			continue
		}
		if best == nil || f.Height > best.Height {
			best = &formats[i]
		}
	}
	return best
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
