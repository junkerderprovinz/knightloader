package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

const clipURL = "https://video.example/watch?v=clip"

// pick stages one paste of the clip as a queued video, thumbnail and
// description row, the way a variant family shares its URL and creation time.
func pick(t *testing.T, a *App, id string, at time.Time) (video, thumb, desc string) {
	t.Helper()
	video, thumb, desc = id+"-video", id+"-thumb", id+"-desc"
	for _, r := range []struct{ id, variant string }{{video, "video"}, {thumb, "thumbnail"}, {desc, "description"}} {
		putTask(t, a, core.Task{ID: r.id, URL: clipURL, Variant: r.variant, Status: core.StatusQueued, Enabled: true, CreatedAt: at})
	}
	return video, thumb, desc
}

// finish writes name into dir and settles row id on it, as the backend does.
func finish(t *testing.T, a *App, dir, id, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	a.onUpdate(id, core.Update{Status: core.StatusDone, File: path, Name: strings.TrimSuffix(name, filepath.Ext(name))})
	return path
}

// sideFile is the name a side row writes before its video names it.
func sideFile(id, ext string) string {
	return "Clip" + ytdlp.SideMark(id) + ext
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func noMarkLeft(t *testing.T, dir string) {
	t.Helper()
	for _, name := range filesIn(t, dir) {
		if strings.Contains(name, ".kl-") {
			t.Errorf("%q still carries its side mark", name)
		}
	}
}

func TestTwoPicksFetchingSideFilesFirstEachGetTheirOwn(t *testing.T) {
	a := newQueueApp(t)
	a.SetHalted(true)
	dir := t.TempDir()
	highVideo, highThumb, highDesc := pick(t, a, "high", time.Unix(1_000, 0))
	lowVideo, lowThumb, lowDesc := pick(t, a, "low", time.Unix(2_000, 0))

	// Both picks fetch their thumbnail and description before either video.
	for _, id := range []string{highThumb, lowThumb} {
		finish(t, a, dir, id, sideFile(id, ".jpg"))
	}
	for _, id := range []string{highDesc, lowDesc} {
		finish(t, a, dir, id, sideFile(id, ".description"))
	}
	if got := taskOf(t, a, lowThumb).Name; got != "Clip"+ytdlp.SideMark(lowThumb) {
		t.Fatalf("the waiting thumbnail row is named %q, want its marked name until the video comes", got)
	}
	finish(t, a, dir, lowVideo, "Clip (720p).mkv")
	finish(t, a, dir, highVideo, "Clip (1080p).mkv")

	for id, want := range map[string]string{
		highThumb: "Clip (1080p).jpg",
		highDesc:  "Clip (1080p).description",
		lowThumb:  "Clip (720p).jpg",
		lowDesc:   "Clip (720p).description",
	} {
		got := taskOf(t, a, id)
		if got.File != filepath.Join(dir, want) {
			t.Errorf("%s at %q, want %q", id, got.File, want)
		}
		if _, err := os.Stat(got.File); err != nil {
			t.Errorf("no file at %q: %v", got.File, err)
		}
		if name := strings.TrimSuffix(want, filepath.Ext(want)); got.Name != name {
			t.Errorf("%s named %q, want %q", id, got.Name, name)
		}
	}
	noMarkLeft(t, dir)
}

func TestASideFileWhoseVideoFailsTakesThePlainTitle(t *testing.T) {
	a := newQueueApp(t)
	a.SetHalted(true)
	dir := t.TempDir()
	video, thumb, _ := pick(t, a, "lone", time.Unix(1_000, 0))
	finish(t, a, dir, thumb, sideFile(thumb, ".jpg"))
	a.onUpdate(video, core.Update{Status: core.StatusError, Err: "no space left on device", Reason: core.ReasonDiskFull})

	if got, want := taskOf(t, a, thumb).File, filepath.Join(dir, "Clip.jpg"); got != want {
		t.Errorf("thumbnail at %q, want %q", got, want)
	}
	noMarkLeft(t, dir)
}

func TestRemovingTheVideoOrASideRowLeavesNoMarkedFile(t *testing.T) {
	a := newQueueApp(t)
	a.SetHalted(true)
	dir := t.TempDir()
	// A Clip.jpg from an earlier download keeps its name.
	if err := os.WriteFile(filepath.Join(dir, "Clip.jpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	video, thumb, desc := pick(t, a, "gone", time.Unix(1_000, 0))
	finish(t, a, dir, thumb, sideFile(thumb, ".jpg"))
	finish(t, a, dir, desc, sideFile(desc, ".description"))

	a.Remove(desc, false)
	if _, err := os.Stat(filepath.Join(dir, "Clip.description")); err != nil {
		t.Errorf("the removed description row left no Clip.description: %v", err)
	}
	a.Remove(video, false)
	if got, want := taskOf(t, a, thumb).File, filepath.Join(dir, "Clip (2).jpg"); got != want {
		t.Errorf("thumbnail at %q, want %q beside the earlier Clip.jpg", got, want)
	}
	noMarkLeft(t, dir)
}

func TestARowNamedByHandOrAVideoWithoutQualityKeepsItsName(t *testing.T) {
	a := newQueueApp(t)
	a.SetHalted(true)
	dir := t.TempDir()
	video, thumb, _ := pick(t, a, "typed", time.Unix(1_000, 0))
	finish(t, a, dir, thumb, sideFile(thumb, ".jpg"))
	finish(t, a, dir, video, "Clip.mkv")
	if got, want := taskOf(t, a, thumb).File, filepath.Join(dir, "Clip.jpg"); got != want {
		t.Errorf("thumbnail beside a video without quality at %q, want %q", got, want)
	}

	video, thumb, _ = pick(t, a, "hand", time.Unix(2_000, 0))
	a.mu.Lock()
	a.tasks[thumb].Filename = "mine.jpg"
	a.mu.Unlock()
	finish(t, a, dir, thumb, sideFile(thumb, ".jpg"))
	finish(t, a, dir, video, "Clip (720p).mkv")
	if got, want := taskOf(t, a, thumb).File, filepath.Join(dir, "mine.jpg"); got != want {
		t.Errorf("thumbnail named by hand at %q, want %q", got, want)
	}
}

func TestAVideoConfirmedLaterStillNamesItsSideFiles(t *testing.T) {
	a := newQueueApp(t)
	a.SetHalted(true)
	dir := t.TempDir()
	// A Clip.jpg from an earlier download keeps its name.
	if err := os.WriteFile(filepath.Join(dir, "Clip.jpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	video, thumb, desc := pick(t, a, "late", time.Unix(1_000, 0))
	a.mu.Lock()
	a.tasks[video].Status = core.StatusCollected
	a.mu.Unlock()
	finish(t, a, dir, thumb, sideFile(thumb, ".jpg"))
	finish(t, a, dir, desc, sideFile(desc, ".description"))
	if got, want := taskOf(t, a, thumb).File, filepath.Join(dir, "Clip (2).jpg"); got != want {
		t.Fatalf("thumbnail beside a video left in the collector at %q, want %q", got, want)
	}

	a.mu.Lock()
	a.tasks[video].Status = core.StatusQueued
	a.mu.Unlock()
	finish(t, a, dir, video, "Clip (1080p).mkv")
	for id, want := range map[string]string{thumb: "Clip (1080p).jpg", desc: "Clip (1080p).description"} {
		if got := taskOf(t, a, id).File; got != filepath.Join(dir, want) {
			t.Errorf("%s at %q, want %q", id, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Clip.jpg")); err != nil {
		t.Errorf("the earlier Clip.jpg was moved: %v", err)
	}
}
