package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const rickRoll = "youtube dQw4w9WgXcQ"

// videoDownloadedBefore files a finished download of the video key, saved as
// file, in the history and the media archive, as after the list was cleared.
func videoDownloadedBefore(t *testing.T, a *App, id, url, key, file string) time.Time {
	t.Helper()
	at := time.UnixMilli(time.Date(2026, 10, 3, 20, 15, 0, 0, time.Local).UnixMilli())
	if err := a.Store.Save(&core.Task{
		ID: id, URL: url, Name: filepath.Base(file), Media: key, File: file, Resolver: "ytdlp",
		Status: core.StatusDone, CreatedAt: at.Add(-time.Minute), FinishedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	return at
}

// probedAs wires a yt-dlp whose probe names every link the video key.
func probedAs(a *App, extractor, id string) {
	fake, _ := newFakeYtdlp()
	fake.title = "Never Gonna Give You Up"
	fake.offers = ytdlp.ProbeResult{Extractor: extractor, ID: id}
	wireYtdlp(a, fake)
}

func heldAsDownloaded(t core.Task) bool { return t.Skipped && t.SkipCode == skipDownloaded }

func TestTheSameVideoUnderAnotherLinkIsRejectedWithTheDownloadItRepeats(t *testing.T) {
	a := historyApp(t, nil)
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	file := filepath.Join("media", "Rick Astley", "Never Gonna Give You Up [1080p].mp4")
	at := videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, file)

	got := onlyTask(t, a.AddLinksFrom([]string{"https://youtu.be/dQw4w9WgXcQ?si=share"}, "", OriginPaste))
	if !heldAsDownloaded(*got) {
		t.Fatalf("the link was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}
	want := map[string]string{
		"name":     "Never Gonna Give You Up [1080p].mp4",
		"finished": at.Format(time.RFC3339),
		"folder":   filepath.Join("media", "Rick Astley"),
	}
	for k, v := range want {
		if got.SkipParams[k] != v {
			t.Errorf("the reason has %s = %q, want %q", k, got.SkipParams[k], v)
		}
	}
}

// A link that names a playlist as well is only known as the video once the
// probe has said so, and then its whole family moves to the rejected links.
func TestAVideoKnownOnlyAfterItsProbeIsRejectedWithItsWholeFamily(t *testing.T) {
	a := historyApp(t, nil)
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")

	const link = "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"
	staged := onlyTask(t, a.AddLinks([]string{link}, ""))
	if staged.Skipped {
		t.Fatalf("the link was rejected before its probe (%s)", staged.SkipReason)
	}
	waitFor(t, "the probed family held as downloaded", func() bool {
		rows := tasksSharingURL(a, link)
		if len(rows) == 0 {
			return false
		}
		for _, x := range rows {
			if !heldAsDownloaded(x) {
				return false
			}
		}
		return true
	})
}

func TestDownloadingAHeldVideoAnywayBringsBackItsWholeFamily(t *testing.T) {
	a := historyApp(t, nil)
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")
	const link = "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"
	staged := onlyTask(t, a.AddLinks([]string{link}, ""))
	waitFor(t, "the family held", func() bool { return heldAsDownloaded(snapshot(t, a, staged.ID)) })
	rows := len(tasksSharingURL(a, link))

	a.RestoreFiltered([]string{staged.ID})
	back := tasksSharingURL(a, link)
	if len(back) != rows {
		t.Fatalf("the restore left %d rows, want the family's %d", len(back), rows)
	}
	for _, x := range back {
		if x.Skipped || x.Status != core.StatusCollected {
			t.Errorf("row %s (%s) came back as skipped=%v status=%q, want it in the collector", x.ID, x.Variant, x.Skipped, x.Status)
		}
	}
}

func TestClearingAHeldVideoTakesItsWholeFamily(t *testing.T) {
	a := historyApp(t, nil)
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")
	const link = "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"
	staged := onlyTask(t, a.AddLinks([]string{link}, ""))
	waitFor(t, "the family held", func() bool { return heldAsDownloaded(snapshot(t, a, staged.ID)) })

	a.ClearFiltered([]string{staged.ID})
	if left := tasksSharingURL(a, link); len(left) != 0 {
		t.Errorf("clearing the held video left %d of its rows", len(left))
	}
}

// Only the rows that fetch the video count: a thumbnail on its own is not
// having the video.
func TestAFinishedVideoRowIsFiledInTheArchive(t *testing.T) {
	a := historyApp(t, nil)
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	const link = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	onlyTask(t, a.AddLinks([]string{link}, ""))
	waitForProbedFamily(t, a, link)

	var video string
	for _, x := range tasksSharingURL(a, link) {
		kind, _ := variantDecode(x.Variant)
		switch kind {
		case ytdlp.VariantVideo:
			video = x.ID
		case ytdlp.VariantThumbnail:
			a.onUpdate(x.ID, core.Update{Status: core.StatusDone, File: filepath.Join(t.TempDir(), "clip.jpg")})
		}
	}
	if _, ok, _ := a.Store.DownloadedMedia(rickRoll); ok {
		t.Fatal("a finished thumbnail filed the video as downloaded")
	}
	dir := t.TempDir()
	a.onUpdate(video, core.Update{Status: core.StatusDone, File: filepath.Join(dir, "clip.mp4")})
	got, ok, err := a.Store.DownloadedMedia(rickRoll)
	if err != nil || !ok {
		t.Fatalf("the finished video is not in the archive (%v)", err)
	}
	if got.Folder != dir {
		t.Errorf("the archive files the video in %q, want %q", got.Folder, dir)
	}
}

func TestTheVideoCheckIsOffWithTheSetting(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.RejectDownloaded = false })
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")

	if got := onlyTask(t, a.AddLinks([]string{"https://youtu.be/dQw4w9WgXcQ"}, "")); got.Skipped {
		t.Errorf("the video was rejected (%s), want it staged with the check switched off", got.SkipReason)
	}
}

// A subscription is checked whatever the instance says, unless the
// subscription allows what was downloaded before.
func TestASubscriptionDecidesTheCheckForItsEntries(t *testing.T) {
	for _, c := range []struct {
		name    string
		setting bool
		check   core.DownloadedCheck
		held    bool
	}{
		{"checked with the setting off", false, core.DownloadedCheckOn, true},
		{"allowed with the setting on", true, core.DownloadedCheckOff, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := historyApp(t, func(s *settings.Settings) { s.RejectDownloaded = c.setting })
			probedAs(a, "Youtube", "dQw4w9WgXcQ")
			videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")

			got := onlyTask(t, a.detached(a.addLinksFrom([]string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}, "", OriginFeed,
				LinkBatchOptions{downloaded: c.check})))
			if got.Skipped != c.held {
				t.Errorf("the entry came back skipped=%v (%s), want %v", got.Skipped, got.SkipReason, c.held)
			}
		})
	}
}

