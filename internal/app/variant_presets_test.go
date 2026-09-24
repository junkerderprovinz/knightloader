package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const presetHost = "youtube.com"

// putHostFamily builds a five-row family on presetHost the way
// expandYtdlpVariants leaves it under the default preset: every row in view
// and switched on.
func putHostFamily(t *testing.T, a *App, url string) map[ytdlp.Variant]string {
	t.Helper()
	ids := map[ytdlp.Variant]string{}
	for _, v := range ytdlp.Variants() {
		sub := ""
		if v == ytdlp.VariantAudio {
			sub = "opus"
		}
		ids[v] = putTask(t, a, core.Task{
			URL: url, Name: "Some Title", Package: "Some Title", Host: presetHost,
			Status: core.StatusCollected, Enabled: true, Resolver: "ytdlp",
			Variant: variantEncode(v, sub),
		}).ID
	}
	return ids
}

func presetWithout(off ...ytdlp.Variant) ytdlp.HosterPreset {
	p := ytdlp.DefaultHosterPreset()
	var kept []ytdlp.Variant
	for _, v := range p.Variants {
		drop := false
		for _, o := range off {
			drop = drop || v == o
		}
		if !drop {
			kept = append(kept, v)
		}
	}
	p.Variants = kept
	return p
}

func setPreset(t *testing.T, a *App, p ytdlp.HosterPreset) {
	t.Helper()
	if err := a.SetHosterPreset(presetHost, p); err != nil {
		t.Fatalf("SetHosterPreset: %v", err)
	}
}

