package ytdlp

import (
	"context"
	"strings"
	"testing"
)

// Every test here drives ProbePlaylist through the same fake yt-dlp the title
// probe's own tests use: this package's test binary, re-executed as the
// "yt-dlp binary" (see fakeYtdlpBackend and TestMain in backend_test.go). No
// real yt-dlp, no network, identical behaviour on every platform.

// TestProbePlaylistReadsEveryEntryOfAListing is the answer the whole feature
// rests on: one link, the videos it lists, each with the title the listing
// already carried - which is what lets a hundred rows be named without a
// hundred extractions.
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
	// The nested playlist and the entry with no URL: left out, and COUNTED.
	// A listing of four that stages two is a number the person looking at the
	// collector has to be told, which is why this is a field and not a silent
	// skip - see Playlist.Dropped.
	if pl.Dropped != 2 {
		t.Errorf("Dropped = %d, want 2 (a nested playlist and an entry naming no link)", pl.Dropped)
	}
}

// TestProbePlaylistOnAnOrdinaryVideoListsNothing is the case that decides
// whether this feature is safe to leave switched on: an ordinary video URL is
// probed by exactly the same call, and it must come back with no entries and
// NO error, so the caller stages the link the way it always did.
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

// TestProbePlaylistAsksForTheListingAndNotForTheVideos pins the flag the whole
// promise rests on. Without --flat-playlist, yt-dlp opens every entry to
// answer, which is fifty extractions for one paste - the exact cost
// resolver.go's own "no Checker here" comment refuses to pay, arriving by a
// different door.
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

// TestProbePlaylistReportsAFailingInvocation covers the link that is gone or
// private. The caller reads an error as "stage this as one link", so nothing
// here may invent an empty listing that looks like a successful "not a
// playlist" answer.
func TestProbePlaylistReportsAFailingInvocation(t *testing.T) {
	b := fakeYtdlpBackend(t, "fail")
	if _, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=gone"); err == nil {
		t.Fatal("ProbePlaylist returned no error for a failing invocation")
	}
}

// TestProbePlaylistReportsUnparseableOutput is the same line drawn for a
// binary that answered with something that is not a listing at all.
func TestProbePlaylistReportsUnparseableOutput(t *testing.T) {
	b := fakeYtdlpBackend(t, "badjson")
	if _, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=PL1"); err == nil {
		t.Fatal("ProbePlaylist returned no error for output that is not JSON")
	}
}

// TestParsePlaylistKeepsOnlyAddressableEntries drives the reading half
// directly, without a process, for the shapes a real listing carries that the
// fake above cannot all hold at once.
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

// TestParsePlaylistReadsAMultiVideoSet covers the other kind yt-dlp reports
// for "several videos under one name" - a stream split into parts. The caller
// does the same thing with it as with a playlist, so this must not be read as
// an ordinary single video.
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
