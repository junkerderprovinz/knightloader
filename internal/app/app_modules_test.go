package app

// Module switches as dispatch and the subsystems see them. The resolvers are
// fakes named after the real ones, since resolverModule goes by id.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

func switchModulesOff(t *testing.T, a *App, ids ...string) {
	t.Helper()
	s := a.Settings.Get()
	s.ModulesOff = ids
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
}

// dispatchQueued queues task, runs one dispatch pass and returns the task as
// the pass left it.
func dispatchQueued(a *App, task *core.Task) core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	return *task
}

// torrentHostApp has a made-up torrent backend that takes the links at host
// and holds every transfer it starts, so a started task stays started while a
// test looks.
func torrentHostApp(t *testing.T, host string, extra ...fakeResolver) *App {
	t.Helper()
	a := newQueueApp(t)
	a.bmu.Lock()
	a.debrid["torrent"] = &stubBackend{got: make(chan string, 8)}
	a.bmu.Unlock()
	isolateResolvers(a, fakeResolver{id: "torrent", prio: 50, host: host})
	for _, r := range extra {
		a.Registry.Register(r)
	}
	return a
}

func TestALinkOnlyASwitchedOffBackendTakesWaitsForIt(t *testing.T) {
	const host = "modules-off.example"
	a := torrentHostApp(t, host)
	switchModulesOff(t, a, "torrents")

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/get?id=1", Status: core.StatusQueued, Enabled: true})
	if got.Status != core.StatusQueued || got.Waiting != core.WaitingModule || got.Resolver != "torrent" {
		t.Fatalf("status %q, waiting %q, resolver %q; want a queued task waiting for the torrent module", got.Status, got.Waiting, got.Resolver)
	}
}

func TestSwitchingABackendBackOnReleasesTheWaitingLink(t *testing.T) {
	const host = "modules-back.example"
	a := torrentHostApp(t, host)
	switchModulesOff(t, a, "torrents")
	dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/get?id=1", Status: core.StatusQueued, Enabled: true})

	switchModulesOff(t, a)
	a.mu.Lock()
	resolver, active := a.tasks["t1"].Resolver, a.active["t1"]
	a.mu.Unlock()
	if resolver != "torrent" || !active {
		t.Fatalf("after switching torrents back on: resolver %q, active %v; want it started on torrent", resolver, active)
	}
}

func TestADebridBelowASwitchedOffBackendTakesTheLinkOver(t *testing.T) {
	const host = "modules-debrid.example"
	a := torrentHostApp(t, host, fakeResolver{id: "fakedebrid", prio: 45, host: host})
	switchModulesOff(t, a, "torrents")

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/get?id=1", Status: core.StatusQueued, Enabled: true})
	if got.Resolver != "fakedebrid" {
		t.Fatalf("resolver = %q, want the debrid service below the switched-off backend", got.Resolver)
	}
}

func TestAStartedTransferDoesNotResumeInASwitchedOffBackend(t *testing.T) {
	const host = "modules-resume.example"
	a := torrentHostApp(t, host)
	switchModulesOff(t, a, "torrents")
	a.mu.Lock()
	a.started["t1"] = true
	a.mu.Unlock()

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/get?id=1", Resolver: "torrent", Status: core.StatusQueued, Enabled: true})
	a.mu.Lock()
	active := a.active["t1"]
	a.mu.Unlock()
	if active || got.Waiting != core.WaitingModule {
		t.Fatalf("active %v, waiting %q; want the paused transfer held until torrents are back on", active, got.Waiting)
	}
}

// JDownloader and yt-dlp have no switch, so their ids in ModulesOff hold none
// of their links.
func TestJDAndYtdlpTakeTheirLinksWhateverModulesOffSays(t *testing.T) {
	const host = "modules-always-on.example"
	a := newQueueApp(t)
	a.bmu.Lock()
	a.jd = &stubBackend{got: make(chan string, 8)}
	a.ytdlp = &stubBackend{got: make(chan string, 8)}
	a.bmu.Unlock()
	isolateResolvers(a,
		fakeResolver{id: "ytdlp", prio: 30, host: "video." + host},
		fakeResolver{id: "jd", prio: 10, host: host},
	)
	switchModulesOff(t, a, "jd", "ytdlp")

	for id, url := range map[string]string{
		"jd":    "https://" + host + "/file.bin",
		"ytdlp": "https://video." + host + "/watch?v=x",
	} {
		got := dispatchQueued(a, &core.Task{ID: id, URL: url, Status: core.StatusQueued, Enabled: true})
		a.mu.Lock()
		active := a.active[id]
		a.mu.Unlock()
		if got.Resolver != id || got.Waiting == core.WaitingModule || !active {
			t.Errorf("%s: resolver %q, waiting %q, active %v; want it started on %s", url, got.Resolver, got.Waiting, active, id)
		}
	}
}

func TestSwitchedOffConnectionsSendDownloadsOutDirectly(t *testing.T) {
	a := newQueueApp(t)
	s := a.Settings.Get()
	s.Connections = []proxycfg.Entry{{ID: "one", Kind: proxycfg.KindHTTP, Host: "proxy.invalid", Port: 8080, Enabled: true}}
	s.ModulesOff = []string{"connections"}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	off := a.picker == nil
	a.mu.Unlock()
	if !off {
		t.Fatal("a connection picker was built while connections are switched off")
	}

	s.ModulesOff = nil
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	on := a.picker != nil
	a.mu.Unlock()
	if !on {
		t.Fatal("no connection picker after switching connections back on")
	}
}

func TestSwitchingCaptchaOffClosesTheOpenPrompts(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusRunning, Reason: core.ReasonCaptcha}
	a.mu.Unlock()
	st := a.captchaStateFor()
	st.store.Sync([]captcha.Challenge{{ID: "c1", Host: "host.example", TaskID: "t1", Kind: captcha.KindImage}})

	switchModulesOff(t, a, "captcha")
	a.pollCaptchasOnce(st)

	if n := len(st.store.List()); n != 0 {
		t.Fatalf("%d challenges still open after switching captcha off", n)
	}
	a.mu.Lock()
	reason := a.tasks["t1"].Reason
	a.mu.Unlock()
	if reason == core.ReasonCaptcha {
		t.Error("the task still says it waits for a captcha")
	}
}