// taskFrames returns every "task" frame a fake hub connection received for id.
func taskFrames(f *activityFakeConn, id string) []core.Task {
	var out []core.Task
	for _, raw := range f.snapshot() {
		var env struct {
			Type string    `json:"type"`
			Data core.Task `json:"data"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Type == "task" && env.Data.ID == id {
			out = append(out, env.Data)
		}
	}
	return out
}

// Unticking a kind takes its rows out of view on every open page, and ticking
// it again puts them back as they were, the edited audio format included.
func TestUnlistingAKindSetsItsRowsAsideLiveAndListingItBringsThemBack(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets001")
	audio := family[ytdlp.VariantAudio]

	setPreset(t, a, presetWithout(ytdlp.VariantAudio))

	got := snapshot(t, a, audio)
	if !got.VariantOff || got.Enabled {
		t.Fatalf("audio row VariantOff=%v Enabled=%v, want it set aside and switched off", got.VariantOff, got.Enabled)
	}
	for kind, id := range family {
		if kind == ytdlp.VariantAudio {
			continue
		}
		if x := snapshot(t, a, id); x.VariantOff || !x.Enabled {
			t.Errorf("%s row VariantOff=%v Enabled=%v, want it untouched", kind, x.VariantOff, x.Enabled)
		}
	}
	waitFor(t, "a browser to be told the audio row left the view", func() bool {
		frames := taskFrames(fc, audio)
		return len(frames) > 0 && frames[len(frames)-1].VariantOff
	})

	setPreset(t, a, ytdlp.DefaultHosterPreset())

	got = snapshot(t, a, audio)
	if got.VariantOff || !got.Enabled {
		t.Fatalf("audio row VariantOff=%v Enabled=%v, want it back in view and switched on", got.VariantOff, got.Enabled)
	}
	if got.Variant != "audio:opus" {
		t.Errorf("audio row came back as %q, want its own pick %q kept", got.Variant, "audio:opus")
	}
	waitFor(t, "a browser to be told the audio row is back", func() bool {
		frames := taskFrames(fc, audio)
		return len(frames) > 0 && !frames[len(frames)-1].VariantOff
	})
}

// Saving a preset must not undo a switch somebody flipped on a row whose kind
// the save did not change.
func TestARowsOwnSwitchHoldsWhileItsKindStaysListed(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets002")
	thumb := family[ytdlp.VariantThumbnail]
	a.SetEnabled([]string{thumb}, false)

	setPreset(t, a, presetWithout(ytdlp.VariantAudio))

	if x := snapshot(t, a, thumb); x.Enabled || x.VariantOff {
		t.Errorf("thumbnail row Enabled=%v VariantOff=%v, want it still switched off by hand and in view", x.Enabled, x.VariantOff)
	}
}

// A preset decides what a link collects; a row already in the queue stays.
func TestAPresetLeavesQueuedRowsAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets003")
	audio := family[ytdlp.VariantAudio]
	a.mu.Lock()
	a.tasks[audio].Status = core.StatusQueued
	a.mu.Unlock()

	setPreset(t, a, presetWithout(ytdlp.VariantAudio))

	if x := snapshot(t, a, audio); x.VariantOff || !x.Enabled {
		t.Errorf("queued audio row VariantOff=%v Enabled=%v, want it untouched", x.VariantOff, x.Enabled)
	}
}

// A set-aside row is neither started nor reported as a disabled link, and once
// its link has left the collector it has nothing to come back beside.
func TestStartingALinkLeavesItsSetAsideRowsOutAndDropsThem(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.SetHalted(true)
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets004")
	setPreset(t, a, presetWithout(ytdlp.VariantAudio))

	var ids []string
	for _, id := range family {
		ids = append(ids, id)
	}
	res := a.StartTasks(ids)

	if res.Started != 4 || res.Disabled != 0 {
		t.Errorf("started %d, disabled %d; want the four rows in view started and nothing counted as disabled", res.Started, res.Disabled)
	}
	a.mu.Lock()
	_, stillThere := a.tasks[family[ytdlp.VariantAudio]]
	a.mu.Unlock()
	if stillThere {
		t.Error("the set-aside audio row outlived its link leaving the collector")
	}
}

// Removing a link's rows from the collector takes its set-aside rows along, so
// ticking their kind later does not bring back a row for a link that is gone,
// and the undo brings back the whole link without counting them.
func TestRemovingALinksShownRowsTakesItsSetAsideRowsAlong(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets005")
	setPreset(t, a, presetWithout(ytdlp.VariantAudio))
	audio := family[ytdlp.VariantAudio]

	var shown []string
	for kind, id := range family {
		if kind != ytdlp.VariantAudio {
			shown = append(shown, id)
		}
	}
	removed, token := a.RemoveTasksUndoable(shown, false)
	if len(removed) != len(shown) {
		t.Errorf("removal answered %d rows, want the %d that were asked for", len(removed), len(shown))
	}
	a.mu.Lock()
	_, stillThere := a.tasks[audio]
	a.mu.Unlock()
	if stillThere {
		t.Fatal("the set-aside audio row stayed behind its removed link")
	}

	back := a.UndoRemove(token)
	if len(back) != len(shown) {
		t.Errorf("undo answered %d rows, want the %d that were removed by hand", len(back), len(shown))
	}
	if x := snapshot(t, a, audio); !x.VariantOff {
		t.Error("the audio row came back in view, want it back where it was, set aside")
	}
}

// Removing one row of several leaves the link in the collector, and its
// set-aside rows with it.
func TestRemovingOneOfSeveralShownRowsKeepsTheSetAsideRows(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	family := putHostFamily(t, a, "https://youtube.com/watch?v=presets006")
	setPreset(t, a, presetWithout(ytdlp.VariantAudio))

	a.RemoveTasks([]string{family[ytdlp.VariantThumbnail]}, false)

	a.mu.Lock()
	_, kept := a.tasks[family[ytdlp.VariantAudio]]
	a.mu.Unlock()
	if !kept {
		t.Error("the set-aside audio row went with one sibling while three are still in view")
	}
}

// With AutoConfirm on, a pasted yt-dlp link moves into the download list whole:
// every row in view, the audio row included, and none held back as a copy of
// its siblings. The row the preset left out goes with the link rather than
// staying behind in the collector.
func TestAutoConfirmMovesTheWholeFamilyIntoTheDownloadList(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.AutoConfirm = true
		s.OnDupes = string(confirm.Exclude)
	})
	a.SetHalted(true)
	fake, _ := newFakeYtdlp()
	fake.title = "Some Video"
	wireYtdlp(a, fake)
	setPreset(t, a, presetWithout(ytdlp.VariantThumbnail))

	const url = "https://youtube.com/watch?v=autoconf001"
	if created := a.AddLinks([]string{url}, ""); len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}

	waitFor(t, "the four rows in view to leave the collector", func() bool {
		rows := tasksSharingURL(a, url)
		if len(rows) != 4 {
			return false
		}
		for _, x := range rows {
			if x.Status == core.StatusCollected {
				return false
			}
		}
		return true
	})
	for _, x := range tasksSharingURL(a, url) {
		if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantThumbnail {
			t.Errorf("the thumbnail row the preset left out is still listed with status %q", x.Status)
		}
	}
}

// The rows of one link share its URL and name, which is what a duplicate
// looks like; the kind tells them apart. A second video row of the same link
// is still a duplicate.
func TestTheRowsOfOneLinkAreNotDuplicatesOfEachOther(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=dupes000001"
	putHostFamily(t, a, url)

	a.mu.Lock()
	dupes := a.duplicatesLocked()
	a.mu.Unlock()
	if len(dupes) != 0 {
		t.Fatalf("duplicatesLocked = %v, want none among one link's five rows", dupes)
	}

	second := putTask(t, a, core.Task{
		URL: url, Name: "Some Title", Status: core.StatusCollected, Enabled: true,
		Variant: variantEncode(ytdlp.VariantVideo, ""), CreatedAt: time.Now().Add(time.Minute),
	})
	a.mu.Lock()
	dupes = a.duplicatesLocked()
	a.mu.Unlock()
	if len(dupes) != 1 || dupes[0] != second.ID {
		t.Errorf("duplicatesLocked = %v, want the second video row %s", dupes, second.ID)
	}
}
