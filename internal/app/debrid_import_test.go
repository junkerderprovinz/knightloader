package app

// The import from a debrid account: what the user adds on the service's
// website lands in the collector once, is never taken for one of this
// instance's own jobs, survives a restart without coming in twice, and is
// deleted on the service when its task is removed here.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
)

// websiteAccount is a debrid account the test fills the way the user would on
// the service's website. Jobs this instance adds show up in its list as well,
// without an info hash, so only the claim can tell them apart.
type websiteAccount struct {
	mu      sync.Mutex
	list    []debrid.Listed
	polls   int
	added   int
	deleted []string
	// refusals is how many deletes the service still turns down.
	refusals int
}

func (w *websiteAccount) ID() string    { return "fakedebrid" }
func (w *websiteAccount) Label() string { return "Fake debrid" }

func (w *websiteAccount) AddTorrent(context.Context, debrid.TorrentSource) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.added++
	id := fmt.Sprintf("OWN%d", w.added)
	w.list = append(w.list, debrid.Listed{ID: id, Name: "Show", Added: time.Now()})
	return id, false, nil
}

// TorrentStatus keeps every job fetching, so no task gets as far as the
// engine.
func (w *websiteAccount) TorrentStatus(context.Context, string) (debrid.TorrentJob, error) {
	return debrid.TorrentJob{Name: "Show", Progress: 0.1}, nil
}

func (w *websiteAccount) addCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.added
}

func (w *websiteAccount) FileURL(context.Context, string, debrid.TorrentFile) (debrid.Direct, error) {
	return debrid.Direct{}, errors.New("nothing is ready")
}

func (w *websiteAccount) DeleteTorrent(_ context.Context, id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refusals > 0 {
		w.refusals--
		return errors.New("503 Service Unavailable")
	}
	w.deleted = append(w.deleted, id)
	w.list = slices.DeleteFunc(w.list, func(l debrid.Listed) bool { return l.ID == id })
	return nil
}

func (w *websiteAccount) List(context.Context) ([]debrid.Listed, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.polls++
	return slices.Clone(w.list), true, nil
}

// addOnWebsite puts a download on the account the way the website does.
func (w *websiteAccount) addOnWebsite(id, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, debrid.Listed{ID: id, Name: name, Hash: "hash-" + id, Added: time.Now()})
}

func (w *websiteAccount) pollCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.polls
}

func (w *websiteAccount) deletedJobs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.deleted)
}

// fastImports polls every few milliseconds, deletes a removed download after
// delay rather than the undo window, and tries a refused delete again after a
// few milliseconds.
func fastImports(t *testing.T, delay time.Duration) {
	t.Helper()
	poll, drop, retry := importPoll, importDropDelay, importDropRetry
	importPoll, importDropDelay, importDropRetry = 5*time.Millisecond, delay, 5*time.Millisecond
	t.Cleanup(func() { importPoll, importDropDelay, importDropRetry = poll, drop, retry })
}

// openImportApp builds an App in dir that keeps new links in the collector
// and has site wired as the account "fakedebrid".
func openImportApp(t *testing.T, dir string, site *websiteAccount, keep bool) *App {
	t.Helper()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.AutoConfirm = false
	s.Torrent.KeepOnService = keep
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.bmu.Lock()
	a.debrid["fakedebrid"] = a.torrentsVia("fakedebrid", site, &routeSpy{name: "links", events: make(chan string, 16)}, engineHandoff{a.Engine, a})
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: "fakedebrid", Prio: 90, Torrents: true})
	return a
}

// importApp is openImportApp in a fresh directory with the import switched on
// and its first look at the account taken.
func importApp(t *testing.T, site *websiteAccount, keep bool) *App {
	t.Helper()
	a := openImportApp(t, t.TempDir(), site, keep)
	t.Cleanup(func() { a.Close() })
	a.SetAccountImport("fakedebrid", "", true)
	firstLook(t, a)
	return a
}

