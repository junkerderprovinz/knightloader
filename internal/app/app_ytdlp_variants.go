package app

import (
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
// task's own Variant. A pick is a resolution cap, a probed track or a preset's
// format on a video row, and a format or a probed track on an audio row.
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
		sub, base.VideoConvert = ytdlp.SplitConversion(sub)
		switch {
		case ytdlp.IsVideoPick(sub):
			base.VideoPick = sub
		case sub != "":
			base.Quality = ytdlp.Quality(sub)
		}
		base.NameTag = ytdlp.NameTag(sub, a.otherVideoPicks(t))
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
		if t.AudioLang != "" {
			base.AudioLang = t.AudioLang
		}
	case ytdlp.VariantThumbnail, ytdlp.VariantSubtitle, ytdlp.VariantDescription:
		base.SideTag = t.ID
		if kind == ytdlp.VariantThumbnail {
			// A format yt-dlp cannot write is one the host serves.
			if slices.Contains(ytdlp.ThumbnailFormats, sub) {
				base.ThumbnailFormat = sub
			} else {
				base.ThumbnailAsIs = sub != ""
			}
		}
		if kind == ytdlp.VariantSubtitle && sub != "" {
			p := subtitleDecode(sub)
			base.SubtitleFormat = p.format
			if p.langs != "" {
				base.SubtitleLangs = p.langs
			}
			base.SubtitleAuto = base.SubtitleAuto || p.auto
		}
	}
	return base
}

// HosterPresetFor is the preset a host's links stage with: the one saved for
// the host or for another address of its site (ytdlp.PresetKeys), or
// ytdlp.DefaultHosterPreset.
func (a *App) HosterPresetFor(host string) ytdlp.HosterPreset {
	if _, p, ok := savedPreset(a.Settings.Get().YtdlpPresets, host); ok {
		return p.Sanitize()
	}
	return ytdlp.DefaultHosterPreset()
}

// savedPreset finds the preset host's links stage with and the key it is saved
// under.
func savedPreset(presets map[string]ytdlp.HosterPreset, host string) (string, ytdlp.HosterPreset, bool) {
	for _, k := range ytdlp.PresetKeys(host) {
		if p, ok := presets[k]; ok {
			return k, p, true
		}
	}
	return "", ytdlp.HosterPreset{}, false
}

// SetHosterPreset saves the preset host's links stage with. Where they stage
// with one saved for another address of the site, that one is replaced, so the
// gear on a youtu.be package edits the youtube.com preset rather than starting
// a second one. Otherwise it is saved for the whole site (ytdlp.SitePresetKey),
// so one set from an m.youtube.com package covers youtu.be as well, and a host
// the site table does not know is saved lower-cased and stripped of "www." as
// task hosts are.
//
// It goes through PatchSettings so a concurrent save of another field is not
// clobbered. The presets map itself is still read then written, which only
// matters if two hosts' presets are saved at the same instant.
func (a *App) SetHosterPreset(host string, p ytdlp.HosterPreset) error {
	cur := a.Settings.Get().YtdlpPresets
	key, _, ok := savedPreset(cur, host)
	if !ok {
		key = ytdlp.SitePresetKey(host)
	}
	if key == "" {
		return nil
	}
	presets := make(map[string]ytdlp.HosterPreset, len(cur)+1)
	for k, v := range cur {
		presets[k] = v
	}
	presets[key] = p.Sanitize()
	raw, err := json.Marshal(presets)
	if err != nil {
		return err
	}
	_, err = a.PatchSettings(map[string]json.RawMessage{"ytdlpPresets": raw})
	return err
}

// otherVideoPicks lists the picks of the video rows of t's link other than t,
// leaving out those set aside.
func (a *App) otherVideoPicks(t *core.Task) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, x := range a.tasks {
		if kind, sub := variantDecode(x.Variant); kind == ytdlp.VariantVideo && x.ID != t.ID && x.URL == t.URL && !x.VariantOff {
			out = append(out, sub)
		}
	}
	return out
}

// familyRow is one row a yt-dlp link is staged as: its kind with its pick,
// the audio row's bitrate and language, and whether it is in view.
type familyRow struct {
	kind    ytdlp.Variant
	variant string
	bitrate string
	lang    string
	on      bool
}

// key tells rows of one link apart by what they fetch.
func (r familyRow) key() string { return r.variant + "\x00" + r.bitrate + "\x00" + r.lang }

func rowKey(t *core.Task) string { return t.Variant + "\x00" + t.AudioBitrate + "\x00" + t.AudioLang }

