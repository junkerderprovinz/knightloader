package app

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedSource is a feed address that is an IP literal, so working out its scope
// needs no DNS lookup.
const feedSource = "https://192.0.2.10/rss.xml"

func feedJob(url string, sub feed.Subscription) feed.Job {
	sub.URL = feedSource
	sub = feed.Sanitize([]feed.Subscription{sub})[0]
	return feed.Job{URL: url, Package: "Folge 1", Show: "Kellerfunk", Title: "Folge 1", Source: feedSource, Sub: sub}
}

func TestAFeedEntryStartsOnItsOwnWhenItsSubscriptionSaysSo(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	a.stageFeedJob(feedJob("https://host.example/auto.bin", feed.Subscription{AutoStart: true}))
	a.stageFeedJob(feedJob("https://host.example/held.bin", feed.Subscription{}))

	for _, x := range a.Tasks() {
		switch x.URL {
		case "https://host.example/auto.bin":
			if x.Status == core.StatusCollected {
				t.Errorf("the entry of a subscription that starts on its own waits in the collector")
			}
		case "https://host.example/held.bin":
			if x.Status != core.StatusCollected {
				t.Errorf("an entry of an ordinary subscription left the collector as %q", x.Status)
			}
		}
	}
}

func TestAFeedEntryWaitsOutTheCountdownLikeEverythingElse(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	a.stageFeedJob(feedJob("https://host.example/auto.bin", feed.Subscription{AutoStart: true}))
	for _, x := range a.Tasks() {
		if x.Status != core.StatusCollected {
			t.Errorf("%s skipped the instance's countdown with status %q", x.URL, x.Status)
		}
	}
}

func TestAFeedEntryGetsTheRowsItsSubscriptionPicked(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	fake, _ := newFakeYtdlp()
	fake.title = "Folge 1"
	fake.formats = probeUpTo720
	wireYtdlp(a, fake)
	const url = "https://youtube.com/watch?v=feedmedia01"
	audio := ytdlp.HosterPreset{Variants: []ytdlp.Variant{ytdlp.VariantAudio}, VideoFormat: "best", Quality: ytdlp.Quality720p, AudioFormat: "best"}
	job := feedJob(url, feed.Subscription{Media: &audio})
	// Stored without being applied, so no runner starts polling the address.
	cfg := a.Settings.Get()
	cfg.Feeds = []feed.Subscription{job.Sub}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	a.stageFeedJob(job)
	waitFor(t, "the family to be built", func() bool { return len(tasksSharingURL(a, url)) == 5 })

	check := func(when string) {
		t.Helper()
		for _, x := range tasksSharingURL(a, url) {
			kind, sub := variantDecode(x.Variant)
			if on := !x.VariantOff; on != (kind == ytdlp.VariantAudio) {
				t.Errorf("%s: the %s row is in view = %v, want only the audio row", when, kind, on)
			}
			if kind == ytdlp.VariantVideo && sub != "720p" {
				t.Errorf("%s: the video row picks %q, want the subscription's 720p", when, sub)
			}
		}
	}
	check("staged")
	// A save of any setting brings the collector in line with the presets,
	// which must not hand the entry back to the host's.
	setPreset(t, a, ytdlp.DefaultHosterPreset())
	check("after a settings save")
}

func TestChannelNamingFilesTheEntryUnderTheShow(t *testing.T) {
	a, base := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	// Jobs as internal/feed hands them over with channel naming: the show is
	// the package.
	for _, j := range []feed.Job{
		feedJob("https://host.example/f1.bin", feed.Subscription{Naming: feed.NamingChannel, Title: "Kellerfunk"}),
		feedJob("https://host.example/f2.bin", feed.Subscription{Naming: feed.NamingChannel, Dir: filepath.Join(base, "own")}),
	} {
		j.Package = j.Show
		j.Dir = j.Sub.Dir
		a.stageFeedJob(j)
	}

	for _, x := range a.Tasks() {
		want := filepath.Join(base, "Kellerfunk")
		if x.URL == "https://host.example/f2.bin" {
			want = filepath.Join(base, "own", "Kellerfunk")
		}
		if x.Package != "Kellerfunk" || x.Dir != want {
			t.Errorf("%s: package %q in %q, want %q in %q", x.URL, x.Package, x.Dir, "Kellerfunk", want)
		}
	}
}

