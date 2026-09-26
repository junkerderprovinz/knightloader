package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The direct download and the HTTP fallback take a link for a file by its
// look, so a stream playlist behind one goes on to yt-dlp. Every other backend
// that uses the engine chose its link on purpose, and keeps what it fetched.
func TestOnlyALinkTakenByItsLookHandsOnAPlaylist(t *testing.T) {
	a := newQueueApp(t)
	for resolverID, want := range map[string]bool{
		"direct":      true,
		"http":        true,
		"alldebrid":   false,
		"hostheaders": false,
		"torrent":     false,
	} {
		task := &core.Task{ID: "t-" + resolverID, URL: "https://cdn.example/hls/master.txt", Resolver: resolverID}
		a.mu.Lock()
		job := a.engineJobLocked(task, a.Settings.Get(), task.URL, nil, 1)
		a.mu.Unlock()
		if job.PassOnPlaylists != want {
			t.Errorf("a job for %s has PassOnPlaylists = %v, want %v", resolverID, job.PassOnPlaylists, want)
		}
	}
}
