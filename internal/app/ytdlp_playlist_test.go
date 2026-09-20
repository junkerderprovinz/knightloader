package app

// The playlist half of the yt-dlp intake: one pasted playlist link becomes one
// task per video, in one package, and the playlist link itself never becomes a
// task.
//
// Same arrangement as ytdlp_probe_test.go beside it, a fake backend rather than
// a real yt-dlp process, since what is tested here is this package's wiring:
// the gate on the setting, the expansion, the package, the entry limit, and the
// duplicate rule it leaves to stage.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakePlaylistBackend answers both probes: the listing and the per-entry
// title and format probe the expansion fires afterwards. It counts calls rather
// than closing a channel as ytdlp_probe_test.go's fake does, because a hundred
// entries mean a hundred title probes and a close-once fake would panic.
type fakePlaylistBackend struct {
	pl     ytdlp.Playlist
	err    error
	mu     sync.Mutex
	listed []string
	probed []string
}

func (*fakePlaylistBackend) Download(string, string, map[string]string, int) {}
func (*fakePlaylistBackend) Pause(string)                                    {}
func (*fakePlaylistBackend) Resume(string)                                   {}
func (*fakePlaylistBackend) Remove(string, bool)                             {}

func (b *fakePlaylistBackend) ProbePlaylist(_ context.Context, url string) (ytdlp.Playlist, error) {
	b.mu.Lock()
	b.listed = append(b.listed, url)
	b.mu.Unlock()
	if b.err != nil {
		return ytdlp.Playlist{}, b.err
	}
	// Only the playlist link lists anything, as a real yt-dlp answers: a video
	// URL comes back with no entries. A fake that listed for every link would
	// expand the entries again when a test pastes one of them on its own.
	if url != playlistURL {
		return ytdlp.Playlist{}, nil
	}
	return b.pl, nil
}

func (b *fakePlaylistBackend) ProbeTitle(_ context.Context, url string) (ytdlp.ProbeResult, error) {
	b.mu.Lock()
	b.probed = append(b.probed, url)
	b.mu.Unlock()
	// No title: a playlist entry already carries the name the listing gave it,
	// and this probe exists for the formats. A title here would hide a listing
	// name that never landed.
	return ytdlp.ProbeResult{Title: "", Formats: nil}, nil
}

func (b *fakePlaylistBackend) listings() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.listed...)
}

// playlistApp is an app with the playlist setting on and pl as the one listing
// yt-dlp will answer with.
func playlistApp(t *testing.T, pl ytdlp.Playlist) (*App, *fakePlaylistBackend) {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Ytdlp.Playlist = true })
	b := &fakePlaylistBackend{pl: pl}
	wireYtdlp(a, b)
	return a, b
}

func entries(n int) []ytdlp.PlaylistEntry {
	out := make([]ytdlp.PlaylistEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ytdlp.PlaylistEntry{
			URL:   fmt.Sprintf("https://youtube.com/watch?v=vid%03d", i),
			Title: fmt.Sprintf("Track %03d", i),
		})
	}
	return out
}

const playlistURL = "https://youtube.com/playlist?list=PLtest"

// Three claims that only mean something together: one task per entry, all of
// them in one package named after the playlist, and no task for the playlist
// link, which would still be a single job fetching every video into one file.
func TestOnePlaylistLinkBecomesOneTaskPerVideoInOnePackage(t *testing.T) {
	a, _ := playlistApp(t, ytdlp.Playlist{Title: "Greatest Hits", Entries: entries(3)})

	created := a.AddLinks([]string{playlistURL}, "")

	if len(created) != 3 {
		t.Fatalf("one playlist link created %d tasks, want one per entry (3)", len(created))
	}
	for _, task := range created {
		if task.Package != "Greatest Hits" {
			t.Errorf("entry %q landed in package %q, want the playlist's own name", task.Name, task.Package)
		}
		if task.URL == playlistURL {
			t.Errorf("a task was staged for the playlist link itself (%q)", task.URL)
		}
		if task.Name == task.URL {
			t.Errorf("entry %q kept its URL as its name, want the title the listing already carried", task.URL)
		}
		if !task.Enabled {
			t.Errorf("entry %q was staged disabled, so it cannot be unticked from on", task.Name)
		}
		if task.Source != playlistURL {
			t.Errorf("entry %q has Source %q, want the listing it came from", task.Name, task.Source)
		}
	}
	// Nothing in the list is the playlist link, including the variant siblings
	// the expansion creates, which never appear in the returned slice.
	for _, row := range a.Tasks() {
		if row.URL == playlistURL {
			t.Fatalf("the playlist link is still in the list as %q (%s)", row.Name, row.Status)
		}
	}
}

// One task per video buys independence: a failure written onto one row leaves
// the rest as they were, which holds only because they are separate tasks.
func TestEachPlaylistEntryIsItsOwnJob(t *testing.T) {
	a, _ := playlistApp(t, ytdlp.Playlist{Title: "Greatest Hits", Entries: entries(4)})
	created := a.AddLinks([]string{playlistURL}, "")
	if len(created) != 4 {
		t.Fatalf("staged %d tasks, want 4", len(created))
	}

	// One row errors, one row is unticked, and the other two are untouched.
	a.onUpdate(created[1].ID, core.Update{Status: core.StatusError, Err: "yt-dlp: Video unavailable"})
	a.SetEnabled([]string{created[2].ID}, false)

	if got := snapshot(t, a, created[0].ID); got.Status != core.StatusCollected || got.Error != "" {
		t.Errorf("first entry = %s/%q after a sibling failed, want it untouched", got.Status, got.Error)
	}
	if got := snapshot(t, a, created[3].ID); got.Status != core.StatusCollected || !got.Enabled {
		t.Errorf("last entry = %s/enabled=%v, want it untouched by the other two", got.Status, got.Enabled)
	}
	if got := snapshot(t, a, created[2].ID); got.Enabled {
		t.Error("unticking one video did not stick")
	}
}

