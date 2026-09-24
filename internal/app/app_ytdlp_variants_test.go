package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// tasksSharingURL returns copies of every row of a family, which all share the
// link's exact URL. Copies taken under a.mu, since the probe goroutine writes
// these fields and live pointers would let a reader see half a rename.
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

// With no preset saved, a bare paste becomes five enabled rows sharing one URL
// and package.
func TestExpandYtdlpVariantsCreatesAllFiveRowsWithDefaultPreset(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Never Gonna Give You Up"
	wireYtdlp(a, fake)

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

// Variants a saved preset leaves out are still staged as rows, but set aside
// and switched off.
func TestExpandYtdlpVariantsRespectsASavedHosterPreset(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Some Video"
	wireYtdlp(a, fake)

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
				t.Errorf("%q row enabled, want it disabled; the saved preset leaves it out of Variants()", kind)
			}
			if !x.VariantOff {
				t.Errorf("%q row is in view, want it set aside; the saved preset leaves it out of Variants()", kind)
			}
			continue
		}
		if x.VariantOff {
			t.Errorf("%q row is set aside, want it in view; the saved preset lists it", kind)
		}
	}
}

// An unconfigured host answers with what a bare paste would stage.
func TestHosterPresetForReturnsTheDefaultWhenNothingWasSaved(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	got := a.HosterPresetFor("neverconfigured.example")
	want := ytdlp.DefaultHosterPreset()
	if len(got.Variants) != len(want.Variants) || got.Quality != want.Quality || got.AudioFormat != want.AudioFormat {
		t.Errorf("HosterPresetFor(unset host) = %+v, want the default %+v", got, want)
	}
}

// Saving one host's preset must not disturb another host's.
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

// Picking a quality re-encodes only the sub-value; a video row stays a video
// row.
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

// The four siblings exist before the title probe answers, so the rename guard
// in setTaskName must treat same-URL rows as family, or all five stay in the
// package "watch".
func TestExpandYtdlpVariantsFamilyStillRenamesThePackageOnceNamed(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Me at the zoo"
	wireYtdlp(a, fake)

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

// familyOf stages one yt-dlp link whose title probe is still outstanding and
// returns the five rows it became.
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

// wiredYtdlpApp is an app whose yt-dlp links go to a fake that answers the
// title probe at once, with mutate applied to its settings.
func wiredYtdlpApp(t *testing.T, mutate func(s *settings.Settings, base string)) (*App, string) {
	t.Helper()
	a, base := newRuleApp(t, mutate)
	fake, _ := newFakeYtdlp()
	fake.title = "Some Video"
	wireYtdlp(a, fake)
	return a, base
}

// fiveRows returns the rows url became, failing unless there are five.
func fiveRows(t *testing.T, a *App, url string) []core.Task {
	t.Helper()
	rows := tasksSharingURL(a, url)
	if len(rows) != 5 {
		t.Fatalf("the link became %d rows, want 5", len(rows))
	}
	return rows
}

// The add-links form's folder, passwords, priority and comment are for the
// link, and a yt-dlp link is all five of its rows.
func TestTheAddLinksFormReachesEveryRowOfAYtdlpLink(t *testing.T) {
	a, base := wiredYtdlpApp(t, func(*settings.Settings, string) {})
	dir := filepath.Join(base, "Chosen")
	prio := 2
	const url = "https://youtube.com/watch?v=formopts001"
	if _, err := a.AddLinksWithOptions([]string{url}, "", OriginPaste, LinkBatchOptions{
		Dir: dir, Password: "archivepw", DownloadPassword: "linkpw",
		Comment: "from the form", Priority: &prio, Overrule: true,
	}); err != nil {
		t.Fatal(err)
	}

	for _, x := range fiveRows(t, a, url) {
		if x.Dir != dir || x.Password != "archivepw" || x.DownloadPassword != "linkpw" {
			t.Errorf("%q row has dir %q, passwords %q and %q; want the form's %q, archivepw and linkpw",
				x.Variant, x.Dir, x.Password, x.DownloadPassword, dir)
		}
		if x.Comment != "from the form" || x.Priority != 2 {
			t.Errorf("%q row has comment %q and priority %d, want the form's", x.Variant, x.Comment, x.Priority)
		}
	}
}

// A Packagizer rule shapes the row staging creates, and the rest of a yt-dlp
// link's rows are made from that one, so they land where the rule said.
func TestEveryRowOfAYtdlpLinkTakesWhatThePackagizerDecided(t *testing.T) {
	var dir string
	a, _ := wiredYtdlpApp(t, func(s *settings.Settings, base string) {
		dir = filepath.Join(base, "Clips")
		prio, yes := 3, true
		s.Categories = []settings.Category{{ID: "clips", Name: "Clips"}}
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name:       "clips",
			Conditions: []rules.Condition{{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "youtube.com"}},
			Action: rules.Action{
				DownloadDir: dir, Category: "clips", Comment: "a clip", Priority: &prio, AutoExtract: &yes,
			},
		}}}
	})
	const url = "https://youtube.com/watch?v=packagize01"
	a.AddLinks([]string{url}, "")

	for _, x := range fiveRows(t, a, url) {
		if x.Dir != dir || x.Category != "clips" || x.Comment != "a clip" || x.Priority != 3 {
			t.Errorf("%q row has dir %q, category %q, comment %q, priority %d; want the rule's %q, clips, a clip, 3",
				x.Variant, x.Dir, x.Category, x.Comment, x.Priority, dir)
		}
		if x.AutoExtract == nil || !*x.AutoExtract {
			t.Errorf("%q row has auto-extract %v, want the rule's on", x.Variant, x.AutoExtract)
		}
	}
}

