package debrid

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Show"

// scriptedService plays one torrent's life on a service: each status read
// takes the next job from jobs and repeats the last one once they run out.
type scriptedService struct {
	mu       sync.Mutex
	addErr   error
	jobs     []TorrentJob
	reads    int
	added    []TorrentSource
	selected []string
	deleted  []string
}

func (s *scriptedService) ID() string    { return "fake" }
func (s *scriptedService) Label() string { return "Fake" }

func (s *scriptedService) AddTorrent(_ context.Context, src TorrentSource) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, src)
	if s.addErr != nil {
		return "", false, s.addErr
	}
	return "job1", false, nil
}

func (s *scriptedService) addCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.added)
}

func (s *scriptedService) TorrentStatus(context.Context, string) (TorrentJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := min(s.reads, len(s.jobs)-1)
	s.reads++
	return s.jobs[i], nil
}

func (s *scriptedService) SelectFiles(_ context.Context, _ string, files []TorrentFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range files {
		s.selected = append(s.selected, f.ID)
	}
	return nil
}

func (s *scriptedService) FileURL(_ context.Context, _ string, f TorrentFile) (Direct, error) {
	return Direct{URL: "https://cdn.example/" + f.ID}, nil
}

func (s *scriptedService) DeleteTorrent(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *scriptedService) deletedJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.deleted)
}

// partRecorder stands in for the engine and finishes every file at once,
// unless hold is set, in which case a file waits for ctx. The file failOnce
// names fails the first time it is asked for.
type partRecorder struct {
	mu       sync.Mutex
	hold     bool
	failOnce string
	got      []Part
	removed  []string
	paused   []string
}

func (p *partRecorder) FetchPart(ctx context.Context, part Part) (string, error) {
	p.mu.Lock()
	p.got = append(p.got, part)
	hold, fail := p.hold, p.failOnce == part.ID
	if fail {
		p.failOnce = ""
	}
	p.mu.Unlock()
	if fail {
		return "", errors.New("503 Service Unavailable")
	}
	if hold {
		<-ctx.Done()
		return "", ctx.Err()
	}
	part.Progress(part.Size, 0, "/dl/"+part.Path)
	return "/dl/" + part.Path, nil
}

func (p *partRecorder) Pause(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = append(p.paused, id)
}

func (p *partRecorder) Resume(string) {}

func (p *partRecorder) Remove(id string, _ bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removed = append(p.removed, id)
}

func (p *partRecorder) parts() []Part {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func (p *partRecorder) removedParts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.removed)
}

func partIDs(parts []Part) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.ID
	}
	return out
}

// noLinks is the hoster backend beside the torrents, which these tests never
// expect to be asked.
type noLinks struct{ t *testing.T }

func (n noLinks) Download(taskID, link string, _ map[string]string, _ int) {
	n.t.Errorf("hoster backend asked to download %s for %s", link, taskID)
}
func (noLinks) Pause(string)        {}
func (noLinks) Resume(string)       {}
func (noLinks) Remove(string, bool) {}

// updates collects what the backend tells the app, in order.
type updates struct {
	mu  sync.Mutex
	got []core.Update
	ch  chan core.Update
}

func newUpdates() *updates { return &updates{ch: make(chan core.Update, 1024)} }

// record never blocks: a test that stops reading must not wedge the backend's
// polling.
func (u *updates) record(_ string, up core.Update) {
	u.mu.Lock()
	u.got = append(u.got, up)
	u.mu.Unlock()
	select {
	case u.ch <- up:
	default:
	}
}

// until waits for the first update that settles the task.
func (u *updates) until(t *testing.T, status core.Status) core.Update {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case up := <-u.ch:
			if up.Status == status {
				return up
			}
		case <-deadline:
			t.Fatalf("no %s update came", status)
		}
	}
}

func (u *updates) all() []core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.got)
}

func newTestBackend(t *testing.T, svc TorrentService, parts PartDownloader) (*TorrentBackend, *updates) {
	up := newUpdates()
	b := NewTorrentBackend(svc, noLinks{t}, parts, NewRuns("fake"), up.record)
	b.poll = time.Millisecond
	return b, up
}

var twoFiles = []TorrentFile{
	{ID: "1", Path: "Show/e01.mkv", Size: 700, Held: true},
	{ID: "2", Path: "Show/extras/sample.mkv", Size: 30, Held: true},
}

