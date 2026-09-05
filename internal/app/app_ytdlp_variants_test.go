package app

// Coverage for the "Variante" row family a yt-dlp-routed link stages with
// (app_ytdlp_variants.go): expandYtdlpVariants turning one staged task into
// five, HosterPresetFor/SetHosterPreset's own persistence, and
// SetTaskOptions's VariantQuality re-encoding a row's own sub-value without
// touching its kind.

import (
	"context"
	"sync"
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
//
// It returns COPIES, not the live pointers. Handing out pointers and releasing
// the lock let a caller read one row's Package before the title probe landed
// and the next row's after, so a family that was consistent at every instant
// looked torn - "sibling package = %q, want every row in the same package %q"
// with the two halves of the same rename on either side of the comma. It was
// also a plain data race: the probe goroutine writes those fields under a.mu
// while the reader held nothing.
func tasksSharingURL(a *App, url string) []core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []core.Task
	for _, x := range a.tasks {
		if x.URL == url {
			out = append(out, *x)
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

// familyOf is the two lines every test below opens with: stage one yt-dlp link
// whose title probe is still outstanding, and hand back the five rows it
// became.
func familyOf(t *testing.T, url, title string) (*App, []core.Task) {
	t.Helper()
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wireYtdlp(a, blockingYtdlpBackend{title: title, release: release})

	if created := a.AddLinks([]string{url}, ""); len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	waitFor(t, "expandYtdlpVariants to add the four sibling rows", func() bool {
		return len(tasksSharingURL(a, url)) == 5
	})
	return a, tasksSharingURL(a, url)
}

// rowOf picks one named row out of a family.
func rowOf(t *testing.T, family []core.Task, want ytdlp.Variant) string {
	t.Helper()
	for _, x := range family {
		if kind, _ := variantDecode(x.Variant); kind == want {
			return x.ID
		}
	}
	t.Fatalf("no %q row in the family", want)
	return ""
}

// TestSetPackageMovesTheWholeVariantFamily locks the invariant the rest of
// this area is built on: the five rows of one video are ONE thing, so they
// share one package, and picking a package for any one of them files all five.
//
// The row picked here is deliberately not the primary. A family that only
// holds together when the video row leads is not holding together.
func TestSetPackageMovesTheWholeVariantFamily(t *testing.T) {
	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	a, family := familyOf(t, url, "Me at the zoo")

	a.SetPackage([]string{rowOf(t, family, ytdlp.VariantSubtitle)}, "Zoo trip")

	for _, x := range tasksSharingURL(a, url) {
		if x.Package != "Zoo trip" {
			t.Errorf("row %q is in package %q, want the whole family in %q", x.Variant, x.Package, "Zoo trip")
		}
	}
}

// TestAFamilyLeavesTheURLGuessWhenOnlyThePrimaryIsInTheIdList is the losing
// ordering's own end state, set up by hand rather than raced for.
//
// TestExpandYtdlpVariantsFamilyStillRenamesThePackageOnceNamed reaches this
// same defect through a real paste, but only when the probe happens to answer
// inside one particular gap - about one run in seven, which is a fine way to
// DISCOVER a bug and a poor way to guard against its return. This states the
// state directly: every row of the family already carries the real name (that
// is what setTaskName's own propagation loop does when it runs while the
// package is still unset), all five are filed under the URL path's guess, and
// the id list nameBucket hands on holds the primary alone, because the four
// siblings were created inside stage() after the bucket was assembled and are
// in no list anywhere.
//
// It failed twice over before the fix: noSiblingHasARealNameYet counted the
// family's own rows as strangers with real names, so all five vetoed each
// other's rename; and even once the primary was let through, the package was
// written to that one row, leaving its four siblings behind.
func TestAFamilyLeavesTheURLGuessWhenOnlyThePrimaryIsInTheIdList(t *testing.T) {
	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	a, family := familyOf(t, url, "Me at the zoo")
	primary := rowOf(t, family, ytdlp.VariantVideo)

	a.mu.Lock()
	for _, x := range a.tasks {
		if x.URL == url {
			x.Name = "Me at the zoo"
			x.Package = "watch"
		}
	}
	a.mu.Unlock()

	a.regressGuessedPackages([]string{primary})

	for _, x := range tasksSharingURL(a, url) {
		if x.Package != "Me at the zoo" {
			t.Errorf("row %q stayed in %q, want the whole family re-filed under %q", x.Variant, x.Package, "Me at the zoo")
		}
	}
}

// TestACoincidentalPackageCollisionIsStillLeftAlone is the other half of the
// same guard, and the reason it cannot simply be deleted: two bare YouTube
// links pasted together both guess the package "watch" without being related
// at all, and one of them resolving its title must not drag the other one's
// row along. Only rows sharing an EXACT URL are family.
func TestACoincidentalPackageCollisionIsStillLeftAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wireYtdlp(a, blockingYtdlpBackend{title: "unused", release: release})

	const mine = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	const theirs = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	for _, u := range []string{mine, theirs} {
		if created := a.AddLinks([]string{u}, ""); len(created) != 1 {
			t.Fatalf("AddLinks(%s) created %d tasks, want 1", u, len(created))
		}
	}
	waitFor(t, "both links to become their own five-row families", func() bool {
		return len(tasksSharingURL(a, mine)) == 5 && len(tasksSharingURL(a, theirs)) == 5
	})

	// Both families land in the same guessed package, which is the whole
	// point: every bare YouTube watch page guesses "watch". The other link's
	// rows already carry a real name, so they are a resolved batch this one
	// must not touch.
	var primary string
	a.mu.Lock()
	for _, x := range a.tasks {
		switch x.URL {
		case mine:
			x.Name, x.Package = "Me at the zoo", "watch"
			if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantVideo {
				primary = x.ID
			}
		case theirs:
			x.Name, x.Package = "Never Gonna Give You Up", "watch"
		}
	}
	a.mu.Unlock()

	a.regressGuessedPackages([]string{primary})

	for _, x := range tasksSharingURL(a, mine) {
		if x.Package != "watch" {
			t.Errorf("row %q moved to %q, want the whole batch left in %q - an unrelated link already resolved into it", x.Variant, x.Package, "watch")
		}
	}
	for _, x := range tasksSharingURL(a, theirs) {
		if x.Package != "watch" {
			t.Errorf("the other link's row %q moved to %q, want it untouched in %q", x.Variant, x.Package, "watch")
		}
	}
}

// putYtdlpFamily builds a five-row "Variante" family directly (putTask, no
// AddLinks/probe involved) sharing one URL, one per subs entries plus
// thumbnail/subtitle (which take no sub-value) - the shape
// expandYtdlpVariants itself would have produced, minus the async probe
// applyProbeFormats's own tests want to call by hand instead.
func putYtdlpFamily(t *testing.T, a *App, url string, subs map[ytdlp.Variant]string) map[ytdlp.Variant]*core.Task {
	t.Helper()
	family := map[ytdlp.Variant]*core.Task{}
	for _, v := range ytdlp.Variants() {
		family[v] = putTask(t, a, core.Task{
			URL: url, Name: "Some Title", Package: "some-package", Status: core.StatusCollected, Enabled: true,
			Variant: variantEncode(v, subs[v]),
		})
	}
	return family
}

// A realistic mixed format list, the same shape backend_test.go's own
// "formats" TestMain case uses: two video-only tracks (144p/1080p), one
// audio-only track, one combined progressive track. Declared once so every
// test below reasons about the identical source.
var testProbeFormats = []ytdlp.FormatEntry{
	{FormatID: "160", Ext: "mp4", Vcodec: "avc1.4d400b", Acodec: "none", Height: 144, Filesize: 195278},
	{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FilesizeApprox: 52428800},
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Filesize: 3145728, Abr: 129},
	{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, Filesize: 8388608},
}

// TestApplyProbeFormatsMarksTheWholeFamilyOnline is [83b]'s own fix (jdp,
// 2026-08-25: "der [status-punkt] zeigt immer noch keine farbe an"): a
// probe that came back AT ALL is itself the availability check a yt-dlp
// link never otherwise gets before download, and since every row in the
// family is the same source, all five get the same verdict.
func TestApplyProbeFormatsMarksTheWholeFamilyOnline(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=online0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	for kind, task := range family {
		if live := snapshot(t, a, task.ID); live.Online != core.AvailOnline {
			t.Errorf("%q row Online = %q, want %q", kind, live.Online, core.AvailOnline)
		}
	}
}

// TestApplyProbeFormatsSetsDescriptionExt is [87]'s zero-cost, always
// correct case: yt-dlp hardcodes .description for this output regardless
// of the source, so it needs no format-list lookup at all.
func TestApplyProbeFormatsSetsDescriptionExt(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=ext0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantDescription].ID)
	if live.Ext != "description" {
		t.Errorf("description row Ext = %q, want %q", live.Ext, "description")
	}
}