// The same holds when only the probe names the video.
func TestASubscriptionsCheckReachesTheProbe(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.RejectDownloaded = false })
	probedAs(a, "Youtube", "dQw4w9WgXcQ")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", rickRoll, "clip.mp4")

	const link = "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"
	staged := onlyTask(t, a.detached(a.addLinksFrom([]string{link}, "", OriginFeed,
		LinkBatchOptions{downloaded: core.DownloadedCheckOn})))
	waitFor(t, "the probed entry held as downloaded", func() bool { return heldAsDownloaded(snapshot(t, a, staged.ID)) })
}

// The history keeps no folders, and the archive knows the folder of a video
// the history found by its link.
func TestALinkTheHistoryHasNamesTheFolderItsVideoWasSavedIn(t *testing.T) {
	a := historyApp(t, nil)
	file := filepath.Join("media", "Rick Astley", "clip.mp4")
	videoDownloadedBefore(t, a, "old", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789", rickRoll, file)

	got := onlyTask(t, a.AddLinks([]string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"}, ""))
	if !heldAsDownloaded(*got) || got.SkipParams["folder"] != filepath.Join("media", "Rick Astley") {
		t.Errorf("the link came back skipped=%v with folder %q, want it held with the folder", got.Skipped, got.SkipParams["folder"])
	}
}
