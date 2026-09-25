package app

// Captchas: a hoster or a login asking a human something before a download can
// continue. This wires internal/captcha's Source and Store into the App: a poll
// loop, the hub events a browser needs to show a prompt, and the check
// dispatchLocked uses to hold a waiting task.
//
// Which challenge a task waits on lives in captcha.Store (indexed by task id),
// not on core.Task. The only task field touched is Reason, set to
// core.ReasonCaptcha while a challenge is pending; Status stays whatever the JD
// backend's poll says.
//
// The state lives in a package-level map keyed by *App.

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// captchaPollInterval is how often pending challenges are listed: often enough
// that a solved or expired challenge clears within seconds, coarser than the
// JD download poll because nothing here needs sub-second precision.
const captchaPollInterval = 2 * time.Second

// captchaCallTimeout bounds one answer or abort round trip to the JD sidecar,
// so a wedged sidecar cannot hang the HTTP handler.
const captchaCallTimeout = 15 * time.Second

// captchaState is one App's captcha wiring.
type captchaState struct {
	source captcha.Source
	store  *captcha.Store

	startOnce sync.Once
	// pollMu serialises poll passes, so a manual refresh and a tick do not
	// both ask JD for the same list.
	pollMu sync.Mutex

	paid paidLedger
}

var (
	captchaMu  sync.Mutex
	captchaReg = map[*App]*captchaState{}
)

// captchaStateFor returns this App's captcha wiring, building it on first use.
func (a *App) captchaStateFor() *captchaState {
	captchaMu.Lock()
	defer captchaMu.Unlock()
	st, ok := captchaReg[a]
	if !ok {
		st = &captchaState{store: captcha.NewStore()}
		st.source = captcha.NewJDSource(jdBaseEnv, a.resolveJDTask)
		st.paid.path = filepath.Join(a.DataDir, paidLedgerFile)
		captchaReg[a] = st
	}
	return st
}

// jdBaseEnv reads KL_JD on every call, so a changed variable or a JD that
// comes up later is picked up without a restart.
func jdBaseEnv() string { return os.Getenv("KL_JD") }

// ensureCaptchaPoller starts the poll loop once per App. It is called from
// dispatchLocked, which runs at start-up and on nearly every task change, and
// is cheap after the first call.
func (a *App) ensureCaptchaPoller() {
	st := a.captchaStateFor()
	st.startOnce.Do(func() { a.spawn(a.captchaPollLoop) })
}

// captchaPollLoop runs until a.ctx is done. It waits for the first tick rather
// than polling at once, which without KL_JD would only find
// ErrJDNotConfigured anyway.
func (a *App) captchaPollLoop() {
	st := a.captchaStateFor()
	tick := time.NewTicker(captchaPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.pollCaptchasOnce(st)
		}
	}
}

// pollCaptchasOnce lists, syncs and publishes the pending challenges once and
// returns the resulting snapshot.
func (a *App) pollCaptchasOnce(st *captchaState) []captcha.Challenge {
	st.pollMu.Lock()
	defer st.pollMu.Unlock()

	if a.ModuleOff("captcha") || a.ModuleOff("jd") {
		// Open prompts close and JD is not asked. JD keeps its challenges and
		// gives up on those links itself once they expire.
		for _, c := range st.store.List() {
			a.settleCaptcha(c, "switchedOff")
		}
		a.setActivityGauge(ActivityCaptcha, 0)
		return nil
	}

	list, err := st.source.List(a.ctx)
	if err != nil {
		if !errors.Is(err, captcha.ErrJDNotConfigured) {
			log.Printf("captcha: listing challenges failed (will retry): %v", err)
		}
		// A failed list is not "everything resolved"; clearing the store would
		// close every open prompt.
		return st.store.List()
	}

	added, changed, removed := st.store.Sync(list)
	for _, c := range added {
		a.Hub.Broadcast("captcha", c)
		// Fired on arrival only, or a script would be notified every two
		// seconds while the challenge waits.
		a.fireCaptchaPending(c)
		a.spawn(func() { a.trySolveCaptchaAutomatically(c) })
	}
	for _, c := range changed {
		a.Hub.Broadcast("captcha", c)
	}
	if len(added) > 0 {
		a.markCaptchaTasks(added)
	}
	for _, c := range removed {
		reason := "resolved"
		if !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt) {
			reason = "timedOut"
		}
		a.settleCaptcha(c, reason)
	}
	current := st.store.List()
	// Published only on success, so a failing poll does not rebroadcast an
	// unchanged count.
	a.setActivityGauge(ActivityCaptcha, len(current))
	return current
}

