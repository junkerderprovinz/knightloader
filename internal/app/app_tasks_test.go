package app

import (
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The rows a yt-dlp link stages share one creation time.
func TestTasksKeepRowsWithTheSameAgeInOneOrder(t *testing.T) {
	a := newQueueApp(t)
	at := time.Now()
	for _, id := range []string{"e", "b", "d", "a", "c"} {
		putTask(t, a, core.Task{ID: id, URL: "https://video.example/watch", CreatedAt: at,
			Status: core.StatusCollected})
	}
	putTask(t, a, core.Task{ID: "0", URL: "https://host.example/later.bin", CreatedAt: at.Add(time.Second),
		Status: core.StatusCollected})

	want := []string{"a", "b", "c", "d", "e", "0"}
	for range 50 {
		var got []string
		for _, task := range a.Tasks() {
			got = append(got, task.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("Tasks() = %v, want %v", got, want)
		}
	}
}

// A send hands back the first row of a yt-dlp link, and the link's other rows
// download beside it. The same link pasted again later is not one of them.
func TestTasksOfAnswersTheNamedRowsWithTheRestOfTheirLinks(t *testing.T) {
	a := newQueueApp(t)
	at := time.Now()
	const video = "https://video.example/watch"
	for _, id := range []string{"v", "s", "t"} {
		putTask(t, a, core.Task{ID: id, URL: video, Variant: id, CreatedAt: at, Status: core.StatusQueued})
	}
	putTask(t, a, core.Task{ID: "again", URL: video, Variant: "v", CreatedAt: at.Add(time.Minute), Status: core.StatusQueued})
	putTask(t, a, core.Task{ID: "other", URL: "https://host.example/file.bin", CreatedAt: at, Status: core.StatusQueued})

	var got []string
	for _, task := range a.TasksOf([]string{"v", "gone"}) {
		got = append(got, task.ID)
	}
	if want := []string{"s", "t", "v"}; !slices.Equal(got, want) {
		t.Fatalf("TasksOf(v) = %v, want %v", got, want)
	}
	if got := a.TasksOf([]string{"gone"}); len(got) != 0 {
		t.Fatalf("TasksOf(gone) = %v, want nothing", got)
	}
}
