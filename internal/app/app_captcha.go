package app

// Captcha: a hoster (or an account's own login gate) asking a human
// something before a download can continue. This file is the App-level
// wiring around internal/captcha's Source and its own Store
// (internal/captcha/store.go, also this wave's to add): a poll loop that
// drives Source.List, the hub events a connected browser needs to show a
// prompt without polling itself, and the seam dispatchLocked
// (app_dispatch.go) uses to hold a captcha-waiting task the way it already
// holds Hold. See build-plan.md section 3's Wave 7 table, section 8's Wave 7
// amendment and section 9 package 16 for what this wave asks for, and
// internal/captcha's own package comment plus jdsource.go for what 7F
// already built underneath this file.
//
// NO NEW core.Task FIELD, on purpose. core.Task already carries Status and
// Reason, and core.ReasonCaptcha (internal/core/task.go) is already the
// classification app_errors.go gives this exact condition ("...captcha..."
// in a failure sentence, textReasons). The one fact neither of those can
// answer on its own - which challenge, if any, task T is waiting on right
// now - is internal/captcha.Store's whole job (keyed by challenge id, with a
// taskID index); see markCaptchaTasks/settleCaptcha below for the only two
// places this file ever touches Task.Reason. build-plan.md section 4
// conflict 2 would force a new field only for a real gap this design did not
// find, and rules that if it ever does, it must be a flag, never a new
// core.Status value - Task.Status is left exactly as whatever the JD
// backend's own poll loop (internal/resolver/jd/backend.go) already has it
// at, ordinarily StatusRunning, since a captcha only ever blocks a link
// already in JD's download list.
//
// Kept at package level and keyed by the owning *App, not as a field on App
// (app.go) - the same trade app_hosterauth.go's Reconciler registry and
// app_accounts.go's accountHealthState already document for the identical
// reason: app.go's struct is another agent's file this wave (only
// app_dispatch.go is 7A's to touch there - build-plan.md section 3's Wave 7
// table).

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// captchaPollInterval is how often the poller asks Source.List for what is
// currently pending, once started. Short enough that a modal's countdown
// feels live and a solved or timed-out challenge clears within a couple of
// seconds; long enough that it does not lean on a local sidecar harder than
// the ordinary download-progress poll already does (internal/resolver/jd/backend.go's
// own poll ticks at 750ms, for comparison - deliberately coarser here,
// because nothing about a captcha needs sub-second precision the way a byte
// counter does).
const captchaPollInterval = 2 * time.Second

// captchaCallTimeout bounds one CaptchaAnswer/CaptchaAbort round trip to the
// JD sidecar - the same 15s VerifyCredential/TestAccount already spend on a
// live call elsewhere in this package, long enough for a slow box, short
// enough that a wedged sidecar cannot hang the HTTP handler indefinitely.
const captchaCallTimeout = 15 * time.Second

// captchaState is one App's captcha wiring.
type captchaState struct {
	source captcha.Source
	store  *captcha.Store

	startOnce sync.Once
	// pollMu serialises one poll pass: the ticker's own tick and an on-demand
	// CaptchaRefresh both call pollCaptchasOnce, and without this a refresh
	// pressed right as the ticker also fires would send two concurrent
	// List() round-trips to the local JD sidecar for the one answer either
	// alone would have gotten. Store.Sync is independently safe for
	// concurrent use on its own, so nothing besides that redundant network
	// call was ever at risk - this exists to not make it anyway.
	pollMu sync.Mutex
}

var (
	captchaMu  sync.Mutex
	captchaReg = map[*App]*captchaState{}
)

// captchaStateFor returns this App's captcha wiring, building it on first
// use - the same lazy-registry shape app_hosterauth.go's hosterAuth() and
// app_accounts.go's healthState() already use for the identical reason
// (app.go's struct is not this file's to grow this wave).
func (a *App) captchaStateFor() *captchaState {
	captchaMu.Lock()
	defer captchaMu.Unlock()
	st, ok := captchaReg[a]
	if !ok {
		st = &captchaState{store: captcha.NewStore()}
		st.source = captcha.NewJDSource(jdBaseEnv, a.resolveJDTask)
		captchaReg[a] = st
	}
	return st
}

