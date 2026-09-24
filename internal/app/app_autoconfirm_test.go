package app

import (
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// longCountdown runs out long after any test here is over: a day, which at ten
// milliseconds a second is more than fourteen minutes.
const longCountdown = 24 * 60 * 60

// autoConfirmApp returns an app with AutoConfirm on and a countdown of delay
// seconds, each lasting ten milliseconds. The queue is halted, so a confirmed
// link stays queued where the test can see it.
func autoConfirmApp(t *testing.T, delay int) *App {
	t.Helper()
	return autoConfirmAppIn(t, t.TempDir(), delay)
}

// autoConfirmAppIn is autoConfirmApp on the data folder dir, so a test can
// start a second app where the first one left off.
func autoConfirmAppIn(t *testing.T, dir string, delay int) *App {
	t.Helper()
	orig := autoConfirmSecond
	autoConfirmSecond = 10 * time.Millisecond
	// Registered before the app's Close, so it runs after it.
	t.Cleanup(func() { autoConfirmSecond = orig })
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		AutoConfirm: true, AutoConfirmDelay: delay,
	}); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)
	return a
}

// setAutoConfirm saves the two settings as the settings page would.
func setAutoConfirm(t *testing.T, a *App, on bool, delay int) {
	t.Helper()
	s := a.Settings.Get()
	s.AutoConfirm, s.AutoConfirmDelay = on, delay
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
}

// autoConfirmRow is the auto-confirm row as the status strip has it now.
func autoConfirmRow(a *App) Activity {
	for _, s := range a.ActivitySnapshot() {
		if s.Kind == ActivityAutoConfirm {
			return s
		}
	}
	return Activity{}
}

func collected(a *App, id string) bool {
	return liveTask(a, id).Status == core.StatusCollected
}

// waitUntilParked blocks until a countdown waits on its timer. A countdown
// reads the settings and its links on its way in, so a change made before
// that would pass without the wake the test is about.
func waitUntilParked(t *testing.T, a *App) {
	t.Helper()
	waitFor(t, "the countdown to wait", func() bool {
		autoConfirmMu.Lock()
		defer autoConfirmMu.Unlock()
		return autoConfirmWakes[a] != nil
	})
}

func TestAutoConfirmWaitsOutTheDelayThenConfirms(t *testing.T) {
	const delay = 20
	a := autoConfirmApp(t, delay)
	link := putTask(t, a, collectedTask("link", nil))

	begun := time.Now()
	a.autoConfirm([]string{link.ID})

	waitFor(t, "the countdown to confirm the link", func() bool { return !collected(a, link.ID) })
	if waited, want := time.Since(begun), delay*autoConfirmSecond; waited < want {
		t.Errorf("confirmed after %v, want no sooner than the %v delay", waited, want)
	}
}

func TestAutoConfirmWithoutADelayConfirmsBeforeReturning(t *testing.T) {
	a := autoConfirmApp(t, 0)
	link := putTask(t, a, collectedTask("link", nil))

	a.autoConfirm([]string{link.ID})

	if collected(a, link.ID) {
		t.Error("the link is still in the collector, want a zero delay to confirm it at once")
	}
	if row := autoConfirmRow(a); row.Cancellable != 0 || !row.Deadline.IsZero() {
		t.Errorf("strip shows %+v, want no countdown for a zero delay", row)
	}
}

func TestCallingOffTheCountdownLeavesTheLinksInTheCollector(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	link := putTask(t, a, collectedTask("link", nil))

	begun := time.Now()
	a.autoConfirm([]string{link.ID})

	row := autoConfirmRow(a)
	if row.Active != 1 || row.Cancellable != 1 {
		t.Fatalf("strip shows %+v, want one countdown with a stop button", row)
	}
	if due := begun.Add(longCountdown * autoConfirmSecond); row.Deadline.Before(due) {
		t.Errorf("strip counts down to %v, want no sooner than %v", row.Deadline, due)
	}

	if n := a.AbortActivity(ActivityAutoConfirm); n != 1 {
		t.Fatalf("AbortActivity called off %d runs, want the one countdown", n)
	}
	waitFor(t, "the countdown to leave the strip", func() bool { return autoConfirmRow(a).Active == 0 })
	if !collected(a, link.ID) {
		t.Errorf("status = %q after the countdown was called off, want the link still collected", liveTask(a, link.ID).Status)
	}
	if row := autoConfirmRow(a); !row.Deadline.IsZero() {
		t.Errorf("strip still counts down to %v with nothing pending", row.Deadline)
	}
}

