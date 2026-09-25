package settings

import (
	"reflect"
	"testing"
)

// The numbers in defaultTorrent are gopeed's, so a dependency bump that changes
// its DefaultConfig fails here rather than drifting between what this app
// claims its default is and what gopeed does.
func TestTorrentDefaultsMirrorGopeedsOwn(t *testing.T) {
	d := Defaults()
	if d.Torrent.SeedRatioTarget != 1.0 {
		t.Errorf("SeedRatioTarget = %v, want 1.0 (gopeed's own DefaultConfig)", d.Torrent.SeedRatioTarget)
	}
	if d.Torrent.SeedDurationSeconds != 7200 {
		t.Errorf("SeedDurationSeconds = %d, want 7200 (gopeed's own SeedTime: 120*60)", d.Torrent.SeedDurationSeconds)
	}
	if d.Torrent.Port != 0 {
		t.Errorf("Port = %d, want 0 (let gopeed/the OS pick, matching gopeed's own ListenPort: 0)", d.Torrent.Port)
	}
	if d.Torrent.UploadLimitKiBs != 0 {
		t.Errorf("UploadLimitKiBs = %d, want 0 (unlimited)", d.Torrent.UploadLimitKiBs)
	}
	if !d.Torrent.DHTEnabled {
		t.Error("DHTEnabled defaults to false, want true (ordinary public-swarm behaviour)")
	}
	if !d.Torrent.PEXEnabled {
		t.Error("PEXEnabled defaults to false, want true (ordinary public-swarm behaviour)")
	}
}

