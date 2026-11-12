package app

import (
	"context"
	"errors"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// archive.org's item pages hold their videos as files with a height and no
// codec, and JDownloader has a plugin for the host.
const archiveItem = "https://archive.org/details/BigBuckBunny_124"

var archiveFormats = []ytdlp.FormatEntry{
	{FormatID: "ogv", Ext: "ogv", Width: 533, Height: 300},
	{FormatID: "mp4", Ext: "mp4", Width: 640, Height: 360},
}

// ytdlpLeavingToJD is an app whose yt-dlp leaves host to JDownloader, as it
// does with every host JD has a plugin for.
func ytdlpLeavingToJD(t *testing.T, host string) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Big Buck Bunny"
	fake.formats = archiveFormats
	wireYtdlp(a, fake)
	a.Registry.Register(ytdlp.Resolver{Leave: func(h string) bool { return h == host }})
	a.Registry.Register(jd.Resolver{})
	return a
}

func TestAPinnedProbeAsksYtdlpAboutAHostItLeavesToJD(t *testing.T) {
	a := ytdlpLeavingToJD(t, "archive.org")
	if _, err := a.LinkQualities(context.Background(), archiveItem, false); !errors.Is(err, ErrNotAMediaLink) {
		t.Errorf("unpinned, the probe answers %v, want ErrNotAMediaLink", err)
	}
	menus, err := a.LinkQualities(context.Background(), archiveItem, true)
	if err != nil {
		t.Fatalf("pinned, the probe answers %v", err)
	}
	if len(menus.VideoTracks) == 0 || len(menus.Qualities) == 0 {
		t.Errorf("pinned, the probe offers tracks %v and qualities %v, want the item's videos", menus.VideoTracks, menus.Qualities)
	}
}

func TestALinkPinnedToYtdlpStagesEveryRowThereWhileAPasteKeepsItsRouting(t *testing.T) {
	a := ytdlpLeavingToJD(t, "archive.org")

	pasted := a.AddLinks([]string{"https://archive.org/details/another_item"}, "")
	if len(pasted) != 1 || pasted[0].Resolver != "jd" || pasted[0].ResolverPin != "" {
		t.Fatalf("a pasted archive.org link staged as %+v, want one unpinned row on jd", pasted)
	}

	choice := &ytdlp.LinkChoice{Variants: []ytdlp.Variant{ytdlp.VariantVideo, ytdlp.VariantDescription}}
	if _, err := a.AddLinkBatch([]string{archiveItem}, "", OriginCnL, LinkBatchOptions{Choice: choice, Resolver: "ytdlp"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pinned link's rows", func() bool { return len(tasksSharingURL(a, archiveItem)) > 1 })
	for _, row := range tasksSharingURL(a, archiveItem) {
		if row.Resolver != "ytdlp" || row.ResolverPin != "ytdlp" {
			t.Errorf("row %s is on %q pinned to %q, want ytdlp for each", row.Variant, row.Resolver, row.ResolverPin)
		}
	}
}

func TestATaskPinnedToYtdlpGoesThereOnAHostJDKnows(t *testing.T) {
	a := defaultSetupApp(t, true)
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	jd.SetKnownHosts([]string{"archive.org"})

	if got := resolverIDOf(a.resolverForTaskLocked(&core.Task{URL: archiveItem})); got != "jd" {
		t.Errorf("an unpinned archive.org link goes to %q, want jd", got)
	}
	if got := resolverIDOf(a.resolverForTaskLocked(&core.Task{URL: archiveItem, ResolverPin: "ytdlp"})); got != "ytdlp" {
		t.Errorf("an archive.org link pinned to ytdlp goes to %q, want ytdlp", got)
	}
}

func TestABatchWithAPinItCannotHaveIsRefused(t *testing.T) {
	a := ytdlpLeavingToJD(t, "archive.org")
	for name, opts := range map[string]LinkBatchOptions{
		"an unknown backend":      {Resolver: "nosuch"},
		"a file from the browser": {Resolver: "ytdlp", File: true},
	} {
		if _, err := a.AddLinkBatch([]string{archiveItem}, "", OriginCnL, opts); err == nil {
			t.Errorf("%s: the batch was staged, want it refused", name)
		}
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks were staged for refused batches", n)
	}
}