// A Click'n'Load submission's archive password goes on every row of the link.
func TestASubmittedPasswordReachesEveryRowOfAYtdlpLink(t *testing.T) {
	a, _ := wiredYtdlpApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=cnlpass0001"
	a.AddLinksCnL([]string{url}, "CnL", []string{"secret"})

	for _, x := range fiveRows(t, a, url) {
		if x.Password != "secret" {
			t.Errorf("%q row has password %q, want the submitted secret", x.Variant, x.Password)
		}
	}
}

// A dropped job with one link names one file. For a yt-dlp link the name goes
// on the row staging handed back, not on all five, which would point them all
// at one file.
func TestADroppedJobsFileNameNamesTheStagedRowOfAYtdlpLink(t *testing.T) {
	a, _ := wiredYtdlpApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=dropname001"
	a.stageWatchJob(watch.Job{URLs: []string{url}, Filename: "clip.mkv"})

	named := 0
	for _, x := range fiveRows(t, a, url) {
		if x.Filename == "" {
			continue
		}
		named++
		if kind, _ := variantDecode(x.Variant); kind != ytdlp.VariantVideo || x.Filename != "clip.mkv" {
			t.Errorf("%q row took the file name %q, want only the video row named clip.mkv", x.Variant, x.Filename)
		}
	}
	if named != 1 {
		t.Errorf("%d rows took the job's file name, want the one staged row", named)
	}
}

// The five rows of one video share one package, so moving any one of them
// moves all five. The row moved here is not the primary.
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

// The state a real paste reaches only when the probe lands in a narrow gap, set
// up by hand: every row already carries the real name, all five sit in the
// guessed package, and only the primary is in the id list, since the siblings
// were created after the bucket was assembled. The family's own rows must not
// veto each other's rename, and the new package must reach all five.
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

// Two unrelated YouTube links both guess the package "watch", and one resolving
// its title must not drag the other along. Only rows sharing an exact URL are
// family.
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

	// Every bare watch page guesses "watch". The other link's rows already
	// carry a real name, so they are a resolved batch this one must not touch.
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
			t.Errorf("row %q moved to %q, want the whole batch left in %q; an unrelated link already resolved into it", x.Variant, x.Package, "watch")
		}
	}
	for _, x := range tasksSharingURL(a, theirs) {
		if x.Package != "watch" {
			t.Errorf("the other link's row %q moved to %q, want it untouched in %q", x.Variant, x.Package, "watch")
		}
	}
}

// putYtdlpFamily builds a five-row family directly, the shape
// expandYtdlpVariants produces, without the async probe so tests can call
// applyProbeFormats by hand. subs gives each kind its sub-value.
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

// testProbeFormats is a realistic mixed format list, as in backend_test.go: two
// video-only tracks (144p, 1080p), one audio-only track and one combined
// progressive track.
var testProbeFormats = []ytdlp.FormatEntry{
	{FormatID: "160", Ext: "mp4", Vcodec: "avc1.4d400b", Acodec: "none", Height: 144, Filesize: 195278},
	{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FilesizeApprox: 52428800},
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Filesize: 3145728, Abr: 129},
	{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360, Filesize: 8388608},
}