func TestACachedTorrentIsFetchedFileByFileIntoItsOwnFolder(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Size: 730, State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)

	b.Download("t1", testMagnet, nil, 4)
	done := up.until(t, core.StatusDone)

	got := parts.parts()
	if len(got) != 2 {
		t.Fatalf("the engine got %d files, want 2", len(got))
	}
	if got[0].Path != "Show/e01.mkv" || got[1].Path != "Show/extras/sample.mkv" {
		t.Errorf("paths = %q, %q; a multi-file torrent lands in a folder of its own name", got[0].Path, got[1].Path)
	}
	if got[0].URL != "https://cdn.example/1" || got[0].Conns != 4 {
		t.Errorf("first file went out as %s with %d connections", got[0].URL, got[0].Conns)
	}
	if done.Loaded != 730 {
		t.Errorf("done with %d bytes, want 730", done.Loaded)
	}
	if done.File != "" {
		t.Errorf("a folder of files reported the single file %q", done.File)
	}
	if svc.added[0].Magnet != testMagnet {
		t.Errorf("the service got %+v, not the magnet", svc.added[0])
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the finished torrent deleted on the service")
}

func TestAnUncachedTorrentShowsTheServiceProgress(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{
		{Name: "Show", Size: 730, Progress: 0.25, Speed: 5000, Seeds: 3},
		{Name: "Show", Size: 730, Progress: 0.8, Speed: 9000, Seeds: 4},
		{Name: "Show", Size: 730, State: TorrentReady, Files: twoFiles},
	}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	var seen []float64
	for _, u := range up.all() {
		if u.Remote != nil {
			seen = append(seen, u.Remote.Progress)
		}
	}
	if !slices.Contains(seen, 0.25) || !slices.Contains(seen, 0.8) {
		t.Errorf("the service's progress on the task went %v, want 0.25 and 0.8 among it", seen)
	}
	for _, u := range up.all() {
		if u.Remote != nil && u.Status != core.StatusRunning {
			t.Errorf("a %s update carried the remote progress", u.Status)
		}
	}
}

func TestATorrentTheServiceRefusesMovesToTheNextBackend(t *testing.T) {
	svc := &scriptedService{addErr: &Refusal{Reason: "torrent too big"}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	u := up.until(t, core.StatusError)
	if !u.Unsupported || u.Reason != core.ReasonUnsupported {
		t.Errorf("a refusal came back as %+v; it has to hand the task on without benching the account", u)
	}
	if u.Err != "fake: torrent too big" {
		t.Errorf("Err = %q, want the service's reason", u.Err)
	}
}

func TestATorrentTheServiceGivesUpOnIsDeletedAndHandedOn(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentFailed, Reason: "dead torrent"}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	u := up.until(t, core.StatusError)
	if !u.Unsupported {
		t.Errorf("a torrent the service gave up on was not handed on: %+v", u)
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the dead job deleted")
}

func TestAFailedAddIsAnOrdinaryFailure(t *testing.T) {
	svc := &scriptedService{addErr: errors.New("connection reset")}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	u := up.until(t, core.StatusError)
	if u.Unsupported {
		t.Error("a network failure handed the task on; it is worth another try on the same service")
	}
}

func TestAKeptTorrentStaysOnTheService(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	b, up := newTestBackend(t, svc, &partRecorder{})
	b.Keep = func() bool { return true }

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)
	time.Sleep(50 * time.Millisecond)
	if d := svc.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v although finished torrents are to stay", d)
	}
	b.Remove("t1", false)
	time.Sleep(50 * time.Millisecond)
	if d := svc.deletedJobs(); len(d) != 0 {
		t.Errorf("removing the finished task deleted the kept torrent %v", d)
	}
}

func TestRemovingATaskDeletesItsJobOnTheService(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Progress: 0.1}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	for u := range up.ch {
		if u.Remote != nil && u.Remote.Progress > 0 {
			break
		}
	}
	b.Remove("t1", true)
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the job deleted with the task")
}

