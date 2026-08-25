package app

// Coverage for the "Variante" row family a yt-dlp-routed link stages with
// (app_ytdlp_variants.go): expandYtdlpVariants turning one staged task into
// five, HosterPresetFor/SetHosterPreset's own persistence, and
// SetTaskOptions's VariantQuality re-encoding a row's own sub-value without
// touching its kind.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// tasksSharingURL is a family's own membership test: every row
// expandYtdlpVariants created for one staged link shares that link's exact
// URL (see insertVariantSibling's own doc comment on why that sharing is
// deliberate rather than the ordinary "same link pasted twice" case put()
// refuses).
func tasksSharingURL(a *App, url string) []*core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*core.Task
	for _, x := range a.tasks {
		if x.URL == url {
			out = append(out, x)
		}
	}
	return out
}

// TestExpandYtdlpVariantsCreatesAllFiveRowsWithDefaultPreset is [79]'s own
// locked design (jdp, 2026-08-25's AskUserQuestion answer: "Alle 5 immer
// als Zeile"): a bare YouTube paste, with no preset saved for the host yet,
// ends up as five sibling tasks sharing one URL and package, every one of
// them enabled (ytdlp.DefaultHosterPreset's own Variants() default).
func TestExpandYtdlpVariantsCreatesAllFiveRowsWithDefaultPreset(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{title: "Never Gonna Give You Up", done: done})

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1 (the sibling rows are not staged through this same path)", len(created))
	}

	waitFor(t, "expandYtdlpVariants to add the four sibling rows", func() bool {
		return len(tasksSharingURL(a, url)) == 5
	})

	family := tasksSharingURL(a, url)
	seen := map[ytdlp.Variant]bool{}
	for _, x := range family {
		if x.URL != url {
			t.Errorf("sibling URL = %q, want every row to share %q", x.URL, url)
		}
		if x.Package != family[0].Package {
			t.Errorf("sibling package = %q, want every row in the same package %q", x.Package, family[0].Package)
		}
		if !x.Enabled {
			t.Errorf("row %q started disabled, want every row enabled under the default preset", x.Variant)
		}
		kind, _ := variantDecode(x.Variant)
		seen[kind] = true
	}
	for _, v := range ytdlp.Variants() {
		if !seen[v] {
			t.Errorf("no row was created for variant %q", v)
		}
	}
}

// TestExpandYtdlpVariantsRespectsASavedHosterPreset is the gear badge's own
// write path, read back at staging time: a preset that leaves the
// thumbnail and description rows out of Variants() (the "which variants
// this host starts with enabled" list) stages those two rows disabled -
// still present as their own rows (jdp: "Alle 5 immer als Zeile" was never
// conditional on the preset), just not enabled by default.
func TestExpandYtdlpVariantsRespectsASavedHosterPreset(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{title: "Some Video", done: done})

	if err := a.SetHosterPreset("youtube.com", ytdlp.HosterPreset{
		Variants:    []ytdlp.Variant{ytdlp.VariantVideo, ytdlp.VariantAudio, ytdlp.VariantSubtitle},
		Quality:     ytdlp.Quality720p,
		AudioFormat: "mp3",
	}); err != nil {
		t.Fatalf("SetHosterPreset: %v", err)
	}

	const url = "https://youtube.com/watch?v=preseeded00"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}

	waitFor(t, "expandYtdlpVariants to add the four sibling rows", func() bool {
		return len(tasksSharingURL(a, url)) == 5
	})

	for _, x := range tasksSharingURL(a, url) {
		kind, sub := variantDecode(x.Variant)
		switch kind {
		case ytdlp.VariantVideo:
			if !x.Enabled {
				t.Error("video row disabled, want it enabled per the saved preset")
			}
			if sub != string(ytdlp.Quality720p) {
				t.Errorf("video row's own quality = %q, want the saved preset's %q", sub, ytdlp.Quality720p)
			}
		case ytdlp.VariantAudio:
			if !x.Enabled {
				t.Error("audio row disabled, want it enabled per the saved preset")
			}
			if sub != "mp3" {
				t.Errorf("audio row's own format = %q, want the saved preset's %q", sub, "mp3")
			}
		case ytdlp.VariantSubtitle:
			if !x.Enabled {
				t.Error("subtitle row disabled, want it enabled per the saved preset")
			}
		case ytdlp.VariantThumbnail, ytdlp.VariantDescription:
			if x.Enabled {
				t.Errorf("%q row enabled, want it disabled - the saved preset leaves it out of Variants()", kind)
			}
		}
	}
}