// A probe that answered proves the source is there, and all five rows share
// that source.
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

// yt-dlp always writes .description, whatever the source.
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

// With a fixed --audio-format the extension is certain but the transcoded size
// is not, so Size stays unknown.
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

// "best" audio is a straight extract, so the matched track's extension and size
// are what lands on disk.
func TestApplyProbeFormatsSetsBestAudioExtAndSize(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=size0001"
	family := putYtdlpFamily(t, a, url, nil) // the audio sub-value "" means best

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if live.Ext != "m4a" {
		t.Errorf("best-audio row Ext = %q, want the matched track's own %q", live.Ext, "m4a")
	}
	if live.Size != 3145728 {
		t.Errorf("best-audio row Size = %d, want the audio-only entry's own filesize %d", live.Size, 3145728)
	}
}

// The audio row offers the formats the source carries, "best" first, and the
// tracks in them for the bitrate beside the format. Transcode targets such as
// flac do not appear.
func TestApplyProbeFormatsSetsTheAudioFormatsAndTracks(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=formats0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if want := []string{"best", "m4a"}; !stringSlicesEqual(live.AvailableAudioFormats, want) {
		t.Errorf("AvailableAudioFormats = %v, want %v", live.AvailableAudioFormats, want)
	}
	if want := []string{"m4a 129k"}; !stringSlicesEqual(live.AvailableAudioTracks, want) {
		t.Errorf("AvailableAudioTracks = %v, want %v", live.AvailableAudioTracks, want)
	}
}

// The audio track reports abr=129, so the bitrate menu keeps Auto, 64, 96 and
// 128 and drops 160 and above.
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

// A video-only and an audio-only track will be merged, and buildArgs forces
// --merge-output-format mkv for that.
func TestApplyProbeFormatsSetsVideoExtOnlyWhenAMergeWouldHappen(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=merge0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if live.Ext != "mkv" {
		t.Errorf("video row Ext = %q, want %q; the source has both a video-only and an audio-only track to merge", live.Ext, "mkv")
	}
}

// Without a video-only and audio-only pair, as with some very old uploads, the
// pre-muxed fallback is used and --merge-output-format has no effect.
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
		t.Errorf("video row Ext = %q, want it left unset; no video-only/audio-only pair exists to merge", live.Ext)
	}
}

// Both are forced conversions (--convert-thumbnails jpg, --sub-format srt).
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

// The tallest track is 1080p, so 2160p and 1440p are not offered; best and
// custom always are.
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
			t.Errorf("AvailableQualities = %v, want %q excluded; the source has no track above 1080p", live.AvailableQualities, unwanted)
		}
	}
}

// The size estimate follows the row's picked quality, not the tallest track.
// yt-dlp's bestvideo takes video-only tracks alone, so under a 360p cap it
// merges the 144p one with the audio rather than taking the pre-muxed 360p.
func TestApplyProbeFormatsSetsVideoSizeAtItsOwnQualityCap(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=size0002"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantVideo: "360p"})

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if want := int64(195278 + 3145728); live.Size != want {
		t.Errorf("360p-capped video row Size = %d, want %d for the 144p track and the audio, not the 1080p track's", live.Size, want)
	}
}

// A finished row's size and extension are what its download wrote. A later
// probe of the same link, from a sibling still in the collector, must leave
// them alone while it still marks the source online.
func TestAProbeLeavesAFinishedRowAsItsDownloadLeftIt(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=finished001"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantAudio: "mp3"})
	finished := []string{family[ytdlp.VariantAudio].ID, family[ytdlp.VariantVideo].ID}
	for _, id := range finished {
		editTask(a, id, func(x *core.Task) {
			x.Status, x.Size, x.Loaded, x.Ext = core.StatusDone, 4096, 4096, "webm"
		})
	}

	a.applyProbeFormats(url, testProbeFormats)

	for _, id := range finished {
		got := snapshot(t, a, id)
		if got.Size != 4096 || got.Ext != "webm" {
			t.Errorf("finished %q row has Size %d and Ext %q, want the 4096 bytes of webm it downloaded", got.Variant, got.Size, got.Ext)
		}
		if got.Online != core.AvailOnline {
			t.Errorf("finished %q row Online = %q, want %q", got.Variant, got.Online, core.AvailOnline)
		}
	}
}