// familyRows lists the rows a link of host is staged as, video first, from
// the host's preset or a choice the link arrived with.
func (a *App) familyRows(host string, choice *ytdlp.LinkChoice) []familyRow {
	preset := a.HosterPresetFor(host)
	var own ytdlp.LinkChoice
	if choice != nil {
		preset = preset.ForChoice(*choice)
		own = *choice
	}
	rows := make([]familyRow, 0, len(ytdlp.Variants()))
	for _, v := range ytdlp.Variants() {
		r := familyRow{kind: v, on: preset.HasVariant(v)}
		sub := ""
		switch {
		case v == ytdlp.VariantVideo:
			sub = preset.VideoPick()
			if own.VideoTrack != "" {
				sub = own.VideoTrack
			}
			if own.VideoConvert != "" {
				sub = ytdlp.ConvertedPick(sub, own.VideoConvert)
			}
		case v == ytdlp.VariantAudio:
			sub, r.bitrate, r.lang = preset.AudioFormat, preset.AudioBitrate, own.AudioLang
			if own.AudioTrack != "" {
				sub = own.AudioTrack
			}
		case v == ytdlp.VariantSubtitle && choice != nil:
			sub = subtitleEncode(own)
		case v == ytdlp.VariantThumbnail && choice != nil:
			sub = own.ThumbnailFormat
			if sub == "" {
				sub = thumbnailAsIs
			}
		}
		r.variant = variantEncode(v, sub)
		rows = append(rows, r)
	}
	return rows
}

// lackedPicks returns the rows the list has of u, by what they fetch, where
// it lacks a row the choice puts in view, and nil where it has none of u or
// every one. A row set aside fetches nothing.
func (a *App) lackedPicks(u string, choice *ytdlp.LinkChoice) map[string]bool {
	key := dedupeURL(u)
	a.mu.Lock()
	have := map[string]bool{}
	for _, t := range a.tasks {
		if t.Variant != "" && !t.VariantOff && dedupeURL(t.URL) == key {
			have[rowKey(t)] = true
			if t.PickSent != "" {
				have[t.PickSent] = true
			}
		}
	}
	a.mu.Unlock()
	if len(have) == 0 {
		return nil
	}
	for _, r := range a.familyRows(torrentHost(u), choice) {
		if r.on && !have[r.key()] {
			return have
		}
	}
	return nil
}

// expandYtdlpVariants turns the task stage created into a full family: it
// becomes the video row, and the other four variants are added as rows
// regardless of the preset. A row whose kind the host's preset leaves out is
// set aside and switched off rather than left out, so ticking the kind later
// brings it back without pasting the link again. It runs once, right after
// staging, so each sibling takes the folder, category, priority, comment and
// unpacking switch the batch and the Packagizer gave the staged row. A choice
// the batch arrived with stands in for the preset for this link alone, and
// the rows in have, which the list already fetches, are set aside too.
func (a *App) expandYtdlpVariants(primary *core.Task, choice *ytdlp.LinkChoice, have map[string]bool) {
	rows := a.familyRows(primary.Host, choice)
	video := rows[0]

	a.mu.Lock()
	live := a.tasks[primary.ID]
	if live == nil {
		a.mu.Unlock()
		return
	}
	live.Variant = video.variant
	live.PickSent = video.key()
	if _, sub := variantDecode(video.variant); fixedVariantExt(ytdlp.VariantVideo, sub) != "" {
		live.Ext = fixedVariantExt(ytdlp.VariantVideo, sub)
	}
	if !video.on || have[video.key()] {
		live.VariantOff = true
		live.Enabled = false
	}
	pc := a.copyLocked(live)
	a.mu.Unlock()
	a.publish(&pc)

	for _, r := range rows[1:] {
		on := r.on && !have[r.key()]
		_, sub := variantDecode(r.variant)
		a.insertVariantSibling(&core.Task{
			URL: pc.URL,
			// pc.Name rather than the URL: a playlist entry is already named by
			// the listing, and setTaskName does not rename siblings of a primary
			// that already has a name.
			Name:         pc.Name,
			Package:      pc.Package,
			Status:       core.StatusCollected,
			Enabled:      on,
			VariantOff:   !on,
			Source:       pc.Source,
			Origin:       pc.Origin,
			Host:         pc.Host,
			Resolver:     pc.Resolver,
			ResolverPin:  pc.ResolverPin,
			Variant:      r.variant,
			PickSent:     r.key(),
			AudioBitrate: r.bitrate,
			AudioLang:    r.lang,
			Ext:          fixedVariantExt(r.kind, sub),
			CreatedAt:    pc.CreatedAt,
			Dir:          pc.Dir,
			Category:     pc.Category,
			Priority:     pc.Priority,
			Comment:      pc.Comment,
			AutoExtract:  copyBool(pc.AutoExtract),
		})
	}
}

