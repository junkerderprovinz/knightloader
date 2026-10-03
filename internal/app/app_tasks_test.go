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