// countingYtdlpBackend answers ProbeTitle with a fixed format list and records
// every URL it was asked about. fakeYtdlpBackend panics on a second call. The
// slice sits behind a pointer so the value receivers share one record.
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

// Rows staged without probe data have empty menus, which the picker reads as
// the full static menu, flac included. The backfill fetches what they lack.
func TestBackfillNarrowsTheMenusOfRowsStagedBeforeTheProbeExisted(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=backfill01"
	family := putYtdlpFamily(t, a, url, nil)

	// Asserted so the test cannot pass against rows that already had menus.
	if before := snapshot(t, a, family[ytdlp.VariantAudio].ID); len(before.AvailableAudioFormats) != 0 {
		t.Fatalf("the audio row starts with %v, want nothing; this test is about rows that have no menu yet", before.AvailableAudioFormats)
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
			t.Errorf("AvailableAudioFormats = %v, want flac excluded; the fixture's only audio track is mp4a", audio.AvailableAudioFormats)
		}
	}
	if len(audio.AvailableAudioBitrates) == 0 {
		t.Error("the audio row has no bitrate menu after the backfill")
	}
	if video := snapshot(t, a, family[ytdlp.VariantVideo].ID); len(video.AvailableQualities) == 0 {
		t.Error("the video row has no quality menu after the backfill")
	}

	// One probe for five rows, since applyProbeFormats updates every row
	// sharing the URL.
	if len(asked) != 1 || asked[0] != url {
		t.Errorf("the backfill asked about %v, want exactly one probe for %q", asked, url)
	}
}

// The backfill runs at every boot, so already-probed rows must cost nothing.
func TestBackfillLeavesRowsThatAlreadyHaveAMenuAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=backfill02"
	putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)

	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: testProbeFormats, mu: &sync.Mutex{}, asked: &asked})
	a.backfillYtdlpProbes()

	if len(asked) != 0 {
		t.Errorf("the backfill probed %v, want nothing; those rows already carry their menus", asked)
	}
}

// Thumbnail, subtitle, description and a fixed-format audio row know their
// extension without a probe, so an unreachable source still shows them.
func TestTheFourFixedExtensionsNeedNoProbe(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=fixedext01"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantAudio: "opus"})

	// No backend wired: a box without yt-dlp.
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
	// The video row cannot know: mkv depends on whether the source needs a
	// merge.
	if got := snapshot(t, a, family[ytdlp.VariantVideo].ID).Ext; got != "" {
		t.Errorf("video row Ext = %q, want it left blank until a probe answers", got)
	}
}

// A "best" audio row the probe resolved to m4a keeps it.
func TestAResolvedExtensionSurvivesTheFixedTable(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=fixedext02"
	family := putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)

	before := snapshot(t, a, family[ytdlp.VariantAudio].ID).Ext
	if before != "m4a" {
		t.Fatalf("the probe left Ext = %q, want m4a; the fixture's only audio track is mp4a", before)
	}
	a.applyFixedVariantExts()
	if after := snapshot(t, a, family[ytdlp.VariantAudio].ID).Ext; after != before {
		t.Errorf("Ext went from %q to %q, want the probed answer kept", before, after)
	}
}

// The video row offers the formats its tracks come in and every distinct
// track beside the height caps, so a container or codec can be picked, not
// only a height.
func TestApplyProbeFormatsListsTheVideoFormatsAndTracks(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=tracks0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, testProbeFormats)

	live := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if want := []string{"best", "mp4 avc1"}; !stringSlicesEqual(live.AvailableVideoFormats, want) {
		t.Errorf("AvailableVideoFormats = %v, want %v", live.AvailableVideoFormats, want)
	}
	if want := []string{"1080p mp4 avc1", "360p mp4 avc1", "144p mp4 avc1"}; !stringSlicesEqual(live.AvailableVideoTracks, want) {
		t.Errorf("AvailableVideoTracks = %v, want %v", live.AvailableVideoTracks, want)
	}
}