func TestRemovingATaskMidFileTakesItsFilesOutOfTheEngine(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{hold: true}
	b, _ := newTestBackend(t, svc, parts)

	b.Download("t1", testMagnet, nil, 1)
	waitFor(t, func() bool { return len(parts.parts()) == 1 }, "the first file handed to the engine")
	b.Pause("t1")
	b.Remove("t1", true)

	parts.mu.Lock()
	defer parts.mu.Unlock()
	if !slices.Equal(parts.paused, []string{"t1/0"}) {
		t.Errorf("paused %v, want the file in flight", parts.paused)
	}
	if !slices.Equal(parts.removed, []string{"t1/0"}) {
		t.Errorf("removed %v, want the file in flight", parts.removed)
	}
}

func TestAPausedFetchResumesWithoutAddingTheTorrentAgain(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Progress: 0.1}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	for u := range up.ch {
		if u.Remote != nil && u.Remote.Progress > 0 {
			break
		}
	}
	b.Pause("t1")
	up.until(t, core.StatusPaused)
	svc.mu.Lock()
	svc.jobs = []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}
	svc.reads = 0
	svc.mu.Unlock()
	b.Resume("t1")
	up.until(t, core.StatusDone)

	if n := len(svc.added); n != 1 {
		t.Errorf("the torrent was added %d times; a resume keeps the service's job", n)
	}
}

func TestOnlyTheSelectedFilesAreFetched(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{
		{Name: "Show", State: TorrentChoosing, Files: []TorrentFile{
			{ID: "1", Path: "/e01.mkv", Size: 700}, {ID: "2", Path: "/extras/sample.mkv", Size: 30},
		}},
		{Name: "Show", State: TorrentReady, Files: []TorrentFile{
			{ID: "l1", Path: "/e01.mkv", Size: 700, Held: true}, {ID: "2", Path: "/extras/sample.mkv", Size: 30},
		}},
	}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Files = func(string) []core.TorrentFile {
		return []core.TorrentFile{{Path: "e01.mkv", Size: 700, Selected: true}, {Path: "extras/sample.mkv", Size: 30}}
	}

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	if !slices.Equal(svc.selected, []string{"1"}) {
		t.Errorf("the service was told to fetch %v, want only the selected file", svc.selected)
	}
	if !svc.added[0].Choose {
		t.Error("the torrent was added without asking the service to wait for the selection")
	}
	got := parts.parts()
	if len(got) != 1 || got[0].Path != "Show/e01.mkv" {
		t.Errorf("the engine got %+v, want only Show/e01.mkv", got)
	}
}

func TestAServiceThatCannotSelectStillFetchesOnlyTheSelection(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Files = func(string) []core.TorrentFile {
		return []core.TorrentFile{{Path: "e01.mkv", Selected: false}, {Path: "extras/sample.mkv", Selected: true}}
	}

	b.Download("t1", testMagnet, nil, 1)
	done := up.until(t, core.StatusDone)

	got := parts.parts()
	if len(got) != 1 || got[0].Path != "Show/extras/sample.mkv" {
		t.Errorf("the engine got %+v, want only the selected sample", got)
	}
	if done.Loaded != 30 {
		t.Errorf("done with %d bytes, want the 30 selected", done.Loaded)
	}
}

func TestATorrentTheServiceLeavesUnnamedTakesItsOwnName(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{State: TorrentReady, Files: []TorrentFile{
		{ID: "1", Path: "e01.mkv", Size: 1, Held: true}, {ID: "2", Path: "e02.mkv", Size: 1, Held: true},
	}}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)
	if got := parts.parts(); got[0].Path != "Show/e01.mkv" {
		t.Errorf("path = %q, want the folder named after the magnet's own name", got[0].Path)
	}
}

func TestASingleFileTorrentLandsAsTheFileItself(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "movie.mkv", State: TorrentReady, Files: []TorrentFile{
		{ID: "1", Path: "/movie.mkv", Size: 900, Held: true},
	}}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)

	b.Download("t1", testMagnet, nil, 1)
	done := up.until(t, core.StatusDone)
	if got := parts.parts(); got[0].Path != "movie.mkv" {
		t.Errorf("path = %q, want the file on its own", got[0].Path)
	}
	if done.File != "/dl/movie.mkv" {
		t.Errorf("done named %q as the file, want where the engine wrote it", done.File)
	}
}

