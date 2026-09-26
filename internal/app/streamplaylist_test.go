package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The direct download, the HTTP fallback and a header profile take a link by
// its look or its origin, so a stream playlist behind one goes on to yt-dlp.
// Every other backend that uses the engine chose its link on purpose, and
// keeps what it fetched. Only the fallback, which nothing comes after, turns
// a web page down.
func TestWhichJobsCheckWhatTheyFetched(t *testing.T) {
	a := newQueueApp(t)
	for _, c := range []struct {
		resolverID           string
		passOn, refusesPages bool
	}{
		{"direct", true, false},
		{"http", true, true},
		{"hostheaders", true, false},
		{"alldebrid", false, false},
		{"torrent", false, false},
	} {
		task := &core.Task{ID: "t-" + c.resolverID, URL: "https://cdn.example/hls/master.txt", Resolver: c.resolverID}
		a.mu.Lock()
		job := a.engineJobLocked(task, a.Settings.Get(), task.URL, nil, 1)
		a.mu.Unlock()
		if job.PassOnPlaylists != c.passOn {
			t.Errorf("a job for %s has PassOnPlaylists = %v, want %v", c.resolverID, job.PassOnPlaylists, c.passOn)
		}
		if job.RefusePages != c.refusesPages {
			t.Errorf("a job for %s has RefusePages = %v, want %v", c.resolverID, job.RefusePages, c.refusesPages)
		}
	}
}