// A pick made after the probe is a different file, so the row's extension and
// size follow it without asking the host again, and going back to the cap
// puts back the merge's mkv.
func TestPickingAVideoTrackGivesTheRowThatTracksFile(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=tracks0002"
	family := putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)
	video := family[ytdlp.VariantVideo].ID

	pick := func(q string) core.Task {
		t.Helper()
		if err := a.SetTaskOptions([]string{video}, TaskOptions{VariantQuality: &q}); err != nil {
			t.Fatalf("SetTaskOptions(%q): %v", q, err)
		}
		return snapshot(t, a, video)
	}

	// Only pre-muxed at 360p: nothing to merge, the track's own container.
	if got := pick("360p mp4 avc1"); got.Ext != "mp4" || got.Size != 8388608 {
		t.Errorf("360p mp4 avc1: Ext %q Size %d, want mp4 and the pre-muxed track's 8388608", got.Ext, got.Size)
	}
	// Video-only at 1080p, merged with the m4a track into its own mp4.
	if got := pick("1080p mp4 avc1"); got.Ext != "mp4" || got.Size != 52428800+3145728 {
		t.Errorf("1080p mp4 avc1: Ext %q Size %d, want mp4 and the 1080p track and the audio together", got.Ext, got.Size)
	}
	if got := pick("best"); got.Ext != "mkv" {
		t.Errorf("best: Ext %q, want mkv for the merge", got.Ext)
	}
}

func TestPickingAnAudioTrackGivesTheRowThatTracksFile(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=tracks0003"
	family := putYtdlpFamily(t, a, url, nil)
	a.applyProbeFormats(url, testProbeFormats)
	audio := family[ytdlp.VariantAudio].ID

	q := "m4a 129k"
	if err := a.SetTaskOptions([]string{audio}, TaskOptions{VariantQuality: &q}); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, a, audio); got.Ext != "m4a" || got.Size != 3145728 {
		t.Errorf("m4a 129k: Ext %q Size %d, want m4a and the track's 3145728", got.Ext, got.Size)
	}
	q = "mp3"
	if err := a.SetTaskOptions([]string{audio}, TaskOptions{VariantQuality: &q}); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, a, audio); got.Ext != "mp3" || got.Size != 0 {
		t.Errorf("mp3: Ext %q Size %d, want mp3 and an unknown size for a conversion", got.Ext, got.Size)
	}
}

// What the row stores is what yt-dlp is asked for: a track as a track, a
// height as a cap.
func TestAPickReachesYtdlpAsTheKindOfPickItIs(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=tracks0004"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{
		ytdlp.VariantVideo: "1080p60 webm vp9",
		ytdlp.VariantAudio: "opus 160k",
	})
	capped := putTask(t, a, core.Task{URL: url + "x", Status: core.StatusCollected, Enabled: true, Variant: "video:720p"})

	if o := a.ytdlpOptionsForTask(family[ytdlp.VariantVideo].ID); o.VideoPick != "1080p60 webm vp9" {
		t.Errorf("video row VideoPick = %q, want the picked track", o.VideoPick)
	}
	if o := a.ytdlpOptionsForTask(family[ytdlp.VariantAudio].ID); o.AudioTrack != "opus 160k" || o.AudioFormat == "opus 160k" {
		t.Errorf("audio row AudioTrack = %q AudioFormat = %q, want the track as a track", o.AudioTrack, o.AudioFormat)
	}
	if o := a.ytdlpOptionsForTask(capped.ID); o.Quality != ytdlp.Quality720p || o.VideoPick != "" {
		t.Errorf("capped row Quality = %q VideoPick = %q, want the 720p cap and no track", o.Quality, o.VideoPick)
	}
}

// rowByKind is the family row of one kind, copied under a.mu.
func rowByKind(t *testing.T, a *App, url string, kind ytdlp.Variant) core.Task {
	t.Helper()
	for _, x := range tasksSharingURL(a, url) {
		if k, _ := variantDecode(x.Variant); k == kind {
			return x
		}
	}
	t.Fatalf("no %q row for %s", kind, url)
	return core.Task{}
}