func TestAPrivateTorrentIsNeverSentToTheService(t *testing.T) {
	upload, _ := builtTorrent(t, true)
	for name, link := range map[string]string{
		"an uploaded .torrent marked private": upload,
		"a magnet whose tracker carries a passkey": testMagnet +
			"&tr=https%3A%2F%2Ftracker.private.example%2Fannounce.php%3Fpasskey%3D0123456789abcdef0123456789abcdef",
	} {
		t.Run(name, func(t *testing.T) {
			svc := &scriptedService{jobs: []TorrentJob{{State: TorrentFailed}}}
			b, up := newTestBackend(t, svc, &partRecorder{})

			b.Download("t1", link, nil, 1)
			u := up.until(t, core.StatusError)
			if !u.Unsupported {
				t.Errorf("a private torrent was not handed on: %+v", u)
			}
			if svc.addCount() != 0 {
				t.Error("a private torrent's passkey went to the service")
			}
		})
	}
}

func TestAMagnetWithAPublicTrackerGoesToTheService(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet+"&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce", nil, 1)
	up.until(t, core.StatusDone)
	if svc.addCount() != 1 {
		t.Error("a public magnet was kept from the service")
	}
}

func TestAHosterLinkGoesToTheBackendBeside(t *testing.T) {
	links := &linkSpy{}
	b := NewTorrentBackend(&scriptedService{}, links, &partRecorder{}, NewRuns("fake"), func(string, core.Update) {})
	b.Download("t1", "https://hoster.example/f/1", nil, 2)
	b.Pause("t1")
	b.Remove("t1", true)
	if !slices.Equal(links.calls, []string{"download", "pause", "remove"}) {
		t.Errorf("the hoster backend saw %v", links.calls)
	}
}

type linkSpy struct{ calls []string }

func (l *linkSpy) Download(string, string, map[string]string, int) {
	l.calls = append(l.calls, "download")
}
func (l *linkSpy) Pause(string)        { l.calls = append(l.calls, "pause") }
func (l *linkSpy) Resume(string)       { l.calls = append(l.calls, "resume") }
func (l *linkSpy) Remove(string, bool) { l.calls = append(l.calls, "remove") }

func TestTheResolverClaimsTorrentsOnlyForAServiceThatTakesThem(t *testing.T) {
	with := Resolver{ServiceID: "realdebrid", Torrents: true}
	without := Resolver{ServiceID: "rpnet"}
	if !with.Match(testMagnet) {
		t.Error("a service that takes torrents does not claim a magnet")
	}
	if without.Match(testMagnet) {
		t.Error("a service without a torrent API claims a magnet")
	}
	res, err := with.Resolve(context.Background(), resolver.Request{URL: testMagnet})
	if err != nil || res.Name != "Show" {
		t.Errorf("Resolve = %+v, %v; want the magnet checked and named like the built-in client does", res, err)
	}
	if _, err := with.Resolve(context.Background(), resolver.Request{URL: "magnet:?dn=nothing"}); err == nil {
		t.Error("a magnet without an info hash went through")
	}
}

// The app's retry after a failed file goes through Download again, which has
// to keep the job and the files already here.
func TestARetryCarriesOnFromTheFileThatFailed(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{failOnce: "t1/1"}
	b, up := newTestBackend(t, svc, parts)

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusError)
	if !b.Holds("t1") {
		t.Fatal("after a failed file the backend holds nothing to carry on with")
	}
	b.Download("t1", testMagnet, nil, 1)
	done := up.until(t, core.StatusDone)

	if got := partIDs(parts.parts()); !slices.Equal(got, []string{"t1/0", "t1/1", "t1/1"}) {
		t.Errorf("the engine was asked for %v; the retry fetches only the file that failed", got)
	}
	if n := svc.addCount(); n != 1 {
		t.Errorf("the torrent was added %d times", n)
	}
	if got := parts.removedParts(); !slices.Equal(got, []string{"t1/1"}) {
		t.Errorf("the retry took %v out of the engine, want only the failed attempt at the second file", got)
	}
	if done.Loaded != 730 {
		t.Errorf("done with %d bytes, want 730", done.Loaded)
	}
}

func TestTheJobAndItsProgressGoToTheAppAsTheyChange(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	var jobs []core.ServiceJob
	for _, u := range up.all() {
		if u.Job != nil {
			jobs = append(jobs, *u.Job)
		}
	}
	want := []core.ServiceJob{
		{Slot: "fake", ID: "job1", Owned: true},
		{Slot: "fake", ID: "job1", Owned: true, Partial: "/dl/Show/e01.mkv"},
		{Slot: "fake", ID: "job1", Owned: true, Done: 1},
		{Slot: "fake", ID: "job1", Owned: true, Done: 1, Partial: "/dl/Show/extras/sample.mkv"},
		{Slot: "fake", ID: "job1", Owned: true, Done: 2},
		{},
	}
	if !slices.Equal(jobs, want) {
		t.Errorf("the app was told %+v, want %+v", jobs, want)
	}
}

