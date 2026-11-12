package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// probeCall is one yt-dlp probe of a link. done closes once res, err and at
// are set.
type probeCall struct {
	done chan struct{}
	res  ytdlp.ProbeResult
	err  error
	at   time.Time
}

// stale reports an answered probe that the next caller should not take: one
// that failed, or one older than probeKeep.
func (c *probeCall) stale() bool {
	select {
	case <-c.done:
		return c.err != nil || time.Since(c.at) > probeKeep
	default:
		return false
	}
}

// probeKeep is how long an answer stands in for a fresh probe. It covers the
// extension's menu asking about a video and the video arriving a moment
// later, and the menu opened again.
const probeKeep = 10 * time.Minute

// ytdlpMissing is the reason a link's pickers give when there is no yt-dlp to
// ask.
const ytdlpMissing = "yt-dlp is not available on this instance"

// linkProbe asks yt-dlp about a link once for everyone who wants to know
// within probeKeep: the staging, a pick measured again, the boot's backfill
// and the extension's menu. ctx bounds the wait and not the probe, which
// others may be waiting on too.
func (a *App) linkProbe(ctx context.Context, tp titleProber, rawurl string, sent map[string]string) (ytdlp.ProbeResult, error) {
	a.mu.Lock()
	c := a.linkProbes[rawurl]
	if c == nil || c.stale() {
		c = &probeCall{done: make(chan struct{})}
		if a.linkProbes == nil {
			a.linkProbes = map[string]*probeCall{}
		}
		for u, old := range a.linkProbes {
			if old.stale() {
				delete(a.linkProbes, u)
			}
		}
		a.linkProbes[rawurl] = c
		a.mu.Unlock()
		a.runProbe(c, tp, rawurl, sent)
	} else {
		a.mu.Unlock()
	}
	select {
	case <-c.done:
		return c.res, c.err
	case <-ctx.Done():
		return ytdlp.ProbeResult{}, ctx.Err()
	}
}

func (a *App) runProbe(c *probeCall, tp titleProber, rawurl string, sent map[string]string) {
	if !a.track() {
		c.err = errors.New("the instance is shutting down")
		close(c.done)
		return
	}
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(a.ctx, ytdlpProbeTimeout)
		defer cancel()
		c.res, c.err = tp.ProbeTitle(ctx, rawurl, sent)
		if c.err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.err = fmt.Errorf("yt-dlp did not answer within %s", ytdlpProbeTimeout)
		}
		c.at = time.Now()
		close(c.done)
	}()
}

