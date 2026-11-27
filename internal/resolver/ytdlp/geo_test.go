package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// recordedMedia is the address yt-dlp 2026.10.26 printed for an on.orf.at
// episode, asked from Germany: one marked right = austria, which ORF's CDN
// redirects to its stand-in clip, and one marked worldwide.
func recordedMedia(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "geo", name))
	if err != nil {
		t.Fatal(err)
	}
	u, ok := strings.CutPrefix(strings.TrimSpace(string(b)), mediaPrefix)
	if !ok {
		t.Fatalf("%s holds %q, not a line the media print writes", name, b)
	}
	return u
}

func TestBuildArgsHasTheMediaRowsNameTheAddressTheyFetch(t *testing.T) {
	for _, o := range []Options{
		{Variant: VariantVideo},
		{Variant: VariantVideo, OutputTemplate: "%(id)s.%(ext)s"},
		{Variant: VariantAudio},
	} {
		args := buildArgs("d", o)
		var prints []string
		for i, a := range args {
			if a == "--print" && i+1 < len(args) {
				prints = append(prints, args[i+1])
			}
		}
		if !strings.Contains(strings.Join(prints, "\n"), "video:"+mediaPrefix) {
			t.Errorf("%+v: prints %q, want the media address among them", o, prints)
		}
		if !hasArg(args, "--no-simulate") || !hasArg(args, "--no-quiet") {
			t.Errorf("%+v: --print without --no-simulate and --no-quiet skips the download: %v", o, args)
		}
		if n := strings.Count(strings.Join(args, "\x00"), "--no-simulate"); n != 1 {
			t.Errorf("%+v: --no-simulate appears %d times", o, n)
		}
	}
}

func TestGeoPlaceholderKnowsORFsStandInClip(t *testing.T) {
	if u := recordedMedia(t, "orf-austria-only.txt"); !geoPlaceholder(u) {
		t.Errorf("geoPlaceholder(%q) = false", u)
	}
	for _, u := range []string{
		recordedMedia(t, "orf-worldwide.txt"),
		"https://rr3---sn-4g5e6nzz.googlevideo.com/videoplayback?expire=1",
		"",
	} {
		if geoPlaceholder(u) {
			t.Errorf("geoPlaceholder(%q) = true", u)
		}
	}
}

// From Germany yt-dlp downloads ORF's ten-second stand-in for an episode only
// Austria may see and exits 0, so the row would finish with the wrong video.
func TestRunFailsORFsStandInClipAsARegionBlock(t *testing.T) {
	dir, rec := runFake(t, "geoplaceholder:full", Options{Variant: VariantVideo})

	got := rec.last()
	if got.Status != core.StatusError || got.Reason != core.ReasonGeoBlocked {
		t.Fatalf("last update = %+v, want an Error with Reason %q", got, core.ReasonGeoBlocked)
	}
	if got.File != "" {
		t.Errorf("the row kept %q as its file", got.File)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("the stand-in left %s behind", e.Name())
	}
}

func TestRunFinishesAnORFEpisodeTheWholeWorldMaySee(t *testing.T) {
	_, rec := runFake(t, "geoworldwide:full", Options{Variant: VariantVideo})
	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
}

// testdata/geo/mediathekviewweb.txt holds one file of each folder the German
// and Austrian Mediatheks serve from, as MediathekViewWeb listed them, marked
// limited where only some countries may fetch it.
func TestGeoLimitedKnowsTheMediathekFoldersOfSomeCountries(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "geo", "mediathekviewweb.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		label, u, _ := strings.Cut(strings.TrimSpace(line), " ")
		if got, want := GeoLimited(u), label == "limited"; got != want {
			t.Errorf("GeoLimited(%q) = %v, want %v", u, got, want)
		}
	}
}

// An ordinary refusal elsewhere stays a refusal, whatever the path says.
func TestGeoLimitedLeavesOtherHostsAlone(t *testing.T) {
	for _, u := range []string{
		"https://example.com/de/file.mp4",
		"https://files.example.org/geo/de/file.mp4",
		"https://cdn-storage.br.de.example.com/geo/x.mp4",
		"not a url",
		"",
	} {
		if GeoLimited(u) {
			t.Errorf("GeoLimited(%q) = true", u)
		}
	}
}

func TestRunCallsAMediathekFileRefusedOutsideItsCountryARegionBlock(t *testing.T) {
	_, rec := runFake(t, "refusedlimited:full", Options{})
	if got := rec.last(); got.Status != core.StatusError || got.Reason != core.ReasonGeoBlocked {
		t.Fatalf("last update = %+v, want an Error with Reason %q", got, core.ReasonGeoBlocked)
	}
}

func TestRunLeavesARefusalOfAWorldwideFileToTheClassifier(t *testing.T) {
	_, rec := runFake(t, "refusedopen:full", Options{})
	if got := rec.last(); got.Status != core.StatusError || got.Reason != core.ReasonUnknown {
		t.Fatalf("last update = %+v, want an Error with no Reason of its own", got)
	}
}