func TestARestoredJobIsFetchedWithoutAddingTheTorrentAgain(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.runs.Restore("t1", testMagnet, core.ServiceJob{Slot: "fake", ID: "job9", Owned: true, Done: 1, Partial: "/dl/Show/extras/sample.mkv"})

	b.Download("t1", testMagnet, nil, 1)
	done := up.until(t, core.StatusDone)

	if n := svc.addCount(); n != 0 {
		t.Errorf("the torrent was added again although the service holds it")
	}
	got := parts.parts()
	if len(got) != 1 || got[0].Path != "Show/extras/sample.mkv" {
		t.Fatalf("the engine got %v, want only the file that was not here yet", partURLs(got))
	}
	if got[0].Leftover != "/dl/Show/extras/sample.mkv" {
		t.Errorf("the file went out with the leftover %q, want where the attempt before the restart wrote it", got[0].Leftover)
	}
	if done.Loaded != 730 {
		t.Errorf("done with %d bytes, want the 700 already here and the 30 fetched", done.Loaded)
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job9"}) }, "the finished job deleted on the service")
}

func TestRemovingARestoredTaskDeletesItsJob(t *testing.T) {
	svc := &scriptedService{}
	b, _ := newTestBackend(t, svc, &partRecorder{})
	b.runs.Restore("t1", testMagnet, core.ServiceJob{Slot: "fake", ID: "job9", Owned: true})

	b.Remove("t1", false)
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job9"}) }, "the job deleted with the task")
}

func TestABackendBuiltForTheSameAccountReachesTheRunsOfTheOldOne(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Progress: 0.1}}}
	runs := NewRuns("fake")
	up := newUpdates()
	old := NewTorrentBackend(svc, noLinks{t}, &partRecorder{}, runs, up.record)
	old.poll = time.Millisecond

	old.Download("t1", testMagnet, nil, 1)
	for u := range up.ch {
		if u.Remote != nil && u.Remote.Progress > 0 {
			break
		}
	}
	NewTorrentBackend(svc, noLinks{t}, &partRecorder{}, runs, up.record).Remove("t1", false)

	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the job deleted through the new backend")
	svc.mu.Lock()
	reads := svc.reads
	svc.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.reads > reads+1 {
		t.Errorf("the old backend kept reading the job after the removal (%d more reads)", svc.reads-reads)
	}
}

// slowAdd is a service that has taken the torrent in the moment it is asked,
// but answers only once release is closed, or gives up with ctx.
type slowAdd struct {
	scriptedService
	entered chan struct{}
	release chan struct{}
}

func (s *slowAdd) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	id, held, err := s.scriptedService.AddTorrent(ctx, src)
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return id, held, err
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

func newSlowAdd() *slowAdd {
	return &slowAdd{
		scriptedService: scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}},
		entered:         make(chan struct{}, 4),
		release:         make(chan struct{}),
	}
}

