package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// downloadedBefore files a finished download in the history without putting it
// in the list, as after the list was cleared or trimmed.
func downloadedBefore(t *testing.T, a *App, id, url, name string, size int64) time.Time {
	t.Helper()
	at := time.UnixMilli(time.Date(2026, 9, 14, 18, 30, 0, 0, time.Local).UnixMilli())
	if err := a.Store.Save(&core.Task{
		ID: id, URL: url, Name: name, Size: size, Loaded: size,
		Status: core.StatusDone, CreatedAt: at.Add(-time.Minute), FinishedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	return at
}

// historyApp is an app with the history check at its default and mirrors
// matched on name and size.
func historyApp(t *testing.T, mutate func(s *settings.Settings)) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.MirrorPolicy = string(dedupe.PolicyFilenameAndSize)
		if mutate != nil {
			mutate(s)
		}
	})
	return a
}

func onlyTask(t *testing.T, created []*core.Task) *core.Task {
	t.Helper()
	if len(created) != 1 {
		t.Fatalf("staging made %d tasks, want 1", len(created))
	}
	return created[0]
}

func TestALinkFromTheHistoryIsRejectedWithTheDownloadItRepeats(t *testing.T) {
	a := historyApp(t, nil)
	at := downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinksFrom([]string{"https://host.example/film.mkv"}, "", OriginFeed))
	if !got.Skipped {
		t.Fatalf("the link was staged as %q, want it among the rejected links", got.Status)
	}
	if got.SkipCode != skipDownloaded {
		t.Errorf("skip code = %q, want %q", got.SkipCode, skipDownloaded)
	}
	if got.SkipParams["name"] != "film.mkv" {
		t.Errorf("the reason names %q, want the downloaded file", got.SkipParams["name"])
	}
	if got.SkipParams["finished"] != at.Format(time.RFC3339) {
		t.Errorf("the reason dates the download %q, want %q", got.SkipParams["finished"], at.Format(time.RFC3339))
	}
	if held := a.FilteredLinks(); len(held) != 1 || held[0].ID != got.ID {
		t.Errorf("the rejected links are %+v, want the one link", held)
	}
}

// Host case and a default port do not make a second download of one link.
func TestTheHistoryMatchesALinkWrittenDifferently(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinks([]string{"https://HOST.example:443/film.mkv"}, ""))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the link was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}
}

func TestTheSameFileAtAnotherLinkIsRejectedUnderTheMirrorPolicy(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
		{DirectURL: "https://two.example/f/abc", Name: "film.mkv", Size: 4096},
	}, "", OriginPaste))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the mirror was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}
}

func TestAMirrorOfADownloadIsAddedWhenThePolicyIsOff(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.MirrorPolicy = string(dedupe.PolicyOff) })
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
		{DirectURL: "https://two.example/f/abc", Name: "film.mkv", Size: 4096},
	}, "", OriginPaste))
	if got.Skipped {
		t.Errorf("the link was rejected (%s), want it staged: with mirrors off only the same link counts", got.SkipReason)
	}
}

func TestALinkFromTheHistoryIsAddedWhenTheCheckIsOff(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.RejectDownloaded = false })
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinks([]string{"https://host.example/film.mkv"}, ""))
	if got.Skipped {
		t.Errorf("the link was rejected (%s), want it staged with the check switched off", got.SkipReason)
	}
}

func TestRestoringALinkTheHistoryRejectedAddsItAnyway(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)
	held := onlyTask(t, a.AddLinks([]string{"https://host.example/film.mkv"}, ""))

	restored := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if restored.Skipped || restored.Status != core.StatusCollected {
		t.Errorf("restored as skipped=%v status=%q, want it in the collector", restored.Skipped, restored.Status)
	}
}

// The restore overrules the history, which held the link, and not the filter,
// which never refused it.
func TestALinkRestoredFromTheHistoryStillMeetsTheFilterAtTheQueue(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/sample.mkv", "sample.mkv", 4096)
	held := onlyTask(t, a.AddLinks([]string{"https://host.example/sample.mkv"}, ""))
	s := a.Settings.Get()
	s.LinkFilter = rejectRule("sample files are not wanted here")
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	restored := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	a.StartTasks([]string{restored.ID})
	a.mu.Lock()
	live := *a.tasks[restored.ID]
	a.mu.Unlock()
	if live.Status != core.StatusError || !strings.Contains(live.Error, "sample files are not wanted here") {
		t.Errorf("the queue took the link as %q (%s), want the filter rule to refuse it", live.Status, live.Error)
	}
}

func TestAnUploadedTorrentFromTheHistoryIsRejected(t *testing.T) {
	a := historyApp(t, nil)
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}})
	downloadedBefore(t, a, "old", uri, "Pack", 900)

	got, err := a.AddTorrent(uri, nil, "", OriginWatch)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the torrent came back as %+v, want it rejected as downloaded", got)
	}
}

