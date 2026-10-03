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
	tests  *captcha.TestSource
	store  *captcha.Store

	startOnce sync.Once
	// pollMu serialises poll passes, so a manual refresh and a tick do not
	// both ask JD for the same list.
	pollMu sync.Mutex

	paid paidLedger

	// unanswerable holds the pending challenges a window or the phone app could
	// not load, and which of the two said so, see ReportCaptchaUnanswerable.
	// settleCaptcha drops a challenge from it under unanswerableMu once it has
	// left the store.
	unanswerableMu sync.Mutex
	unanswerable   map[string]map[CaptchaViewer]bool
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
		st = &captchaState{store: captcha.NewStore(), tests: captcha.NewTestSource()}
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
			if captcha.IsTest(c.ID) {
				st.tests.Abort(c.ID)
			}
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
		// A failed list is not "everything resolved": the JD challenges stay
		// as they were, or every open prompt would close. The test captchas
		// do not depend on JD and still come and go.
		list = list[:0]
		for _, c := range st.store.List() {
			if !captcha.IsTest(c.ID) {
				list = append(list, c)
			}
		}
	}

	added, changed, removed := st.store.Sync(append(list, st.tests.List()...))
	if err != nil && len(added) == 0 && len(removed) == 0 {
		return st.store.List()
	}
	for _, c := range added {
		a.Hub.Broadcast("captcha", c)
		// Fired on arrival only, or a script would be notified every two
		// seconds while the challenge waits.
		a.fireCaptchaPending(c)
		if !captcha.IsTest(c.ID) || st.tests.ForSolvers(c.ID) {
			a.spawn(func() { a.trySolveCaptchaAutomatically(c) })
		}
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
	// A failing poll returned above unless a test captcha came or went, so it
	// does not rebroadcast an unchanged count.
	a.setActivityGauge(ActivityCaptcha, len(current))
	return current
}

// markCaptchaTasks sets core.ReasonCaptcha on every task a new challenge names
// and publishes only the tasks that changed. A challenge without a TaskID has
// nothing to mark.
func (a *App) markCaptchaTasks(added []captcha.Challenge) {
	a.mu.Lock()
	var copies []taskCopy
	for _, c := range added {
		if c.TaskID == "" {
			continue
		}
		t := a.tasks[c.TaskID]
		if t == nil || t.Reason == core.ReasonCaptcha {
			continue
		}
		t.Reason = core.ReasonCaptcha
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	if len(copies) > 0 {
		a.publishTasks(copies)
	}
}

// settleCaptcha ends one challenge for reason, see endCaptcha.
func (a *App) settleCaptcha(c captcha.Challenge, reason string) {
	a.endCaptcha(c, CaptchaResolution{Reason: reason})
}

// endCaptcha ends one challenge: it removes it from the store (idempotent),
// clears the task's Reason if it is still core.ReasonCaptcha, and broadcasts
// end with c's id, task and host filled in. It is called both right after an
// answer or abort and when the poll finds the challenge gone.
func (a *App) endCaptcha(c captcha.Challenge, end CaptchaResolution) {
	st := a.captchaStateFor()
	st.store.Remove(c.ID)
	st.unanswerableMu.Lock()
	delete(st.unanswerable, c.ID)
	st.unanswerableMu.Unlock()

	a.mu.Lock()
	var pub *taskCopy
	if c.TaskID != "" {
		// Any other reason set meanwhile is the newer fact and is kept.
		if t := a.tasks[c.TaskID]; t != nil && t.Reason == core.ReasonCaptcha {
			t.Reason = core.ReasonUnknown
			cp := a.copyLocked(t)
			pub = &cp
		}
	}
	a.mu.Unlock()
	if pub != nil {
		a.publish(pub)
	}
	end.ID, end.TaskID, end.Host, end.TestCaptcha = c.ID, c.TaskID, c.Host, c.Test
	a.Hub.Broadcast("captchaResolved", end)
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
	// Test is how a test captcha's answer compared, set when one was solved.
	Test *TestCaptchaResult `json:"test,omitempty"`
	// TestCaptcha marks the end of a test captcha however it ended, so a
	// timeout does not read as a download left stuck.
	TestCaptcha bool `json:"testCaptcha,omitempty"`
}

// TestCaptchaResult is how an answer to a test captcha compares with the text
// drawn in it.
type TestCaptchaResult struct {
	Correct bool `json:"correct"`
	// Want is the text drawn in the picture.
	Want string `json:"want"`
	// Given is the answer as it arrived, without the spaces around it.
	Given string `json:"given"`
	// Solver is the captcha account that answered, by its catalogue label,
	// and empty when a person did.
	Solver string `json:"solver,omitempty"`
}

// The refusals of CreateTestCaptcha. With the captcha or JD module off no
// prompt would show the captcha, and the Modules page lets the captcha module
// back on only once JD is.
var (
	ErrCaptchaOff   = errors.New("captchas are switched off on the Modules page")
	ErrCaptchaJDOff = errors.New("captchas come through JDownloader, which is switched off on the Modules page")
	// ErrNoCaptchaAccount refuses a test for the captcha accounts while none
	// of them could take it: none is enabled with a key, or the account is
	// switched off.
	ErrNoCaptchaAccount = errors.New("no captcha account is enabled with a key")
)

// CreateTestCaptcha draws a test captcha and publishes it at once, the way the
// poll publishes one JD listed: to the windows, the phone app and the event
// targets. solvers lets the paid solvers take it too, which they bill.
func (a *App) CreateTestCaptcha(solvers bool) (captcha.Challenge, error) {
	switch {
	case a.ModuleOff("jd"):
		return captcha.Challenge{}, ErrCaptchaJDOff
	case a.ModuleOff("captcha"):
		return captcha.Challenge{}, ErrCaptchaOff
	case solvers && len(a.captchaSolvers()) == 0:
		return captcha.Challenge{}, ErrNoCaptchaAccount
	}
	st := a.captchaStateFor()
	c, err := st.tests.New(solvers)
	if err != nil {
		return captcha.Challenge{}, err
	}
	a.ensureCaptchaPoller()
	a.pollCaptchasOnce(st)
	return c, nil
}

// CaptchaChallenges lists the challenges this session knows about. It reads
// the cache only, so a page load never waits on JD.
func (a *App) CaptchaChallenges() []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.captchaStateFor().store.List()
}

