package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// app_ytdlp_variants.go: the "Variante" rows a yt-dlp-routed link stages
// (jdp, 2026-08-25, after a first, narrower attempt at this same request
// only added a bundle of on/off flags to one task: "ich glaub du hast
// nicht verstanden was ich mein... wenn ich in JD ein Youtub-link einfüge
// listet es video, audio, bild, untertitel, text auf... genau so soll es
// auch in KL sein"). One pasted link becomes up to five sibling tasks -
// video, audio, thumbnail, subtitle, description - sharing the same URL
// and package, each its own row with its own Enabled switch and (for
// video/audio) its own quality choice.

// variantEncode/variantDecode are core.Task.Variant's own compact
// encoding: "<kind>" or "<kind>:<quality-or-format>" - one plain string
// rather than a second persisted column, matching that field's own doc
// comment ("a yt-dlp format, a quality") and needing no store migration.
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

// ytdlpOptionsForTask is ytdlp.Backend.Options's own per-task closure body
// (wired in rewireBackends, app_accounts.go): the instance-wide defaults
// (subtitle language, playlist, output template - nothing a task's own
// Variant overrides), with Variant/Quality/AudioFormat replaced by what
// THIS task's own core.Task.Variant says.
func (a *App) ytdlpOptionsForTask(taskID string) ytdlp.Options {
	base := a.Settings.Get().Ytdlp
	a.mu.Lock()
	t := a.tasks[taskID]
	a.mu.Unlock()
	if t == nil {
		return base
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

// HosterPresetFor is the preset a host's own links stage with - what was
// saved for it, or ytdlp.DefaultHosterPreset() when nothing was.
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

// SetHosterPreset saves host's own preset (the "Variante" gear badge's own
// write path - PackageGroup's header, TaskList.tsx). host is lower-cased
// and "www."-stripped the same way hostOf/torrentHost already normalise
// every task's own Host field, so a preset saved from one row always
// matches every future link from the same site regardless of which
// subdomain the pasted URL happened to carry.
//
// Goes through PatchSettings, not a Get-merge-ApplySettings round trip:
// SetPartial reads every OTHER top-level field under the same lock that
// writes the merged result, so a settings save racing this one on some
// unrelated field (Quality on the Resolvers page, say) can never be
// clobbered by a stale full snapshot - see PatchSettings's own doc
// comment. YtdlpPresets itself is still read-then-written here, a narrow
// window PatchSettings does not close on its own, but two people editing
// two different hosts' presets at the same literal instant is a
// vanishingly unlikely collision next to the whole-document race this
// avoids.
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

// expandYtdlpVariants turns the one task stage() already created (video's
// own row from here on) into a full family: video plus, per the staging
// host's own preset, the other four variants - each present as its own
// row regardless of the preset (jdp: "Alle 5 immer als Zeile"), enabled or
// not per what that preset says. Called once, synchronously, right after
// staging succeeds - from the same call site stage() already spawns the
// async title probe from, see that call site's own comment (app_links.go).
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
			continue // already the primary task itself, handled above
		}
		sub := ""
		if v == ytdlp.VariantAudio {
			sub = preset.AudioFormat
		}
		a.insertVariantSibling(&core.Task{
			URL:       pc.URL,
			Name:      pc.URL,
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

// fixedVariantExt is the extension a variant row already knows about itself
// the moment it is created, without asking the source anything (jdp,
// 2026-09-05: "Bei allen links in der dateiliste soll es die Dateiendung
// immer anzeigen").
//
// Three of the five kinds land in a format buildArgs pins outright
// (--convert-thumbnails jpg, --sub-format srt, and a description that IS a
// text file), and an audio row with an explicit format was told its own
// extension by the preset that created it. Only "best" audio and video depend
// on what the source actually has, and those two wait for the probe.
//
// Set here rather than left to applyProbeFormats alone, which is where all
// five used to be decided: a probe that never answers left a row with no
// extension at all, for four cases whose answer was never in doubt.
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

// insertVariantSibling is put's own shape (fresh ID, insert, save,
// broadcast) without put's dedupe check - a.dupes keys on URL among other
// things, and every task in this family deliberately shares the primary's
// URL, which put() would read as "the same link pasted twice" and refuse.
// These are not a duplicate paste; they are several different jobs against
// the one URL, so the ordinary dedupe rule does not apply to them.
func (a *App) insertVariantSibling(t *core.Task) {
	a.mu.Lock()
	t.ID = a.freshIDLocked()
	a.tasks[t.ID] = t
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// applyProbeFormats is what a completed probe's own format list (
// ytdlp.ProbeResult.Formats) lets the family answer for certain, plus the
// availability signal a probe that came back AT ALL already proves (jdp,
// 2026-08-25: "der [status-punkt] zeigt immer noch keine farbe an" - a
// freshly staged link's Online field starts unset/grey until something
// checks it, and unlike a plain HTTP link (analyze's own HEAD probe, called
// automatically at staging - app_links.go), nothing did that for a yt-dlp
// link before now; the title/format probe this function rides along with
// already IS that check, for free, so there is no reason to leave the
// family gray until somebody presses "recheck" by hand). A probe that
// failed never reaches this function at all (probeYtdlpTitle returns
// before calling it) - failure is deliberately NOT read as "offline": too
// many failure causes (a timeout, an age gate, a transient site hiccup)
// are not the host actually saying the file is gone, and this package's
// own analyze()/RecheckTasks already draw that same line.
//
// Per variant kind, matching buildArgs's own per-variant extension table
// (backend.go) exactly - honestly answerable ahead of time or not. Every
// kind now gets an Ext (jdp, 2026-08-26: "dateiendungen werden immer noch
// nicht angezeigt" - the video/thumbnail/subtitle rows answered nothing at
// all before this pass, because their own real container genuinely used to
// depend on the source; buildArgs now forces a fixed target for each
// (--merge-output-format mkv, --convert-thumbnails jpg, --sub-format srt),
// which turns "depends on the source" into a fact this function can state
// instead of a guess it has to avoid making):
//   - description: Ext is always "description" - no probe maths needed.
//   - audio, a FIXED format (mp3/m4a/opus/wav/flac): Ext is that format
//     itself (deterministic - ffmpeg's own --audio-format target) - but
//     not Size, since transcoding changes the byte count unpredictably
//     from the source track's own. AvailableAudioFormats always narrows to
//     what the source's own audio-only tracks actually carry, regardless
//     of which format is currently picked.
//   - audio, "best" (-x with no --audio-format - a straight extract, not a
//     transcode): both Ext and Size come from the best-matching source
//     audio-only track's own reported values - a real fact, not a guess,
//     since nothing re-encodes it and the container it lands in IS
//     whatever that specific format's own extension already says.
//   - video: AvailableQualities from every real video-track height found
//     (ytdlp.AvailableQualities), Size from the best-matching video-only
//     track at or under this row's own currently-picked quality cap, and
//     Ext="mkv" ONLY when the source actually offers a real video-only +
//     audio-only pair to merge (buildArgs' own forced merge format has no
//     effect on the muxed-fallback path a source without one takes
//     instead - see that branch's own comment).
//   - thumbnail: Ext="jpg" always (the forced conversion never fails to
//     produce one once any thumbnail exists at all).
//   - subtitle: Ext="srt" always, same reasoning.
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
				// -x with no --audio-format copies the source track without
				// re-encoding it, so the container it lands in genuinely IS
				// the matched format's own reported extension - not a guess,
				// the same value yt-dlp itself will write.
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
			// mkv only when a real merge will actually happen - buildArgs'
			// own --merge-output-format mkv (backend.go) has no effect on
			// the muxed-fallback path a source with no true video-only/
			// audio-only pair takes instead (formatSelector's own selector
			// falls back to a single pre-muxed stream, unchanged by that
			// flag), so claiming mkv there would be wrong rather than merely
			// unhelpful - see this case's own AvailableQualities collapsing
			// to best/custom alone for the same source-shape signal.
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
			// --convert-thumbnails jpg (backend.go) makes this a fact rather
			// than a guess: whatever format the source's own thumbnail
			// arrives in, ffmpeg converts it before it lands on disk.
			if t.Ext != "jpg" {
				t.Ext = "jpg"
				changed = true
			}
		case ytdlp.VariantSubtitle:
			// --sub-format srt (backend.go), same reasoning as the
			// thumbnail case just above.
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

// backfillYtdlpProbes probes the variant rows already sitting in the collector
// whose menus have nothing to narrow them.
//
// Every one of the five rows a yt-dlp link expands into is created at stage
// time, and the probe that fills AvailableQualities/AvailableAudioFormats/
// AvailableAudioBitrates (and the real Ext) is fired at that same moment. A row
// staged before that probe existed, or one whose probe never answered, keeps
// those fields empty forever - and empty means "no opinion", so the picker
// falls back to the full static menu. Measured on the live instance
// (2026-09-05): five YouTube rows from August, all five with no probe data, so
// the audio row offered flac for a source that has never had a lossless track
// (jdp: "z.b. flac wir gar nicht von Youtube angeboten"). The narrowing was
// built and correct; there was simply nothing to narrow WITH.
//
// Deliberately not gated on the row's current resolver. Those same five rows
// now read resolver "torbox", because routing is re-decided per attempt while
// the variant family is fixed at stage time - but the question this answers is
// "what does the SOURCE offer", and yt-dlp is what can answer it whichever
// backend ends up doing the fetching.
//
// One probe per distinct URL, because applyProbeFormats already updates every
// URL-sharing sibling, and one at a time: this is a spawn of yt-dlp per link
// against somebody's collector, and a boot that starts forty of them at once
// is a boot that looks like a hang.
func (a *App) backfillYtdlpProbes() {
	// The four extensions that never needed a probe, first and without a
	// network call, so a box with no yt-dlp binary at all still stops showing
	// four of the five rows with no extension.
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
			// A row with no variant is not a yt-dlp family member, and the
			// three fixed-extension kinds have nothing to probe for.
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
			// Same silence as the stage-time probe: a source that cannot be
			// reached right now leaves the menu as wide as it was, which is
			// exactly the state this function found it in.
			continue
		}
		a.applyProbeFormats(u, res.Formats)
	}
}

// applyFixedVariantExts gives every existing variant row the extension
// fixedVariantExt already knows for its kind. Rows created from now on get it
// at expansion time; this is for the ones already sitting in a collector.
//
// Only fills a blank. A row whose extension a probe has since resolved (an
// audio row that came back "m4a" for a "best" preset) knows better than a
// table does, and this must never overwrite that.
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

// formatSize is a FormatEntry's own best available byte count - exact when
// the host reports one, yt-dlp's own estimate otherwise, 0 when neither is
// known.
func formatSize(f ytdlp.FormatEntry) int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}

// bestVideoAtOrUnder is the largest (best-quality) video-only track at or
// under capHeight - the same "closest to the cap from below" choice
// formatSelector's own "<=?H" selector makes for a real download, mirrored
// here so the Size estimate matches what buildArgs would actually pick.
// capHeight <= 0 (this row's own Variant is "best", or nothing was probed)
// picks the single tallest track available, matching "best"'s own
// no-cap meaning.
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