// A plain file link names no size until the collector's HEAD answers, and the
// mirror policy compares sizes.
func TestAMirrorIsRejectedOnceTheProbeFindsItsSize(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		resp := probeAnswer(req, http.StatusOK)
		resp.ContentLength = 4096
		return resp, nil
	})

	const mirror = "https://two.example/film.mkv?ref=1"
	got := onlyTask(t, a.AddLinks([]string{mirror}, ""))
	a.analyze(got.ID, mirror)
	a.mu.Lock()
	live := *a.tasks[got.ID]
	a.mu.Unlock()
	if !live.Skipped || live.SkipCode != skipDownloaded {
		t.Errorf("the mirror stayed (skipped=%v, code=%q, size=%d), want it rejected as downloaded",
			live.Skipped, live.SkipCode, live.Size)
	}
}

func TestPastingALinkTheHistoryRejectedAgainNamesTheHistory(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)
	onlyTask(t, a.AddLinks([]string{"https://host.example/film.mkv"}, ""))

	if again := a.AddLinks([]string{"https://host.example/film.mkv"}, ""); len(again) != 0 {
		t.Fatalf("the second paste staged %d tasks, want none", len(again))
	}
	skipped := a.SkippedLinks()
	if len(skipped) != 1 || skipped[0].Reason != "the download history has already rejected this link" {
		t.Errorf("the skipped links are %+v, want the second paste explained by the history", skipped)
	}
}

// A download that finishes while the app runs counts from then on, and a
// cleared history stops counting.
func TestTheCheckFollowsTheHistoryAsItChanges(t *testing.T) {
	a := historyApp(t, nil)
	first := onlyTask(t, a.AddLinks([]string{"https://host.example/a.bin"}, ""))
	if first.Skipped {
		t.Fatalf("a link the history does not have was rejected: %s", first.SkipReason)
	}

	downloadedBefore(t, a, "b", "https://host.example/b.bin", "b.bin", 10)
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/b.bin"}, "")); !got.Skipped {
		t.Error("a download finished after the first check was not seen by the next one")
	}

	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/c.bin"}, "")); got.Skipped {
		t.Errorf("a link was rejected after the history was cleared: %s", got.SkipReason)
	}
	downloadedBefore(t, a, "d", "https://host.example/d.bin", "d.bin", 10)
	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/d.bin"}, "")); got.Skipped {
		t.Errorf("a cleared history still rejected a link: %s", got.SkipReason)
	}
}

// Every site that lists a torrent names it and picks trackers its own way.
func TestAMagnetFromTheHistoryIsRejectedUnderAnotherNameAndTrackers(t *testing.T) {
	for _, uri := range []string{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Pack.2026&tr=udp%3A%2F%2Ftwo.example%3A6969",
		"magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567",
	} {
		a := historyApp(t, nil)
		downloadedBefore(t, a, "old",
			"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Pack&tr=udp%3A%2F%2Fone.example%3A6969",
			"Pack", 900)
		got := onlyTask(t, a.AddLinks([]string{uri}, ""))
		if !got.Skipped || got.SkipCode != skipDownloaded {
			t.Errorf("%s was staged (skipped=%v, code=%q), want it rejected as downloaded", uri, got.Skipped, got.SkipCode)
		}
	}
}

// The restore overrules the filter, which held the link, and not the history,
// which never got to see it.
func TestALinkRestoredFromTheFilterStillMeetsTheHistory(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.LinkFilter = rejectRule("sample files are not wanted here") })
	downloadedBefore(t, a, "old", "https://host.example/sample.mkv", "sample.mkv", 4096)
	held := onlyTask(t, a.AddLinks([]string{"https://host.example/sample.mkv"}, ""))
	if held.SkipCode == skipDownloaded {
		t.Fatal("the history held the link before the filter could")
	}

	got := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the restored link came back as skipped=%v code=%q, want it held as downloaded", got.Skipped, got.SkipCode)
	}
}

// Restored past both, the link is not refused by the filter at the queue.
func TestALinkRestoredPastTheFilterAndTheHistoryIsNotRefusedAtTheQueue(t *testing.T) {
	const reason = "sample files are not wanted here"
	a := historyApp(t, func(s *settings.Settings) { s.LinkFilter = rejectRule(reason) })
	downloadedBefore(t, a, "old", "https://host.example/sample.mkv", "sample.mkv", 4096)
	held := onlyTask(t, a.AddLinks([]string{"https://host.example/sample.mkv"}, ""))
	onlyTask(t, a.RestoreFiltered([]string{held.ID}))

	restored := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if restored.Skipped {
		t.Fatalf("the second restore left the link held: %s", restored.SkipReason)
	}
	a.StartTasks([]string{restored.ID})
	if live := liveTask(a, restored.ID); strings.Contains(live.Error, reason) {
		t.Errorf("the queue refused the link with the filter rule the user overruled (%q)", live.Error)
	}
}