// TestApplyProbeFormatsSetsFixedAudioFormatExtNotSize is [87]/[89]'s own
// split for a fixed --audio-format target: the extension is certain
// (ffmpeg's own conversion target), the size is not (transcoding changes
// the byte count unpredictably from the source track's own) - so only Ext
// may be set, Size must stay at its own zero/unknown value.
func TestApplyProbeFormatsSetsFixedAudioFormatExtNotSize(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=ext0002"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantAudio: "mp3"})

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if live.Ext != "mp3" {
		t.Errorf("fixed-format audio row Ext = %q, want %q", live.Ext, "mp3")
	}
	if live.Size != 0 {
		t.Errorf("fixed-format audio row Size = %d, want 0 (transcoded size is not derivable from the source track)", live.Size)
	}
}

// TestApplyProbeFormatsSetsBestAudioExtAndSize is the "best" audio row's own
// case: -x with no --audio-format is a straight extract, not a transcode,
// so BOTH the container and the size are real facts read straight off the
// matched source track's own reported values, not guesses (jdp, 2026-08-26:
// "dateiendungen werden immer noch nicht angezeigt" - an earlier version of
// this function left Ext unset here on the theory that "the container
// varies by source", which is true across different sources but not true
// of the ONE specific matched format this row already resolved to: its own
// Ext field already says what container it is in).
func TestApplyProbeFormatsSetsBestAudioExtAndSize(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=size0001"
	family := putYtdlpFamily(t, a, url, nil) // VariantAudio's own sub defaults to "" (best)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if live.Ext != "m4a" {
		t.Errorf("best-audio row Ext = %q, want the matched track's own %q", live.Ext, "m4a")
	}
	if live.Size != 3145728 {
		t.Errorf("best-audio row Size = %d, want the audio-only entry's own filesize %d", live.Size, 3145728)
	}
}

