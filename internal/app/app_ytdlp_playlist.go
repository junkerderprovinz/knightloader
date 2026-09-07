package app

// app_ytdlp_playlist.go: a playlist link becomes the videos in it - one task
// per entry, all in one package named after the playlist.
//
// WHY THIS SITS BESIDE THE CRAWL AND NOT BESIDE THE TITLE PROBE. Two existing
// mechanisms answer nearly this question and only one of them has the right
// shape:
//
//   - The async title probe (mediaProbePending/awaitingMediaProbe,
//     app_links.go, and probeYtdlpTitle, app_tasks.go) finds something out
//     about a link AFTER it is staged. That is the right shape for a name,
//     which only decorates a row that already exists, and the wrong shape for
//     this: the playlist link would have to be staged as a task first and then
//     taken away again once the listing came back. On an install with
//     AutoConfirm on, staging a task IS starting it - addLinksFrom hands
//     everything it created straight to ConfirmTasks before it returns - so
//     that task would have started downloading the whole playlist into one row
//     a heartbeat before the listing that was meant to replace it arrived,
//     which is the exact bug this feature exists to end. It is also the same
//     "the selection has to be on the task before put() runs" lesson
//     app_torrents.go's own package comment already records.
//   - The page crawl (crawl/addLinksFrom, app_links.go) turns ONE pasted link
//     into the many it points at, before any of them is staged, naming the
//     batch after the page. That is exactly this, so this is built the same
//     way and in the same place: a listing is fetched, and either it yields
//     entries - in which case the link itself never becomes a task at all - or
//     it does not and the link is staged as itself, exactly as before.
//
// The title probe is still reused, per entry, for what it is actually good at:
// filling in each video's real formats and availability once the rows are
// there (probePlaylistEntries below).

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
// The number is about ROWS, not about videos. Every staged yt-dlp link expands
// into the five "Variante" rows of its own family (expandYtdlpVariants,
// app_ytdlp_variants.go), so a hundred entries is up to five hundred rows -
// and the collector renders every one of them, with no windowing anywhere in
// the list (web/src/components). Several hundred rows is what a large paste
// already produces today, so it is the size this list is already asked to
// handle; several thousand is not, and a channel's "all uploads" (tens of
// thousands of entries) would hand it exactly that from one pasted line.
//
// A hundred also sits above what people actually paste a playlist FOR - an
// album, a season, a course, a mix - so the limit is reached by the cases that
// were never going to be one collector's worth of work in the first place. It
// is a truncation and not a refusal, and it is SAID OUT LOUD rather than
// quietly applied: the first hundred are staged and the rest are reported
// through the same skipped-links trace a folded duplicate uses, because a
// paste that silently produces fewer links than the source had is the exact
// failure that trace exists to prevent.
const maxPlaylistEntries = 100

// ytdlpPlaylistTimeout bounds one flat listing.
//
// Longer than ytdlpProbeTimeout's twenty seconds (app.go) for one reason: this
// runs while the person is waiting at the paste box, but so does the page
// crawl right beside it, which has allowed itself thirty seconds since it was
// written - and a listing of a thousand entries is a bigger answer to build
// than a page of anchors. Sixty seconds is that same judgement call with room
// for a long listing, and it is the ceiling on how long a paste can be held up
// by this: the listing either answers inside it or the link is staged the way
// it always was.
const ytdlpPlaylistTimeout = 60 * time.Second

// playlistProber is implemented by a backend that can list what a link points
// at without downloading any of it. Optional, exactly like titleProber and
// speedLimiter beside it in app.go: yt-dlp is the only backend that can answer
// this at all, and an install with no yt-dlp binary has no answer rather than
// a wrong one.
type playlistProber interface {
	ProbePlaylist(ctx context.Context, url string) (ytdlp.Playlist, error)
}

// ytdlpPlaylistProber returns the yt-dlp backend as a playlistProber, and
// whether it actually is one - ytdlpTitleProber's own shape (app.go).
func (a *App) ytdlpPlaylistProber() (playlistProber, bool) {
	a.bmu.RLock()
	b := a.ytdlp
	a.bmu.RUnlock()
	p, ok := b.(playlistProber)
	return p, ok
}

