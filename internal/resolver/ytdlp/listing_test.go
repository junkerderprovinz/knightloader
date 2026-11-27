package ytdlp

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// testdata/listing/kurzgesagt.jsonl is what listingArgs made yt-dlp print for
// https://www.youtube.com/@kurzgesagt/videos with a limit of three.

func TestAListingReadsEveryEntryWithItsLengthAndDate(t *testing.T) {
	f, err := os.Open("testdata/listing/kurzgesagt.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []ListingEntry
	if err := readListing(f, func(e ListingEntry) bool { got = append(got, e); return true }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want the three listed", len(got))
	}
	first := got[0]
	if first.ID != "fEjvSbI_PT4" || first.Extractor != "Youtube" || first.URL != "https://www.youtube.com/watch?v=fEjvSbI_PT4" {
		t.Errorf("first entry: %+v", first)
	}
	if first.Title != "Dying Is Complicated" || first.Duration != 840 || first.Timestamp != 1794528000 {
		t.Errorf("first entry lost its title, length or date: %+v", first)
	}
	if !strings.HasPrefix(first.Channel, "Kurzgesagt") || first.ChannelID != "UCsXVk37bltHxD1rDPwtNM8Q" || !strings.HasSuffix(first.Playlist, "Videos") {
		t.Errorf("first entry does not say whose it is: %+v", first)
	}
}

func TestAListingStopsWhereTheCallerSaysSo(t *testing.T) {
	f, err := os.Open("testdata/listing/kurzgesagt.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	if err := readListing(f, func(ListingEntry) bool { n++; return n < 2 }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("the listing went on for %d entries after being told to stop at the second", n)
	}
}

func TestAListingLeavesOutWhatCannotBeStaged(t *testing.T) {
	lines := strings.Join([]string{
		`{"_type": "url", "ie_key": "Youtube", "id": "a", "url": "https://www.youtube.com/watch?v=a", "title": "kept"}`,
		`{"_type": "url", "ie_key": "Youtube", "id": "b", "url": "https://www.youtube.com/watch?v=b", "title": "premiere", "live_status": "is_upcoming"}`,
		`{"_type": "url", "ie_key": "Generic", "id": "c", "url": "c", "title": "bare id"}`,
		`{"_type": "playlist", "id": "d", "title": "a nested tab"}`,
		`not json at all`,
		``,
	}, "\n")
	var got []string
	if err := readListing(strings.NewReader(lines), func(e ListingEntry) bool { got = append(got, e.Title); return true }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("got %v, want only the entry that can be staged", got)
	}
}

func TestListingArgsAskForTheFieldsAndTheLimit(t *testing.T) {
	args := listingArgs(20)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--flat-playlist", "--playlist-end 20", "youtubetab:approximate_date", "-O"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the arguments lack %q: %v", want, args)
		}
	}
	if slices.Contains(listingArgs(0), "--playlist-end") {
		t.Error("a listing of everything was given an end")
	}
}