// TestApplyProbeFormatsSetsAvailableAudioFormats is [87]/[88]'s own
// extension to the audio row (jdp, 2026-08-26: "bei der audio spur sollen
// nur die formate angezeigt werden die wirklich von hoster angeboten
// werden. Youtube bietet zb keine flac audio"): testProbeFormats' own
// audio-only entry is mp4a.40.2 (AAC), which maps to "m4a" - "best" is
// always kept alongside it, and the fixed transcode targets nothing in the
// source actually offers (mp3/opus/wav/flac) do not appear.
func TestApplyProbeFormatsSetsAvailableAudioFormats(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=formats0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if got := live.AvailableAudioFormats; len(got) != 2 || got[0] != "best" || got[1] != "m4a" {
		t.Errorf("AvailableAudioFormats = %v, want exactly [best m4a]", got)
	}
}

// TestApplyProbeFormatsSetsAvailableAudioBitrates is [87]/[88]'s own
// bitrate case (jdp, 2026-08-26: "auch die audioqualitäten! bei allen
// hostern!"): testProbeFormats' own audio-only entry reports abr=129, so
// the bitrate menu keeps Auto/64/96/128 and drops 160 and above.
func TestApplyProbeFormatsSetsAvailableAudioBitrates(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=bitrates0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	want := []string{"", "64", "96", "128"}
	got := live.AvailableAudioBitrates
	if len(got) != len(want) {
		t.Fatalf("AvailableAudioBitrates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AvailableAudioBitrates = %v, want %v", got, want)
			break
		}
	}
}

// TestApplyProbeFormatsSetsVideoExtOnlyWhenAMergeWouldHappen is [87]'s own
// video case: testProbeFormats has both a real video-only track and a real
// audio-only track, so formatSelector's own bestvideo+bestaudio selector
// will merge them - buildArgs' own forced --merge-output-format mkv
// (backend.go) then makes mkv a fact this function can state.
func TestApplyProbeFormatsSetsVideoExtOnlyWhenAMergeWouldHappen(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=merge0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if live.Ext != "mkv" {
		t.Errorf("video row Ext = %q, want %q - the source has both a video-only and an audio-only track to merge", live.Ext, "mkv")
	}
}

