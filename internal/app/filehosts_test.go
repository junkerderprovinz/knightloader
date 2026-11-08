package app

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile/gftest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire/mftest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain/pdtest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeHosters are the stand-ins the app's clients for Pixeldrain, Gofile and
// Mediafire are pointed at.
type fakeHosters struct {
	pd *pdtest.Server
	gf *gftest.Server
	mf *mftest.Server
}

func useFakeHosters(t *testing.T, a *App) fakeHosters {
	f := fakeHosters{pd: pdtest.New(t), gf: gftest.New(t), mf: mftest.New(t)}
	eng := engineHandoff{a.Engine, a}
	pd := pixeldrain.NewClient(f.pd.URL, f.pd.Client())
	pd.Key = func() string { return a.hosterSecret("pixeldrain.com") }
	gf := gofile.NewClient(f.gf.API, f.gf.Site, f.gf.Client())
	mf := mediafire.NewClient(f.mf.URL, f.mf.Client())
	a.bmu.Lock()
	a.hosters = map[string]ownHoster{
		pixeldrain.ResolverID: {pixeldrain.Resolver{Client: pd}, a.hosterBackend("Pixeldrain", pd, eng)},
		gofile.ResolverID:     {gofile.Resolver{Client: gf}, a.hosterBackend("Gofile", gf, eng)},
		mediafire.ResolverID:  {mediafire.Resolver{Client: mf}, a.hosterBackend("Mediafire", mf, eng)},
	}
	hosters := a.hosters
	a.bmu.Unlock()
	for _, h := range hosters {
		a.Registry.Register(h.res)
	}
	return f
}

func TestTheOwnHosterClientsGoBelowDebridAndAJDLoginAndAboveJDsFreeMode(t *testing.T) {
	for host, c := range map[string]struct{ id, link string }{
		"pixeldrain.com": {pixeldrain.ResolverID, "https://pixeldrain.com/u/iniGVSnw"},
		"gofile.io":      {gofile.ResolverID, "https://gofile.io/d/AMZyDw"},
		"mediafire.com":  {mediafire.ResolverID, "https://www.mediafire.com/file/wt7dc5e9jgnym04/Poses.tar.gz/file"},
	} {
		t.Run(host, func(t *testing.T) {
			a := newCrawlApp(t, false)
			useFakeHosters(t, a)
			a.Registry.Register(jd.Resolver{})
			// The smaller multihosters share the lowest debrid priority.
			a.Registry.Register(debrid.Resolver{ServiceID: "zevera", Prio: 43, Hosts: map[string]bool{host: true}})
			jd.SetKnownHosts([]string{host})
			t.Cleanup(func() { jd.SetKnownHosts(nil) })

			if got := chainIDs(a, c.link); !slices.Equal(got[:3], []string{"zevera", c.id, "jd"}) {
				t.Errorf("chain = %v, want the debrid service, then our own client, then JD", got)
			}
			if _, ok := a.backendFor(c.id).(*hoster.Backend); !ok {
				t.Errorf("%s tasks go to %T, which would save the hoster's page", c.id, a.backendFor(c.id))
			}
			if !a.HonoursCollisionPolicy(c.id) {
				t.Error("the collision policy is said not to reach the file the engine writes")
			}
			a.mu.Lock()
			mode := a.modeForLocked(&core.Task{URL: c.link}, c.id)
			a.mu.Unlock()
			if mode != core.ModeFree {
				t.Errorf("mode = %q, want free", mode)
			}
			if rows := a.ResolverPriority(""); !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == c.id }) {
				t.Errorf("the priority card has no row for %s: %v", c.id, rows)
			}

			jd.SetHostActive(host, true)
			t.Cleanup(func() { jd.SetHostActive(host, false) })
			if got := chainIDs(a, c.link); got[0] != "jd" {
				t.Errorf("chain = %v, want JD with the login first", got)
			}
		})
	}
}

// staged waits for the collector to hold n rows and returns them.
func staged(t *testing.T, a *App, created []*core.Task, n int) []*core.Task {
	t.Helper()
	if len(created) != n {
		names := make([]string, 0, len(created))
		for _, c := range created {
			names = append(names, c.URL)
		}
		t.Fatalf("staged %v, want %d rows", names, n)
	}
	return created
}

