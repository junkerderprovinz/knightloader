package app

// Module switches as dispatch and the subsystems see them. The resolvers are
// fakes named after the real ones, since resolverModule goes by id.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
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

// jdHostApp has a JD that ranks above the direct download for host, the way
// a hoster with an active login does.
func jdHostApp(t *testing.T, host string, extra ...fakeResolver) *App {
	t.Helper()
	a := newQueueApp(t)
	isolateResolvers(a, fakeResolver{id: "jd", prio: 10, host: host}, fakeResolver{id: "direct", prio: 40, host: host})
	for _, r := range extra {
		a.Registry.Register(r)
	}
	jd.SetHostActive(host, true)
	t.Cleanup(func() { jd.SetHostActive(host, false) })
	return a
}

func TestALinkForASwitchedOffJDWaitsInsteadOfGoingOutDirect(t *testing.T) {
	const host = "modules-jd-off.example"
	a := jdHostApp(t, host)
	switchModulesOff(t, a, "jd")

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/file.bin", Status: core.StatusQueued, Enabled: true})
	if got.Status != core.StatusQueued || got.Waiting != core.WaitingModule {
		t.Fatalf("status %q, waiting %q; want a queued task waiting for the module", got.Status, got.Waiting)
	}
	if got.Resolver == "direct" {
		t.Fatal("the link went to the direct download, which would save the hoster's page")
	}
}

func TestSwitchingJDBackOnReleasesTheWaitingLink(t *testing.T) {
	const host = "modules-jd-back.example"
	a := jdHostApp(t, host)
	switchModulesOff(t, a, "jd")
	dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/file.bin", Status: core.StatusQueued, Enabled: true})

	switchModulesOff(t, a)
	a.mu.Lock()
	resolver, active := a.tasks["t1"].Resolver, a.active["t1"]
	a.mu.Unlock()
	if resolver != "jd" || !active {
		t.Fatalf("after switching JD back on: resolver %q, active %v; want it started on jd", resolver, active)
	}
}

func TestADebridBelowASwitchedOffJDTakesTheLinkOver(t *testing.T) {
	const host = "modules-jd-debrid.example"
	a := jdHostApp(t, host, fakeResolver{id: "fakedebrid", prio: 45, host: host})
	switchModulesOff(t, a, "jd")

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/file.bin", Status: core.StatusQueued, Enabled: true})
	if got.Resolver != "fakedebrid" {
		t.Fatalf("resolver = %q, want the debrid service below the switched-off JD", got.Resolver)
	}
}

func TestAStartedTransferDoesNotResumeInASwitchedOffBackend(t *testing.T) {
	const host = "modules-jd-resume.example"
	a := jdHostApp(t, host)
	switchModulesOff(t, a, "jd")
	a.mu.Lock()
	a.started["t1"] = true
	a.mu.Unlock()

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/file.bin", Resolver: "jd", Status: core.StatusQueued, Enabled: true})
	a.mu.Lock()
	active := a.active["t1"]
	a.mu.Unlock()
	if active || got.Waiting != core.WaitingModule {
		t.Fatalf("active %v, waiting %q; want the paused JD transfer held until JD is back on", active, got.Waiting)
	}
}

func TestALinkYtdlpDeclinedWaitsForTheSwitchedOffJDInsteadOfGoingBackToYtdlp(t *testing.T) {
	const host = "modules-ytdlp-declined.example"
	a := newQueueApp(t)
	isolateResolvers(a,
		fakeResolver{id: "ytdlp", prio: 30, host: host},
		fakeResolver{id: "jd", prio: 10, host: host},
		fakeResolver{id: "http", prio: -100, host: host},
	)
	switchModulesOff(t, a, "jd")

	// Where a decline from yt-dlp leaves the task: recorded on the next backend.
	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/download/123", Resolver: "jd", Status: core.StatusQueued, Enabled: true})
	if got.Resolver != "jd" || got.Waiting != core.WaitingModule {
		t.Fatalf("resolver %q, waiting %q; want the task held on jd, not handed back to yt-dlp", got.Resolver, got.Waiting)
	}
}

func TestAFallbackToTheDirectDownloadWaitsWhileJDIsOff(t *testing.T) {
	const host = "modules-direct-fallback.example"
	a := jdHostApp(t, host)
	switchModulesOff(t, a, "jd")

	// Where a fallback leaves the task once no debrid account takes it:
	// recorded on direct.
	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/file.bin", Resolver: "direct", Status: core.StatusQueued, Enabled: true})
	if got.Waiting != core.WaitingModule || got.Resolver != "jd" {
		t.Fatalf("resolver %q, waiting %q; want the task held for jd instead of fetching the hoster's page", got.Resolver, got.Waiting)
	}
}

func TestAVideoLinkWaitsWhileYtdlpIsOffInsteadOfGoingToJD(t *testing.T) {
	const host = "modules-video.example"
	a := newQueueApp(t)
	isolateResolvers(a,
		fakeResolver{id: "ytdlp", prio: 30, host: host},
		fakeResolver{id: "jd", prio: 10, host: host},
	)
	switchModulesOff(t, a, "ytdlp")

	got := dispatchQueued(a, &core.Task{ID: "t1", URL: "https://" + host + "/watch?v=x", Status: core.StatusQueued, Enabled: true})
	if got.Waiting != core.WaitingModule || got.Resolver != "ytdlp" {
		t.Fatalf("resolver %q, waiting %q; want the video held for yt-dlp", got.Resolver, got.Waiting)
	}
}

func TestAMediaLinkStagedWhileYtdlpIsOffStillGetsAPackage(t *testing.T) {
	a := newQueueApp(t)
	switchModulesOff(t, a, "ytdlp")
	const url = "https://media.example/watch/some-clip"
	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: url, Name: url, Resolver: "ytdlp", Status: core.StatusCollected, Enabled: true}
	a.mu.Unlock()

	a.probeYtdlpTitle("t1", url)
	a.mu.Lock()
	pkg := a.tasks["t1"].Package
	a.mu.Unlock()
	if pkg == "" {
		t.Fatal("the link was left without a package, and no probe will come to file it")
	}
}

func TestSwitchedOffYtdlpOffersNoProber(t *testing.T) {
	a := newQueueApp(t)
	switchModulesOff(t, a, "ytdlp")
	if _, ok := a.ytdlpTitleProber(); ok {
		t.Error("ytdlpTitleProber answered while yt-dlp is switched off")
	}
	if _, ok := a.ytdlpPlaylistProber(); ok {
		t.Error("ytdlpPlaylistProber answered while yt-dlp is switched off")
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