// ytdlpPlaylist asks yt-dlp what a link lists, and reports whether that answer
// is worth expanding into tasks. It is crawl()'s own shape (app_links.go),
// gate for gate, and for the same reasons - see that function.
//
// It returns false for everything that should be staged as one link: the
// setting is off, the link is not yt-dlp's, no yt-dlp backend is wired, the
// listing could not be read, or the link simply is not a playlist. Every one
// of those ends in exactly the behaviour this app had before this file
// existed, which is the property that makes an opt-in setting safe to turn on.
func (a *App) ytdlpPlaylist(u string) (ytdlp.Playlist, bool) {
	// The setting the user already has for this (settings.Ytdlp.Playlist, the
	// "download the whole playlist when a link points into one" toggle on the
	// resolvers page). It is deliberately the SAME field the yt-dlp backend
	// reads for --no-playlist rather than a second one beside it: both answer
	// the one question a person is asking - "is a playlist link a list, or is
	// it the single video it happens to point at" - and two fields would let
	// an install answer it two different ways at once. What changes here is
	// only HOW the "it is a list" answer is carried out: one task per entry
	// instead of one task that quietly fetches every entry into a single row
	// with a single progress bar and a single point of failure.
	if !a.Settings.Get().Ytdlp.Playlist {
		return ytdlp.Playlist{}, false
	}
	// Only links yt-dlp would actually handle. The registry is asked rather
	// than the URL inspected, so a hoster link that a debrid or JD backend
	// claims never reaches a yt-dlp process at all - the same routing question
	// crawl() asks one line further down.
	if res := a.Registry.For(u); res == nil || res.Info().ID != "ytdlp" {
		return ytdlp.Playlist{}, false
	}
	pp, ok := a.ytdlpPlaylistProber()
	if !ok {
		return ytdlp.Playlist{}, false
	}
	// Counted as a crawl in the activity strip, because that is what it is: a
	// listing being fetched while the paste box waits. Begun here, after the
	// gates above, for crawl()'s own reason - a gate that returns instantly
	// with no network call must not flash the strip.
	a.beginActivity(ActivityCrawl, 1)
	defer a.endActivity(ActivityCrawl, 1)
	ctx, cancel := context.WithTimeout(context.Background(), ytdlpPlaylistTimeout)
	defer cancel()
	pl, err := pp.ProbePlaylist(ctx, u)
	if err != nil {
		// Logged, not surfaced, and not fatal to the paste: the link is about
		// to be staged as itself, which is what it would have been without
		// this call. Same silence as probeYtdlpTitle's own failure path.
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
// Every entry goes through stage() (app_links.go), which is the point: the
// link filter, the mirror set, the Packagizer, the resolver and the variant
// expansion all apply to a playlist entry exactly as they do to a pasted link,
// and none of them is reimplemented here. Duplicates in particular are NOT
// handled in this file - a video already in the list is folded away by the
// mirror set inside stage() and reported in the skipped trace with the reason,
// the same way a link pasted twice is, whether the copy came from this same
// playlist, from an earlier paste of it, or from somebody typing the video's
// own URL last week.
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

	// The package these entries land in. A name typed into the paste box is
	// the more specific answer and wins, exactly as it does over every other
	// derived name in this app; otherwise it is the playlist's own title,
	// which is the whole reason the listing is fetched with -J rather than -j
	// (see ProbePlaylist). A listing with no title of its own falls through to
	// the ordinary naming pass in addLinksFrom, which derives one from the
	// entries themselves - the same two-stage fallback a crawled page gets.
	entryPkg := strings.TrimSpace(pkg)
	if entryPkg == "" && pl.Title != "" {
		entryPkg = sanitizeSegment(pl.Title)
	}

	created := make([]*core.Task, 0, len(entries))
	probes := make([]probeTarget, 0, len(entries))
	for _, e := range entries {
		t := a.stage(e.URL, e.Title, 0, intake{
			pkg: entryPkg,
			// OriginCrawl and not the paste's own origin, for the reason the
			// crawl branch gives (app_links.go): a link nobody typed did not
			// arrive by the path the playlist did. Which listing it came from
			// is on Source right beside it, where a rule keyed on where a link
			// came from can read it - and where ytdlpOptionsForTask reads it
			// too, to be sure an entry is downloaded as the one video it is.
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
		if !t.Skipped {
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

// probePlaylistEntries fills in what the listing could not say - each video's
// real formats, its extension and its size, and the availability a probe that
// answered at all already proves (applyProbeFormats, app_ytdlp_variants.go).
//
// ONE AT A TIME, which is the whole reason this exists instead of letting
// stage() spawn its usual per-link probe. Every probe is a real yt-dlp process
// against one site, and a playlist is the one paste that can produce a hundred
// links from a single line of input: spawned the ordinary way that is a
// hundred processes at once, aimed at one host, from one keystroke - the same
// stampede backfillYtdlpProbes (app_ytdlp_variants.go) already refuses to
// start at boot, for the same reason, in the same shape.
//
// Nothing is waiting on this. The names are already on the rows, straight from
// the listing, so a person can read, sort and untick the whole playlist while
// this works through it - and a probe that never runs at all leaves exactly
// the "no opinion yet" state every one of those fields documents as its own
// empty value.
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