// jdBaseEnv reads KL_JD live on every call - the same convention every other
// JD-address reader in this package already follows (app_hosterauth.go's
// hosterAuth(), app_accounts.go's rewireBackends/JDStatus): a container that
// changes KL_JD, or a headless JD that comes up after this App already
// started, has to be picked up without a restart.
func jdBaseEnv() string { return os.Getenv("KL_JD") }

// ensureCaptchaPoller starts the poll loop exactly once per App.
//
// Triggered from dispatchLocked (app_dispatch.go) rather than from
// cmd/knightloader/main.go the way StartHosterAuth is started: main.go is
// not this wave's file to add a line to (build-plan.md section 3's Wave 7
// table names internal/captcha, this file, app_dispatch.go,
// internal/api/routes_captcha.go and the frontend - not main.go, and not
// app.go's own New/Close either). dispatchLocked is the closest thing this
// file owns to "runs once at start-up and on nearly everything after": the
// schedule runner's first Apply calls it directly (see app.go's own comment
// on a.sched.Start(), "the runner's first pass...") before a single browser
// could have loaded the page, and it runs again on essentially every
// task-affecting call thereafter. Idempotent and cheap after the first call
// (a mutex lock and a sync.Once check), so calling it unconditionally at the
// top of dispatchLocked costs nothing worth avoiding.
func (a *App) ensureCaptchaPoller() {
	st := a.captchaStateFor()
	st.startOnce.Do(func() { a.spawn(a.captchaPollLoop) })
}

// captchaPollLoop runs until a.ctx is done, which is what makes Close wait
// for it - a.spawn's own contract (see app.go's doc comment on spawn/Close,
// and the CI-only race commit 813cf29 fixed the same way: a goroutine that
// outlives Close writing to a store that has already closed). Nothing in
// this file ever writes to a.Store or the filesystem directly from this
// goroutine except through publishTasks, which is the same helper every
// other spawned writer in this package already goes through.
//
// No immediate first pass. This starts from dispatchLocked, which every
// table-driven test in this package also reaches on the very first call
// app.New makes (most configure no KL_JD; a few configure a fake JD
// specifically to stay clear of a real one). An immediate List() would only
// ever find ErrJDNotConfigured in that ordinary case - JDSource's own
// client() short-circuits before any network call when jdBase() is
// empty - so nothing here risks a stray real HTTP request the way
// app_accounts.go's accountHealthLoop worried about for live debrid/torbox
// calls; waiting for the first tick is simply the calmer default, matching
// that file's own established shape rather than inventing a second one.
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