// A link restored past the filter whose HEAD finds the size of a file the
// history has is held like any other.
func TestALinkRestoredFromTheFilterIsHeldOnceTheProbeFindsAMirror(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.LinkFilter = rejectRule("sample files are not wanted here") })
	downloadedBefore(t, a, "old", "https://one.example/sample.mkv", "sample.mkv", 4096)
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		resp := probeAnswer(req, http.StatusOK)
		resp.ContentLength = 4096
		return resp, nil
	})

	held := onlyTask(t, a.AddLinks([]string{"https://two.example/sample.mkv?ref=1"}, ""))
	if restored := onlyTask(t, a.RestoreFiltered([]string{held.ID})); restored.Skipped {
		t.Fatalf("the restore kept the link held before its size was known: %s", restored.SkipReason)
	}
	// The restore's recheck learns the name and then the size, in the
	// background.
	waitFor(t, "the restored mirror held as downloaded", func() bool {
		live := liveTask(a, held.ID)
		return live.Skipped && live.SkipCode == skipDownloaded
	})
}

// Whoever sent the link reads the answer as the verdict: the browser extension
// drops its own download when the link is taken. A size the history needs is
// learned before that answer, not after it.
func TestAMirrorIsRejectedInTheAnswerWhenOnlyItsSizeWasMissing(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		resp := probeAnswer(req, http.StatusOK)
		resp.ContentLength = 4096
		return resp, nil
	})

	got := onlyTask(t, a.AddLinks([]string{"https://two.example/film.mkv?ref=1"}, ""))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the answer has the mirror as skipped=%v code=%q size=%d, want it rejected as downloaded",
			got.Skipped, got.SkipCode, got.Size)
	}
}

// A file the history does not know is not worth a wait at the paste box.
func TestALinkTheHistoryCannotMatchIsAnsweredBeforeItsProbe(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		<-release
		return probeAnswer(req, http.StatusOK), nil
	})

	done := make(chan []*core.Task, 1)
	go func() { done <- a.AddLinks([]string{"https://two.example/other.mkv"}, "") }()
	select {
	case got := <-done:
		if onlyTask(t, got).Skipped {
			t.Error("a file the history does not have was rejected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the answer waited for the probe of a file the history does not have")
	}
}

// The probes a paste waits for run side by side, so a slow host costs the
// answer one probe rather than one per link.
func TestAPasteWaitsForItsSlowestProbeRatherThanForEachInTurn(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)
	const links = 3
	var mu sync.Mutex
	arrived := 0
	all := make(chan struct{})
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		if arrived++; arrived == links {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
		case <-time.After(2 * time.Second):
			return nil, errors.New("the other probes never started")
		}
		resp := probeAnswer(req, http.StatusOK)
		resp.ContentLength = 4096
		return resp, nil
	})

	var urls []string
	for i := range links {
		urls = append(urls, fmt.Sprintf("https://mirror%d.example/film.mkv", i))
	}
	for _, got := range a.AddLinks(urls, "") {
		if !got.Skipped || got.SkipCode != skipDownloaded {
			t.Errorf("%s was answered as skipped=%v code=%q, want it rejected as downloaded", got.URL, got.Skipped, got.SkipCode)
		}
	}
}

// Under size-only every link of unknown size could be a file the history has.
func TestAMirrorIsRejectedInTheAnswerUnderTheSizeOnlyPolicy(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.MirrorPolicy = string(dedupe.PolicySizeOnly) })
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4321)
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		resp := probeAnswer(req, http.StatusOK)
		resp.ContentLength = 4321
		return resp, nil
	})

	got := onlyTask(t, a.AddLinks([]string{"https://two.example/f/renamed.bin"}, ""))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the answer has the mirror as skipped=%v code=%q size=%d, want it rejected as downloaded",
			got.Skipped, got.SkipCode, got.Size)
	}
}

// A banned tracker holds a torrent before the history is asked, so the
// restore asks it.
func TestATorrentRestoredFromTheBanStillMeetsTheHistory(t *testing.T) {
	a := historyApp(t, nil)
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}})
	downloadedBefore(t, a, "old", uri, "Pack", 900)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"example.org"} })
	held, err := a.AddTorrent(uri, nil, "", OriginWatch)
	if err != nil {
		t.Fatal(err)
	}
	if held == nil || held.SkipCode != skipBannedTracker {
		t.Fatalf("the torrent came back as %+v, want it held for its tracker", held)
	}

	got := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the restored torrent came back as skipped=%v code=%q, want it held as downloaded", got.Skipped, got.SkipCode)
	}
}

