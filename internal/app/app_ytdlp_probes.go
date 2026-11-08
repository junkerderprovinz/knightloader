package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
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

// LinkMenus is what the browser extension's video menu offers for one link.
type LinkMenus struct {
	// Qualities are the resolution caps that download a track of their own,
	// highest first (see ytdlp.SourceQualities).
	Qualities []string `json:"qualities"`
	// Audio reports whether the source has sound to download on its own.
	Audio bool `json:"audio"`
}

// LinkQualities is what one link offers, from the format list a collector
// probe kept or from the probe its staging shares (linkProbe), so the
// extension's menu lists what the video has and nothing else.
func (a *App) LinkQualities(ctx context.Context, rawurl string) (LinkMenus, error) {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return LinkMenus{}, ErrNotAMediaLink
	}
	if res := a.stagingResolverFor(rawurl); res == nil || res.Info().ID != "ytdlp" {
		return LinkMenus{}, ErrNotAMediaLink
	}
	a.mu.Lock()
	formats, kept := a.probed[rawurl]
	a.mu.Unlock()
	if !kept {
		tp, ok := a.ytdlpTitleProber()
		if !ok {
			return LinkMenus{}, errors.New(ytdlpMissing)
		}
		res, err := a.linkProbe(ctx, tp, rawurl, nil)
		if err != nil {
			return LinkMenus{}, err
		}
		formats = res.Formats
	}
	menus := LinkMenus{Qualities: []string{}}
	for _, q := range ytdlp.SourceQualities(formats) {
		menus.Qualities = append(menus.Qualities, string(q))
	}
	menus.Audio = slices.ContainsFunc(formats, func(f ytdlp.FormatEntry) bool {
		return f.Acodec != "" && f.Acodec != "none"
	})
	return menus, nil
}