func firstLook(t *testing.T, a *App) {
	t.Helper()
	waitFor(t, "the import's first look at the account", func() bool {
		return !a.loadImportRecord("fakedebrid").Since.IsZero()
	})
}

// morePolls waits until the import has read the account n more times.
func morePolls(t *testing.T, site *websiteAccount, n int) {
	t.Helper()
	target := site.pollCount() + n
	waitFor(t, "more reads of the account", func() bool { return site.pollCount() >= target })
}

// imported lists the tasks staged from the account's job id, as copies.
func imported(a *App, id string) []core.Task {
	link := debrid.JobLink("fakedebrid", id)
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []core.Task
	for _, t := range a.tasks {
		if t.URL == link {
			out = append(out, *t)
		}
	}
	return out
}

func TestADownloadAddedOnTheWebsiteLandsInTheCollectorOnce(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	site.addOnWebsite("OLD", "Old show")
	a := importApp(t, site, false)

	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })
	morePolls(t, site, 3)

	got := imported(a, "NEW")
	if len(got) != 1 {
		t.Fatalf("%d tasks for the download, want one", len(got))
	}
	if task := got[0]; task.Status != core.StatusCollected || task.Name != "New show" || task.Origin != OriginAccount || task.Resolver != "fakedebrid" {
		t.Errorf("staged as %q, %s, from %q on %q; want it collected under its own name, from the account, for that account",
			task.Name, task.Status, task.Origin, task.Resolver)
	}
	if old := imported(a, "OLD"); len(old) != 0 {
		t.Errorf("imported %q, which was on the account before the import was switched on", old[0].Name)
	}
}

func TestAnImportedDownloadIsNotImportedAgainAfterARestart(t *testing.T) {
	fastImports(t, time.Millisecond)
	dir := t.TempDir()
	site := &websiteAccount{}
	before := openImportApp(t, dir, site, true)
	stop := sync.OnceFunc(func() { before.Close() })
	t.Cleanup(stop)
	before.SetAccountImport("fakedebrid", "", true)
	firstLook(t, before)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(before, "NEW")) > 0 })
	// Removed, and kept on the service, so only what the import remembers
	// stands between the download and a second import.
	before.Remove(imported(before, "NEW")[0].ID, false)
	stop()
	site.addOnWebsite("WHILE-DOWN", "Next show")

	after := openImportApp(t, dir, site, true)
	t.Cleanup(func() { after.Close() })
	after.applyAccountImports()
	waitFor(t, "the download added while the instance was down in the collector", func() bool {
		return len(imported(after, "WHILE-DOWN")) > 0
	})
	morePolls(t, site, 3)
	if got := imported(after, "NEW"); len(got) != 0 {
		t.Errorf("imported the download again after the restart")
	}
}

func TestAJobThisInstanceAddedIsNeverImported(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)

	queueTorrent(a, &core.Task{ID: "m1", URL: debridMagnet, ResolverPin: "fakedebrid", Status: core.StatusQueued, Enabled: true})
	defer a.Remove("m1", false)
	waitFor(t, "the magnet added to the account", func() bool {
		site.mu.Lock()
		defer site.mu.Unlock()
		return site.added == 1
	})
	morePolls(t, site, 3)
	if got := imported(a, "OWN1"); len(got) != 0 {
		t.Errorf("imported the torrent this instance added itself")
	}
}

func TestRemovingAnImportedDownloadDeletesItOnTheService(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })

	a.Remove(imported(a, "NEW")[0].ID, false)
	waitFor(t, "the download deleted on the service", func() bool { return slices.Equal(site.deletedJobs(), []string{"NEW"}) })
}