// pollCaptchasOnce is one List/Sync/publish pass, shared by the ticker and
// CaptchaRefresh's on-demand call - see captchaState.pollMu's own doc
// comment for why the two serialise. It returns the resulting snapshot so a
// caller (CaptchaRefresh) can hand it straight back to an HTTP response
// without a second read.
func (a *App) pollCaptchasOnce(st *captchaState) []captcha.Challenge {
	st.pollMu.Lock()
	defer st.pollMu.Unlock()

	list, err := st.source.List(a.ctx)
	if err != nil {
		if !errors.Is(err, captcha.ErrJDNotConfigured) {
			log.Printf("captcha: listing challenges failed (will retry): %v", err)
		}
		// The store is left exactly as it was - internal/hosterauth/reconcile.go's
		// reconcileAndLog makes the identical choice on its own query failure.
		// A transient error is not "everything just resolved": wiping the
		// store here would tell every open modal its challenge vanished when
		// nothing about it actually changed, which is precisely the "relay
		// that drops challenges" build-plan.md section 9 package 16 warns is
		// worse than none.
		return st.store.List()
	}

	added, changed, removed := st.store.Sync(list)
	for _, c := range added {
		a.Hub.Broadcast("captcha", c)
		if c.Kind == captcha.KindImage || c.Kind == captcha.KindClick {
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
	// The status strip's aggregate signal (build-plan.md section 9 package 1
	// / section 8's Wave 9 note, 9A), alongside the per-challenge
	// "captcha"/"captchaResolved" broadcasts above rather than instead of
	// them - see app_activity.go's own package comment. Only on this success
	// path, matching the two broadcasts above: a transient List() failure
	// leaves the store exactly as it was (see the error branch), and
	// re-publishing the same count on every failed 2s tick would tell every
	// connected browser something changed when nothing did.
	a.setActivityGauge(ActivityCaptcha, len(current))
	return current
}

// markCaptchaTasks stamps core.ReasonCaptcha onto every task a brand-new
// challenge names, and publishes only the tasks that actually changed - a
// challenge Store already knew about (Sync's changed slice, an ExpiresAt
// tick most often) never touches the task a second time, and one with no
// TaskID (internal/captcha/challenge.go's own doc comment: "a real, expected
// answer, not a bug") has nothing to mark.
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

// settleCaptcha is one challenge's end: removed from Store if it is still
// there (it usually already is not - CaptchaAnswer/CaptchaAbort below remove
// it themselves the moment they know the outcome, which is what makes this
// function safe to call from both that immediate path and the poll loop's
// own "it vanished" discovery, idempotently either way - see Store.Remove's
// own doc comment), the task's Reason cleared back to unknown ONLY if it is
// still core.ReasonCaptcha (never clobbering a fact something else has since
// set - a real transfer error that arrived in the meantime is the truer
// story by then, and is left alone), and one hub broadcast naming which of
// the two this was: "no longer waiting" on its own does not say whether the
// download can now proceed.
func (a *App) settleCaptcha(c captcha.Challenge, reason string) {
	a.captchaStateFor().store.Remove(c.ID)

	a.mu.Lock()
	var pub *core.Task
	if c.TaskID != "" {
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

// CaptchaResolution is one challenge's end, broadcast over the hub as
// "captchaResolved" - see settleCaptcha. Deliberately not part of
// internal/captcha's own vocabulary (Challenge/Kind/AbortScope/Source): why a
// challenge stopped being active is an App-level story, told from what this
// file's own CaptchaAnswer/CaptchaAbort just did or from the poll loop's own
// clock against ExpiresAt, not from anything 7F's package computes itself.
type CaptchaResolution struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId,omitempty"`
	Host   string `json:"host"`
	// Reason is one of "solved" (Answer, stillValid), "expired" (Answer,
	// !stillValid - it arrived too late), "aborted" (Abort succeeded),
	// "timedOut" (the poller noticed it gone, past its last-known
	// ExpiresAt) or "resolved" (the poller noticed it gone with no ExpiresAt
	// to judge by, or before one - solved or aborted somewhere this session
	// did not do it from, and the two are not distinguishable from here).
	Reason string `json:"reason"`
}

// CaptchaChallenges lists every challenge this session currently knows
// about - a pure cache read, never a live JD call, for the same reason
// app_accounts.go's AccountStates never makes one on a page load: a GET
// route that can block on an external service is a page load that can hang
// on it. Named as a plural noun rather than Verb+Subject, matching this
// package's own existing split between a read (HosterLogins, AccountStates,
// Tasks) and an action (SetHosterLogin, AbortExtraction, and this file's own
// AbortCaptcha below).
func (a *App) CaptchaChallenges() []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.captchaStateFor().store.List()
}

// RefreshCaptchas polls Source.List right now instead of waiting for the
// next tick, and returns what it found - the prompt modal's "Refresh", for
// somebody who wants to know before the automatic interval catches up. Like
// the poll loop's own pass, a transient failure hands back the last good
// snapshot rather than an error - see pollCaptchasOnce's own doc comment.
func (a *App) RefreshCaptchas(_ context.Context) []captcha.Challenge {
	a.ensureCaptchaPoller()
	return a.pollCaptchasOnce(a.captchaStateFor())
}

// AnswerCaptcha submits text as id's solution. stillValid is JD's own,
// authoritative answer to whether id was still live when it arrived - see
// captcha.Source.Answer's own doc comment for why a caller must trust this
// over any client-side countdown, and never re-derive it from one.
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

// AbortCaptcha tells the Source the user chose not to answer id, at scope -
// see captcha.AbortScope's own doc comment for what each value reaches.
//
// Name and shape (ctx, id, scope) are routes_captcha_skip.go's own contract
// (internal/api, 7D's file this wave, landed before this one and documented
// there as "THE CONTRACT THIS FILE ASSUMES OF *app.App"): matched exactly,
// rather than this file choosing its own spelling and leaving that route's
// one call site to be fixed up separately.
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

// captchaWaitingLocked reports whether taskID is currently blocked on a
// captcha this session knows about - dispatchLocked's own seam
// (app_dispatch.go) for treating one the way it already treats Hold. Caller
// holds a.mu; this only reads captchaState's own, independently-locked
// store, so nothing here can deadlock against that lock.
func (a *App) captchaWaitingLocked(taskID string) bool {
	_, ok := a.captchaStateFor().store.ByTask(taskID)
	return ok
}

// resolveJDTask maps one JD download-link id to the KnightLoader task it
// blocks - the reverse index internal/captcha/jdsource.go's own doc comment
// names as this wave's seam to add (NewJDSource's resolveTask parameter),
// because that package has no access to app-level task state and must not
// import internal/app to get it.
//
// It replicates internal/resolver/jd/backend.go's own pkgName format
// ("KL-" + taskID) rather than that package exporting a method for it: that
// file belongs to another agent this wave (build-plan.md section 3's Wave 7
// table only lists this package's files, routes_captcha.go and the frontend
// for 7A), and the two call sites agreeing on one literal format is the
// existing contract - jd.Backend.linkIDs (unexported, same package) computes
// the identical taskID -> JD-link-id direction the same way. If that format
// ever changes, both call sites have to change together; there is nowhere
// else this could live without either package reaching into the other's
// files this wave.
//
// Scoped to tasks currently routed through jd AND active right now, not the
// whole task history: a captcha can only ever block a link JD is presently
// holding, and asking JD about a task it finished or never touched wastes a
// call for an answer that is always "no". This runs fresh on every call
// rather than being cached, which is deliberately cheap to accept: it is
// only ever reached from inside JDSource.List, itself only called from this
// file's own poll loop, and only walks as far as the FIRST task whose link
// ids contain a match - in the overwhelmingly common case of at most one or
// two links routed through jd at a time, that is one or two extra JD round
// trips, and only while a human is actually waiting on a captcha, never on
// an ordinary quiet tick.
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

// jdRoutedActiveTaskIDs is resolveJDTask's own candidate list: every task
// presently dispatched (a.active) whose backend is jd. Order is whatever
// Go's map iteration gives, which is fine here - resolveJDTask stops at the
// first match, and nothing about correctness depends on trying them in any
// particular order.
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

// captchaSolvers returns this App's configured, credentialed automatic
// solvers, in the order settings.Settings.CaptchaSolverOrder names - see
// internal/captcha/solver_2captcha.go's own package comment for what
// "solver order" means: each is tried in turn before a new image/click
// challenge is shown to a human at all. An order entry with no stored
// credential, or one sanitizeCaptcha has already dropped as unrecognised
// (internal/settings/settings_captcha.go), is silently skipped - the same
// tolerance credentialFor/accountEnabled already extend to every other
// optional service in this file, none of which treats "configured but not
// set up yet" as an error.
func (a *App) captchaSolvers() []captcha.Solver {
	order := a.Settings.Get().CaptchaSolverOrder
	if len(order) == 0 {
		return nil
	}
	out := make([]captcha.Solver, 0, len(order))
	for _, id := range order {
		svc, ok := accounts.Lookup(id)
		if !ok || svc.Group != accounts.GroupCaptchaSolver {
			continue
		}
		cred := a.credentialFor(svc, "")
		if cred.IsZero() {
			continue
		}
		switch id {
		case "2captcha":
			out = append(out, captcha.NewTwoCaptchaSolver(cred.APIKey))
		case "anticaptcha":
			out = append(out, captcha.NewAntiCaptchaSolver(cred.APIKey))
		}
	}
	return out
}

// trySolveCaptchaAutomatically tries c against every configured solver, in
// order, and settles c the moment one succeeds - the connective piece
// between 7B's two solver clients (internal/captcha/solver_*.go) and 7A's
// poll loop above, neither of which called the other: 7B built Solve and the
// credential catalogue entries, 7A built the human-facing path, and the two
// ran in parallel this wave (build-plan.md section 8's Wave 7 note) with
// nothing in either brief asking either one to wire the connection. Added
// after the wave's own gate, once go build/vet/test made the gap visible as
// two fully working, fully tested packages with zero callers into each
// other - see internal/captcha's own Solver interface doc comment ("tried in
// a configured order before KnightLoader ever shows a human the prompt
// modal") for the intent neither file alone could deliver on.
//
// Runs ALONGSIDE pollCaptchasOnce's own immediate human-facing broadcast for
// c, never instead of it: an install with no solver configured must see a
// challenge exactly as fast as before this function existed, and even a
// slow or ultimately-failing solver must never make a challenge invisible
// for however long solverMaxWait allows. AnswerCaptcha's own "already gone"
// handling (captcha.Source.Answer's contract, stillValid=false with a nil
// error) is what makes it safe for an automatic solve and a human's own
// answer to race - whichever reaches JD first simply wins, and the other
// resolves as a no-op rather than an error either side has to handle
// specially.
//
// Spawned per challenge via a.spawn (see the caller), never a bare
// goroutine, for the reason this file's own package comment on
// captchaPollLoop already gives: Close() has to wait for anything that can
// still call AnswerCaptcha - which itself calls publishTasks - or it is the
// exact shape of bug commit 813cf29 fixed in Wave 6, a write landing after
// the store believes itself closed.
func (a *App) trySolveCaptchaAutomatically(c captcha.Challenge) {
	a.solveCaptchaWith(a.captchaSolvers(), c)
}

// solveCaptchaWith is trySolveCaptchaAutomatically's own logic, taking the
// solver list as a parameter rather than reading captchaSolvers() itself -
// the seam a test uses to exercise the ordering/first-success/ctx-deadline
// behaviour with a fake captcha.Solver, the same way internal/hosterauth's
// Reconciler takes an injectable newJD rather than calling newJDClient
// directly, and for the identical reason: a real TwoCaptchaSolver/
// AntiCaptchaSolver only gets built from a real stored API key, which a unit
// test has no business needing.
func (a *App) solveCaptchaWith(solvers []captcha.Solver, c captcha.Challenge) {
	if len(solvers) == 0 {
		return
	}
	// KindWidget/KindUnsupported are out of scope for both solvers - see
	// captcha.Solver's own doc comment - and ImagePayload is what KindImage
	// and KindClick both carry (challenge.go's ClickPayload is a type alias
	// of it, not a separate shape).
	payload, ok := c.Payload.(*captcha.ImagePayload)
	if !ok || payload == nil || payload.DataURL == "" {
		return
	}

	ctx := a.ctx
	if !c.ExpiresAt.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(a.ctx, c.ExpiresAt)
		defer cancel()
	}

	for _, s := range solvers {
		text, err := s.Solve(ctx, c.Kind, payload.DataURL, c.Prompt)
		if err != nil {
			if ctx.Err() != nil {
				return // the challenge's own window (or app shutdown) closed; trying the next solver could not help
			}
			continue
		}
		if _, err := a.AnswerCaptcha(ctx, c.ID, text); err != nil {
			log.Printf("captcha: an automatic solver answered %s but submitting it failed: %v", c.ID, err)
		}
		return
	}
}
