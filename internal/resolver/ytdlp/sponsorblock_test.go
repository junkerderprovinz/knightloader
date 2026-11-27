package ytdlp

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestSponsorBlockKeepsOnlyCategoriesYtdlpKnows(t *testing.T) {
	got := SponsorBlock{
		Remove: []string{" Sponsor", "selfpromo", "sponsor", "ads", "poi_highlight", "chapter"},
		Mark:   []string{"intro", "poi_highlight", "chapter", "", "all"},
	}.Sanitize()

	if want := []string{"sponsor", "selfpromo"}; !slices.Equal(got.Remove, want) {
		t.Errorf("Remove = %q, want %q", got.Remove, want)
	}
	if want := []string{"intro", "poi_highlight", "chapter"}; !slices.Equal(got.Mark, want) {
		t.Errorf("Mark = %q, want %q", got.Mark, want)
	}
}

func TestSponsorBlockReadsTheSettingsShape(t *testing.T) {
	var o Options
	if err := json.Unmarshal([]byte(`{"sponsorBlock":{"remove":["sponsor"],"mark":["intro"]}}`), &o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.SponsorBlock.Remove, []string{"sponsor"}) || !slices.Equal(o.SponsorBlock.Mark, []string{"intro"}) {
		t.Errorf("SponsorBlock = %+v", o.SponsorBlock)
	}
	if o = o.Sanitize(); !slices.Equal(o.SponsorBlock.Remove, []string{"sponsor"}) {
		t.Errorf("Sanitize dropped a valid category: %+v", o.SponsorBlock)
	}
}

func TestBuildArgsPassesSponsorBlockToTheMediaRows(t *testing.T) {
	sb := SponsorBlock{Remove: []string{"sponsor", "selfpromo"}, Mark: []string{"intro"}}
	for _, v := range []Variant{VariantVideo, VariantAudio} {
		args := buildArgs("d", Options{Variant: v, SponsorBlock: sb})
		if got, _ := valueAfter(args, "--sponsorblock-remove"); got != "sponsor,selfpromo" {
			t.Errorf("%s: --sponsorblock-remove = %q, want sponsor,selfpromo", v, got)
		}
		if got, _ := valueAfter(args, "--sponsorblock-mark"); got != "intro" {
			t.Errorf("%s: --sponsorblock-mark = %q, want intro", v, got)
		}
	}
	for _, v := range []Variant{VariantThumbnail, VariantSubtitle, VariantDescription} {
		args := buildArgs("d", Options{Variant: v, SponsorBlock: sb})
		if hasArg(args, "--sponsorblock-remove") || hasArg(args, "--sponsorblock-mark") {
			t.Errorf("%s row asks SponsorBlock for segments it has nothing to cut: %v", v, args)
		}
	}
}

func TestBuildArgsWithoutSponsorBlockAsksForNoSegments(t *testing.T) {
	args := buildArgs("d", Options{})
	if hasArg(args, "--sponsorblock-remove") || hasArg(args, "--sponsorblock-mark") {
		t.Errorf("buildArgs(Options{}) asks SponsorBlock: %v", args)
	}
}

// Cutting segments out makes the file shorter than the runtime the site
// announced, which is no sign of a broken download.
func TestRunDoesNotCallAFileCutBySponsorBlockShort(t *testing.T) {
	_, rec := runFake(t, "sponsorcut:short", Options{
		SponsorBlock: SponsorBlock{Remove: []string{"sponsor"}},
		Measure:      Measure{Enabled: true, FailOnShort: true},
	})
	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
}

func TestRunStillCallsAFileShortWhenSponsorBlockCutNothing(t *testing.T) {
	_, rec := runFake(t, "video:short", Options{
		SponsorBlock: SponsorBlock{Remove: []string{"sponsor"}},
		Measure:      Measure{Enabled: true, FailOnShort: true},
	})
	if got := rec.last(); got.Status != core.StatusError {
		t.Fatalf("last update = %+v, want Error for a file half as long as announced", got)
	}
}