// A host preset's formats reach a new link's rows before anything is known
// about the link, and yt-dlp is asked for them as they are.
func TestAPresetsFormatsReachANewLinkBeforeItsProbeAnswers(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wireYtdlp(a, blockingYtdlpBackend{title: "Some Video", release: release})
	if err := a.SetHosterPreset("youtube.com", ytdlp.HosterPreset{
		Variants:    ytdlp.Variants(),
		VideoFormat: "mp4 avc1", Quality: ytdlp.Quality720p,
		AudioFormat: "m4a", AudioBitrate: "128",
	}); err != nil {
		t.Fatal(err)
	}

	const url = "https://youtube.com/watch?v=presetfmt01"
	a.AddLinks([]string{url}, "")
	waitFor(t, "expandYtdlpVariants to add the four sibling rows", func() bool {
		return len(tasksSharingURL(a, url)) == 5
	})

	video, audio := rowByKind(t, a, url, ytdlp.VariantVideo), rowByKind(t, a, url, ytdlp.VariantAudio)
	if video.Variant != "video:mp4 avc1 720p" {
		t.Errorf("video row Variant = %q, want the preset's container capped at its quality", video.Variant)
	}
	if audio.Variant != "audio:m4a" || audio.AudioBitrate != "128" || audio.Ext != "m4a" {
		t.Errorf("audio row = %q at %q kbit/s, Ext %q; want the preset's m4a at 128 and its extension",
			audio.Variant, audio.AudioBitrate, audio.Ext)
	}
	if o := a.ytdlpOptionsForTask(video.ID); o.VideoPick != "mp4 avc1 720p" {
		t.Errorf("VideoPick = %q, want the preset's wish passed on as it is", o.VideoPick)
	}
}

// Once the probe answers, the preset's format and quality become the link's
// own nearest track, and its size is known.
func TestAProbeTurnsAPresetsFormatsIntoTheLinksTracks(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Some Video"
	fake.formats = testProbeFormats
	wireYtdlp(a, fake)
	if err := a.SetHosterPreset("youtube.com", ytdlp.HosterPreset{
		Variants:    ytdlp.Variants(),
		VideoFormat: "mp4 avc1", Quality: ytdlp.Quality720p,
		AudioFormat: "m4a", AudioBitrate: "128",
	}); err != nil {
		t.Fatal(err)
	}

	const url = "https://youtube.com/watch?v=presetfmt02"
	a.AddLinks([]string{url}, "")
	waitFor(t, "the probe to resolve the video row's pick", func() bool {
		rows := tasksSharingURL(a, url)
		return len(rows) == 5 && rowByKind(t, a, url, ytdlp.VariantVideo).Variant != "video:mp4 avc1 720p"
	})

	// Under 720p the mp4 avc1 tracks are 360p (pre-muxed) and 144p.
	if video := rowByKind(t, a, url, ytdlp.VariantVideo); video.Variant != "video:360p mp4 avc1" || video.Size != 8388608 {
		t.Errorf("video row = %q, Size %d; want the 360p track and its 8388608 bytes", video.Variant, video.Size)
	}
	if audio := rowByKind(t, a, url, ytdlp.VariantAudio); audio.Variant != "audio:m4a 129k" || audio.AudioBitrate != "" || audio.Size != 3145728 {
		t.Errorf("audio row = %q at %q, Size %d; want the 129k track, no bitrate of its own, 3145728 bytes",
			audio.Variant, audio.AudioBitrate, audio.Size)
	}
}

// A link without the preset's format keeps the preset's quality on the video
// row, and its audio is converted to the preset's format.
func TestAPresetFormatTheLinkLacksFallsBackToTheQuality(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=presetfmt03"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{
		ytdlp.VariantVideo: "webm vp9 1080p",
		ytdlp.VariantAudio: "opus",
	})
	editTask(a, family[ytdlp.VariantAudio].ID, func(x *core.Task) { x.AudioBitrate = "160" })

	a.applyProbeFormats(url, testProbeFormats)

	if video := snapshot(t, a, family[ytdlp.VariantVideo].ID); video.Variant != "video:1080p" {
		t.Errorf("video row Variant = %q, want the 1080p cap the preset asked for", video.Variant)
	}
	audio := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if audio.Variant != "audio:opus" || audio.AudioBitrate != "160" || audio.Ext != "opus" || audio.Size != 0 {
		t.Errorf("audio row = %q at %q, Ext %q, Size %d; want a conversion to opus at 160 of unknown size",
			audio.Variant, audio.AudioBitrate, audio.Ext, audio.Size)
	}
}

