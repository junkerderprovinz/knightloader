package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// family stages one paste of a link: a finished video row named by its
// quality and a finished subtitle row with the plain title, written into dir.
func family(t *testing.T, a *App, dir, id string, at time.Time, video, subtitle string) (videoID, subID string) {
	t.Helper()
	for _, name := range []string{video, subtitle} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	videoID, subID = id+"-video", id+"-sub"
	a.tasks[videoID] = &core.Task{ID: videoID, URL: "https://video.example/watch?v=clip", Variant: "video", Status: core.StatusDone, File: filepath.Join(dir, video), CreatedAt: at}
	a.tasks[subID] = &core.Task{ID: subID, URL: "https://video.example/watch?v=clip", Variant: "subtitle", Status: core.StatusDone, File: filepath.Join(dir, subtitle), CreatedAt: at}
	return videoID, subID
}

func TestEachPickOfOneVideoKeepsItsOwnSubtitlesBesideIt(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	dir := t.TempDir()
	first, second := time.Unix(1_000, 0), time.Unix(2_000, 0)
	// The second family's subtitles met the first's under the plain title
	// and were counted by the collision policy.
	highVideo, highSub := family(t, a, dir, "high", first, "Clip (1080p).mkv", "Clip.de.srt")
	lowVideo, lowSub := family(t, a, dir, "low", second, "Clip (720p).mkv", "Clip.de (2).srt")

	a.mu.Lock()
	renamed := len(a.matchVariantNamesLocked(a.tasks[highVideo])) + len(a.matchVariantNamesLocked(a.tasks[lowVideo]))
	gotHigh, gotLow := a.tasks[highSub].File, a.tasks[lowSub].File
	nameHigh := a.tasks[highSub].Name
	a.mu.Unlock()

	if renamed != 2 {
		t.Errorf("%d side rows renamed, want both subtitle rows", renamed)
	}
	if want := filepath.Join(dir, "Clip (1080p).de.srt"); gotHigh != want {
		t.Errorf("1080p subtitles at %q, want %q", gotHigh, want)
	}
	if want := filepath.Join(dir, "Clip (720p).de.srt"); gotLow != want {
		t.Errorf("720p subtitles at %q, want %q", gotLow, want)
	}
	if nameHigh != "Clip (1080p).de" {
		t.Errorf("subtitle row named %q, want it after its file", nameHigh)
	}
	for _, f := range []string{gotHigh, gotLow} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("no file at %q: %v", f, err)
		}
	}
}

func TestASideRowFinishingLaterStillFollowsTheVideo(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	dir := t.TempDir()
	_, subID := family(t, a, dir, "clip", time.Unix(1_000, 0), "Clip (best).mp4", "Clip.jpg")
	a.mu.Lock()
	a.tasks[subID].Variant = "thumbnail"
	a.matchVariantNamesLocked(a.tasks[subID])
	got := a.tasks[subID].File
	a.mu.Unlock()
	if want := filepath.Join(dir, "Clip (best).jpg"); got != want {
		t.Errorf("thumbnail at %q, want %q beside the video", got, want)
	}
}

func TestARowNamedByHandOrAVideoWithoutQualityKeepsItsName(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	dir := t.TempDir()
	videoID, subID := family(t, a, dir, "typed", time.Unix(1_000, 0), "Clip.mkv", "Clip.de.srt")
	a.mu.Lock()
	if n := len(a.matchVariantNamesLocked(a.tasks[videoID])); n != 0 {
		t.Errorf("%d rows renamed after a video without a quality in its name", n)
	}
	a.mu.Unlock()

	videoID, subID = family(t, a, dir, "hand", time.Unix(2_000, 0), "Other (720p).mkv", "Other.de.srt")
	a.mu.Lock()
	a.tasks[subID].Filename = "mine.srt"
	if n := len(a.matchVariantNamesLocked(a.tasks[videoID])); n != 0 {
		t.Errorf("%d rows renamed although the subtitle row was named by hand", n)
	}
	a.mu.Unlock()
}