// TestHosterPresetForReturnsTheDefaultWhenNothingWasSaved is the gear
// badge's own read path before anybody has ever opened it for a host: it
// must not 404 or panic, it answers with exactly what a bare paste would
// already stage.
func TestHosterPresetForReturnsTheDefaultWhenNothingWasSaved(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	got := a.HosterPresetFor("neverconfigured.example")
	want := ytdlp.DefaultHosterPreset()
	if len(got.Variants) != len(want.Variants) || got.Quality != want.Quality || got.AudioFormat != want.AudioFormat {
		t.Errorf("HosterPresetFor(unset host) = %+v, want the default %+v", got, want)
	}
}

// TestSetHosterPresetPersistsAcrossHosts is the multi-host shape
// SetHosterPreset's own map-rebuild has to get right: saving one host's
// preset must not disturb another host's already-saved one, the exact bug
// class a loop-variable/map-key mistake in sanitizeResolvers was found and
// fixed for elsewhere this same round (settings_resolvers.go).
func TestSetHosterPresetPersistsAcrossHosts(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	if err := a.SetHosterPreset("youtube.com", ytdlp.HosterPreset{
		Variants: []ytdlp.Variant{ytdlp.VariantVideo}, Quality: ytdlp.Quality480p, AudioFormat: "best",
	}); err != nil {
		t.Fatalf("SetHosterPreset(youtube.com): %v", err)
	}
	if err := a.SetHosterPreset("vimeo.com", ytdlp.HosterPreset{
		Variants: []ytdlp.Variant{ytdlp.VariantAudio}, Quality: ytdlp.QualityBest, AudioFormat: "opus",
	}); err != nil {
		t.Fatalf("SetHosterPreset(vimeo.com): %v", err)
	}

	yt := a.HosterPresetFor("youtube.com")
	if len(yt.Variants) != 1 || yt.Variants[0] != ytdlp.VariantVideo || yt.Quality != ytdlp.Quality480p {
		t.Errorf("youtube.com preset = %+v, want the one just saved for it", yt)
	}
	vm := a.HosterPresetFor("vimeo.com")
	if len(vm.Variants) != 1 || vm.Variants[0] != ytdlp.VariantAudio || vm.AudioFormat != "opus" {
		t.Errorf("vimeo.com preset = %+v, want the one just saved for it, undisturbed by youtube.com's own save", vm)
	}
}

// TestSetTaskOptionsVariantQualityKeepsTheRowsOwnKind is the Variante
// column's own write path (columns.tsx's VarianteCell): editing a row's
// quality/format picker must re-encode only the sub-value, never the row's
// own fixed kind - a video row stays a video row no matter what quality is
// picked for it.
func TestSetTaskOptionsVariantQualityKeepsTheRowsOwnKind(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=quality0001"
	task := putTask(t, a, core.Task{
		URL: url, Name: url, Package: "watch", Status: core.StatusCollected, Enabled: true,
		Variant: "video:best",
	})

	q := "720p"
	if err := a.SetTaskOptions([]string{task.ID}, TaskOptions{VariantQuality: &q}); err != nil {
		t.Fatalf("SetTaskOptions: %v", err)
	}

	live := snapshot(t, a, task.ID)
	kind, sub := variantDecode(live.Variant)
	if kind != ytdlp.VariantVideo {
		t.Errorf("kind = %q after a quality-only edit, want it left as %q", kind, ytdlp.VariantVideo)
	}
	if sub != "720p" {
		t.Errorf("sub-value = %q, want the newly picked %q", sub, "720p")
	}
}

// wireYtdlp/fakeYtdlpBackend used above are ytdlp_probe_test.go's own
// fixtures, reused here rather than duplicated.

// TestExpandYtdlpVariantsFamilyStillRenamesThePackageOnceNamed is the
// intersection [79] and [35b]'s own fix never got tested together: a real
// AddLinks paste creates the primary PLUS four siblings before the title
// probe ever answers (stage()'s own synchronous expandYtdlpVariants call,
// app_links.go), so by the time setTaskName's own guard asks
// noSiblingHasARealNameYet, every one of those four siblings is already a
// "sibling sharing this package" - if that guard were not sibling-URL-aware
// the same way setTaskName's OWN propagation loop already is, a link's
// entire five-row family would stay named "watch" forever, the very bug
// this test exists to catch before a live deploy does.
func TestExpandYtdlpVariantsFamilyStillRenamesThePackageOnceNamed(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{title: "Me at the zoo", done: done})

	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}

	waitFor(t, "every row in the family to pick up the resolved package", func() bool {
		family := tasksSharingURL(a, url)
		if len(family) != 5 {
			return false
		}
		for _, x := range family {
			if x.Package != "Me at the zoo" {
				return false
			}
		}
		return true
	})
}