// CaptchaSeen records a reader of the captcha list that holds no socket, such
// as the phone app, as watching for the kinds it can answer, so the paid
// solvers wait only for those; nil stands for every kind. A name that is no
// kind a person answers is dropped, so a reader cannot grow the hub's table.
// A reader that runs a Turnstile names captchaWatchTurnstile as well.
func (a *App) CaptchaSeen(kinds []string) {
	if kinds == nil {
		a.Hub.Seen("captcha")
		return
	}
	for _, k := range kinds {
		switch kind := captcha.Kind(k); kind {
		case captcha.KindImage, captcha.KindClick, captcha.KindWidget, captchaWatchTurnstile:
			a.Hub.Seen(captchaWatchKey(kind))
		}
	}
}

// captchaWatchTurnstile is what a reader lists besides the widget kind when it
// runs a Cloudflare Turnstile too. Every Turnstile key runs only on the
// hostnames its owner lists, which the phone app's page can claim and a
// browser tab on this instance cannot, and an app that lists only the widget
// kind cannot run one either.
const captchaWatchTurnstile captcha.Kind = "turnstile"

// captchaWatchKey is the hub kind a reader that answers only some kinds of
// challenge is seen under.
func captchaWatchKey(k captcha.Kind) string { return "captcha:" + string(k) }

// CaptchaViewer is a way of watching the captcha prompt. A report that a
// challenge will not load takes one of them out of the count for it.
type CaptchaViewer string

const (
	// CaptchaWindow is a web interface tab or the desktop app's window,
	// watching over a socket.
	CaptchaWindow CaptchaViewer = "window"
	// CaptchaPhone is the phone app, watching by reading the list for the
	// kinds it answers (CaptchaSeen).
	CaptchaPhone CaptchaViewer = "phone"
)

