package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// testTorrentURI's fixtures announce to tracker.example.org, and so does this
// magnet.
const bannableMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Show&tr=udp%3A%2F%2Ftracker.example.org%3A6969%2Fannounce"

func setTorrentSettings(t *testing.T, a *App, change func(*settings.Torrent)) {
	t.Helper()
	s := a.Settings.Get()
	change(&s.Torrent)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
}

func privateTorrentURI(t *testing.T) string {
	t.Helper()
	private := true
	info := metainfo.Info{
		Name:        "Private",
		Files:       []metainfo.FileInfo{{Length: 1 << 20, Path: []string{"a.mkv"}}},
		PieceLength: testPieceLength,
		Pieces:      testPieces(1 << 20),
		Private:     &private,
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib, Announce: "https://tracker.private.example/announce"})
	if err != nil {
		t.Fatal(err)
	}
	return torrent.EncodeBytes(b)
}

func TestAMagnetAnnouncingABannedTrackerIsHeldWithTheReason(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"example.org"} })

	created := a.AddLinksFrom([]string{bannableMagnet}, "Banned", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("AddLinksFrom returned %d tasks, want the held one", len(created))
	}
	held := created[0]
	if !held.Skipped {
		t.Fatalf("the magnet was staged: %+v", held)
	}
	if !strings.Contains(held.SkipReason, "tracker.example.org") || !strings.Contains(held.SkipReason, "banned") {
		t.Errorf("reason = %q, want the banned tracker named", held.SkipReason)
	}
	if held.Host != "torrent-magnet" {
		t.Errorf("host = %q, want the magnet bucket rather than the link", held.Host)
	}
}

func TestAnUploadAnnouncingABannedTrackerIsHeld(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"udp://tracker.example.org:6969/announce"} })
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}})

	task, err := a.AddTorrent(uri, []core.TorrentFile{{Path: "one.mkv", Size: 900, Selected: true}}, "Pack", OriginPaste)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || !task.Skipped || !strings.Contains(task.SkipReason, "tracker.example.org") {
		t.Fatalf("task = %+v, want it held for its tracker", task)
	}
}

func TestATorrentWithNoBannedTrackerIsStaged(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"elsewhere.example"} })
	created := a.AddLinksFrom([]string{bannableMagnet}, "Fine", OriginPaste)
	if len(created) != 1 || created[0].Skipped {
		t.Fatalf("created = %+v, want the magnet staged", created)
	}
}

// A tracker banned after the torrent was staged still keeps it from starting,
// the way a filter rule added later does.
func TestATrackerBannedAfterStagingStopsTheStart(t *testing.T) {
	a := newTorrentTestApp(t)
	created := a.AddLinksFrom([]string{bannableMagnet}, "Later", OriginPaste)
	if len(created) != 1 || created[0].Skipped {
		t.Fatalf("created = %+v, want the magnet staged", created)
	}
	id := created[0].ID
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"tracker.example.org"} })

	a.StartTasks([]string{id})
	waitFor(t, "the start refused for the banned tracker", func() bool {
		for _, tsk := range a.Tasks() {
			if tsk.ID == id {
				return tsk.Status == core.StatusError && strings.Contains(tsk.Error, "banned")
			}
		}
		return false
	})
}

func torrentJob(a *App, t *core.Task) engine.Job {
	job := engine.Job{TaskID: t.ID, URL: t.URL}
	a.mu.Lock()
	a.torrentJobLocked(&job, t, a.Settings.Get(), t.File)
	a.mu.Unlock()
	return job
}

func TestAMagnetsJobCarriesTheFileRulesAndTheExtraTrackers(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) {
		tr.MinFileSize = 1 << 20
		tr.ExcludeFiles = []string{`(?i)sample`}
		tr.ExtraTrackers = []string{
			"udp://tracker.opentrackr.org:1337/announce",
			"udp://tracker.example.org:6969/announce",
			"http://tracker.bad.example/announce",
		}
		tr.BannedTrackers = []string{"bad.example"}
	})

	job := torrentJob(a, &core.Task{URL: bannableMagnet})
	want := torrent.FileRules{MinSize: 1 << 20, Exclude: []string{`(?i)sample`}}
	if job.FileRules.MinSize != want.MinSize || !slices.Equal(job.FileRules.Exclude, want.Exclude) {
		t.Errorf("FileRules = %+v, want %+v", job.FileRules, want)
	}
	if !slices.Equal(job.Trackers, []string{"udp://tracker.opentrackr.org:1337/announce"}) {
		t.Errorf("Trackers = %q, want only the one the magnet lacks and nobody banned", job.Trackers)
	}
}