func TestAMediathekEntryIsSavedUnderItsTitleWithItsSubtitles(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	j := feedJob("https://cdn.example/video/TV-20261126-2023-0200.webxxl.h264.mp4", feed.Subscription{})
	j.Title = "tagesschau 20:00 Uhr, 26.11.2026"
	j.Package = j.Title
	j.FileName = j.Title + ".mp4"
	j.Subtitle = "https://cdn.example/subtitle/ebutt/urn:ard:subtitle:a65360b2701c1868"
	j.SubtitleName = j.Title + ".ttml"
	a.stageFeedJob(j)

	var names []string
	for _, x := range a.Tasks() {
		names = append(names, x.Name)
		if !x.BrowserFile {
			t.Errorf("%s may be taken by a backend that renames it", x.URL)
		}
		if x.Package != j.Title {
			t.Errorf("%s is filed under %q", x.URL, x.Package)
		}
	}
	slices.Sort(names)
	want := []string{"tagesschau 20-00 Uhr, 26.11.2026.mp4", "tagesschau 20-00 Uhr, 26.11.2026.ttml"}
	if !slices.Equal(names, want) {
		t.Fatalf("saved as %q, want %q", names, want)
	}
}

func TestTheSeenMemoryLivesInItsOwnTable(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	cfg := a.Settings.Get()
	cfg.Feeds = []feed.Subscription{{URL: "https://192.0.2.11/a.xml"}}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	st := &feedState{app: a}
	if err := a.Store.SetFeedSeen("https://192.0.2.12/gone.xml", []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeen("https://192.0.2.11/a.xml", []string{"k"}); err != nil {
		t.Fatal(err)
	}
	if keys, known, err := a.Store.FeedSeen("https://192.0.2.11/a.xml"); err != nil || !known || !slices.Equal(keys, []string{"k"}) {
		t.Fatalf("got %v %v %v", keys, known, err)
	}
	if _, known, _ := a.Store.FeedSeen("https://192.0.2.12/gone.xml"); known {
		t.Error("a removed subscription kept its memory, so adding it back would not seed")
	}
	if v, _ := a.UIState("feeds"); v != "" {
		t.Errorf("the memory went to the interface-state bucket: %s", v)
	}
}

func TestAListingBecomesAFeedNamedAfterTheChannel(t *testing.T) {
	var f feed.Feed
	q := feed.ListQuery{Limit: 2}
	for i, e := range []ytdlp.ListingEntry{
		{ID: "a", Extractor: "Youtube", URL: "https://www.youtube.com/watch?v=a", Title: "A", Duration: 61, Timestamp: 1794528000,
			Playlist: "Kellerfunk - Videos", Channel: "Kellerfunk", ChannelID: "UCsXVk37bltHxD1rDPwtNM8Q"},
		{ID: "b", Extractor: "Vimeo", URL: "https://vimeo.com/b", Title: "B", Playlist: "Kellerfunk - Videos", Channel: "Kellerfunk"},
		{ID: "c", Extractor: "Youtube", URL: "https://www.youtube.com/watch?v=c", Title: "C"},
	} {
		if more := addListed(&f, q, e); more != (i < 2) {
			t.Fatalf("entry %d: going on = %v", i, more)
		}
	}
	if f.Title != "Kellerfunk" || f.Link != "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q" || len(f.Items) != 2 {
		t.Fatalf("got %+v", f)
	}
	if a := f.Items[0]; a.GUID != "yt:video:a" || a.Duration.Seconds() != 61 || a.Published.Unix() != 1794528000 {
		t.Errorf("first entry %+v", a)
	}
	if f.Items[1].GUID != "vimeo:b" {
		t.Errorf("second entry %+v", f.Items[1])
	}

	var pl feed.Feed
	addListed(&pl, feed.ListQuery{}, ytdlp.ListingEntry{ID: "x", URL: "https://www.youtube.com/watch?v=x", Playlist: "The Universe and Space stuff", Channel: "Kellerfunk"})
	if pl.Title != "The Universe and Space stuff" {
		t.Errorf("a playlist is called %q, want its own title", pl.Title)
	}
}

func TestAListingByDateGoesOnUntilTheDate(t *testing.T) {
	var f feed.Feed
	since := time.Unix(1794000000, 0)
	q := feed.ListQuery{Limit: 1, Since: since}
	if !addListed(&f, q, ytdlp.ListingEntry{ID: "a", URL: "https://www.youtube.com/watch?v=a", Timestamp: 1794528000}) {
		t.Fatal("stopped at the first entry")
	}
	if !addListed(&f, q, ytdlp.ListingEntry{ID: "b", URL: "https://www.youtube.com/watch?v=b", Timestamp: 1794100000}) {
		t.Fatal("stopped past the limit before reaching the date")
	}
	if addListed(&f, q, ytdlp.ListingEntry{ID: "c", URL: "https://www.youtube.com/watch?v=c", Timestamp: 1790000000}) {
		t.Fatal("went on past the date")
	}
	if len(f.Items) != 2 {
		t.Fatalf("kept %d entries", len(f.Items))
	}
}

func TestASubscriptionEntryCarriesItsSponsorBlockAndSeriesChoice(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	sb := feed.SponsorBlock{Remove: []string{"sponsor"}, Mark: []string{"intro"}}
	a.stageFeedJob(feedJob("https://host.example/ep.bin", feed.Subscription{SponsorBlock: &sb, Library: feed.LibrarySeries}))

	for _, x := range a.Tasks() {
		var gotSB ytdlp.SponsorBlock
		if err := json.Unmarshal(x.SponsorBlock, &gotSB); err != nil || !slices.Equal(gotSB.Remove, sb.Remove) || !slices.Equal(gotSB.Mark, sb.Mark) {
			t.Errorf("the row's SponsorBlock choice is %s (%v), want the subscription's %+v", x.SponsorBlock, err, sb)
		}
		var gotSeries ytdlp.Series
		if err := json.Unmarshal(x.Series, &gotSeries); err != nil || gotSeries.Show != "Kellerfunk" {
			t.Errorf("the row is filed under show %s (%v), want Kellerfunk", x.Series, err)
		}
	}
}

func TestASeriesSubscriptionLeavesTheShowFolderToTheLayout(t *testing.T) {
	a, base := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	j := feedJob("https://host.example/ep.bin", feed.Subscription{Naming: feed.NamingChannel, Library: feed.LibrarySeries, Dir: filepath.Join(base, "tv")})
	j.Dir = j.Sub.Dir
	a.stageFeedJob(j)

	for _, x := range a.Tasks() {
		if want := filepath.Join(base, "tv"); x.Dir != want {
			t.Errorf("a series entry landed in %q, want %q, where the layout adds the show folder itself", x.Dir, want)
		}
	}
}

func TestASubscriptionChecksWhatWasDownloadedUnlessItAllowsRepeats(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	a.stageFeedJob(feedJob("https://host.example/checked.bin", feed.Subscription{}))
	a.stageFeedJob(feedJob("https://host.example/repeats.bin", feed.Subscription{AllowDownloaded: true}))

	want := map[string]core.DownloadedCheck{
		"https://host.example/checked.bin": core.DownloadedCheckOn,
		"https://host.example/repeats.bin": core.DownloadedCheckOff,
	}
	for _, x := range a.Tasks() {
		if x.DownloadedCheck != want[x.URL] {
			t.Errorf("%s is checked against the downloads with %q, want %q", x.URL, x.DownloadedCheck, want[x.URL])
		}
	}
}

func TestASeriesMediathekEntryStillGetsItsShowFolder(t *testing.T) {
	a, base := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	j := feedJob("https://host.example/folge.mp4", feed.Subscription{Naming: feed.NamingChannel, Library: feed.LibrarySeries})
	j.FileName = "Folge 1.mp4"
	a.stageFeedJob(j)

	for _, x := range a.Tasks() {
		if want := filepath.Join(base, "Kellerfunk"); x.Dir != want {
			t.Errorf("a direct file of a series subscription landed in %q, want %q, since the series layout only files yt-dlp videos", x.Dir, want)
		}
	}
}