// thumbnailAsIs is a thumbnail row's pick for the picture as the host serves
// it, which a link sent with a choice of its own takes unless it names a
// format. A thumbnail row's other picks are the format it comes in.
const thumbnailAsIs = "source"

// subtitlePick is what a subtitle row stores as its pick: the format the
// host serves the subtitles in ("best" for the host's own), the languages,
// empty for the instance's, and whether generated ones count.
type subtitlePick struct {
	format string
	langs  string
	auto   bool
}

// subtitleEncode is a choice's subtitle pick as the row stores it: the
// format, then the languages and "auto" as further words.
func subtitleEncode(c ytdlp.LinkChoice) string {
	words := []string{c.SubtitleFormat}
	if c.SubtitleFormat == "" {
		words[0] = "best"
	}
	if len(c.SubtitleLangs) > 0 {
		words = append(words, strings.Join(c.SubtitleLangs, ","))
	}
	if c.SubtitleAuto {
		words = append(words, "auto")
	}
	return strings.Join(words, " ")
}

func subtitleDecode(sub string) subtitlePick {
	words := strings.Fields(sub)
	p := subtitlePick{format: words[0]}
	for _, w := range words[1:] {
		if w == "auto" {
			p.auto = true
		} else {
			p.langs = w
		}
	}
	return p
}