// Only what is still collected when the countdown runs out is confirmed. A link
// confirmed by hand in the meantime must not go through a second confirm, and a
// removed one must stay gone.
func TestTheCountdownConfirmsOnlyWhatIsStillCollected(t *testing.T) {
	// A second, so the hand-made changes below land well before it runs out.
	a := autoConfirmApp(t, 100)
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	kept := putTask(t, a, collectedTask("kept", nil))
	byHand := putTask(t, a, collectedTask("byhand", nil))
	removed := putTask(t, a, collectedTask("removed", nil))

	a.autoConfirm([]string{kept.ID, byHand.ID, removed.ID})
	// StartTasks rather than the route's StartTasksByHand, which would lift the
	// halt and let the queue settle the link as a failure.
	a.StartTasks([]string{byHand.ID})
	a.RemoveTasks([]string{removed.ID}, false)

	waitFor(t, "the countdown to confirm the link left over", func() bool {
		return !collected(a, kept.ID) && autoConfirmRow(a).Active == 0
	})
	if liveTask(a, removed.ID).ID != "" {
		t.Error("the removed link is back in the list")
	}
	a.Hub.Broadcast("test-sentinel", nil)
	waitForType(t, fc, "test-sentinel")
	for _, m := range activityMessages(t, fc, ActivityAutoConfirm, 1) {
		if m.Total > 1 {
			t.Fatalf("auto-confirm counted %d links at once (%+v), want only the one still collected", m.Total, m)
		}
	}
}

// A link the filter held is not part of what the countdown confirms, even when
// somebody lets it through while the countdown runs.
func TestALinkRestoredDuringTheCountdownIsNotConfirmedWithIt(t *testing.T) {
	a := autoConfirmApp(t, 20)
	held := putTask(t, a, collectedTask("held", func(c *core.Task) { c.Skipped = true }))
	kept := putTask(t, a, collectedTask("kept", nil))

	a.autoConfirm([]string{held.ID, kept.ID})
	a.RestoreFiltered([]string{held.ID})

	waitFor(t, "the countdown to confirm the batch", func() bool { return !collected(a, kept.ID) })
	if !collected(a, held.ID) {
		t.Errorf("status = %q, want the restored link left in the collector", liveTask(a, held.ID).Status)
	}
}

// A countdown whose links were all dealt with by hand goes away, rather than
// running out over nothing while the strip still shows it.
func TestTheCountdownEndsOnceItsLinksAreDealtWith(t *testing.T) {
	for name, deal := range map[string]func(a *App, id string){
		"confirmed": func(a *App, id string) { a.StartTasks([]string{id}) },
		"removed":   func(a *App, id string) { a.RemoveTasks([]string{id}, false) },
	} {
		t.Run(name, func(t *testing.T) {
			a := autoConfirmApp(t, longCountdown)
			link := putTask(t, a, collectedTask("link", nil))
			a.autoConfirm([]string{link.ID})
			if autoConfirmRow(a).Cancellable != 1 {
				t.Fatal("no countdown started")
			}
			waitUntilParked(t, a)

			deal(a, link.ID)

			waitFor(t, "the countdown to leave the strip", func() bool { return autoConfirmRow(a).Active == 0 })
		})
	}
}

// A row outside the collector is nothing a countdown waits for, so clearing
// finished downloads must not send every countdown back over the whole list.
func TestRemovingRowsOutsideTheCollectorLeavesTheCountdownAsleep(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	link := putTask(t, a, collectedTask("link", nil))
	one := putTask(t, a, collectedTask("one", func(c *core.Task) { c.Status = core.StatusDone }))
	putTask(t, a, collectedTask("two", func(c *core.Task) { c.Status = core.StatusDone }))
	a.autoConfirm([]string{link.ID})
	waitUntilParked(t, a)
	wake := a.autoConfirmWake()

	a.Remove(one.ID, false)
	if _, err := a.Cleanup(CleanupFinished, false); err != nil {
		t.Fatal(err)
	}

	select {
	case <-wake:
		t.Error("removing finished rows woke the countdown")
	default:
	}
}