// TestApplyProbeFormatsLeavesVideoExtUnsetWithNoMergeToPromise is the
// opposite end of the same rule: a source with no real video-only/audio-only
// pair (the exact shape a very old upload's format list can have) takes
// formatSelector's own muxed-fallback path instead, where
// --merge-output-format has no effect at all - claiming mkv there would be
// wrong, not merely unhelpful, so Ext stays unset the same way it already
// does for AvailableQualities collapsing to best/custom alone.
func TestApplyProbeFormatsLeavesVideoExtUnsetWithNoMergeToPromise(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=nomerge0001"
	family := putYtdlpFamily(t, a, url, nil)
	noAdaptiveTracks := []ytdlp.FormatEntry{
		{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, Filesize: 8388608},
	}

	a.applyProbeFormats(url, noAdaptiveTracks)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if live.Ext != "" {
		t.Errorf("video row Ext = %q, want it left unset - no video-only/audio-only pair exists to merge", live.Ext)
	}
}

// TestApplyProbeFormatsSetsThumbnailAndSubtitleExt is [87]'s own remaining
// two cases: both are forced conversions (--convert-thumbnails jpg,
// --sub-format srt, backend.go), so Ext is a fixed fact independent of
// anything in the probed formats list.
func TestApplyProbeFormatsSetsThumbnailAndSubtitleExt(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=fixedext0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	if live := snapshot(t, a, family[ytdlp.VariantThumbnail].ID); live.Ext != "jpg" {
		t.Errorf("thumbnail row Ext = %q, want %q", live.Ext, "jpg")
	}
	if live := snapshot(t, a, family[ytdlp.VariantSubtitle].ID); live.Ext != "srt" {
		t.Errorf("subtitle row Ext = %q, want %q", live.Ext, "srt")
	}
}

// TestApplyProbeFormatsConstrainsVideoAvailableQualities is [88]'s own fix
// (jdp, 2026-08-25: "man soll nur die varianten auswählen können die
// wirklich verfügbar sind"): testProbeFormats' own tallest real video track
// is 1080p, so nothing above that should be offered - 2160p/1440p drop out,
// 1080p and everything under it (plus best/custom, always kept) survive.
func TestApplyProbeFormatsConstrainsVideoAvailableQualities(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=avail0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	got := map[string]bool{}
	for _, q := range live.AvailableQualities {
		got[q] = true
	}
	for _, want := range []string{"best", "1080p", "720p", "480p", "360p", "custom"} {
		if !got[want] {
			t.Errorf("AvailableQualities = %v, missing expected %q", live.AvailableQualities, want)
		}
	}
	for _, unwanted := range []string{"2160p", "1440p"} {
		if got[unwanted] {
			t.Errorf("AvailableQualities = %v, want %q excluded - the source has no track above 1080p", live.AvailableQualities, unwanted)
		}
	}
}

// TestApplyProbeFormatsSetsVideoSizeAtItsOwnQualityCap is [89]'s own video
// case: the estimate must match THIS row's own currently-picked quality,
// not the source's tallest track - a 360p-capped row gets the 360p track's
// own size (8388608), not the 1080p track's.
func TestApplyProbeFormatsSetsVideoSizeAtItsOwnQualityCap(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=size0002"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantVideo: "360p"})

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if live.Size != 8388608 {
		t.Errorf("360p-capped video row Size = %d, want the 360p track's own size %d, not the 1080p track's", live.Size, 8388608)
	}
}

// countingYtdlpBackend answers ProbeTitle with a fixed format list and records
// every URL it was asked about.
//
// fakeYtdlpBackend (ytdlp_probe_test.go) closes a channel on every call, which
// makes a SECOND call a panic - the right shape for "the probe ran", the wrong
// one for a function whose whole contract is which rows it does and does not
// ask about. The slice lives behind a pointer so the value receivers every
// backend method uses still share one record.
type countingYtdlpBackend struct {
	formats []ytdlp.FormatEntry
	mu      *sync.Mutex
	asked   *[]string
}

func (countingYtdlpBackend) Download(string, string, map[string]string, int) {}
func (countingYtdlpBackend) Pause(string)                                    {}
func (countingYtdlpBackend) Resume(string)                                   {}
func (countingYtdlpBackend) Remove(string, bool)                             {}

func (c countingYtdlpBackend) ProbeTitle(_ context.Context, url string) (ytdlp.ProbeResult, error) {
	c.mu.Lock()
	*c.asked = append(*c.asked, url)
	c.mu.Unlock()
	return ytdlp.ProbeResult{Title: "Some Title", Formats: c.formats}, nil
}