// Nothing typed into a number field produces a value with no honest meaning.
// The cases start from arbitrary settings rather than Defaults(), as
// TestSanitizeKeepsLimitsUsable does, so this holds for any bad document.
func TestSanitizeTorrentFloorsNegativesAndBadPort(t *testing.T) {
	cases := []struct {
		name string
		in   Torrent
		want Torrent
	}{
		{
			name: "negative ratio and duration and upload limit are floored to zero",
			in:   Torrent{SeedRatioTarget: -2.5, SeedDurationSeconds: -100, UploadLimitKiBs: -50},
			want: Torrent{SeedRatioTarget: 0, SeedDurationSeconds: 0, UploadLimitKiBs: 0},
		},
		{
			name: "a negative port collapses to 0 (let the OS pick)",
			in:   Torrent{Port: -1},
			want: Torrent{Port: 0},
		},
		{
			name: "a port above 65535 collapses to 0",
			in:   Torrent{Port: 70000},
			want: Torrent{Port: 0},
		},
		{
			name: "a legitimate port is left exactly as typed",
			in:   Torrent{Port: 51413},
			want: Torrent{Port: 51413},
		},
		{
			name: "positive values of every field survive untouched",
			in:   Torrent{SeedRatioTarget: 2.0, SeedDurationSeconds: 3600, UploadLimitKiBs: 512, Port: 6881},
			want: Torrent{SeedRatioTarget: 2.0, SeedDurationSeconds: 3600, UploadLimitKiBs: 512, Port: 6881},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeTorrent(Settings{Torrent: c.in}).Torrent
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("sanitizeTorrent(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

// A fresh install picks every file and adds and bans no tracker.
func TestFileRulesAndTrackerListsStartOff(t *testing.T) {
	d := Defaults().Torrent
	if d.MinFileSize != 0 || len(d.IncludeFiles) != 0 || len(d.ExcludeFiles) != 0 {
		t.Errorf("file rules default to %d, %q, %q, want none", d.MinFileSize, d.IncludeFiles, d.ExcludeFiles)
	}
	if len(d.ExtraTrackers) != 0 || d.TrackerListURL != "" || len(d.BannedTrackers) != 0 {
		t.Errorf("trackers default to %q, %q, %q, want none", d.ExtraTrackers, d.TrackerListURL, d.BannedTrackers)
	}
}

// A textarea leaves blank lines behind, and a blank exclude pattern matches
// every file. Patterns keep their spaces, which can be part of a match;
// addresses do not.
func TestSanitizeTorrentDropsBlankLinesAndKeepsPatternsAsTyped(t *testing.T) {
	in := Torrent{
		TorrentFileRules: TorrentFileRules{
			MinFileSize:  -5,
			IncludeFiles: []string{"", `\.mkv$`, "   "},
			ExcludeFiles: []string{" sample ", "", `(unclosed`},
		},
		ExtraTrackers:  []string{"  udp://tracker.example.org:6969/announce ", "", "\t"},
		TrackerListURL: "  https://lists.example.org/best.txt\n",
		BannedTrackers: []string{" tracker.bad.example ", ""},
	}
	got := sanitizeTorrent(Settings{Torrent: in}).Torrent
	want := Torrent{
		TorrentFileRules: TorrentFileRules{
			IncludeFiles: []string{`\.mkv$`},
			ExcludeFiles: []string{" sample ", `(unclosed`},
		},
		ExtraTrackers:  []string{"udp://tracker.example.org:6969/announce"},
		TrackerListURL: "https://lists.example.org/best.txt",
		BannedTrackers: []string{"tracker.bad.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeTorrent = %+v, want %+v", got, want)
	}
	if in.ExtraTrackers[0] != "  udp://tracker.example.org:6969/announce " {
		t.Error("sanitizeTorrent trimmed the caller's own list in place")
	}
}

// A drawer's own file selection replaces the global one as a whole, so a
// minimum of 0 there lets through the small files a global minimum skips.
func TestADrawersOwnFileSelectionReplacesTheGlobalOne(t *testing.T) {
	s := Defaults()
	s.Torrent.TorrentFileRules = TorrentFileRules{MinFileSize: 50 << 20, ExcludeFiles: []string{`\.nfo$`}}
	s.Categories = []Category{
		{ID: "music", TorrentFiles: &TorrentFileRules{}},
		{ID: "films", Dir: absPath("media", "films")},
	}
	if got := s.TorrentFileRulesFor("music"); got.MinFileSize != 0 || len(got.ExcludeFiles) != 0 {
		t.Errorf("TorrentFileRulesFor(music) = %+v, want the drawer's empty set", got)
	}
	for _, id := range []string{"films", "", "gone"} {
		if got := s.TorrentFileRulesFor(id); !reflect.DeepEqual(got, s.Torrent.TorrentFileRules) {
			t.Errorf("TorrentFileRulesFor(%q) = %+v, want the Torrents page's", id, got)
		}
	}
}

func TestSanitizeCleansADrawersFileSelectionOnACopy(t *testing.T) {
	own := &TorrentFileRules{MinFileSize: -1, ExcludeFiles: []string{"", `(?i)sample`}}
	got := sanitizeCategories(Settings{Categories: []Category{{ID: "films", TorrentFiles: own}}}).Categories[0].TorrentFiles
	if got == nil || got.MinFileSize != 0 || !reflect.DeepEqual(got.ExcludeFiles, []string{`(?i)sample`}) {
		t.Fatalf("TorrentFiles = %+v, want the minimum floored and the blank line gone", got)
	}
	if got == own || own.MinFileSize != -1 {
		t.Error("sanitizeCategories wrote through the caller's pointer")
	}
}

// Whatever the instance default says, a private torrent's EffectiveDHT and
// EffectivePEX are false, and a public torrent's are the instance default in
// both directions: a setting that is off must not read as on either.
func TestEffectiveDHTPEXPrivateAlwaysWins(t *testing.T) {
	cases := []struct {
		name              string
		dht, pex, private bool
		wantDHT, wantPEX  bool
	}{
		{"defaults, public torrent: both follow the setting", true, true, false, true, true},
		{"both enabled, private torrent: both forced off", true, true, true, false, false},
		{"both disabled, public torrent: both stay off", false, false, false, false, false},
		{"both disabled, private torrent: still off, not re-enabled by privacy", false, false, true, false, false},
		{"DHT only, private torrent: forced off despite the setting", true, false, true, false, false},
		{"PEX only, public torrent: PEX on, DHT off, independently", false, true, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := Torrent{DHTEnabled: c.dht, PEXEnabled: c.pex}
			if got := tr.EffectiveDHT(c.private); got != c.wantDHT {
				t.Errorf("EffectiveDHT(private=%v) with DHTEnabled=%v = %v, want %v", c.private, c.dht, got, c.wantDHT)
			}
			if got := tr.EffectivePEX(c.private); got != c.wantPEX {
				t.Errorf("EffectivePEX(private=%v) with PEXEnabled=%v = %v, want %v", c.private, c.pex, got, c.wantPEX)
			}
		})
	}
}
