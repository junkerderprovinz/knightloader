package app

import (
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
			CreatedAt: pc.CreatedAt,
		})
	}
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
// (backend.go) exactly - honestly answerable ahead of time or not:
//   - description: Ext is always "description" - no probe maths needed.
//   - audio, a FIXED format (mp3/m4a/opus/wav/flac): Ext is that format
//     itself (deterministic - ffmpeg's own --audio-format target) - but
//     not Size, since transcoding changes the byte count unpredictably
//     from the source track's own.
//   - audio, "best" (-x with no --audio-format - a straight extract, not a
//     transcode): Size from the best-matching source audio-only track's
//     own filesize, a close estimate since nothing re-encodes it - but not
//     Ext, since the container it keeps varies by source (m4a/webm/opus…).
//   - video: AvailableQualities from every real video-track height found
//     (ytdlp.AvailableQualities), and Size from the best-matching
//     video-only track at or under this row's own currently-picked quality
//     cap - not Ext, since a muxed video+audio file's real container
//     depends on which two streams actually get picked and merged.
//   - thumbnail/subtitle: neither - format varies by site/track in ways
//     nothing here predicts.
func (a *App) applyProbeFormats(rawurl string, formats []ytdlp.FormatEntry) {
	var maxVideoHeight int
	var bestAudio ytdlp.FormatEntry
	hasBestAudio := false
	for _, f := range formats {
		isVideo := f.Vcodec != "" && f.Vcodec != "none"
		isAudio := f.Acodec != "" && f.Acodec != "none"
		if isVideo && f.Height > maxVideoHeight {
			maxVideoHeight = f.Height
		}
		if isAudio && !isVideo && (!hasBestAudio || formatSize(f) > formatSize(bestAudio)) {
			bestAudio = f
			hasBestAudio = true
		}
	}
	qualities := ytdlp.AvailableQualities(maxVideoHeight)
	availableQualities := make([]string, len(qualities))
	for i, q := range qualities {
		availableQualities[i] = string(q)
	}

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
			if sub != "" && sub != "best" {
				if t.Ext != sub {
					t.Ext = sub
					changed = true
				}
			} else if hasBestAudio {
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