// fixedVariantExt is the extension a variant row knows without asking the
// source. buildArgs converts thumbnails to jpg or the format picked and
// subtitles to srt, a description
// is a text file, and audio in a named format or a picked track is written
// with that codec's. Best audio and video depend on the source and wait for
// the probe. Setting these at creation means a probe that never answers still
// leaves an extension.
func fixedVariantExt(v ytdlp.Variant, sub string) string {
	switch v {
	case ytdlp.VariantThumbnail:
		switch sub {
		case "":
			return "jpg"
		case thumbnailAsIs:
			return ""
		}
		return sub
	case ytdlp.VariantSubtitle:
		if sub == "" {
			return "srt"
		}
		if f := subtitleDecode(sub).format; f != "best" {
			return f
		}
		return ""
	case ytdlp.VariantDescription:
		return "description"
	case ytdlp.VariantVideo:
		if _, convert := ytdlp.SplitConversion(sub); convert != "" {
			return "mkv"
		}
	case ytdlp.VariantAudio:
		if ytdlp.IsAudioTrack(sub) {
			return ytdlp.AudioTrackExt(sub)
		}
		return ytdlp.AudioFormatExt(sub)
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
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
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
	yt := a.Settings.Get().Ytdlp

	a.mu.Lock()
	a.keepProbeLocked(rawurl, formats)
	var touched []taskCopy
	for _, t := range a.tasks {
		if t.URL != rawurl {
			continue
		}
		changed := t.ProbeError != ""
		t.ProbeError = ""
		if t.Online != core.AvailOnline {
			t.Online = core.AvailOnline
			if t.Status != core.StatusError {
				t.SetError("", "", nil)
				t.Reason = ""
			}
			changed = true
		}
		if t.Status == core.StatusCollected && applyProbeLocked(t, p, yt) {
			changed = true
		}
		if changed {
			touched = append(touched, a.copyLocked(t))
		}
	}
	a.mu.Unlock()
	a.publishTasks(touched)
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
	formats       []ytdlp.FormatEntry
	qualities     []string
	videoFormats  []string
	videoTracks   []string
	audioFormats  []string
	audioTracks   []string
	audioBitrates []string
}

func readProbe(formats []ytdlp.FormatEntry) probeFacts {
	var maxAbr float64
	for _, f := range formats {
		if (f.Vcodec == "" || f.Vcodec == "none") && f.Abr > maxAbr {
			maxAbr = f.Abr
		}
	}
	p := probeFacts{
		formats:       formats,
		qualities:     []string{string(ytdlp.QualityBest)},
		videoFormats:  ytdlp.VideoContainers(formats),
		videoTracks:   ytdlp.VideoTracks(formats),
		audioFormats:  ytdlp.AudioFormatsOf(formats),
		audioTracks:   ytdlp.AudioTracks(formats),
		audioBitrates: ytdlp.AvailableAudioBitrates(maxAbr),
	}
	for _, q := range ytdlp.SourceQualities(formats) {
		p.qualities = append(p.qualities, string(q))
	}
	return p
}

// applyProbeLocked brings one row in line with a probe: its menus, a preset's
// pick resolved to one of the source's tracks, and the extension and size of
// what its kind and pick will download. yt is the instance's yt-dlp settings.
// It reports whether anything changed. Caller holds a.mu.
//
// Per kind, matching buildArgs (backend.go):
//   - description: Ext is always "description".
//   - audio: the menus are the formats the source's audio comes in and every
//     track in them. A track, a format the source has or "best" is copied, so
//     Ext and Size are that track's; a format it lacks is converted to, which
//     gives Ext and an unknown Size.
//   - video: the menus are the resolution caps up to the source's own, the
//     formats, and every track. A track gives its own Ext, following
//     Embed.Thumbnail for a webm one, and the Size of it and the audio merged
//     with it; a cap or best gives the Size of what yt-dlp would take, and a
//     custom format string gives neither.
//   - thumbnail and subtitle: jpg and srt, from the forced conversions.
func applyProbeLocked(t *core.Task, p probeFacts, yt ytdlp.Options) bool {
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
	setPick := func(pick string) {
		if pick != sub {
			t.Variant = variantEncode(kind, pick)
			changed = true
		}
	}
	switch kind {
	case ytdlp.VariantDescription:
		setExt("description")
	case ytdlp.VariantAudio:
		setMenu(&t.AvailableAudioFormats, p.audioFormats)
		setMenu(&t.AvailableAudioTracks, p.audioTracks)
		setMenu(&t.AvailableAudioBitrates, p.audioBitrates)
		pick, bitrate := ytdlp.ResolveAudioPick(sub, t.AudioBitrate, p.formats)
		setPick(pick)
		if t.AudioBitrate != bitrate {
			t.AudioBitrate = bitrate
			changed = true
		}
		ext, size := ytdlp.AudioFile(pick, p.formats)
		setExt(ext)
		setSize(size)
	case ytdlp.VariantVideo:
		setMenu(&t.AvailableQualities, p.qualities)
		setMenu(&t.AvailableVideoFormats, p.videoFormats)
		setMenu(&t.AvailableVideoTracks, p.videoTracks)
		pick := ytdlp.ResolveVideoPick(sub, p.formats)
		setPick(pick)
		ext, size := ytdlp.VideoFile(pick, p.formats, yt)
		setExt(ext)
		setSize(size)
	case ytdlp.VariantThumbnail, ytdlp.VariantSubtitle:
		// The conversion or the format the row asked for (backend.go).
		if ext := fixedVariantExt(kind, sub); ext != "" {
			setExt(ext)
		}
	}
	return changed
}

// reapplyProbeLocked measures a row against its link's kept format list after
// its pick changed. It reports false when no list is kept, as after a restart:
// the extension and size the row shows then belong to its old pick, so they
// are cleared for the caller's fresh probe (reprobeYtdlp) to fill in. Caller
// holds a.mu.
func (a *App) reapplyProbeLocked(t *core.Task) bool {
	formats, ok := a.probed[t.URL]
	if !ok {
		kind, sub := variantDecode(t.Variant)
		t.Ext = fixedVariantExt(kind, sub)
		t.Size = 0
		return false
	}
	applyProbeLocked(t, readProbe(formats), a.Settings.Get().Ytdlp)
	return true
}

// reprobeYtdlp asks yt-dlp for a link's formats again and applies them to its
// rows.
func (a *App) reprobeYtdlp(rawurl string) {
	if tp, ok := a.ytdlpTitleProber(); ok {
		a.probeOnce(tp, rawurl)
	}
}

// probeOnce probes one link and applies its formats, unless a probe of that
// link is already under way. The wheel on a picker sends a change per notch,
// and the boot's backfill can reach a link a pick is being measured for; the
// probe that answers measures whatever pick the rows hold by then, so a second
// yt-dlp process for the same link learns nothing more.
func (a *App) probeOnce(tp titleProber, rawurl string) {
	a.mu.Lock()
	if a.reprobing[rawurl] {
		a.mu.Unlock()
		return
	}
	if a.reprobing == nil {
		a.reprobing = map[string]bool{}
	}
	a.reprobing[rawurl] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.reprobing, rawurl)
		a.mu.Unlock()
	}()
	res, err := a.linkProbe(a.ctx, tp, rawurl, a.browserHeadersForLink(rawurl))
	if err != nil {
		a.failProbe(rawurl, err.Error())
		return
	}
	a.applyProbeFormats(rawurl, res.Formats)
}