func TestAnImportedDownloadRemovedJustBeforeARestartIsStillDeleted(t *testing.T) {
	fastImports(t, time.Second)
	dir := t.TempDir()
	site := &websiteAccount{}
	before := openImportApp(t, dir, site, false)
	stop := sync.OnceFunc(func() { before.Close() })
	t.Cleanup(stop)
	before.SetAccountImport("fakedebrid", "", true)
	firstLook(t, before)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(before, "NEW")) > 0 })
	before.Remove(imported(before, "NEW")[0].ID, false)
	stop()
	if d := site.deletedJobs(); len(d) != 0 {
		t.Fatalf("deleted %v before the restart, so this test proves nothing about it", d)
	}

	after := openImportApp(t, dir, site, false)
	t.Cleanup(func() { after.Close() })
	// The account is wired after New here, so the wait New took up may have
	// found it missing.
	after.resumeImportDrops()
	waitFor(t, "the download deleted after the restart", func() bool { return slices.Equal(site.deletedJobs(), []string{"NEW"}) })
}

// An add can time out after the service has taken the torrent, and the job it
// made is then the failed task's, not something the user added.
func TestTheJobOfAFailedAddIsNotImported(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	a.mu.Lock()
	a.tasks["m1"] = &core.Task{ID: "m1", URL: debridMagnet, InfoHash: "hash-LOST", Resolver: "fakedebrid", Status: core.StatusError, Enabled: true}
	a.mu.Unlock()

	site.addOnWebsite("LOST", "Show")
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })
	if got := imported(a, "LOST"); len(got) != 0 {
		t.Error("imported the job of a torrent whose add failed, which is already in the list")
	}
}

// A feed entry is written by whoever runs the feed. One naming a job on the
// account would fetch it, and removing its task would delete the job there,
// so it becomes no task at all: not in the collector, and not in the holding
// area, from which a restore would set it free.
func TestAFeedEntryCannotNameAJobOnTheAccount(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	site.addOnWebsite("USERS-OWN", "Own show")
	site.addOnWebsite("HELD", "Held show")
	a := openImportApp(t, t.TempDir(), site, false)
	t.Cleanup(func() { a.Close() })
	s := a.Settings.Get()
	s.LinkFilter = rules.Set{Rules: []rules.Rule{{
		Name:       "held",
		Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "HELD"}},
		Action:     rules.Action{Reject: true, Reason: "held for a look"},
	}}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	for _, link := range []string{
		debrid.JobLink("fakedebrid", "USERS-OWN"),
		debrid.JobLink("fakedebrid", "HELD"),
		usenet.FileLink("fakedebrid", "USERS-NZB", usenet.File{ID: "1", Name: "film.mkv"}),
	} {
		a.stageFeedJob(feed.Job{URL: link, Package: "Feed", Source: "https://feed.example/rss"})
	}
	tasks := a.Tasks()
	for _, task := range tasks {
		t.Errorf("the feed staged %s (held %v)", task.URL, task.Skipped)
	}
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	a.RemoveTasks(ids, false)
	time.Sleep(50 * time.Millisecond)
	if d := site.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v on the account", d)
	}
}

func TestAnImportedDownloadStaysOnTheServiceWhenTorrentsAreKept(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, true)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })

	a.Remove(imported(a, "NEW")[0].ID, false)
	time.Sleep(50 * time.Millisecond)
	if d := site.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v; Settings, Torrents keeps them on the service", d)
	}
}

func TestAnUndoneRemovalLeavesTheImportedDownloadOnTheService(t *testing.T) {
	fastImports(t, 300*time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })

	_, token := a.RemoveTasksUndoable([]string{imported(a, "NEW")[0].ID}, false)
	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("the undo brought back %v", back)
	}
	time.Sleep(600 * time.Millisecond)
	if d := site.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v; the task came back and fetches from that download", d)
	}
}