// failProbe leaves reason on the video and audio rows of a collected link
// that have no menus, so their pickers offer best alone and say why. A row
// that kept the menus of an earlier probe goes on offering them.
func (a *App) failProbe(rawurl, reason string) {
	// A probe the shutdown cut short says nothing about the source.
	if a.ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	var touched []taskCopy
	for _, t := range a.tasks {
		if t.URL != rawurl || t.Status != core.StatusCollected || t.ProbeError == reason {
			continue
		}
		kind, _ := variantDecode(t.Variant)
		if (kind != ytdlp.VariantVideo && kind != ytdlp.VariantAudio) || pickerMenusRead(t, kind) {
			continue
		}
		t.ProbeError = reason
		touched = append(touched, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(touched)
}

// ErrNotAMediaLink is LinkQualities' answer for an address yt-dlp does not
// fetch.
var ErrNotAMediaLink = errors.New("yt-dlp does not fetch this address")

// LinkMenus is what the browser extension's video menu offers for one link:
// for every row, what the host has.
type LinkMenus struct {
	// Qualities are the resolution caps that download a track of their own,
	// highest first (see ytdlp.SourceQualities).
	Qualities []string `json:"qualities"`
	// Audio reports whether the source has sound to download on its own.
	Audio bool `json:"audio"`
	// VideoTracks and AudioTracks are the tracks the rows can pick, as
	// ytdlp.VideoTracks and ytdlp.AudioTracks key them.
	VideoTracks []string `json:"videoTracks"`
	AudioTracks []string `json:"audioTracks"`
	// VideoSizes and AudioSizes are the bytes each pick of the two rows
	// downloads, as the collector predicts them (ytdlp.VideoFile,
	// ytdlp.AudioFile): every track, cap and codec, and "best". A pick the
	// host gave no size or bitrate for is left out.
	VideoSizes map[string]int64 `json:"videoSizes"`
	AudioSizes map[string]int64 `json:"audioSizes"`
	// AudioLangs are the languages the audio comes in, originals first.
	AudioLangs []LinkLang `json:"audioLangs"`
	// Subtitles and AutoCaptions are the subtitle languages by hand and
	// generated, and SubtitleFormats what the host serves them as.
	Subtitles       []string `json:"subtitles"`
	AutoCaptions    []string `json:"autoCaptions"`
	SubtitleFormats []string `json:"subtitleFormats"`
	// VideoConversions are the formats the video row can convert its file
	// to after the download.
	VideoConversions []LinkConversion `json:"videoConversions"`
	// Thumbnail is the picture the thumbnail row fetches, nil for none.
	Thumbnail *LinkThumbnail `json:"thumbnail"`
	// Description reports whether there is a description to write.
	Description bool `json:"description"`
}

// LinkConversion is a format the video row converts to, Converted since
// KnightLoader makes it rather than the host serving it.
type LinkConversion struct {
	Format    string `json:"format"`
	Converted bool   `json:"converted"`
}

// LinkLang is one language of a source's audio, Dubbed for an automatic dub.
type LinkLang struct {
	Code   string `json:"code"`
	Dubbed bool   `json:"dubbed"`
}

// LinkThumbnail is a picture's size, 0 where the host does not say, its
// format, and the formats the thumbnail row can fetch it in.
type LinkThumbnail struct {
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Ext     string   `json:"ext"`
	Formats []string `json:"formats"`
}

// LinkQualities is what one link offers, from the probe its staging shares
// (linkProbe), so the extension's menu lists what the video has and nothing
// else. pinned asks as for a link pinned to yt-dlp, past the chain, the way
// the extension's button sends its video.
func (a *App) LinkQualities(ctx context.Context, rawurl string, pinned bool) (LinkMenus, error) {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return LinkMenus{}, ErrNotAMediaLink
	}
	pin := ""
	if pinned {
		pin = "ytdlp"
	}
	if res := a.stagingResolverFor(rawurl, pin); res == nil || res.Info().ID != "ytdlp" {
		return LinkMenus{}, ErrNotAMediaLink
	}
	tp, ok := a.ytdlpTitleProber()
	if !ok {
		return LinkMenus{}, errors.New(ytdlpMissing)
	}
	res, err := a.linkProbe(ctx, tp, rawurl, nil)
	if err != nil {
		return LinkMenus{}, err
	}
	formats := res.Formats
	subs := ytdlp.AvailableSubtitleLangs(res)
	menus := LinkMenus{
		Qualities:       []string{},
		VideoTracks:     orEmpty(ytdlp.VideoTracks(formats)),
		AudioTracks:     orEmpty(ytdlp.AudioTracks(formats)),
		AudioLangs:      []LinkLang{},
		Subtitles:       orEmpty(subs.Manual),
		AutoCaptions:    orEmpty(subs.Auto),
		SubtitleFormats: orEmpty(res.SubtitleFormats),
		Description:     res.Description,
	}
	for _, q := range ytdlp.SourceQualities(formats) {
		menus.Qualities = append(menus.Qualities, string(q))
	}
	menus.VideoConversions = []LinkConversion{}
	if len(menus.Qualities) > 0 {
		menus.VideoConversions = append(menus.VideoConversions, LinkConversion{Format: ytdlp.ConvertHEVC, Converted: true})
	}
	yt := a.Settings.Get().Ytdlp
	menus.VideoSizes = pickSizes(slices.Concat([]string{"best"}, menus.Qualities, menus.VideoTracks), func(pick string) int64 {
		_, size := ytdlp.VideoFile(pick, formats, yt)
		return size
	})
	codecs := []string{"best"}
	for _, k := range menus.AudioTracks {
		if c, _, _ := strings.Cut(k, " "); !slices.Contains(codecs, c) {
			codecs = append(codecs, c)
		}
	}
	menus.AudioSizes = pickSizes(slices.Concat(codecs, menus.AudioTracks), func(pick string) int64 {
		_, size := ytdlp.AudioFile(pick, formats)
		return size
	})
	menus.Audio = slices.ContainsFunc(formats, func(f ytdlp.FormatEntry) bool {
		return f.Acodec != "" && f.Acodec != "none"
	})
	for _, l := range ytdlp.AvailableAudioLangs(formats) {
		menus.AudioLangs = append(menus.AudioLangs, LinkLang{Code: l.Code, Dubbed: l.Dubbed})
	}
	if n := len(res.Thumbnails); n > 0 {
		th := res.Thumbnails[n-1]
		formats := slices.Clone(ytdlp.ThumbnailFormats)
		if th.Ext != "" && !slices.Contains(formats, th.Ext) {
			formats = append(formats, th.Ext)
		}
		menus.Thumbnail = &LinkThumbnail{Width: th.Width, Height: th.Height, Ext: th.Ext, Formats: formats}
	}
	return menus, nil
}

// pickSizes maps each pick to the bytes it downloads, leaving out those
// nothing is known of.
func pickSizes(picks []string, size func(string) int64) map[string]int64 {
	out := map[string]int64{}
	for _, p := range picks {
		if n := size(p); n > 0 {
			out[p] = n
		}
	}
	return out
}

// orEmpty is an empty list for nil, so the answer has [] where a client
// reads a list.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