func TestAPauseWhileTheServiceTakesTheTorrentKeepsItsJob(t *testing.T) {
	svc := newSlowAdd()
	b, up := newTestBackend(t, svc, &partRecorder{})
	claimed := make(chan string, 4)
	b.Added = func(job string) { claimed <- job }

	b.Download("t1", testMagnet, nil, 1)
	<-svc.entered
	b.Pause("t1")
	up.until(t, core.StatusPaused)
	close(svc.release)
	select {
	case job := <-claimed:
		if job != "job1" {
			t.Fatalf("claimed %q", job)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job the service made was never claimed")
	}
	b.Resume("t1")
	up.until(t, core.StatusDone)

	if n := svc.addCount(); n != 1 {
		t.Errorf("the torrent was added %d times; the resume carries on with the job the pause left", n)
	}
}

func TestARemovalWhileTheServiceTakesTheTorrentDeletesItsJob(t *testing.T) {
	svc := newSlowAdd()
	b, _ := newTestBackend(t, svc, &partRecorder{})
	claimed := make(chan string, 4)
	b.Added = func(job string) { claimed <- job }

	b.Download("t1", testMagnet, nil, 1)
	<-svc.entered
	b.Remove("t1", false)
	close(svc.release)

	select {
	case job := <-claimed:
		if job != "job1" {
			t.Errorf("claimed %q", job)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job the service made was never claimed, so the import would take it for the user's")
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the job of the removed task deleted")
}

// lentJob is an account that already had the torrent: its job is lent to
// the task, not made for it.
type lentJob struct{ scriptedService }

func (s *lentJob) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	id, _, err := s.scriptedService.AddTorrent(ctx, src)
	return id, true, err
}

func TestAJobTheAccountAlreadyHeldIsNeverDeleted(t *testing.T) {
	for _, c := range []struct {
		name string
		jobs []TorrentJob
		end  func(t *testing.T, b *TorrentBackend, up *updates)
	}{
		{"finished", []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}, func(t *testing.T, _ *TorrentBackend, up *updates) {
			up.until(t, core.StatusDone)
		}},
		{"refused", []TorrentJob{{Name: "Show", State: TorrentFailed, Reason: "dead"}}, func(t *testing.T, _ *TorrentBackend, up *updates) {
			up.until(t, core.StatusError)
		}},
		{"removed", []TorrentJob{{Name: "Show", Progress: 0.1}}, func(_ *testing.T, b *TorrentBackend, up *updates) {
			for u := range up.ch {
				if u.Remote != nil && u.Remote.Progress > 0 {
					break
				}
			}
			b.Remove("t1", true)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &lentJob{scriptedService{jobs: c.jobs}}
			b, up := newTestBackend(t, svc, &partRecorder{})
			b.Download("t1", testMagnet, nil, 1)
			c.end(t, b, up)
			time.Sleep(50 * time.Millisecond)
			if d := svc.deletedJobs(); len(d) != 0 {
				t.Errorf("deleted %v, which was on the account before the task asked for it", d)
			}
		})
	}
}

const testHash = "0123456789abcdef0123456789abcdef01234567"

// listedAccount is a service whose list names each job's info hash, as
// Real-Debrid's, AllDebrid's and Debrid-Link's do. With lostAnswer set it
// takes the torrent but its answer never arrives.
type listedAccount struct {
	scriptedService
	listed     []Listed
	lostAnswer bool
}

func (s *listedAccount) AddTorrent(ctx context.Context, src TorrentSource) (string, bool, error) {
	id, held, err := s.scriptedService.AddTorrent(ctx, src)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		return id, held, err
	}
	s.listed = append(s.listed, Listed{ID: id, Hash: src.InfoHash})
	if s.lostAnswer {
		return "", false, context.DeadlineExceeded
	}
	return id, held, nil
}

func (s *listedAccount) List(context.Context) ([]Listed, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.listed), true, nil
}

func TestATorrentTheAccountListsIsFetchedFromThereAndNeverDeleted(t *testing.T) {
	for _, c := range []struct {
		name string
		jobs []TorrentJob
		end  func(t *testing.T, b *TorrentBackend, up *updates)
	}{
		{"finished", []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}, func(t *testing.T, _ *TorrentBackend, up *updates) {
			up.until(t, core.StatusDone)
		}},
		{"removed", []TorrentJob{{Name: "Show", Progress: 0.1}}, func(_ *testing.T, b *TorrentBackend, up *updates) {
			for u := range up.ch {
				if u.Remote != nil && u.Remote.Progress > 0 {
					break
				}
			}
			b.Remove("t1", true)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &listedAccount{scriptedService: scriptedService{jobs: c.jobs}, listed: []Listed{{ID: "theirs", Hash: testHash}}}
			b, up := newTestBackend(t, svc, &partRecorder{})
			b.Download("t1", testMagnet, nil, 1)
			c.end(t, b, up)
			time.Sleep(50 * time.Millisecond)

			if n := svc.addCount(); n != 0 {
				t.Errorf("the torrent went to the service %d times although the account holds it", n)
			}
			if d := svc.deletedJobs(); len(d) != 0 {
				t.Errorf("deleted %v, which was on the account before the task asked for it", d)
			}
			if !slices.ContainsFunc(up.all(), func(u core.Update) bool { return u.Job != nil && u.Job.ID == "theirs" && !u.Job.Owned }) {
				t.Error("the task was never told it fetches the account's own job")
			}
		})
	}
}