// The first removal's wait ends while the second removal can still be undone,
// and the undo the list still offers must find the download on the account.
func TestARemovalUndoneAndMadeAgainCanBeUndoneAgain(t *testing.T) {
	fastImports(t, 200*time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })
	id := imported(a, "NEW")[0].ID

	_, first := a.RemoveTasksUndoable([]string{id}, false)
	time.Sleep(50 * time.Millisecond)
	if back := a.UndoRemove(first); len(back) != 1 {
		t.Fatalf("the first undo brought back %v", back)
	}
	time.Sleep(50 * time.Millisecond)
	_, second := a.RemoveTasksUndoable([]string{id}, false)
	// Past the end of the first removal's wait.
	time.Sleep(250 * time.Millisecond)
	if back := a.UndoRemove(second); len(back) != 1 {
		t.Fatalf("the second undo brought back %v", back)
	}
	time.Sleep(300 * time.Millisecond)
	if d := site.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v; the task came back and fetches from that download", d)
	}
}

// A removal of many rows can take longer than the wait before a delete, and
// its undo window opens only once it is through. The download waits for that
// window, not for the moment its own row went.
func TestAnImportedDownloadWaitsForTheUndoOfItsRemoval(t *testing.T) {
	fastImports(t, 0)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })
	slow := &slowSpy{routeSpy: routeSpy{name: "slow", events: make(chan string, 4)}, release: make(chan struct{})}
	a.bmu.Lock()
	a.debrid["slow"] = slow
	a.bmu.Unlock()
	other := putTask(t, a, core.Task{URL: "https://host.example/other.bin", Resolver: "slow", Status: core.StatusCollected, Enabled: true})

	tokens := make(chan string, 1)
	go func() {
		_, token := a.RemoveTasksUndoable([]string{imported(a, "NEW")[0].ID, other.ID}, false)
		tokens <- token
	}()
	<-slow.events
	// The imported row is gone and its wait is over, while the removal is
	// still busy with the next row.
	time.Sleep(50 * time.Millisecond)
	close(slow.release)
	if back := a.UndoRemove(<-tokens); len(back) != 2 {
		t.Fatalf("the undo brought back %v", back)
	}
	time.Sleep(50 * time.Millisecond)
	if d := site.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v while the removal could still be undone", d)
	}
}

func TestADeleteTheServiceRefusesIsTriedAgain(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{refusals: 2}
	a := importApp(t, site, false)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })

	a.Remove(imported(a, "NEW")[0].ID, false)
	waitFor(t, "the download deleted on the service", func() bool { return slices.Equal(site.deletedJobs(), []string{"NEW"}) })
	waitFor(t, "the note of the delete dropped", func() bool {
		a.imports.fileMu.Lock()
		defer a.imports.fileMu.Unlock()
		return len(a.loadImportDropsLocked()) == 0
	})
}

func TestWhatIsAddedWhileTheImportIsOffStaysOnTheAccount(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)

	a.SetAccountImport("fakedebrid", "", false)
	site.addOnWebsite("WHILE-OFF", "Old news")
	a.SetAccountImport("fakedebrid", "", true)
	firstLook(t, a)
	site.addOnWebsite("NEW", "New show")
	waitFor(t, "the new download in the collector", func() bool { return len(imported(a, "NEW")) > 0 })

	if got := imported(a, "WHILE-OFF"); len(got) != 0 {
		t.Errorf("imported a download added while the import was off")
	}
}

func TestAnAccountRowSaysWhetherItCanImport(t *testing.T) {
	a := newAccountsTestApp(t)
	for _, s := range []struct{ service, key string }{{"realdebrid", "rd-key"}, {"offcloud", "oc-key"}} {
		if err := a.Accounts.SetCredential(s.service, "", accounts.Credential{APIKey: s.key}); err != nil {
			t.Fatal(err)
		}
	}
	a.SetAccountImport("realdebrid", "", true)

	rows := map[string]AccountState{}
	for _, r := range a.AccountStates() {
		rows[r.Service] = r
	}
	if rd := rows["realdebrid"]; !rd.CanImport || !rd.Import {
		t.Errorf("Real-Debrid reads canImport %v, import %v; want both after switching it on", rd.CanImport, rd.Import)
	}
	if oc := rows["offcloud"]; oc.CanImport || oc.Import {
		t.Errorf("Offcloud reads canImport %v, import %v; it has no list to import from", oc.CanImport, oc.Import)
	}
}