// A drawer with a file selection of its own replaces the Torrents page's for
// the torrents filed in it, here a music drawer that wants the small files.
func TestADrawersOwnFileSelectionChoosesForItsTorrents(t *testing.T) {
	a := newTorrentTestApp(t)
	s := a.Settings.Get()
	s.Torrent.MinFileSize = 50 << 20
	s.Categories = []settings.Category{
		{ID: "music", Name: "Music", TorrentFiles: &settings.TorrentFileRules{ExcludeFiles: []string{`\.nfo$`}}},
		{ID: "films", Name: "Films"},
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	music := torrentJob(a, &core.Task{URL: bannableMagnet, Category: "music"}).FileRules
	if music.MinSize != 0 || !slices.Equal(music.Exclude, []string{`\.nfo$`}) {
		t.Errorf("a magnet filed in music gets %+v, want the drawer's own selection", music)
	}
	for _, cat := range []string{"films", ""} {
		if got := torrentJob(a, &core.Task{URL: bannableMagnet, Category: cat}).FileRules; got.MinSize != 50<<20 {
			t.Errorf("a magnet filed in %q gets %+v, want the Torrents page's", cat, got)
		}
	}
}

// A file list on the task is a choice made by hand.
func TestATorrentWithChosenFilesGetsNoFileRules(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.ExcludeFiles = []string{`\.nfo$`} })
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}, {Length: 9, Path: []string{"one.nfo"}}})
	task := &core.Task{URL: uri, TorrentFiles: []core.TorrentFile{{Path: "one.mkv", Size: 900, Selected: true}, {Path: "one.nfo", Size: 9, Selected: true}}}
	if job := torrentJob(a, task); !job.FileRules.Empty() {
		t.Fatalf("FileRules = %+v, want none over a selection already made", job.FileRules)
	}
}

func TestAPrivateTorrentsJobGetsNoExtraTrackers(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) {
		tr.ExtraTrackers = []string{"udp://tracker.opentrackr.org:1337/announce"}
	})
	if job := torrentJob(a, &core.Task{URL: privateTorrentURI(t)}); job.Trackers != nil {
		t.Fatalf("a private torrent was given %q", job.Trackers)
	}
}

func TestAPlainDownloadsJobIsLeftAlone(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) {
		tr.ExcludeFiles = []string{`x`}
		tr.ExtraTrackers = []string{"udp://tracker.opentrackr.org:1337/announce"}
	})
	job := torrentJob(a, &core.Task{URL: "https://example.org/file.bin"})
	if !job.FileRules.Empty() || job.Trackers != nil {
		t.Fatalf("job = %+v, want no torrent settings on a plain download", job)
	}
}

func TestTheFetchedTrackerListJoinsTheExtraTrackers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("udp://tracker.opentrackr.org:1337/announce\n\nudp://open.stealth.si:80/announce\n"))
	}))
	t.Cleanup(srv.Close)
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) {
		tr.ExtraTrackers = []string{"https://tracker.gbitt.info:443/announce"}
		tr.TrackerListURL = srv.URL
	})
	// Saving the address starts a fetch of its own; this one waits for the
	// answer, or finds the list already fetched.
	if err := a.publicTrackers().Refresh(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the tracker list fetched", func() bool { return a.TrackerListStatus().Trackers == 2 })

	job := torrentJob(a, &core.Task{URL: bannableMagnet})
	want := []string{
		"https://tracker.gbitt.info:443/announce",
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://open.stealth.si:80/announce",
	}
	if !slices.Equal(job.Trackers, want) {
		t.Fatalf("Trackers = %q, want the typed ones and then the list's", job.Trackers)
	}
}