func TestAnAddWhoseAnswerWentMissingCarriesOnWithTheJobItMade(t *testing.T) {
	svc := &listedAccount{scriptedService: scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}, lostAnswer: true}
	b, up := newTestBackend(t, svc, &partRecorder{})
	claimed := make(chan string, 4)
	b.Added = func(job string) { claimed <- job }

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	if n := svc.addCount(); n != 1 {
		t.Errorf("the torrent was added %d times", n)
	}
	select {
	case job := <-claimed:
		if job != "job1" {
			t.Errorf("claimed %q", job)
		}
	default:
		t.Error("the job the service made was never claimed, so the import would take it for the user's")
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"job1"}) }, "the finished job deleted on the service")
}

func TestAFailedAddThatLeftNoJobIsAnOrdinaryFailure(t *testing.T) {
	svc := &listedAccount{scriptedService: scriptedService{addErr: errors.New("connection reset")}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", testMagnet, nil, 1)
	u := up.until(t, core.StatusError)
	if u.Unsupported || b.Holds("t1") {
		t.Errorf("settled as %+v, holding a job %v; want a failure worth another try", u, b.Holds("t1"))
	}
}

func TestASelectedFileIsNotTakenForOneOfTheSameNameInAFolder(t *testing.T) {
	sel := []core.TorrentFile{{Path: "e01.mkv", Selected: true}, {Path: "Extras/e01.mkv"}}
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/e01.mkv", true},
		{"Show/e01.mkv", true},
		{"/Extras/e01.mkv", false},
		{"Show/Extras/e01.mkv", false},
	} {
		if got := pickedIn(sel, "Show", c.path); got != c.want {
			t.Errorf("pickedIn(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// skipSamples is file rules that leave out anything called sample.
func skipSamples(string) (func(string, int64) bool, error) {
	return func(p string, _ int64) bool { return !strings.Contains(p, "sample") }, nil
}

func TestTheFileRulesChooseWhatAServiceThatSelectsFetches(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{
		{Name: "Show", State: TorrentChoosing, Files: []TorrentFile{
			{ID: "1", Path: "/e01.mkv", Size: 700}, {ID: "2", Path: "/extras/sample.mkv", Size: 30},
		}},
		{Name: "Show", State: TorrentReady, Files: []TorrentFile{
			{ID: "l1", Path: "/e01.mkv", Size: 700, Held: true}, {ID: "2", Path: "/extras/sample.mkv", Size: 30},
		}},
	}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Rules = skipSamples

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	if !svc.added[0].Choose {
		t.Error("the torrent was added without asking the service to wait for the rules' selection")
	}
	if !slices.Equal(svc.selected, []string{"1"}) {
		t.Errorf("the service was told to fetch %v, want only what the rules keep", svc.selected)
	}
	if got := parts.parts(); len(got) != 1 || got[0].Path != "Show/e01.mkv" {
		t.Errorf("the engine got %+v, want only Show/e01.mkv", got)
	}
}

func TestAServiceThatCannotSelectFetchesOnlyWhatTheRulesKeep(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Rules = skipSamples

	b.Download("t1", testMagnet, nil, 1)
	done := up.until(t, core.StatusDone)

	if got := parts.parts(); len(got) != 1 || got[0].Path != "Show/e01.mkv" {
		t.Errorf("the engine got %+v, want only Show/e01.mkv", got)
	}
	if done.Loaded != 700 {
		t.Errorf("done with %d bytes, want the 700 the rules keep", done.Loaded)
	}
}

func TestAHandSelectionWinsOverTheFileRules(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Rules = skipSamples
	b.Files = func(string) []core.TorrentFile {
		return []core.TorrentFile{{Path: "e01.mkv", Selected: true}, {Path: "extras/sample.mkv", Selected: true}}
	}

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	if got := partIDs(parts.parts()); len(got) != 2 {
		t.Errorf("the engine got %v, want both files the hand selection kept", got)
	}
}

func TestFileRulesThatKeepNothingFetchEveryFile(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Rules = func(string) (func(string, int64) bool, error) {
		return func(string, int64) bool { return false }, nil
	}

	b.Download("t1", testMagnet, nil, 1)
	up.until(t, core.StatusDone)

	if got := partIDs(parts.parts()); len(got) != 2 {
		t.Errorf("the engine got %v, want every file, as the built-in client does", got)
	}
}
