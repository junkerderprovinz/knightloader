package app

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// Variant rows: one pasted yt-dlp link becomes five sibling tasks (video,
// audio, thumbnail, subtitle, description) sharing the URL and package, each
// with its own Enabled switch and, for video and audio, its own quality. The
// host's preset decides which of the five the collector shows; the rest wait
// out of view (core.Task.VariantOff) until the preset lists their kind again.

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
// the instance-wide defaults with the variant and its pick taken from the
// task's own Variant. A pick is a height cap or a probed track on a video row,
// and a target format or a probed track on an audio row.
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
		switch {
		case ytdlp.IsVideoFormat(sub):
			base.VideoFormat = sub
		case sub != "":
			base.Quality = ytdlp.Quality(sub)
		}
	case ytdlp.VariantAudio:
		switch {
		case ytdlp.IsAudioTrack(sub):
			base.AudioTrack = sub
		case sub != "":
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
// regardless of the preset. A row whose kind the host's preset leaves out is
// set aside and switched off rather than left out, so ticking the kind later
// brings it back without pasting the link again. It runs once, right after
// staging, so each sibling takes the folder, category, priority, comment and
// unpacking switch the batch and the Packagizer gave the staged row.
func (a *App) expandYtdlpVariants(primary *core.Task) {
	preset := a.HosterPresetFor(primary.Host)

	a.mu.Lock()
	live := a.tasks[primary.ID]
	if live == nil {
		a.mu.Unlock()
		return
	}
	live.Variant = variantEncode(ytdlp.VariantVideo, string(preset.Quality))
	if !preset.HasVariant(ytdlp.VariantVideo) {
		live.VariantOff = true
		live.Enabled = false
	}
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
			Name:        pc.Name,
			Package:     pc.Package,
			Status:      core.StatusCollected,
			Enabled:     preset.HasVariant(v),
			VariantOff:  !preset.HasVariant(v),
			Source:      pc.Source,
			Origin:      pc.Origin,
			Host:        pc.Host,
			Resolver:    pc.Resolver,
			Variant:     variantEncode(v, sub),
			Ext:         fixedVariantExt(v, sub),
			CreatedAt:   pc.CreatedAt,
			Dir:         pc.Dir,
			Category:    pc.Category,
			Priority:    pc.Priority,
			Comment:     pc.Comment,
			AutoExtract: copyBool(pc.AutoExtract),
		})
	}
}