// The switch is the field the yt-dlp backend already reads for --no-playlist
// rather than a second one beside it. Off means a playlist link is the single
// video it points at, and yt-dlp is not asked for a listing at all.
func TestAPlaylistLinkIsOneTaskWhenTheSettingIsOff(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Ytdlp.Playlist = false })
	b := &fakePlaylistBackend{pl: ytdlp.Playlist{Title: "Greatest Hits", Entries: entries(3)}}
	wireYtdlp(a, b)

	created := a.AddLinks([]string{playlistURL}, "")

	if len(created) != 1 || created[0].URL != playlistURL {
		t.Fatalf("created %+v, want the one task for the pasted link itself", created)
	}
	if n := len(b.listings()); n != 0 {
		t.Errorf("yt-dlp was asked for a listing %d times with the setting off", n)
	}
}

// Every failure path ends with the pasted link staged as itself, never with a
// link nobody staged.
func TestAListingThatCannotBeReadStagesTheLinkAsItself(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Ytdlp.Playlist = true })
	wireYtdlp(a, &fakePlaylistBackend{err: errors.New("yt-dlp: Sign in to confirm your age")})

	created := a.AddLinks([]string{playlistURL}, "")

	if len(created) != 1 || created[0].URL != playlistURL {
		t.Fatalf("created %+v, want the pasted link staged as itself after a failed listing", created)
	}
}

// With the setting on, a single-video link gets an empty listing back and is
// staged as one task with its variant rows and its own title probe.
func TestAnOrdinaryVideoLinkIsUntouchedByPlaylistExpansion(t *testing.T) {
	a, _ := playlistApp(t, ytdlp.Playlist{})

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")

	if len(created) != 1 || created[0].URL != url {
		t.Fatalf("created %+v, want the single video staged as one task", created)
	}
	rows := 0
	for _, row := range a.Tasks() {
		if row.URL == url {
			rows++
		}
	}
	if rows != len(ytdlp.Variants()) {
		t.Errorf("the single video has %d rows, want its whole variant family (%d)", rows, len(ytdlp.Variants()))
	}
}

// A thousand-entry channel does not fill the collector, and the cut is recorded
// in the skipped-links trace with both numbers, so the paste does not quietly
// produce fewer links than the source had.
func TestAPlaylistLongerThanTheLimitIsCutAndSaysSo(t *testing.T) {
	const listed = maxPlaylistEntries + 5
	a, _ := playlistApp(t, ytdlp.Playlist{Title: "Everything", Entries: entries(listed)})

	created := a.AddLinks([]string{playlistURL}, "")

	if len(created) != maxPlaylistEntries {
		t.Fatalf("staged %d tasks for a listing of %d, want the limit of %d", len(created), listed, maxPlaylistEntries)
	}
	told := false
	for _, s := range a.SkippedLinks() {
		if s.URL == playlistURL && strings.Contains(s.Reason, fmt.Sprint(listed)) && strings.Contains(s.Reason, fmt.Sprint(maxPlaylistEntries)) {
			told = true
		}
	}
	if !told {
		t.Errorf("nothing in the skipped trace says the playlist was cut: %+v", a.SkippedLinks())
	}
}

// The entries go through stage, so the mirror set folds a video already in the
// list and records why, as it does for a link pasted twice. There is no second
// duplicate rule written for playlists.
func TestAVideoAlreadyInTheListIsNotStagedTwice(t *testing.T) {
	list := entries(3)
	a, _ := playlistApp(t, ytdlp.Playlist{Title: "Greatest Hits", Entries: list})

	// The middle video, pasted on its own first.
	if solo := a.AddLinks([]string{list[1].URL}, ""); len(solo) != 1 {
		t.Fatalf("the single link staged %d tasks, want 1", len(solo))
	}
	created := a.AddLinks([]string{playlistURL}, "")

	if len(created) != 2 {
		t.Fatalf("the playlist staged %d tasks, want the 2 that were not already in the list", len(created))
	}
	folded := false
	for _, s := range a.SkippedLinks() {
		if s.URL == list[1].URL && s.Kind == "duplicate" {
			folded = true
		}
	}
	if !folded {
		t.Errorf("the repeated video was not recorded as a duplicate: %+v", a.SkippedLinks())
	}
}

// The setting that expands the playlist is the one the backend reads for
// --no-playlist, so an entry URL still carrying its own "&list=" parameter
// would ask yt-dlp to fetch the whole playlist once per row.
func TestAPlaylistEntryDownloadsAsOneVideo(t *testing.T) {
	a, _ := playlistApp(t, ytdlp.Playlist{Title: "Greatest Hits", Entries: entries(2)})
	created := a.AddLinks([]string{playlistURL}, "")
	if len(created) != 2 {
		t.Fatalf("staged %d tasks, want 2", len(created))
	}

	if opts := a.ytdlpOptionsForTask(created[0].ID); opts.Playlist {
		t.Error("a playlist entry would be downloaded with the playlist flag still on")
	}
	// The setting still holds for a link somebody pasted themselves, which is
	// the fallback when no listing could be read.
	pasted := putTask(t, a, core.Task{
		URL: "https://youtube.com/watch?v=solo", Name: "Solo", Status: core.StatusCollected,
		Enabled: true, Origin: OriginPaste, Resolver: "ytdlp",
	})
	if opts := a.ytdlpOptionsForTask(pasted.ID); !opts.Playlist {
		t.Error("a pasted link lost the playlist setting, the only fallback when a listing cannot be read")
	}
}