// backfillYtdlpProbes probes collector rows whose format menus the pickers
// cannot read, because their probe never answered or filled them in another
// shape (pickerMenusRead). Until it answers, their pickers wait for it. Without
// yt-dlp the rows are told why they offer best alone.
//
// It does not check the row's current resolver: routing is decided per attempt,
// but the question is what the source offers, which only yt-dlp can answer.
// It probes one distinct URL at a time, since applyProbeFormats updates every
// sibling and forty yt-dlp processes at boot would look like a hang.
func (a *App) backfillYtdlpProbes() {
	// The fixed extensions first and without a network call, so a box without
	// yt-dlp still shows them.
	a.applyFixedVariantExts()
	a.mu.Lock()
	var urls []string
	seen := make(map[string]bool)
	for _, t := range a.tasks {
		if t.Status != core.StatusCollected || t.URL == "" || seen[t.URL] {
			continue
		}
		kind, _ := variantDecode(t.Variant)
		switch kind {
		case ytdlp.VariantVideo, ytdlp.VariantAudio:
			if pickerMenusRead(t, kind) {
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

	tp, ok := a.ytdlpTitleProber()
	if !ok {
		for _, u := range urls {
			a.failProbe(u, ytdlpMissing)
		}
		return
	}
	for _, u := range urls {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		a.probeOnce(tp, u)
	}
}

// pickerMenusRead reports whether a video or audio row holds format lists a
// probe filled in the shape its pickers read: "best" first, then formats. A
// stored row can carry the one-menu shape instead, tracks and formats mixed
// ("m4a 129k" beside "aac"), which the format picker would offer as formats.
// It can also name AAC "m4a" after its file, and its tracks with it.
func pickerMenusRead(t *core.Task, kind ytdlp.Variant) bool {
	formats := t.AvailableVideoFormats
	if kind == ytdlp.VariantAudio {
		formats = t.AvailableAudioFormats
	}
	if len(formats) == 0 || formats[0] != string(ytdlp.QualityBest) {
		return false
	}
	return !slices.ContainsFunc(formats, func(f string) bool {
		return f == "m4a" || ytdlp.IsVideoTrack(f) || ytdlp.IsAudioTrack(f)
	})
}

// HosterFormats is what a host's preset offers: the formats the site is known
// to serve and those that probes of its links found, from the kept format lists
// and from the menus of every variant row of the host, finished ones included,
// since those outlast a restart (see ytdlp.HostFormats).
func (a *App) HosterFormats(host string) ytdlp.HostMenus {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	var video, audio []string
	probed := false
	a.mu.Lock()
	for u, formats := range a.probed {
		if hostOf(u) == host {
			video = append(video, ytdlp.VideoContainers(formats)...)
			audio = append(audio, ytdlp.AudioFormatsOf(formats)...)
			probed = true
		}
	}
	for _, t := range a.tasks {
		if t.Host != host || t.Variant == "" {
			continue
		}
		if len(t.AvailableVideoFormats) > 0 || len(t.AvailableAudioFormats) > 0 {
			video = append(video, t.AvailableVideoFormats...)
			audio = append(audio, t.AvailableAudioFormats...)
			probed = true
		}
	}
	a.mu.Unlock()
	return ytdlp.HostFormats(host, video, audio, probed)
}

// applyFixedVariantExts gives existing variant rows the extension
// fixedVariantExt knows for their kind. It only fills blanks, since a probed
// extension (an audio row resolved to m4a) is more accurate than the table.
func (a *App) applyFixedVariantExts() {
	a.mu.Lock()
	var touched []taskCopy
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
		touched = append(touched, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(touched)
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
	var touched []taskCopy
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
		touched = append(touched, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(touched)
}

// variantSiblingsLocked lists the collected rows of the yt-dlp families of the
// rows in ids that are not in ids themselves. A family is the rows one link
// was staged as together, so a link sent again for another pick leaves the
// rows it already had alone. Caller holds a.mu.
func (a *App) variantSiblingsLocked(ids []string) []string {
	type family struct {
		url    string
		staged int64
	}
	named := make(map[string]bool, len(ids))
	families := map[family]bool{}
	for _, id := range ids {
		named[id] = true
		if t := a.tasks[id]; t != nil && t.Variant != "" {
			families[family{t.URL, t.CreatedAt.UnixNano()}] = true
		}
	}
	if len(families) == 0 {
		return nil
	}
	var out []string
	for id, t := range a.tasks {
		if !named[id] && families[family{t.URL, t.CreatedAt.UnixNano()}] && t.Variant != "" && t.Status == core.StatusCollected {
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