// ReportCaptchaUnanswerable records that a viewer of the kind by could not load
// challenge id, such as a widget whose site key refuses this instance's
// address, so those viewers stop holding the paid solvers back for it. It
// reports false for a challenge that is no longer pending.
//
// The report names no single viewer: the desktop app's window watches through
// the shell's own hub connection, not the page's socket, and the phone app is
// only seen by its reads. So one window that cannot load it releases the
// solvers for every window, and a second one that could still races them to
// the answer.
func (a *App) ReportCaptchaUnanswerable(id string, by CaptchaViewer) bool {
	st := a.captchaStateFor()
	st.unanswerableMu.Lock()
	defer st.unanswerableMu.Unlock()
	// Checked under the lock settleCaptcha deletes under, so a challenge
	// that settles meanwhile does not leave its id behind.
	if !a.captchaPending(id) {
		return false
	}
	if st.unanswerable == nil {
		st.unanswerable = map[string]map[CaptchaViewer]bool{}
	}
	if st.unanswerable[id] == nil {
		st.unanswerable[id] = map[CaptchaViewer]bool{}
	}
	st.unanswerable[id][by] = true
	return true
}

// WithdrawCaptchaUnanswerable takes back ReportCaptchaUnanswerable once the
// viewer has loaded the challenge after all, so those viewers hold the solvers
// back again. A solver already at work on it carries on: the provider may bill
// the task whatever happens, and JD takes whichever answer reaches it first.
func (a *App) WithdrawCaptchaUnanswerable(id string, by CaptchaViewer) {
	st := a.captchaStateFor()
	st.unanswerableMu.Lock()
	defer st.unanswerableMu.Unlock()
	delete(st.unanswerable[id], by)
	if len(st.unanswerable[id]) == 0 {
		delete(st.unanswerable, id)
	}
}

// CaptchaUnanswerable reports whether a viewer of the kind by said it cannot
// load id and has not taken that back.
func (a *App) CaptchaUnanswerable(id string, by CaptchaViewer) bool {
	st := a.captchaStateFor()
	st.unanswerableMu.Lock()
	defer st.unanswerableMu.Unlock()
	return st.unanswerable[id][by]
}

// RefreshCaptchas polls right away instead of waiting for the next tick. A
// failed poll returns the last good snapshot.
func (a *App) RefreshCaptchas(_ context.Context) []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.pollCaptchasOnce(a.captchaStateFor())
}

// CaptchaAnswer is what became of an answer.
type CaptchaAnswer struct {
	// StillValid is the source's own verdict on whether the challenge was
	// still live, which callers trust over any client-side countdown.
	StillValid bool `json:"stillValid"`
	// Test is set for a test captcha answered in time.
	Test *TestCaptchaResult `json:"test,omitempty"`
}

// AnswerCaptcha submits text as a person's solution to id.
func (a *App) AnswerCaptcha(ctx context.Context, id, text string) (CaptchaAnswer, error) {
	return a.answerCaptcha(ctx, id, text, "")
}

// answerCaptcha submits text as the solution to id. solver is the captcha
// account it came from, empty for a person, and is only reported for a test
// captcha.
func (a *App) answerCaptcha(ctx context.Context, id, text, solver string) (CaptchaAnswer, error) {
	st := a.captchaStateFor()
	ch, known := st.store.Get(id)

	if captcha.IsTest(id) {
		want, correct, live := st.tests.Check(id, text)
		if !live {
			if known {
				a.settleCaptcha(ch, "expired")
			}
			return CaptchaAnswer{}, nil
		}
		res := &TestCaptchaResult{Correct: correct, Want: want, Given: strings.TrimSpace(text), Solver: solver}
		if known {
			a.endCaptcha(ch, CaptchaResolution{Reason: "solved", Test: res})
		}
		return CaptchaAnswer{StillValid: true, Test: res}, nil
	}

	cctx, cancel := context.WithTimeout(ctx, captchaCallTimeout)
	defer cancel()
	stillValid, err := st.source.Answer(cctx, id, text)
	if err != nil {
		return CaptchaAnswer{}, err
	}
	if known {
		reason := "expired"
		if stillValid {
			reason = "solved"
		}
		a.settleCaptcha(ch, reason)
	}
	return CaptchaAnswer{StillValid: stillValid}, nil
}

// AbortCaptcha tells the source the user declined to answer id, at the given
// scope (see captcha.AbortScope).
func (a *App) AbortCaptcha(ctx context.Context, id string, scope captcha.AbortScope) error {
	st := a.captchaStateFor()
	ch, known := st.store.Get(id)

	if captcha.IsTest(id) {
		st.tests.Abort(id)
	} else {
		cctx, cancel := context.WithTimeout(ctx, captchaCallTimeout)
		defer cancel()
		if err := st.source.Abort(cctx, id, scope); err != nil {
			return err
		}
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