func TestSwitchingAutoConfirmOffCallsOffTheCountdown(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	link := putTask(t, a, collectedTask("link", nil))
	a.autoConfirm([]string{link.ID})
	waitUntilParked(t, a)

	setAutoConfirm(t, a, false, longCountdown)

	waitFor(t, "the countdown to leave the strip", func() bool { return autoConfirmRow(a).Active == 0 })
	if !collected(a, link.ID) {
		t.Errorf("status = %q, want the link still collected once auto-confirm is off", liveTask(a, link.ID).Status)
	}
}

func TestCuttingTheDelayToZeroConfirmsAPendingBatchAtOnce(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	link := putTask(t, a, collectedTask("link", nil))
	a.autoConfirm([]string{link.ID})
	waitUntilParked(t, a)

	setAutoConfirm(t, a, true, 0)

	waitFor(t, "the pending link to be confirmed", func() bool { return !collected(a, link.ID) })
}

// One save can cut the delay to zero and untick a kind in a host's preset. The
// countdown it ends must not start the rows that preset sets aside, however
// long the rest of the save takes.
func TestASaveThatEndsTheCountdownLeavesOutTheRowsItsPresetSetsAside(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	family := putHostFamily(t, a, "https://youtube.com/watch?v=countdown01")
	a.autoConfirm([]string{family[ytdlp.VariantVideo]})
	waitUntilParked(t, a)

	s := a.Settings.Get()
	s.AutoConfirmDelay = 0
	s.YtdlpPresets = map[string]ytdlp.HosterPreset{presetHost: presetWithout(ytdlp.VariantThumbnail)}
	// Holding the drop folders stands in for a slow reconcile halfway through
	// the save, with the new delay stored and the presets not yet applied.
	a.wmu.Lock()
	saved := make(chan struct{})
	go func() {
		defer close(saved)
		if _, err := a.ApplySettings(s); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(20 * autoConfirmSecond)
	a.wmu.Unlock()
	<-saved

	waitFor(t, "the countdown to confirm the batch", func() bool { return !collected(a, family[ytdlp.VariantVideo]) })
	if x := liveTask(a, family[ytdlp.VariantThumbnail]); x.ID != "" && x.Status != core.StatusCollected {
		t.Errorf("the thumbnail row the saved preset leaves out has status %q, want it kept out of the queue", x.Status)
	}
}

// Each batch counts down on its own, so one arriving halfway through another's
// countdown still gets the whole delay.
func TestASecondBatchGetsACountdownOfItsOwn(t *testing.T) {
	const delay = 30
	a := autoConfirmApp(t, delay)
	first := putTask(t, a, collectedTask("first", nil))
	second := putTask(t, a, collectedTask("second", nil))

	a.autoConfirm([]string{first.ID})
	time.Sleep(delay * autoConfirmSecond / 2)
	begun := time.Now()
	a.autoConfirm([]string{second.ID})

	waitFor(t, "both batches to be confirmed", func() bool {
		return !collected(a, first.ID) && !collected(a, second.ID)
	})
	if waited, want := time.Since(begun), delay*autoConfirmSecond; waited < want {
		t.Errorf("the second batch was confirmed after %v, want its own full %v", waited, want)
	}
}

func TestStoppingAutoConfirmCallsOffEveryPendingBatch(t *testing.T) {
	a := autoConfirmApp(t, longCountdown)
	first := putTask(t, a, collectedTask("first", nil))
	second := putTask(t, a, collectedTask("second", nil))
	a.autoConfirm([]string{first.ID})
	firstDue := autoConfirmRow(a).Deadline
	a.autoConfirm([]string{second.ID})

	row := autoConfirmRow(a)
	if row.Cancellable != 2 {
		t.Fatalf("strip shows %+v, want two countdowns", row)
	}
	if !row.Deadline.Equal(firstDue) {
		t.Errorf("strip counts down to %v, want the sooner deadline %v", row.Deadline, firstDue)
	}
	if n := a.AbortActivity(ActivityAutoConfirm); n != 2 {
		t.Fatalf("AbortActivity called off %d runs, want both countdowns", n)
	}
	waitFor(t, "both countdowns to leave the strip", func() bool { return autoConfirmRow(a).Active == 0 })
	if !collected(a, first.ID) || !collected(a, second.ID) {
		t.Error("a called-off batch left the collector")
	}
}

func TestCloseDoesNotWaitOutAPendingCountdown(t *testing.T) {
	// A minute rather than longCountdown, so a Close that does wait holds the
	// cleanup up for a minute and not a quarter of an hour.
	a := autoConfirmApp(t, 6000)
	link := putTask(t, a, collectedTask("link", nil))
	a.autoConfirm([]string{link.ID})

	done := make(chan struct{})
	go func() {
		a.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close waited on the countdown; its context is not derived from the app's")
	}
	if !collected(a, link.ID) {
		t.Error("closing the app confirmed the pending link")
	}
}

// A countdown a shutdown cut short runs on after the restart, and one that
// fell due while the app was down confirms its batch then.
func TestACountdownCutShortByAShutdownConfirmsAfterTheRestart(t *testing.T) {
	const delay = 20
	dir := t.TempDir()
	first := autoConfirmAppIn(t, dir, delay)
	link := putTask(t, first, collectedTask("link", nil))
	first.autoConfirm([]string{link.ID})
	waitUntilParked(t, first)
	first.Close()
	time.Sleep(delay * autoConfirmSecond)

	again := autoConfirmAppIn(t, dir, delay)

	waitFor(t, "the countdown to confirm the link after the restart", func() bool { return !collected(again, link.ID) })
	got := liveTask(again, link.ID)
	if got.ID == "" {
		t.Fatal("the link did not survive the restart")
	}
	if !got.ConfirmDue.IsZero() {
		t.Errorf("the confirmed link still has a countdown due at %v", got.ConfirmDue)
	}
}

// A restart before the countdown runs out picks it up at the deadline it had,
// rather than starting the delay over.
func TestARestartKeepsTheCountdownsDeadline(t *testing.T) {
	dir := t.TempDir()
	first := autoConfirmAppIn(t, dir, longCountdown)
	link := putTask(t, first, collectedTask("link", nil))
	first.autoConfirm([]string{link.ID})
	due := autoConfirmRow(first).Deadline
	waitUntilParked(t, first)
	first.Close()

	again := autoConfirmAppIn(t, dir, longCountdown)

	row := autoConfirmRow(again)
	if row.Cancellable != 1 {
		t.Fatalf("strip shows %+v after the restart, want the countdown back", row)
	}
	if off := row.Deadline.Sub(due); off < -time.Millisecond || off > time.Millisecond {
		t.Errorf("the countdown runs out at %v after the restart, want the %v it had", row.Deadline, due)
	}
	if !collected(again, link.ID) {
		t.Errorf("status = %q, want the link waiting out its countdown", liveTask(again, link.ID).Status)
	}
}

// A row set aside while its countdown was pending is one that countdown passes
// over. After a restart it must not keep a due time, which would give it a
// countdown of its own once its kind is shown again.
func TestARowSetAsideDuringACountdownLosesItsDueTimeOnRestart(t *testing.T) {
	dir := t.TempDir()
	first := autoConfirmAppIn(t, dir, longCountdown)
	family := putHostFamily(t, first, "https://youtube.com/watch?v=aside000001")
	first.autoConfirm([]string{family[ytdlp.VariantVideo]})
	waitUntilParked(t, first)
	setPreset(t, first, presetWithout(ytdlp.VariantThumbnail))
	first.Close()

	again := autoConfirmAppIn(t, dir, longCountdown)

	if due := liveTask(again, family[ytdlp.VariantThumbnail]).ConfirmDue; !due.IsZero() {
		t.Errorf("the set-aside thumbnail row is still due at %v after the restart, want no countdown", due)
	}
}

// The stop button leaves the links in the collector, and a restart must not
// start the countdown it called off again.
func TestACountdownCalledOffStaysOffAfterARestart(t *testing.T) {
	dir := t.TempDir()
	first := autoConfirmAppIn(t, dir, longCountdown)
	link := putTask(t, first, collectedTask("link", nil))
	first.autoConfirm([]string{link.ID})
	first.AbortActivity(ActivityAutoConfirm)
	waitFor(t, "the countdown to leave the strip", func() bool { return autoConfirmRow(first).Active == 0 })
	first.Close()

	again := autoConfirmAppIn(t, dir, longCountdown)

	if row := autoConfirmRow(again); row.Active != 0 {
		t.Errorf("strip shows %+v after the restart, want the called-off countdown to stay off", row)
	}
	if got := liveTask(again, link.ID); got.ID == "" || got.Status != core.StatusCollected {
		t.Errorf("status = %q after the restart, want the link still in the collector", got.Status)
	}
}

// Every entrance hands its batch to the countdown once, after its own options,
// so none of them confirms early and none counts down twice.
func TestEveryEntranceWaitsForTheCountdown(t *testing.T) {
	files := []metainfo.FileInfo{{Length: 900, Path: []string{"one.mkv"}}}
	for name, add := range map[string]func(t *testing.T, a *App){
		"paste": func(t *testing.T, a *App) {
			a.AddLinks([]string{"https://host.example/paste.bin"}, "")
		},
		"add-links form": func(t *testing.T, a *App) {
			if _, err := a.AddLinksWithOptions([]string{"https://host.example/form.bin"}, "", OriginPaste,
				LinkBatchOptions{Dir: t.TempDir(), Password: "secret"}); err != nil {
				t.Fatal(err)
			}
		},
		"click'n'load": func(t *testing.T, a *App) {
			a.AddLinksCnL([]string{"https://host.example/cnl.bin"}, "CnL", []string{"secret"})
		},
		"feed": func(t *testing.T, a *App) {
			a.stageFeedJob(feed.Job{URL: "https://host.example/episode.bin", Package: "Feed", Dir: t.TempDir()})
		},
		"watch folder": func(t *testing.T, a *App) {
			a.stageWatchJob(watch.Job{URLs: []string{"https://host.example/dropped.bin"}, AutoStart: true})
		},
		"container": func(t *testing.T, a *App) {
			a.AddResolvedLinksFrom([]resolver.Result{{DirectURL: "https://host.example/boxed.bin", Name: "boxed.bin", Size: 10}},
				"Container", OriginContainer)
		},
		"torrent": func(t *testing.T, a *App) {
			if _, err := a.AddTorrent(testTorrentURI(t, "Pack", files),
				[]core.TorrentFile{{Path: "one.mkv", Size: 900, Selected: true}}, "Pack", OriginPaste); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := autoConfirmApp(t, longCountdown)
			add(t, a)

			tasks := a.Tasks()
			if len(tasks) == 0 {
				t.Fatal("nothing was staged")
			}
			for _, task := range tasks {
				if task.Status != core.StatusCollected {
					t.Errorf("%s has status %q, want it waiting in the collector", task.URL, task.Status)
				}
			}
			if row := autoConfirmRow(a); row.Cancellable != 1 {
				t.Errorf("strip shows %+v, want exactly one countdown for the batch", row)
			}
		})
	}
}

// enabled=false in a dropped file keeps the batch out of auto-confirm
// altogether, delay or not.
func TestAParkedDroppedJobIsNotAutoConfirmed(t *testing.T) {
	a := autoConfirmApp(t, 0)
	a.stageWatchJob(watch.Job{URLs: []string{"https://host.example/parked.bin"}, Disabled: true})

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	if tasks[0].Status != core.StatusCollected || tasks[0].Enabled {
		t.Errorf("status %q, enabled %v; want the link parked in the collector", tasks[0].Status, tasks[0].Enabled)
	}
}