// A restore puts back the torrent that was held, file selection included, so
// the files unticked in its review stay unticked.
func TestARestoredUploadKeepsTheFilesTickedByHand(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) {
		tr.BannedTrackers = []string{"example.org"}
		tr.ExcludeFiles = []string{`\.srt$`}
	})
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 9, Path: []string{"one.nfo"}},
		{Length: 12, Path: []string{"one.srt"}},
	})
	ticked := []core.TorrentFile{
		{Path: "one.mkv", Size: 900, Selected: true},
		{Path: "one.nfo", Size: 9, Selected: false},
		{Path: "one.srt", Size: 12, Selected: true},
	}
	held, err := a.AddTorrent(uri, ticked, "Pack", OriginPaste)
	if err != nil || held == nil || !held.Skipped {
		t.Fatalf("AddTorrent = %+v, %v, want the upload held for its tracker", held, err)
	}

	restored := a.RestoreFiltered([]string{held.ID})
	if len(restored) != 1 {
		t.Fatalf("RestoreFiltered gave back %d tasks, want 1", len(restored))
	}
	task := restored[0]
	if got := core.SelectedTorrentIndices(task.TorrentFiles); !slices.Equal(got, []int{0, 2}) {
		t.Fatalf("the restored upload selects %v, want the film and the subtitles as ticked", got)
	}
	if job := torrentJob(a, task); !job.FileRules.Empty() {
		t.Errorf("FileRules = %+v, want none over the selection made by hand", job.FileRules)
	}
	if task.Size != 912 {
		t.Errorf("size = %d, want the 912 bytes ticked", task.Size)
	}
}

// Restoring a torrent lets it past the banned-tracker lines that caught it,
// all of its trackers they cover included, and past no other line.
func TestARestoredTorrentIsPastTheBanItWasHeldForOnly(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Show" +
		"&tr=udp%3A%2F%2Ftracker.example.org%3A6969%2Fannounce" +
		"&tr=udp%3A%2F%2Fbackup.example.org%3A6969%2Fannounce" +
		"&tr=udp%3A%2F%2Ftracker.other.example%3A80%2Fannounce"
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"example.org"} })
	created := a.AddLinksFrom([]string{magnet}, "Show", OriginPaste)
	if len(created) != 1 || !created[0].Skipped {
		t.Fatalf("created = %+v, want the magnet held", created)
	}
	restored := a.RestoreFiltered([]string{created[0].ID})
	if len(restored) != 1 {
		t.Fatalf("RestoreFiltered gave back %d tasks, want 1", len(restored))
	}
	task := restored[0]

	if v := trackerBan(task, a.Settings.Get().Torrent); v.Rejected {
		t.Fatalf("the restored torrent is refused again: %s", v.Reason)
	}
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.BannedTrackers = []string{"example.org", "other.example"} })
	v := trackerBan(task, a.Settings.Get().Torrent)
	if !v.Rejected || !strings.Contains(v.Reason, "tracker.other.example") {
		t.Fatalf("verdict = %+v, want a refusal for the tracker banned since", v)
	}

	a.StartTasks([]string{task.ID})
	waitFor(t, "the start refused for the tracker banned since", func() bool {
		for _, tsk := range a.Tasks() {
			if tsk.ID == task.ID {
				return tsk.Status == core.StatusError && strings.Contains(tsk.Error, "tracker.other.example")
			}
		}
		return false
	})
}

// The settings page saves as the address is typed, so a half-typed address
// can still be fetching when the whole one is saved, which must then be
// fetched as well.
func TestATrackerListSavedDuringAnotherFetchIsFetchedAfterIt(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trackers_be" {
			entered <- struct{}{}
			<-release
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("udp://tracker.opentrackr.org:1337/announce\n"))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(letGo)
	a := newTorrentTestApp(t)

	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.TrackerListURL = srv.URL + "/trackers_be" })
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the half-typed address was never fetched")
	}
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.TrackerListURL = srv.URL + "/trackers_best.txt" })
	letGo()

	waitFor(t, "the whole address fetched", func() bool { return a.TrackerListStatus().Trackers == 1 })
}

func TestPickTorrentFilesReadsTheSettings(t *testing.T) {
	a := newTorrentTestApp(t)
	setTorrentSettings(t, a, func(tr *settings.Torrent) { tr.ExcludeFiles = []string{`\.nfo$`} })
	got, err := a.PickTorrentFiles([]core.TorrentFile{{Path: "a.mkv", Size: 10}, {Path: "a.nfo", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Selected || got[1].Selected {
		t.Fatalf("PickTorrentFiles = %+v, want the .nfo left out", got)
	}
}
