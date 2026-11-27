package feed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

func TestARowFromAnOlderSettingsFileKeepsItsMeaning(t *testing.T) {
	var subs []Subscription
	raw := `[{"url":"https://example.invalid/rss.xml","intervalMinutes":30,"titleFilter":"^Folge","dir":"/dl/funk","priority":2}]`
	if err := json.Unmarshal([]byte(raw), &subs); err != nil {
		t.Fatal(err)
	}
	got := Sanitize(subs)
	if len(got) != 1 {
		t.Fatalf("got %d rows", len(got))
	}
	s := got[0]
	if s.Kind != KindRSS {
		t.Errorf("kind is %q, want an old row to read as an RSS feed", s.Kind)
	}
	if s.ExcludeFilter != "" || s.MinMinutes != 0 || s.MaxMinutes != 0 || s.AutoStart || s.Media != nil ||
		s.Naming != "" || s.Library != "" || s.SponsorBlock != nil || s.Backfill != nil || s.Mediathek != nil || s.AllowDownloaded {
		t.Errorf("an old row gained an opinion it never had: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("an old row is refused: %v", err)
	}
}

func TestEveryFieldSurvivesASaveAndALoad(t *testing.T) {
	two := 2
	in := Subscription{
		URL:             "https://www.youtube.com/@kurzgesagt/videos",
		Kind:            KindChannel,
		Title:           "Kurzgesagt",
		IntervalMinutes: 60,
		TitleFilter:     "Space",
		ExcludeFilter:   "#shorts",
		MinMinutes:      2,
		MaxMinutes:      90,
		Dir:             "/dl/kg",
		Priority:        &two,
		AutoStart:       true,
		Media:           &ytdlp.HosterPreset{Variants: []ytdlp.Variant{ytdlp.VariantAudio}, VideoFormat: "best", Quality: ytdlp.Quality720p, AudioFormat: "best"},
		Naming:          NamingChannel,
		Library:         LibrarySeries,
		SponsorBlock:    &SponsorBlock{Remove: []string{"sponsor"}, Mark: []string{"intro", "chapter"}},
		Backfill:        &Backfill{Mode: BackfillCount, N: 20},
		AllowDownloaded: true,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"kind"`, `"title"`, `"excludeFilter"`, `"minMinutes"`, `"maxMinutes"`, `"autoStart"`,
		`"media"`, `"naming"`, `"library"`, `"sponsorBlock"`, `"backfill"`, `"allowDownloaded"`} {
		if !strings.Contains(string(b), name) {
			t.Errorf("the stored row lacks %s: %s", name, b)
		}
	}
	var out Subscription
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	got := Sanitize([]Subscription{out})
	if len(got) != 1 || !reflect.DeepEqual(got[0], in) {
		t.Fatalf("the row changed on its way through settings.json:\n got %+v\nwant %+v", got, in)
	}
	if err := got[0].Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAMediathekRowSurvivesASaveAndALoad(t *testing.T) {
	in := Subscription{
		URL:        "https://mediathekviewweb.de/#query=%21ard%20%23tatort",
		Kind:       KindMediathek,
		Title:      "Tatort",
		MinMinutes: 80,
		Mediathek:  &Mediathek{Channel: "ARD", Topic: "Tatort", Quality: QualityHD, Subtitles: true},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Subscription
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	got := Sanitize([]Subscription{out})
	if len(got) != 1 || !reflect.DeepEqual(got[0], in) {
		t.Fatalf("got %+v, want %+v", got, in)
	}
}

func TestSanitizeCorrectsWhatNobodyCouldHaveMeant(t *testing.T) {
	got := Sanitize([]Subscription{
		{URL: "https://example.invalid/a.xml", Kind: " RSS ", ExcludeFilter: "  Trailer ", MinMinutes: -3, MaxMinutes: -1, Naming: "bogus", Title: "  Kellerfunk "},
		// Backfill and mediathek mean nothing to an RSS feed.
		{URL: "https://example.invalid/b.xml", Backfill: &Backfill{Mode: BackfillAll}, Mediathek: &Mediathek{Topic: "x"}},
		{URL: "https://www.youtube.com/@x", Kind: KindChannel, Media: &ytdlp.HosterPreset{Variants: []ytdlp.Variant{"audio", "audio", "nonsense"}, Quality: "9000p"}},
		{URL: "https://mediathekviewweb.de/#query=tatort", Kind: KindMediathek, Mediathek: &Mediathek{Topic: " Tatort ", Quality: "ultra"}},
		{URL: "https://example.invalid/c.xml", SponsorBlock: &SponsorBlock{Remove: []string{" Sponsor ", "sponsor"}, Mark: []string{"INTRO"}}},
	})
	if len(got) != 5 {
		t.Fatalf("got %d rows", len(got))
	}
	a := got[0]
	if a.Kind != KindRSS || a.ExcludeFilter != "Trailer" || a.MinMinutes != 0 || a.MaxMinutes != 0 || a.Naming != "" || a.Title != "Kellerfunk" {
		t.Errorf("row 0: %+v", a)
	}
	if got[1].Backfill != nil || got[1].Mediathek != nil {
		t.Errorf("an RSS row kept options of another kind: %+v", got[1])
	}
	m := got[2].Media
	if m == nil || !reflect.DeepEqual(m.Variants, []ytdlp.Variant{ytdlp.VariantAudio}) || m.Quality != ytdlp.QualityBest {
		t.Errorf("the media preset was not cleaned like a host preset: %+v", m)
	}
	if mt := got[3].Mediathek; mt == nil || mt.Topic != "Tatort" || mt.Quality != QualityHD {
		t.Errorf("the mediathek search was not cleaned: %+v", mt)
	}
	if sb := got[4].SponsorBlock; sb == nil || !reflect.DeepEqual(sb.Remove, []string{"sponsor"}) || !reflect.DeepEqual(sb.Mark, []string{"intro"}) {
		t.Errorf("the SponsorBlock categories were not cleaned: %+v", sb)
	}
}

func TestAMediathekRowWithoutAnAddressGetsOne(t *testing.T) {
	got := Sanitize([]Subscription{{Kind: KindMediathek, Mediathek: &Mediathek{Channel: "ARD", Topic: "Tatort", Title: "Kommissar"}, MinMinutes: 80}})
	if len(got) != 1 {
		t.Fatal("a mediathek row typed in by hand was dropped for having no address")
	}
	if !strings.HasPrefix(got[0].URL, "https://mediathekviewweb.de/#query=") {
		t.Fatalf("address is %q", got[0].URL)
	}
	back, ok := ParseMediathekURL(got[0].URL)
	if !ok || back.Mediathek.Channel != "ARD" || back.Mediathek.Topic != "Tatort" || back.Mediathek.Title != "Kommissar" || back.MinMinutes != 80 {
		t.Errorf("the address does not say what the row searches for: %q reads back as %+v", got[0].URL, back)
	}
	if err := got[0].Validate(); err != nil {
		t.Error(err)
	}
}

func TestValidateRefusesWhatWouldChangeWhatTheRowDoes(t *testing.T) {
	base := "https://example.invalid/rss.xml"
	for _, tc := range []struct {
		name string
		sub  Subscription
	}{
		{"an exclude pattern that will not compile", Subscription{URL: base, ExcludeFilter: "(["}},
		{"a lower bound above the upper", Subscription{URL: base, MinMinutes: 30, MaxMinutes: 10}},
		{"an unknown kind", Subscription{URL: base, Kind: "podcast"}},
		{"a backfill count of zero", Subscription{URL: base, Kind: KindChannel, Backfill: &Backfill{Mode: BackfillCount}}},
		{"a backfill of zero days", Subscription{URL: base, Kind: KindChannel, Backfill: &Backfill{Mode: BackfillDays}}},
		{"an unknown backfill", Subscription{URL: base, Kind: KindChannel, Backfill: &Backfill{Mode: "some"}}},
		{"a mediathek search for nothing", Subscription{URL: base, Kind: KindMediathek, Mediathek: &Mediathek{}}},
		{"a mediathek row with no search", Subscription{URL: base, Kind: KindMediathek}},
		{"media that stages nothing", Subscription{URL: base, Media: &ytdlp.HosterPreset{}}},
		{"an unknown SponsorBlock category", Subscription{URL: base, SponsorBlock: &SponsorBlock{Remove: []string{"ads"}}}},
		{"a mark-only category removed", Subscription{URL: base, SponsorBlock: &SponsorBlock{Remove: []string{"chapter"}}}},
		{"an unknown library", Subscription{URL: base, Library: "music"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Sanitize([]Subscription{tc.sub})[0].Validate(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, tc := range []struct {
		name string
		sub  Subscription
	}{
		{"only a lower bound", Subscription{URL: base, MinMinutes: 5}},
		{"only an upper bound", Subscription{URL: base, MaxMinutes: 5}},
		{"equal bounds", Subscription{URL: base, MinMinutes: 5, MaxMinutes: 5}},
		{"a channel with every backfill", Subscription{URL: base, Kind: KindChannel, Backfill: &Backfill{Mode: BackfillAll}}},
		{"a mediathek title alone", Subscription{URL: base, Kind: KindMediathek, Mediathek: &Mediathek{Title: "Tatort"}}},
		{"every SponsorBlock category", Subscription{URL: base, SponsorBlock: &SponsorBlock{
			Remove: []string{"sponsor", "intro", "outro", "selfpromo", "preview", "filler", "interaction", "music_offtopic", "hook"},
			Mark:   []string{"poi_highlight", "chapter"},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Sanitize([]Subscription{tc.sub})[0].Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