// TestBackfillNarrowsTheMenusOfRowsStagedBeforeTheProbeExisted is the live
// complaint (jdp, 2026-09-05: "z.b. flac wir gar nicht von Youtube
// angeboten"). Measured on the running instance the same day: five YouTube
// rows from August, every one of them with no AvailableAudioFormats at all -
// and empty means "no opinion", so the picker fell back to the full static
// menu, flac included. The narrowing was already right; there was nothing to
// narrow with, and nothing ever went back to fetch it.
func TestBackfillNarrowsTheMenusOfRowsStagedBeforeTheProbeExisted(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=backfill01"
	family := putYtdlpFamily(t, a, url, nil)

	// The state the live rows were actually in, asserted rather than assumed:
	// without it this test would still pass against a build that fills the
	// menus at staging time and never needs a backfill at all.
	if before := snapshot(t, a, family[ytdlp.VariantAudio].ID); len(before.AvailableAudioFormats) != 0 {
		t.Fatalf("the audio row starts with %v, want nothing - this test is about rows that have no menu yet", before.AvailableAudioFormats)
	}

	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: testProbeFormats, mu: &sync.Mutex{}, asked: &asked})
	a.backfillYtdlpProbes()

	audio := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if len(audio.AvailableAudioFormats) == 0 {
		t.Fatal("the audio row still has no format menu after the backfill")
	}
	for _, f := range audio.AvailableAudioFormats {
		if f == "flac" {
			t.Errorf("AvailableAudioFormats = %v, want flac excluded - the fixture's only audio track is mp4a", audio.AvailableAudioFormats)
		}
	}
	if len(audio.AvailableAudioBitrates) == 0 {
		t.Error("the audio row has no bitrate menu after the backfill")
	}
	if video := snapshot(t, a, family[ytdlp.VariantVideo].ID); len(video.AvailableQualities) == 0 {
		t.Error("the video row has no quality menu after the backfill")
	}

	// One probe for five rows: applyProbeFormats already writes to every row
	// sharing the URL, so a probe per row would be four yt-dlp processes spent
	// on an answer the first one already gave.
	if len(asked) != 1 || asked[0] != url {
		t.Errorf("the backfill asked about %v, want exactly one probe for %q", asked, url)
	}
}

// TestBackfillLeavesRowsThatAlreadyHaveAMenuAlone is the other half: this runs
// at every boot, so a collector full of already-probed rows must cost nothing.
func TestBackfillLeavesRowsThatAlreadyHaveAMenuAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=backfill02"
	putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)

	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: testProbeFormats, mu: &sync.Mutex{}, asked: &asked})
	a.backfillYtdlpProbes()

	if len(asked) != 0 {
		t.Errorf("the backfill probed %v, want nothing - those rows already carry their menus", asked)
	}
}

// TestTheFourFixedExtensionsNeedNoProbe covers the half of jdp's "die
// Dateiendung immer anzeigen" that never depended on the network: a thumbnail
// is always jpg, a subtitle always srt, a description a text file, and an
// audio row told "opus" by its preset already knows what it will be. Those
// four used to be written only by applyProbeFormats, so a source that could
// not be reached left all five rows with no extension at all.
func TestTheFourFixedExtensionsNeedNoProbe(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=fixedext01"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantAudio: "opus"})

	// No backend wired at all: this is the box with no yt-dlp binary.
	a.backfillYtdlpProbes()

	for v, want := range map[ytdlp.Variant]string{
		ytdlp.VariantThumbnail:   "jpg",
		ytdlp.VariantSubtitle:    "srt",
		ytdlp.VariantDescription: "description",
		ytdlp.VariantAudio:       "opus",
	} {
		if got := snapshot(t, a, family[v].ID).Ext; got != want {
			t.Errorf("%s row Ext = %q, want %q", v, got, want)
		}
	}
	// The video row is the one that genuinely cannot know: mkv only when a
	// real merge will happen, which is a fact about the source.
	if got := snapshot(t, a, family[ytdlp.VariantVideo].ID).Ext; got != "" {
		t.Errorf("video row Ext = %q, want it left blank until a probe answers", got)
	}
}

// TestAResolvedExtensionSurvivesTheFixedTable is the guard on the line above:
// a "best" audio row whose probe already resolved m4a must not be reset to
// anything a static table thinks it knows.
func TestAResolvedExtensionSurvivesTheFixedTable(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=fixedext02"
	family := putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)

	before := snapshot(t, a, family[ytdlp.VariantAudio].ID).Ext
	if before != "m4a" {
		t.Fatalf("the probe left Ext = %q, want m4a - the fixture's only audio track is mp4a", before)
	}
	a.applyFixedVariantExts()
	if after := snapshot(t, a, family[ytdlp.VariantAudio].ID).Ext; after != before {
		t.Errorf("Ext went from %q to %q, want the probed answer kept", before, after)
	}
}
