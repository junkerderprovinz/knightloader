package app

import (
	"bytes"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mega"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mega/megatest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// useMega points the app's MEGA client at srv.
func useMega(a *App, srv *megatest.Server) {
	b := mega.NewBackend(mega.NewClient(srv.URL, srv.Client()), srv.Client(), a.dlDir, a.onUpdate)
	b.Dir = a.taskDir
	a.bmu.Lock()
	a.mega = b
	a.bmu.Unlock()
	a.Registry.Register(mega.Resolver{Client: b.Client()})
}

func chainIDs(a *App, url string) []string {
	var ids []string
	for _, res := range hostChain(a.Registry.All(url), url, a.Settings.Get()) {
		ids = append(ids, res.Info().ID)
	}
	return ids
}

func TestMEGALinksGoToMEGAsOwnClientWithDebridAndJDBehind(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Prio: 49, Hosts: map[string]bool{"mega.nz": true}})
	const link = "https://mega.nz/file/AbCdEfGh#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{mega.ResolverID, "alldebrid", "jd"}) {
		t.Errorf("chain = %v, want MEGA's own client, then the debrid service, then JD", got)
	}
	if be := a.backendFor(mega.ResolverID); be == a.Engine {
		t.Error("MEGA tasks were routed to the engine, which cannot decrypt them")
	}
	a.mu.Lock()
	mode := a.modeForLocked(&core.Task{URL: link}, mega.ResolverID)
	a.mu.Unlock()
	if mode != core.ModeFree {
		t.Errorf("mode = %q, want free: no account is behind it", mode)
	}

	// The priority card can put the others first.
	if _, err := a.SaveResolverOrder([]string{"jd", mega.ResolverID}); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); got[0] != "jd" || got[1] != mega.ResolverID {
		t.Errorf("chain after reordering = %v, want JD first", got)
	}
	if rows := a.ResolverPriority(""); !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == mega.ResolverID }) {
		t.Errorf("the priority card has no row for MEGA: %v", rows)
	}
}

func TestAMEGALoginInJDStillComesFirst(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetHostActive("mega.nz", true)
	t.Cleanup(func() { jd.SetHostActive("mega.nz", false) })
	const link = "https://mega.nz/file/AbCdEfGh#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if got := chainIDs(a, link); got[0] != "jd" {
		t.Errorf("chain = %v, want JD with the account first", got)
	}
}

func TestAMEGAFolderStagesOneRowPerFileInAPackagePerFolder(t *testing.T) {
	srv := megatest.New(t)
	// Page crawling off: a folder link is a folder whatever that setting says.
	a := newCrawlApp(t, false)
	useMega(a, srv)
	link := srv.ShareFolder("HoLiDay1",
		megatest.Folder("root0001", "owner", "Holiday"),
		megatest.FileNode("beach001", "root0001", megatest.NewFile("beach.jpg", megatest.Random(3000))),
		megatest.FileNode("dunes001", "root0001", megatest.NewFile("dunes.jpg", megatest.Random(1000))),
		megatest.Folder("clips001", "root0001", "Clips"),
		megatest.FileNode("wave0001", "clips001", megatest.NewFile("wave.mp4", megatest.Random(20_000))),
	)

	created := a.AddLinks([]string{link}, "")
	type row struct {
		name, pkg string
		size      int64
	}
	var got []row
	a.mu.Lock()
	for _, c := range created {
		task := a.tasks[c.ID]
		got = append(got, row{task.Name, task.Package, task.Size})
		if task.Resolver != mega.ResolverID || task.Status != core.StatusCollected {
			t.Errorf("%s: resolver %q, status %q", task.Name, task.Resolver, task.Status)
		}
	}
	a.mu.Unlock()
	slices.SortFunc(got, func(x, y row) int {
		if x.name < y.name {
			return -1
		}
		return 1
	})
	want := []row{
		{"beach.jpg", "Holiday", 3000},
		{"dunes.jpg", "Holiday", 1000},
		{"wave.mp4", "Holiday - Clips", 20_000},
	}
	if !slices.Equal(got, want) {
		t.Errorf("staged %+v, want %+v", got, want)
	}
}

func TestAMEGAFileIsNamedAndDownloaded(t *testing.T) {
	srv := megatest.New(t)
	a := newCrawlApp(t, false)
	useMega(a, srv)
	f := megatest.NewFile("Isle of Dogs.mkv", megatest.Random(700_000))
	link := srv.Share("IsLe0001", f)

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d rows, want 1", len(created))
	}
	id := created[0].ID
	waitFor(t, "MEGA naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Name == f.Name && task.Size == int64(len(f.Plain)) && task.Online == core.AvailOnline
	})

	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	var file string
	waitFor(t, "the download finishing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		if task.Status == core.StatusError {
			t.Fatalf("the download failed: %s", task.Error)
		}
		file = task.File
		return task.Status == core.StatusDone
	})
	got, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(got, f.Plain) {
		t.Errorf("%s does not hold the decrypted file (%v)", file, err)
	}
}

func TestASpentMEGAQuotaIsAWaitNotAFailure(t *testing.T) {
	srv := megatest.New(t)
	a := newCrawlApp(t, false)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), MaxRetries: 3}); err != nil {
		t.Fatal(err)
	}
	useMega(a, srv)
	link := srv.Share("IsLe0001", megatest.NewFile("Isle of Dogs.mkv", megatest.Random(1000)))
	srv.SpendQuota(5400)

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	waitFor(t, "the quota showing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Reason == core.ReasonLimit && !task.NextTry.IsZero()
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.tasks[id]
	if task.ErrorCode != core.CodeLimit || task.GaveUp {
		t.Errorf("code %q, gave up %v; want a limit that is waited out", task.ErrorCode, task.GaveUp)
	}
	if wait := time.Until(task.NextTry); wait < 80*time.Minute {
		t.Errorf("the retry comes after %v, want MEGA's 90 minutes", wait)
	}
}
