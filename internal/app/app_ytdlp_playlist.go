package app

// A playlist link becomes the videos in it: one task per entry, in one package
// named after the playlist.
//
// This is built like the page crawl, which expands one pasted link before any
// task is staged, rather than like the async title probe. Staging the playlist
// link first and replacing it later would not work: with AutoConfirm on,
// AddLinksFrom can start what it staged before returning, so the whole playlist
// would begin downloading into one row. When the listing yields no entries the
// link is staged as itself. The title probe still runs per entry afterwards for
// formats and availability (probePlaylistEntries).

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// maxPlaylistEntries is the most videos one playlist link may put in the
// collector.
//
// The limit is really about rows: each yt-dlp link expands into its family of
// five variant rows (expandYtdlpVariants), and the collector renders every row
// without windowing. A channel's full upload list would be tens of thousands.
// Entries past the limit are reported in the skipped-links trace rather than
// dropped silently.
const maxPlaylistEntries = 100

// ytdlpPlaylistTimeout bounds one flat listing, and so how long a paste can be
// held up by it. It is longer than the page crawl's thirty seconds because a
// long listing is a bigger answer to build.
const ytdlpPlaylistTimeout = 60 * time.Second

// playlistProber is implemented by a backend that can list what a link points
// at without downloading it. Only yt-dlp can.
type playlistProber interface {
	ProbePlaylist(ctx context.Context, url string) (ytdlp.Playlist, error)
}

// ytdlpPlaylistProber returns the yt-dlp backend as a playlistProber, and
// whether it is one. A switched-off yt-dlp is none.
func (a *App) ytdlpPlaylistProber() (playlistProber, bool) {
	if a.resolverOff("ytdlp") {
		return nil, false
	}
	a.bmu.RLock()
	b := a.ytdlp
	a.bmu.RUnlock()
	p, ok := b.(playlistProber)
	return p, ok
}

// ytdlpPlaylist asks yt-dlp what a link lists, and reports whether the answer
// should be expanded into tasks. It is gated like crawl.
//
// It returns false whenever the link should be staged as one: the setting is
// off, the link is not yt-dlp's, no yt-dlp backend is wired, the listing
// failed, or it is not a playlist. Each of those keeps the single-link
// behaviour.
func (a *App) ytdlpPlaylist(u string) (ytdlp.Playlist, bool) {
	// The same setting the yt-dlp backend reads for --no-playlist, so the
	// question "is a playlist link a list" has one answer on an install.
	if !a.Settings.Get().Ytdlp.Playlist {
		return ytdlp.Playlist{}, false
	}
	// Only links staging routes to yt-dlp, in the order the priority card set,
	// so a link a debrid or JD backend takes never reaches a yt-dlp process.
	if res := a.stagingResolverFor(u); res == nil || res.Info().ID != "ytdlp" {
		return ytdlp.Playlist{}, false
	}
	pp, ok := a.ytdlpPlaylistProber()
	if !ok {
		return ytdlp.Playlist{}, false
	}
	// Shown as a crawl in the activity strip, begun after the instant gates so
	// they do not flash it.
	a.beginActivity(ActivityCrawl, 1)
	defer a.endActivity(ActivityCrawl, 1)
	ctx, cancel := context.WithTimeout(context.Background(), ytdlpPlaylistTimeout)
	defer cancel()
	pl, err := pp.ProbePlaylist(ctx, u)
	if err != nil {
		// The link is staged as itself instead.
		log.Printf("playlist listing %s: %v", u, err)
		return ytdlp.Playlist{}, false
	}
	if len(pl.Entries) == 0 {
		return ytdlp.Playlist{}, false
	}
	return pl, true
}

// stagePlaylistEntries turns one listing into one task per entry.
//
// Every entry goes through stage, so the filter, mirror set, Packagizer,
// resolver and variant expansion apply as they do to a pasted link. Duplicates
// are folded by the mirror set there and reported in the skipped trace.
//
// pkg is what the paste asked for, empty when it asked for nothing.
func (a *App) stagePlaylistEntries(playlistURL string, pl ytdlp.Playlist, pkg string, batch LinkBatchOptions) []*core.Task {
	entries := pl.Entries
	if len(entries) > maxPlaylistEntries {
		a.recordSkippedReason(playlistURL, "playlist", fmt.Sprintf(
			"this playlist lists %d videos; the first %d were staged and the other %d were left out",
			len(entries), maxPlaylistEntries, len(entries)-maxPlaylistEntries))
		entries = entries[:maxPlaylistEntries]
	}
	if pl.Dropped > 0 {
		a.recordSkippedReason(playlistURL, "playlist", fmt.Sprintf(
			"%d of this playlist's entries name nothing this app can open (a removed video, or a playlist inside the playlist) and were left out",
			pl.Dropped))
	}

	// A name typed into the paste box wins; otherwise the playlist's title,
	// which is why the listing uses -J rather than -j. Without either,
	// addLinksFrom's naming pass derives one from the entries.
	entryPkg := strings.TrimSpace(pkg)
	if entryPkg == "" && pl.Title != "" {
		entryPkg = sanitizeSegment(pl.Title)
	}

	created := make([]*core.Task, 0, len(entries))
	probes := make([]probeTarget, 0, len(entries))
	for _, e := range entries {
		t := a.stage(e.URL, e.Title, 0, intake{
			pkg: entryPkg,
			// OriginCrawl, as for crawled links: nobody typed this one. Source
			// records the playlist, for rules and for ytdlpOptionsForTask, which
			// downloads an entry as a single video.
			origin:        OriginCrawl,
			source:        playlistURL,
			priority:      batch.Priority,
			autoExtract:   batch.AutoExtract,
			comment:       batch.Comment,
			playlistEntry: true,
		})
		if t == nil {
			continue
		}
		created = append(created, t)
		// Only rows yt-dlp downloads, as stage probes only those.
		if !t.Skipped && t.Resolver == "ytdlp" {
			probes = append(probes, probeTarget{id: t.ID, url: e.URL})
		}
	}
	if len(probes) > 0 {
		a.spawn(func() { a.probePlaylistEntries(probes) })
	}
	return created
}

// probeTarget is one row waiting for its own format probe.
type probeTarget struct {
	id  string
	url string
}

// probePlaylistEntries fills in each video's formats, extension, size and
// availability (applyProbeFormats).
//
// It probes one at a time: a playlist can yield a hundred links, and the usual
// per-link probe would start a hundred yt-dlp processes against one site, the
// stampede backfillYtdlpProbes also avoids. The rows already carry their names
// from the listing, so nothing waits on this.
func (a *App) probePlaylistEntries(targets []probeTarget) {
	for _, t := range targets {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		a.probeYtdlpTitle(t.id, t.url)
	}
}
