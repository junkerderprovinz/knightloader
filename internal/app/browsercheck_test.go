package app

import (
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filespace"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs/xfstest"
)

func TestABrowserCheckMovesTheDownloadToJDownloaderNamingTheHoster(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"filespace.com"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	task := &core.Task{ID: "1", URL: "https://filespace.com/k3m9x2p7q1ab", Resolver: filespace.Site.ID, Status: core.StatusRunning, Enabled: true}
	useXFS(a, filespace.Site, "https://filespace.com", http.DefaultClient)
	a.mu.Lock()
	// Held, so the task moved on starts nowhere: no JD is set up here.
	a.halted = true
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "FileSpace asks for a browser check KnightLoader cannot pass",
		Unsupported: true, BrowserCheck: "FileSpace"})
	a.mu.Lock()
	resolverID, check, takeover, failed := task.Resolver, task.BrowserCheck, task.Takeover, task.Status == core.StatusError
	a.mu.Unlock()
	if resolverID != "jd" || check != "FileSpace" || takeover != "" || failed {
		t.Errorf("task on %q naming the browser check of %q, takeover %q, failed %v; want it with JD, naming FileSpace",
			resolverID, check, takeover, failed)
	}

	a.mu.Lock()
	a.active[task.ID] = true
	a.mu.Unlock()
	a.onUpdate(task.ID, core.Update{Status: core.StatusDone})
	a.mu.Lock()
	check = task.BrowserCheck
	a.mu.Unlock()
	if check != "" {
		t.Errorf("a finished download still names the browser check of %q", check)
	}
}

func TestABrowserCheckNobodyElseCanTakeFailsTheDownloadSayingSo(t *testing.T) {
	srv := xfstest.New(t, filespace.Site.Host, nil)
	srv.Challenge(http.StatusForbidden)
	a := newCrawlApp(t, false)
	useXFS(a, filespace.Site, srv.URL, httpx.New(httpx.Options{RefuseBrowserChecks: true}))
	link := srv.Add("k3m9x2p7q1ab", xfstest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 1024)})

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 || created[0].Resolver != filespace.Site.ID {
		t.Fatalf("staged %+v, want one row for the FileSpace client", created)
	}
	id := created[0].ID
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	var got core.Task
	waitFor(t, "the download failing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		got = *a.tasks[id]
		return got.Status == core.StatusError
	})
	if got.Error != "FileSpace asks for a browser check KnightLoader cannot pass" || got.BrowserCheck != "FileSpace" {
		t.Errorf("failed with %q naming %q, want the browser check said", got.Error, got.BrowserCheck)
	}
}