// A row saved as "aac" at 128 kbit/s, from the picker that mixed formats and
// tracks, reads as the m4a track nearest 128 once probed. Choosing the format
// again with no bitrate then keeps the format's best track rather than being
// pulled back to the old bitrate.
func TestAnOldAacRowBecomesTheNearestM4aTrack(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=oldaac00001"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantAudio: "aac"})
	audio := family[ytdlp.VariantAudio].ID
	editTask(a, audio, func(x *core.Task) { x.AudioBitrate = "128" })

	a.applyProbeFormats(url, testProbeFormats)
	if got := snapshot(t, a, audio); got.Variant != "audio:m4a 129k" || got.AudioBitrate != "" {
		t.Errorf("audio row = %q at %q, want m4a 129k with no bitrate of its own", got.Variant, got.AudioBitrate)
	}

	format, none := "m4a", ""
	if err := a.SetTaskOptions([]string{audio}, TaskOptions{VariantQuality: &format, AudioBitrate: &none}); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, a, audio); got.Variant != "audio:m4a" || got.Size != 3145728 {
		t.Errorf("audio row = %q, Size %d; want the m4a format's best track, 3145728 bytes", got.Variant, got.Size)
	}
}

// youtubeProbe is how a YouTube probe answers since most of its streams moved
// to HLS: each height is listed first as an HLS copy whose size is only the
// estimate ProbeTitle made from its bitrate, then as direct downloads with an
// exact size. format_id marked the av1 and opus pair yt-dlp picks by default.
var youtubeProbe = []ytdlp.FormatEntry{
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129.502, Filesize: 3449447, Protocol: "https"},
	{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 128.93, Filesize: 3433755, Protocol: "https", Default: true},
	{FormatID: "270", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 25, FilesizeApprox: 124814970, Protocol: "m3u8_native"},
	{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080, FPS: 25, Filesize: 80911999, Protocol: "https"},
	{FormatID: "625", Ext: "mp4", Vcodec: "vp09.00.50.08", Acodec: "none", Height: 2160, FPS: 25, FilesizeApprox: 509008150, Protocol: "m3u8_native"},
	{FormatID: "313", Ext: "webm", Vcodec: "vp9", Acodec: "none", Height: 2160, FPS: 25, Filesize: 358608461, Protocol: "https"},
	{FormatID: "401", Ext: "mp4", Vcodec: "av01.0.12M.08", Acodec: "none", Height: 2160, FPS: 25, Filesize: 240334643, Protocol: "https", Default: true},
}

// The collector showed no size for YouTube links: the tallest track it
// measured was the HLS copy listed first, which reports none, and the audio of
// the merge was never counted. The size is the pair yt-dlp downloads, and it
// follows every change of format and quality.
func TestAYoutubeLinkShowsTheSizeOfWhatItDownloads(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	family := putYtdlpFamily(t, a, url, nil)
	video, audio := family[ytdlp.VariantVideo].ID, family[ytdlp.VariantAudio].ID

	a.applyProbeFormats(url, youtubeProbe)

	if got := snapshot(t, a, video); got.Ext != "mkv" || got.Size != 240334643+3433755 {
		t.Errorf("best video: Ext %q Size %d, want mkv and the av1 and opus pair together", got.Ext, got.Size)
	}
	if got := snapshot(t, a, audio); got.Ext != "opus" || got.Size != 3433755 {
		t.Errorf("best audio: Ext %q Size %d, want the opus track yt-dlp picks", got.Ext, got.Size)
	}

	pick := func(id, v string) core.Task {
		t.Helper()
		if err := a.SetTaskOptions([]string{id}, TaskOptions{VariantQuality: &v}); err != nil {
			t.Fatalf("SetTaskOptions(%q): %v", v, err)
		}
		return snapshot(t, a, id)
	}
	// The direct download of 1080p mp4 avc1, not its larger HLS estimate,
	// with the m4a track that keeps the merge in mp4.
	if got := pick(video, "1080p mp4 avc1"); got.Ext != "mp4" || got.Size != 80911999+3449447 {
		t.Errorf("1080p mp4 avc1: Ext %q Size %d, want mp4 and %d", got.Ext, got.Size, 80911999+3449447)
	}
	if got := pick(video, "2160p mp4 vp9"); got.Size != 509008150+3449447 {
		t.Errorf("2160p mp4 vp9, only on HLS: Size %d, want its estimate and the audio, %d", got.Size, 509008150+3449447)
	}
	if got := pick(audio, "m4a"); got.Ext != "m4a" || got.Size != 3449447 {
		t.Errorf("m4a: Ext %q Size %d, want the m4a track's 3449447", got.Ext, got.Size)
	}
}