// markCaptchaTasks sets core.ReasonCaptcha on every task a new challenge names
// and publishes only the tasks that changed. A challenge without a TaskID has
// nothing to mark.
func (a *App) markCaptchaTasks(added []captcha.Challenge) {
	a.mu.Lock()
	var copies []core.Task
	for _, c := range added {
		if c.TaskID == "" {
			continue
		}
		t := a.tasks[c.TaskID]
		if t == nil || t.Reason == core.ReasonCaptcha {
			continue
		}
		t.Reason = core.ReasonCaptcha
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	if len(copies) > 0 {
		a.publishTasks(copies)
	}
}

// settleCaptcha ends one challenge: it removes it from the store (idempotent),
// clears the task's Reason if it is still core.ReasonCaptcha, and broadcasts
// how the challenge ended. It is called both right after an answer or abort
// and when the poll finds the challenge gone.
func (a *App) settleCaptcha(c captcha.Challenge, reason string) {
	a.captchaStateFor().store.Remove(c.ID)

	a.mu.Lock()
	var pub *core.Task
	if c.TaskID != "" {
		// Any other reason set meanwhile is the newer fact and is kept.
		if t := a.tasks[c.TaskID]; t != nil && t.Reason == core.ReasonCaptcha {
			t.Reason = core.ReasonUnknown
			cp := *t
			pub = &cp
		}
	}
	a.mu.Unlock()
	if pub != nil {
		a.publishTasks([]core.Task{*pub})
	}
	a.Hub.Broadcast("captchaResolved", CaptchaResolution{ID: c.ID, TaskID: c.TaskID, Host: c.Host, Reason: reason})
}

// CaptchaResolution is broadcast as "captchaResolved" when a challenge ends.
type CaptchaResolution struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId,omitempty"`
	Host   string `json:"host"`
	// Reason is "solved" (answered in time), "expired" (answered too late),
	// "aborted", "timedOut" (gone after its ExpiresAt), "switchedOff" (the
	// captcha or JD module was switched off) or "resolved" (gone for a reason
	// this session cannot tell).
	Reason string `json:"reason"`
}

// CaptchaChallenges lists the challenges this session knows about. It reads
// the cache only, so a page load never waits on JD.
func (a *App) CaptchaChallenges() []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.captchaStateFor().store.List()
}

// RefreshCaptchas polls right away instead of waiting for the next tick. A
// failed poll returns the last good snapshot.
func (a *App) RefreshCaptchas(_ context.Context) []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.pollCaptchasOnce(a.captchaStateFor())
}

// AnswerCaptcha submits text as the solution to id. stillValid is JD's own
// verdict on whether the challenge was still live, which callers trust over
// any client-side countdown.
func (a *App) AnswerCaptcha(ctx context.Context, id, text string) (stillValid bool, err error) {
	st := a.captchaStateFor()
	ch, known := st.store.Get(id)

	cctx, cancel := context.WithTimeout(ctx, captchaCallTimeout)
	defer cancel()
	stillValid, err = st.source.Answer(cctx, id, text)
	if err != nil {
		return false, err
	}
	if known {
		reason := "expired"
		if stillValid {
			reason = "solved"
		}
		a.settleCaptcha(ch, reason)
	}
	return stillValid, nil
}

// AbortCaptcha tells the source the user declined to answer id, at the given
// scope (see captcha.AbortScope).
func (a *App) AbortCaptcha(ctx context.Context, id string, scope captcha.AbortScope) error {
	st := a.captchaStateFor()
	ch, known := st.store.Get(id)

	cctx, cancel := context.WithTimeout(ctx, captchaCallTimeout)
	defer cancel()
	if err := st.source.Abort(cctx, id, scope); err != nil {
		return err
	}
	if known {
		a.settleCaptcha(ch, "aborted")
	}
	return nil
}

// captchaWaitingLocked reports whether taskID is blocked on a known captcha.
// Caller holds a.mu; the store has its own lock, so this cannot deadlock.
func (a *App) captchaWaitingLocked(taskID string) bool {
	_, ok := a.captchaStateFor().store.ByTask(taskID)
	return ok
}

// resolveJDTask maps a JD download-link id to the task it blocks, for
// captcha.NewJDSource, which cannot see app state. It repeats the JD backend's
// package name format ("KL-" + taskID), so the two must change together. Only
// active tasks routed through JD are asked, and it stops at the first match;
// it only runs while a captcha is pending.
func (a *App) resolveJDTask(jdLinkID int64) (string, bool) {
	base := strings.TrimSpace(os.Getenv("KL_JD"))
	if base == "" {
		return "", false
	}
	client := jd.NewClient(base)
	for _, taskID := range a.jdRoutedActiveTaskIDs() {
		puuid, err := client.PackageUUID("KL-" + taskID)
		if err != nil || puuid == 0 {
			continue
		}
		links, err := client.QueryDownloads(puuid)
		if err != nil {
			continue
		}
		for _, l := range links {
			if l.UUID == jdLinkID {
				return taskID, true
			}
		}
	}
	return "", false
}

// jdRoutedActiveTaskIDs returns every dispatched task whose backend is JD, in
// no particular order.
func (a *App) jdRoutedActiveTaskIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.active))
	for id := range a.active {
		if t := a.tasks[id]; t != nil && t.Resolver == "jd" {
			out = append(out, id)
		}
	}
	return out
}