// usenetAccount is the Usenet side of a websiteAccount. An .nzb sent to it
// shows up in the account's list as TorBox lists one: without an info hash,
// first under the id of the queued upload and, once it starts, under an id
// of its own.
type usenetAccount struct {
	site *websiteAccount
	mu   sync.Mutex
	sent int
}

func (*usenetAccount) Slot() string        { return "fakedebrid" }
func (*usenetAccount) Label() string       { return "Fake debrid" }
func (*usenetAccount) SubmitsPerHour() int { return 0 }

func (u *usenetAccount) Submit(context.Context, string, []byte) (string, error) {
	u.mu.Lock()
	u.sent++
	u.mu.Unlock()
	u.site.mu.Lock()
	defer u.site.mu.Unlock()
	u.site.list = append(u.site.list, debrid.Listed{ID: "QUEUED1", Name: "Film", Added: time.Now()})
	return "QUEUED1", nil
}

func (u *usenetAccount) Status(_ context.Context, ids []string) (map[string]usenet.Status, error) {
	out := map[string]usenet.Status{}
	for _, id := range ids {
		st := usenet.Status{Phase: usenet.PhaseFetching}
		if id == "QUEUED1" {
			st.ID = "STARTED1"
			u.site.mu.Lock()
			for i, l := range u.site.list {
				if l.ID == "QUEUED1" {
					u.site.list[i].ID = "STARTED1"
				}
			}
			u.site.mu.Unlock()
		}
		out[id] = st
	}
	return out, nil
}

func (*usenetAccount) Link(context.Context, string, string) (string, error) {
	return "", errors.New("nothing is ready")
}
func (*usenetAccount) Delete(context.Context, string) error { return nil }

func (u *usenetAccount) sentCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sent
}

// An .nzb this instance sent to the account comes back in the account's list,
// under both of its ids, like a usenet download added on the website. Only the
// one added there is imported.
func TestAnNZBThisInstanceSentIsNeverImported(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)
	a.SetUsenetServices(&usenetAccount{site: site})
	job, err := a.AddNZB(NZB{Name: "Film", Data: []byte(droppedNZB), Origin: OriginPaste})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the service to start the .nzb under an id of its own", func() bool {
		j, _ := a.UsenetJob(job.ID)
		return j.Remote == "STARTED1"
	})
	site.mu.Lock()
	site.list = append(site.list, debrid.Listed{ID: "WEBSITE1", Name: "Other film", Added: time.Now()})
	site.mu.Unlock()
	waitFor(t, "the usenet download added on the website in the collector", func() bool {
		return len(imported(a, "WEBSITE1")) > 0
	})
	morePolls(t, site, 3)
	for _, id := range []string{"QUEUED1", "STARTED1"} {
		if got := imported(a, id); len(got) != 0 {
			t.Errorf("imported %s, the .nzb this instance sent to the account", id)
		}
	}
}

// A torrent Sonarr hands over through the qBittorrent door is staged and
// started the way the door does it, and the debrid service that fetches it
// holds a job this instance added.
func TestATorrentFromTheQbittorrentDoorIsNeverImported(t *testing.T) {
	fastImports(t, time.Millisecond)
	site := &websiteAccount{}
	a := importApp(t, site, false)

	created, err := a.AddLinksWithOptions([]string{debridMagnet}, "Show", OriginPaste, LinkBatchOptions{})
	if err != nil || len(created) != 1 {
		t.Fatalf("staged %d tasks: %v", len(created), err)
	}
	a.StartTasks([]string{created[0].ID})
	defer a.Remove(created[0].ID, false)
	waitFor(t, "the magnet added to the account", func() bool { return site.addCount() == 1 })
	morePolls(t, site, 3)
	if got := imported(a, "OWN1"); len(got) != 0 {
		t.Errorf("imported the torrent the qBittorrent door handed over")
	}
}
