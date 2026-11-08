package app

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onefichier"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onefichier/fichiertest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// useFichier points the app's 1fichier client at srv.
func useFichier(a *App, srv *fichiertest.Server) {
	b := onefichier.NewBackend(onefichier.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Password = a.downloadPassword
	a.bmu.Lock()
	a.fichier = b
	a.bmu.Unlock()
	a.Registry.Register(onefichier.Resolver{Client: b.Client()})
}

func TestA1fichierLinkGoesToTheOwnClientBelowDebridAndAboveJDsFreeMode(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	// The smaller multihosters share the lowest debrid priority.
	a.Registry.Register(debrid.Resolver{ServiceID: "zevera", Prio: 43, Hosts: map[string]bool{"1fichier.com": true}})
	jd.SetKnownHosts([]string{"1fichier.com"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	const link = "https://1fichier.com/?k3m9x2p7q1"

	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{"zevera", onefichier.ResolverID, "jd"}) {
		t.Errorf("chain = %v, want the debrid service, then the 1fichier client, then JD", got)
	}
	if be := a.backendFor(onefichier.ResolverID); be == a.Engine {
		t.Error("1fichier tasks were routed to the engine, which would save the file page")
	}
	a.mu.Lock()
	mode := a.modeForLocked(&core.Task{URL: link}, onefichier.ResolverID)
	a.mu.Unlock()
	if mode != core.ModeFree {
		t.Errorf("mode = %q, want free", mode)
	}
	if rows := a.ResolverPriority(""); !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == onefichier.ResolverID }) {
		t.Errorf("the priority card has no row for 1fichier: %v", rows)
	}

	jd.SetHostActive("1fichier.com", true)
	t.Cleanup(func() { jd.SetHostActive("1fichier.com", false) })
	if got := chainIDs(a, link); got[0] != "jd" {
		t.Errorf("chain = %v, want JD with the 1fichier login first", got)
	}
}

func TestA1fichierFileIsNamedInTheCollectorAndDownloaded(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	srv := fichiertest.New(t)
	a := newCrawlApp(t, false)
	useFichier(a, srv)
	body := bytes.Repeat([]byte("isle of dogs "), 8192)
	body = body[:len(body)/1000*1000]
	srv.Add("k3m9x2p7q1", fichiertest.File{Name: "Isle of Dogs.mkv", Body: body, Password: "hunter2"})

	created := a.AddLinks([]string{"https://k3m9x2p7q1.1fichier.com/"}, "")
	if len(created) != 1 || created[0].Resolver != onefichier.ResolverID {
		t.Fatalf("staged %+v, want one row for the 1fichier client", created)
	}
	id := created[0].ID
	waitFor(t, "the page naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Name == "Isle of Dogs.mkv" && task.Size == int64(len(body)) && task.Online == core.AvailOnline
	})
	a.mu.Lock()
	a.tasks[id].DownloadPassword = "hunter2"
	a.mu.Unlock()

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
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("%s does not hold the file (%v)", file, err)
	}
	if posts := srv.Posts(); len(posts) == 0 || posts[0].Get("pass") != "hunter2" {
		t.Errorf("the form went in as %v, want the task's download password", posts)
	}
}

func TestA1fichierCoolDownKeepsTheTaskRunningAndIsNoStall(t *testing.T) {
	srv := fichiertest.New(t)
	a := newCrawlApp(t, false)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), StallTimeout: settings.MinStallTimeout}); err != nil {
		t.Fatal(err)
	}
	useFichier(a, srv)
	link := srv.Add("k3m9x2p7q1", fichiertest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 4096)})
	srv.CoolDown(95)

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	t.Cleanup(func() { a.RemoveTasks([]string{id}, true) })
	waitFor(t, "the wait showing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Status == core.StatusRunning && strings.Contains(task.Note, "1 h 35 min")
	})

	// Two passes an hour apart: the first takes the reading, the second would
	// mark a transfer that moved nothing since.
	a.stallPass(time.Now())
	a.stallPass(time.Now().Add(time.Hour))

	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.tasks[id]
	if !task.StalledSince.IsZero() {
		t.Error("a task sitting out 1fichier's wait was marked as stalled")
	}
	if task.Status != core.StatusRunning || task.Error != "" {
		t.Errorf("status %q, error %q; want it running without a failure", task.Status, task.Error)
	}
}
