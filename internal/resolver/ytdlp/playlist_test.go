package ytdlp

import (
	"context"
	"strings"
	"testing"
)

// These tests use the fake yt-dlp from backend_test.go (fakeYtdlpBackend).

func TestProbePlaylistReadsEveryEntryOfAListing(t *testing.T) {
	b := fakeYtdlpBackend(t, "flatlisting")
	pl, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=PL1")
	if err != nil {
		t.Fatalf("ProbePlaylist: %v", err)
	}
	if pl.Title != "Greatest Hits" {
		t.Errorf("Title = %q, want the playlist's own title - it is what the package is named after", pl.Title)
	}
	if len(pl.Entries) != 2 {
		t.Fatalf("Entries = %+v, want the two addressable videos", pl.Entries)
	}
	if pl.Entries[0].URL != "https://youtube.com/watch?v=aaa" || pl.Entries[0].Title != "First Song" {
		t.Errorf("first entry = %+v, want the listing's own url and title", pl.Entries[0])
	}
	if pl.Entries[1].URL != "https://youtube.com/watch?v=bbb" {
		t.Errorf("second entry = %+v, want the entries in the order the listing states them", pl.Entries[1])
	}
	// The nested playlist and the entry without a URL are left out and
	// counted.
	if pl.Dropped != 2 {
		t.Errorf("Dropped = %d, want 2 (a nested playlist and an entry naming no link)", pl.Dropped)
	}
}

// An ordinary video yields no entries and no error, so it is staged as usual.
func TestProbePlaylistOnAnOrdinaryVideoListsNothing(t *testing.T) {
	b := fakeYtdlpBackend(t, "singlevideo")
	pl, err := b.ProbePlaylist(context.Background(), "https://youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ProbePlaylist on a single video: %v - a video is not an error, it simply lists nothing", err)
	}
	if len(pl.Entries) != 0 {
		t.Errorf("Entries = %+v, want none for a link that is not a playlist", pl.Entries)
	}
}

// Without --flat-playlist, yt-dlp would extract every entry.
func TestProbePlaylistAsksForTheListingAndNotForTheVideos(t *testing.T) {
	b := fakeYtdlpBackend(t, "echoargs")
	pl, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=PL1")
	if err != nil {
		t.Fatalf("ProbePlaylist: %v", err)
	}
	for _, want := range []string{"--flat-playlist", "--skip-download", "-J", "https://youtube.com/playlist?list=PL1"} {
		if !strings.Contains(pl.Title, want) {
			t.Errorf("yt-dlp was invoked as %q, want %q in it", pl.Title, want)
		}
	}
}

func TestProbePlaylistReportsAFailingInvocation(t *testing.T) {
	b := fakeYtdlpBackend(t, "fail")
	if _, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=gone"); err == nil {
		t.Fatal("ProbePlaylist returned no error for a failing invocation")
	}
}

func TestProbePlaylistReportsUnparseableOutput(t *testing.T) {
	b := fakeYtdlpBackend(t, "badjson")
	if _, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=PL1"); err == nil {
		t.Fatal("ProbePlaylist returned no error for output that is not JSON")
	}
}

func TestParsePlaylistKeepsOnlyAddressableEntries(t *testing.T) {
	pl, err := parsePlaylist([]byte(`{"_type":"playlist","title":"Mixed","entries":[
		{"_type":"url","url":"https://example.com/v/1","title":"Real"},
		{"_type":"url","url":"abc123","title":"An id, not a link"},
		{"_type":"url","url":"magnet:?xt=urn:btih:deadbeef","title":"Not http"},
		null,
		{"_type":"multi_video","url":"https://example.com/set/2","title":"A set inside the set"}
	]}`))
	if err != nil {
		t.Fatalf("parsePlaylist: %v", err)
	}
	if len(pl.Entries) != 1 || pl.Entries[0].URL != "https://example.com/v/1" {
		t.Fatalf("Entries = %+v, want only the one entry naming an http link", pl.Entries)
	}
	if pl.Dropped != 4 {
		t.Errorf("Dropped = %d, want 4 - every entry left out is counted, never silently swallowed", pl.Dropped)
	}
}

func TestParsePlaylistReadsAMultiVideoSet(t *testing.T) {
	pl, err := parsePlaylist([]byte(`{"_type":"multi_video","title":"Lecture 4","entries":[
		{"_type":"url","url":"https://example.com/part/1","title":"Part 1"},
		{"_type":"url","url":"https://example.com/part/2","title":"Part 2"}
	]}`))
	if err != nil {
		t.Fatalf("parsePlaylist: %v", err)
	}
	if len(pl.Entries) != 2 || pl.Title != "Lecture 4" {
		t.Errorf("multi_video read as %+v, want both parts under the set's own title", pl)
	}
}