// fixedVariantExt is the extension a variant row knows without asking the
// source. buildArgs pins thumbnails to jpg and subtitles to srt, a description
// is a text file, audio with an explicit format has that extension, and a
// picked audio track has its codec's. Best audio and video depend on the
// source and wait for the probe. Setting these at creation means a probe that
// never answers still leaves an extension.
func fixedVariantExt(v ytdlp.Variant, sub string) string {
	switch v {
	case ytdlp.VariantThumbnail:
		return "jpg"
	case ytdlp.VariantSubtitle:
		return "srt"
	case ytdlp.VariantDescription:
		return "description"
	case ytdlp.VariantAudio:
		if ytdlp.IsAudioTrack(sub) {
			return ytdlp.AudioTrackExt(sub)
		}
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

// applyProbeFormats applies a completed probe's format list to the family's
// rows still in the collector, and marks every row online, since a probe that
// answered at all proves the source is there. A failed probe never gets here
// and is not read as offline: a timeout or an age gate is not the host saying
// the file is gone.
//
// A row that has started or finished is measured by its own download, so the
// probe leaves its size and extension alone.
//
// The list is kept (App.probed), so a quality picked later is measured
// against it by applyProbeLocked without another probe.
func (a *App) applyProbeFormats(rawurl string, formats []ytdlp.FormatEntry) {
	p := readProbe(formats)
	embedThumbnail := a.Settings.Get().Ytdlp.Embed.Thumbnail

	a.mu.Lock()
	a.keepProbeLocked(rawurl, formats)
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
		if t.Status == core.StatusCollected && applyProbeLocked(t, p, embedThumbnail) {
			changed = true
		}
		if changed {
			touched = append(touched, *t)
		}
	}
	a.mu.Unlock()
	a.saveAndBroadcast(touched)
}

// keepProbeLocked records a link's format list. The map is pruned to the links
// still in the collector once it outgrows maxKeptProbes, which is the only
// place a pick can still change. Caller holds a.mu.
func (a *App) keepProbeLocked(rawurl string, formats []ytdlp.FormatEntry) {
	if a.probed == nil {
		a.probed = map[string][]ytdlp.FormatEntry{}
	}
	a.probed[rawurl] = formats
	if len(a.probed) <= maxKeptProbes {
		return
	}
	live := map[string]bool{}
	for _, t := range a.tasks {
		if t.Status == core.StatusCollected {
			live[t.URL] = true
		}
	}
	for u := range a.probed {
		if !live[u] {
			delete(a.probed, u)
		}
	}
}

// maxKeptProbes is how many links' format lists are kept before the ones that
// have left the collector are dropped.
const maxKeptProbes = 512

// probeFacts is what one probe says about a source, worked out once for every
// row that shares it.
type probeFacts struct {
	formats        []ytdlp.FormatEntry
	maxVideoHeight int
	bestAudio      ytdlp.FormatEntry
	hasBestAudio   bool
	hasVideoOnly   bool
	qualities      []string
	videoFormats   []string
	audioFormats   []string
	audioBitrates  []string
}

func readProbe(formats []ytdlp.FormatEntry) probeFacts {
	p := probeFacts{formats: formats}
	var maxAudioAbr float64
	audioCodecs := make([]string, 0, len(formats))
	for _, f := range formats {
		isVideo := f.Vcodec != "" && f.Vcodec != "none"
		isAudio := f.Acodec != "" && f.Acodec != "none"
		if isVideo && f.Height > p.maxVideoHeight {
			p.maxVideoHeight = f.Height
		}
		if isVideo && !isAudio {
			p.hasVideoOnly = true
		}
		if isAudio && !isVideo {
			audioCodecs = append(audioCodecs, f.Acodec)
			if f.Abr > maxAudioAbr {
				maxAudioAbr = f.Abr
			}
			if !p.hasBestAudio || f.Size() > p.bestAudio.Size() {
				p.bestAudio = f
				p.hasBestAudio = true
			}
		}
	}
	for _, q := range ytdlp.AvailableQualities(p.maxVideoHeight) {
		p.qualities = append(p.qualities, string(q))
	}
	p.videoFormats = ytdlp.VideoFormats(formats)
	// "best", then the source's own tracks, then what they convert to without
	// promising more than the source has.
	p.audioFormats = append([]string{"best"}, ytdlp.AudioTracks(formats)...)
	for _, f := range ytdlp.AvailableAudioFormats(audioCodecs) {
		if f != "best" {
			p.audioFormats = append(p.audioFormats, f)
		}
	}
	p.audioBitrates = ytdlp.AvailableAudioBitrates(maxAudioAbr)
	return p
}

// applyProbeLocked brings one row in line with a probe: its menus, and the
// extension and size of what its kind and pick will download. It reports
// whether anything changed. Caller holds a.mu.
//
// Per kind, matching buildArgs (backend.go):
//   - description: Ext is always "description".
//   - audio: the menu is the source's own tracks plus the formats they convert
//     to. A picked track or "best" is a straight extract, so Ext and Size are
//     that track's; a conversion target gives Ext and an unknown Size, since
//     transcoding changes it.
//   - video: the menus are the height caps up to the tallest track and every
//     distinct track. A picked track gives its own Ext and Size, the Ext
//     following embedThumbnail for a webm track; a cap gives the best track's
//     Size under it, and Ext "mkv" only when a video-only and audio-only pair
//     will be merged.
//   - thumbnail and subtitle: jpg and srt, from the forced conversions.
func applyProbeLocked(t *core.Task, p probeFacts, embedThumbnail bool) bool {
	changed := false
	setExt := func(ext string) {
		if t.Ext != ext {
			t.Ext = ext
			changed = true
		}
	}
	setSize := func(n int64) {
		if t.Size != n {
			t.Size = n
			changed = true
		}
	}
	setMenu := func(menu *[]string, items []string) {
		if !stringSlicesEqual(*menu, items) {
			*menu = items
			changed = true
		}
	}
	kind, sub := variantDecode(t.Variant)
	switch kind {
	case ytdlp.VariantDescription:
		setExt("description")
	case ytdlp.VariantAudio:
		setMenu(&t.AvailableAudioFormats, p.audioFormats)
		setMenu(&t.AvailableAudioBitrates, p.audioBitrates)
		switch {
		case ytdlp.IsAudioTrack(sub):
			if ext := ytdlp.AudioTrackExt(sub); ext != "" {
				setExt(ext)
			}
			if sz := ytdlp.AudioTrackSize(sub, p.formats); sz > 0 {
				setSize(sz)
			}
		case sub != "" && sub != "best":
			setExt(sub)
			setSize(0)
		case p.hasBestAudio:
			setExt(ytdlp.ExtractedExt(p.bestAudio))
			if sz := p.bestAudio.Size(); sz > 0 {
				setSize(sz)
			}
		}
	case ytdlp.VariantVideo:
		setMenu(&t.AvailableQualities, p.qualities)
		setMenu(&t.AvailableVideoFormats, p.videoFormats)
		if ext, size, ok := ytdlp.VideoFormatFile(sub, p.formats, embedThumbnail); ok {
			setExt(ext)
			if size > 0 {
				setSize(size)
			}
			break
		}
		// --merge-output-format mkv only applies to a real merge; a source
		// without a video-only and audio-only pair falls back to one
		// pre-muxed stream.
		if p.hasVideoOnly && p.hasBestAudio {
			setExt("mkv")
		} else {
			setExt("")
		}
		capHeight := p.maxVideoHeight
		if h, ok := ytdlp.HeightCap(ytdlp.Quality(sub)); ok {
			capHeight = h
		}
		if best := bestVideoAtOrUnder(p.formats, capHeight); best != nil && best.Size() > 0 {
			setSize(best.Size())
		}
	case ytdlp.VariantThumbnail:
		// --convert-thumbnails jpg (backend.go).
		setExt("jpg")
	case ytdlp.VariantSubtitle:
		// --sub-format srt (backend.go).
		setExt("srt")
	}
	return changed
}

// reapplyProbeLocked measures a row against its link's kept format list after
// its pick changed, and reports whether that changed anything. A link nobody
// has probed yet keeps what it shows. Caller holds a.mu.
func (a *App) reapplyProbeLocked(t *core.Task) bool {
	formats, ok := a.probed[t.URL]
	if !ok {
		return false
	}
	return applyProbeLocked(t, readProbe(formats), a.Settings.Get().Ytdlp.Embed.Thumbnail)
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
	a.saveAndBroadcast(touched)
}

// applyVariantPresets brings the collector in line with the hoster presets: a
// variant row whose kind its host's preset leaves out is set aside and
// switched off, and a set-aside row whose kind the preset lists again comes
// back switched on, as a fresh paste would stage it. Only rows whose standing
// changes are touched, so a save that changed no preset rewrites nothing and a
// row's own switch holds while its kind stays listed.
//
// Rows that have left the collector are not touched: a preset decides what a
// link collects, not what is already downloading.
func (a *App) applyVariantPresets() {
	presets := map[string]ytdlp.HosterPreset{}
	a.mu.Lock()
	var touched []core.Task
	for _, t := range a.tasks {
		if t.Status != core.StatusCollected || t.Variant == "" {
			continue
		}
		p, ok := presets[t.Host]
		if !ok {
			p = a.HosterPresetFor(t.Host)
			presets[t.Host] = p
		}
		kind, _ := variantDecode(t.Variant)
		off := !p.HasVariant(kind)
		if off == t.VariantOff {
			continue
		}
		t.VariantOff = off
		t.Enabled = !off
		touched = append(touched, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(touched)
}

// variantSiblingsLocked lists the collected rows that share a yt-dlp link with
// a row in ids and are not in ids themselves. Caller holds a.mu.
func (a *App) variantSiblingsLocked(ids []string) []string {
	named := make(map[string]bool, len(ids))
	urls := map[string]bool{}
	for _, id := range ids {
		named[id] = true
		if t := a.tasks[id]; t != nil && t.Variant != "" {
			urls[t.URL] = true
		}
	}
	if len(urls) == 0 {
		return nil
	}
	var out []string
	for id, t := range a.tasks {
		if !named[id] && urls[t.URL] && t.Variant != "" && t.Status == core.StatusCollected {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// withVariantFamilies widens ids to every collected row of the yt-dlp links
// among them. Staging hands back one row per link, the first of its family,
// so a caller acting on what it staged would otherwise leave the audio,
// thumbnail, subtitle and description rows behind in the collector.
func (a *App) withVariantFamilies(ids []string) []string {
	a.mu.Lock()
	more := a.variantSiblingsLocked(ids)
	a.mu.Unlock()
	return slices.Concat(ids, more)
}

// strandedVariantRowsLocked lists the set-aside rows of those links in urls
// that have no row in the collector's view, not counting the rows in leaving.
// Shown again when their kind is ticked, such a row would stand alone for a
// link that was confirmed or removed. Caller holds a.mu.
func (a *App) strandedVariantRowsLocked(urls, leaving map[string]bool) []string {
	shown := map[string]bool{}
	var aside []*core.Task
	for id, t := range a.tasks {
		if !urls[t.URL] || t.Variant == "" || t.Status != core.StatusCollected || leaving[id] {
			continue
		}
		if t.VariantOff {
			aside = append(aside, t)
		} else {
			shown[t.URL] = true
		}
	}
	var out []string
	for _, t := range aside {
		if !shown[t.URL] {
			out = append(out, t.ID)
		}
	}
	sort.Strings(out)
	return out
}

// strandedBy lists the set-aside rows that removing ids would strand; see
// strandedVariantRowsLocked.
func (a *App) strandedBy(ids []string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	leaving := make(map[string]bool, len(ids))
	urls := map[string]bool{}
	for _, id := range ids {
		leaving[id] = true
		if t := a.tasks[id]; t != nil && t.Variant != "" {
			urls[t.URL] = true
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return a.strandedVariantRowsLocked(urls, leaving)
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