// Restored past the ban and then the history, the torrent is not refused for
// the tracker at the queue.
func TestATorrentRestoredPastTheBanAndTheHistoryIsPastTheBan(t *testing.T) {
	a := historyApp(t, nil)
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}})
	downloadedBefore(t, a, "old", uri, "Pack", 900)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"example.org"} })
	held, err := a.AddTorrent(uri, nil, "", OriginWatch)
	if err != nil {
		t.Fatal(err)
	}
	onlyTask(t, a.RestoreFiltered([]string{held.ID}))

	restored := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if restored.Skipped {
		t.Fatalf("the second restore left the torrent held: %s", restored.SkipReason)
	}
	if v := trackerBan(restored, a.Settings.Get().Torrent); v.Rejected {
		t.Errorf("the torrent is refused for the tracker the user overruled: %s", v.Reason)
	}
}

// A link downloaded again is saved under a numbered name, and its first name
// still counts.
func TestTheHistoryKeepsEveryNameALinkWasSavedUnder(t *testing.T) {
	a := historyApp(t, nil)
	first := downloadedBefore(t, a, "first", "https://one.example/film.mkv", "film.mkv", 4096)
	if err := a.Store.Save(&core.Task{
		ID: "second", URL: "https://one.example/film.mkv", Name: "film (2).mkv", Size: 4096, Loaded: 4096,
		Status: core.StatusDone, CreatedAt: first, FinishedAt: first.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"film.mkv", "film (2).mkv"} {
		got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
			{DirectURL: "https://two.example/" + url.PathEscape(name), Name: name, Size: 4096},
		}, "", OriginPaste))
		if !got.Skipped || got.SkipCode != skipDownloaded {
			t.Errorf("a mirror named %q was staged (skipped=%v, code=%q), want it rejected as downloaded",
				name, got.Skipped, got.SkipCode)
		}
	}
	got := onlyTask(t, a.AddLinks([]string{"https://one.example/film.mkv"}, ""))
	if got.SkipParams["name"] != "film (2).mkv" {
		t.Errorf("the link itself is rejected as %q, want its latest download", got.SkipParams["name"])
	}
}

// The history has the name the file was saved under, where every control
// character of the link's name became a space and a colon an underscore.
func TestAMirrorIsRejectedWhenItsNameHoldsCharactersTheSavedFileDoesNot(t *testing.T) {
	saved := map[string]string{
		"nul\x00here.bin": "nul here.bin",
		"esc\x1bhere.bin": "esc here.bin",
		"del\x7fhere.bin": "del here.bin",
		"tab\there.bin":   "tab here.bin",
		"a:b.bin":         "a_b.bin",
	}
	for raw, name := range saved {
		t.Run(name, func(t *testing.T) {
			a := historyApp(t, nil)
			downloadedBefore(t, a, "old", "https://one.example/"+url.PathEscape(name), name, 32)

			got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
				{DirectURL: "https://two.example/f/abc", Name: raw, Size: 32},
			}, "", OriginPaste))
			if !got.Skipped || got.SkipCode != skipDownloaded {
				t.Errorf("the mirror %q was staged (skipped=%v, code=%q), want it rejected as %q",
					raw, got.Skipped, got.SkipCode, name)
			}
			if !a.historyNeedsSize(rules.Candidate{URL: "https://three.example/f/abc", Filename: raw}) {
				t.Errorf("a mirror %q of unknown size is answered without its size, want it to wait for the probe", raw)
			}
		})
	}
}

// A torrent added as an uploaded .torrent and later met as a magnet, or the
// other way round, is one download.
func TestATorrentFromTheHistoryIsRejectedInItsOtherForm(t *testing.T) {
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}})
	md, err := (torrent.Resolver{}).Describe(uri)
	if err != nil {
		t.Fatal(err)
	}
	magnet := "magnet:?xt=urn:btih:" + strings.ToUpper(md.InfoHash) + "&dn=Other.Name"
	// No mirror policy, so only the link itself can match.
	noMirrors := func(s *settings.Settings) { s.MirrorPolicy = string(dedupe.PolicyOff) }

	a := historyApp(t, noMirrors)
	downloadedBefore(t, a, "old", uri, "Pack", 900)
	if got := onlyTask(t, a.AddLinks([]string{magnet}, "")); !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the magnet was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}

	a = historyApp(t, noMirrors)
	downloadedBefore(t, a, "old", magnet, "Pack", 900)
	got, err := a.AddTorrent(uri, nil, "", OriginWatch)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Skipped || got.SkipCode != skipDownloaded {
		t.Error("the uploaded torrent was staged, want it rejected as downloaded")
	}
}