// downloaded confirms the task and returns the file it ends up in.
func downloaded(t *testing.T, a *App, id string) []byte {
	t.Helper()
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
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAPixeldrainListBecomesOneRowPerFileAndAFileDownloads(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("rating list "), 4096)
	f.pd.AddFile("iniGVSnw", pdtest.File{Name: "2001-2019.zip", Body: body})
	f.pd.AddFile("BvnwvXuV", pdtest.File{Name: "2020-2023.zip", Body: []byte("PK")})
	f.pd.AddList("CcFZTyZ4", pdtest.List{Title: "FIDE rating-list archive", Files: []string{"iniGVSnw", "BvnwvXuV"}})

	rows := staged(t, a, a.AddLinks([]string{"https://pixeldrain.com/l/CcFZTyZ4"}, ""), 2)
	if rows[0].Resolver != pixeldrain.ResolverID || rows[0].Name != "2001-2019.zip" || rows[0].Size != int64(len(body)) {
		t.Errorf("first row %+v, want the list's first file on our own client", rows[0])
	}
	if got := downloaded(t, a, rows[0].ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Pixeldrain served")
	}
}

func TestAProtectedGofileFolderOpensWithTheBatchsDownloadPassword(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("isle of dogs "), 4096)
	f.gf.Add("f1", "", gftest.Item{Name: "Isle of Dogs.mkv", Body: body})
	f.gf.Add("f2", "", gftest.Item{Name: "Isle of Dogs.srt", Body: []byte("1")})
	f.gf.Add("sub", "", gftest.Item{Name: "Subs", Children: []string{"f2"}})
	link := f.gf.Add("root", "AMZyDw", gftest.Item{Name: "Release", Children: []string{"f1", "sub"}, Password: "hunter2"})

	created, err := a.AddLinksWithOptions([]string{link}, "", OriginPaste, LinkBatchOptions{DownloadPassword: "hunter2", KeepCollected: true})
	if err != nil {
		t.Fatal(err)
	}
	rows := staged(t, a, created, 2)
	byName := map[string]*core.Task{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	mkv := byName["Isle of Dogs.mkv"]
	if mkv == nil || byName["Isle of Dogs.srt"] == nil || mkv.Resolver != gofile.ResolverID {
		t.Fatalf("staged %+v, want the film and its subtitles on our own client", rows)
	}
	if got := downloaded(t, a, mkv.ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Gofile served")
	}
}

func TestAMediafireLimitKeepsTheTaskRunningWithTheWaitAndIsNoStall(t *testing.T) {
	a := newCrawlApp(t, false)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), StallTimeout: settings.MinStallTimeout}); err != nil {
		t.Fatal(err)
	}
	f := useFakeHosters(t, a)
	link := f.mf.AddFile("wt7dc5e9jgnym04", mftest.File{Name: "Poses.tar.gz", Body: make([]byte, 4096), Page: mftest.PageLimit})

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	waitFor(t, "the collector naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.tasks[id].Name == "Poses.tar.gz" && a.tasks[id].Online == core.AvailOnline
	})
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	t.Cleanup(func() { a.RemoveTasks([]string{id}, true) })
	waitFor(t, "the wait showing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Status == core.StatusRunning && strings.Contains(task.Note, "Mediafire limits free downloads") && strings.Contains(task.Note, "13 min")
	})

	a.stallPass(time.Now())
	a.stallPass(time.Now().Add(time.Hour))

	a.mu.Lock()
	defer a.mu.Unlock()
	if task := a.tasks[id]; !task.StalledSince.IsZero() || task.Status != core.StatusRunning {
		t.Errorf("status %q, stalled since %v; want it running and not stalled", task.Status, task.StalledSince)
	}
}

func TestAStoredLoginLendsItsKeyUnderAnyOfTheHostersDomainsWhileSwitchedOn(t *testing.T) {
	a := newCrawlApp(t, false)
	if err := hosterauth.NewStore(a.Accounts).Set("pixeldrain.net", accounts.Credential{Username: "reader", Password: pdtest.Key}); err != nil {
		t.Fatal(err)
	}
	if got := a.hosterSecret("pixeldrain.com"); got != pdtest.Key {
		t.Errorf("hosterSecret = %q, want the stored key", got)
	}
	a.SetAccountEnabled(hosterauth.Service, "pixeldrain.net", false)
	if got := a.hosterSecret("pixeldrain.com"); got != "" {
		t.Errorf("a switched-off login lent %q", got)
	}
	if got := a.hosterSecret("gofile.io"); got != "" {
		t.Errorf("Gofile got Pixeldrain's key %q", got)
	}
}
