package app

// The paid captcha solvers: which of them are configured, whether they wait
// for somebody watching, and the walk through them for one challenge. What
// they do is written onto the challenge as a captcha.SolverReport, so the
// prompt can say which solver is at work and why one declined.
//
// A challenge goes to the solvers once. Each is tried in the configured order
// until one answers; the next one is asked only after a refusal that came
// before any task existed. A provider that may hold the task ends the walk,
// since it may bill for the task whatever comes back.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
)

// Both are vars so the tests can shorten them.
var (
	// captchaWatchInterval is how often a held-back solver looks again for
	// somebody watching.
	captchaWatchInterval = time.Second

	// captchaWatchGrace is how long a viewer still counts as watching once it
	// is gone without saying so: a socket that dropped may be reconnecting,
	// and an app that polls the list may look every five seconds.
	captchaWatchGrace = 15 * time.Second
)

// paidLedgerTTL is how long a challenge id stays in the ledger, far past the
// few minutes any captcha lives.
const paidLedgerTTL = time.Hour

// paidLedgerFile holds the challenges a solver was asked to solve. JD keeps
// its challenges across a restart of this app, so without it a restart during
// a solve would send the same captcha again.
const paidLedgerFile = "captcha-ledger.json"

// paidLedger is every challenge the solvers are on or were sent in this
// process, and on disk at path the ones a solver was asked to solve.
type paidLedger struct {
	mu     sync.Mutex
	path   string
	loaded bool
	seen   map[string]time.Time
	sent   map[string]time.Time
}

// claim records id and reports whether it was new. Entries older than
// paidLedgerTTL are dropped on the way, so the file stays small.
func (l *paidLedger) claim(id string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	for _, m := range []map[string]time.Time{l.seen, l.sent} {
		for k, at := range m {
			if now.Sub(at) > paidLedgerTTL {
				delete(m, k)
			}
		}
	}
	if _, ok := l.seen[id]; ok {
		return false
	}
	l.seen[id] = now
	return true
}

// release gives up a claim that nothing was sent for, so the challenge counts
// as new when JD lists it again.
func (l *paidLedger) release(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.seen, id)
}

// markSent writes id down as handed to a solver, before the request goes out.
// A challenge claimed and still held back for a watcher is not written, so a
// restart during the wait lets the solvers take it after all.
func (l *paidLedger) markSent(id string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	if _, ok := l.sent[id]; ok {
		return
	}
	l.sent[id] = now
	b, err := json.Marshal(l.sent)
	if err != nil {
		log.Printf("captcha: could not write down the captchas sent to the solvers: %v", err)
		return
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("captcha: could not write down the captchas sent to the solvers: %v", err)
		return
	}
	if err := os.Rename(tmp, l.path); err != nil {
		log.Printf("captcha: could not put the list of captchas sent to the solvers in place: %v", err)
		_ = os.Remove(tmp)
	}
}

// loadLocked reads the file on first use. A missing or unreadable file is an
// empty ledger. Caller holds l.mu.
func (l *paidLedger) loadLocked() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.seen, l.sent = map[string]time.Time{}, map[string]time.Time{}
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(b, &l.sent); err != nil {
		log.Printf("captcha: the list of captchas sent to the solvers could not be read and starts empty: %v", err)
		l.sent = map[string]time.Time{}
	}
	for id, at := range l.sent {
		l.seen[id] = at
	}
}

// paidSolver is one configured solver and the catalogue label its report lines
// carry.
type paidSolver struct {
	label string
	captcha.Solver
}

// captchaSolvers returns the configured automatic solvers that have a stored
// credential, in CaptchaSolverOrder. Entries without a credential are skipped.
func (a *App) captchaSolvers() []paidSolver {
	order := a.Settings.Get().CaptchaSolverOrder
	if len(order) == 0 {
		return nil
	}
	out := make([]paidSolver, 0, len(order))
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
			out = append(out, paidSolver{svc.Label, captcha.NewTwoCaptchaSolver(cred.APIKey)})
		case "anticaptcha":
			out = append(out, paidSolver{svc.Label, captcha.NewAntiCaptchaSolver(cred.APIKey)})
		}
	}
	return out
}

// trySolveCaptchaAutomatically hands c to the configured solvers. It runs
// alongside the prompt shown to the user, never instead of it; whichever
// answer reaches JD first wins, and the other resolves as "already gone"
// without an error.
func (a *App) trySolveCaptchaAutomatically(c captcha.Challenge) {
	a.solveCaptchaWith(a.captchaSolvers(), c)
}

// solveCaptchaWith takes the solvers as a parameter so tests can pass fakes
// instead of building real clients from stored keys.
func (a *App) solveCaptchaWith(solvers []paidSolver, c captcha.Challenge) {
	if len(solvers) == 0 || !paidSolvable(c) {
		return
	}
	// Claimed first, so a challenge JD lists again after it dropped out of a
	// poll is never paid for a second time.
	ledger := &a.captchaStateFor().paid
	if !ledger.claim(c.ID, time.Now()) {
		return
	}

	// A solver that does not take this kind is reported at once, rather than
	// after a wait that could never end in a solve.
	var report captcha.SolverReport
	var willing []paidSolver
	for _, s := range solvers {
		if err := s.Takes(c); err != nil {
			report.Refusals = append(report.Refusals, captcha.RefusalFor(s.label, err))
			continue
		}
		willing = append(willing, s)
	}
	if len(willing) == 0 {
		report.State = captcha.SolverStopped
		a.reportSolver(c.ID, report)
		return
	}

	ctx := a.ctx
	if !c.ExpiresAt.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(a.ctx, c.ExpiresAt)
		defer cancel()
	}
	for !a.holdForWatchers(ctx, c, report) {
		// A challenge that dropped out of one poll ends the wait, and only
		// markSent has to keep it from a second solve, so the claim goes. If JD
		// listed it again before that, the solve the poll started gave up on
		// the claim, and the challenge is taken up again here.
		ledger.release(c.ID)
		if ctx.Err() != nil || a.captchaSwitchedOff() || !a.captchaPending(c.ID) || !ledger.claim(c.ID, time.Now()) {
			return
		}
	}

	for _, s := range willing {
		// Asked before every paid solve and before the answer goes to JD,
		// since a solve takes long enough for somebody to switch captchas off
		// or answer the prompt meanwhile.
		if a.captchaSwitchedOff() || !a.captchaPending(c.ID) {
			return
		}
		report.State, report.Solver = captcha.SolverSolving, s.label
		a.reportSolver(c.ID, report)

		ledger.markSent(c.ID, time.Now())
		text, err := s.Solve(ctx, c)
		if err == nil {
			if a.captchaSwitchedOff() {
				return
			}
			if _, err := a.AnswerCaptcha(ctx, c.ID, text); err != nil {
				log.Printf("captcha: %s answered %s but submitting it failed: %v", s.label, c.ID, err)
			}
			return
		}
		if ctx.Err() != nil {
			return // the challenge expired or the app is closing
		}
		log.Printf("captcha: %s did not solve %s: %v", s.label, c.ID, err)
		report.Refusals = append(report.Refusals, captcha.RefusalFor(s.label, err))
		if errors.Is(err, captcha.ErrTaskTaken) {
			break
		}
	}
	report.State, report.Solver = captcha.SolverStopped, ""
	a.reportSolver(c.ID, report)
}

// holdForWatchers keeps the solvers back while somebody is watching, when
// CaptchaSolverOnlyUnwatched asks for that. It returns false once the
// challenge needs no solver any more: answered, gone, captchas switched off,
// or ctx over.
func (a *App) holdForWatchers(ctx context.Context, c captcha.Challenge, report captcha.SolverReport) bool {
	s := a.Settings.Get()
	if !s.CaptchaSolverOnlyUnwatched || !a.captchaWatched(c) {
		return true
	}
	until := solverTakeoverAt(time.Now(), time.Duration(s.CaptchaSolverWait)*time.Second, c.ExpiresAt)
	report.State, report.Until = captcha.SolverWaiting, until
	a.reportSolver(c.ID, report)

	tick := time.NewTicker(captchaWatchInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
		if a.captchaSwitchedOff() || !a.captchaPending(c.ID) {
			return false
		}
		if !a.Settings.Get().CaptchaSolverOnlyUnwatched || !a.captchaWatched(c) || !time.Now().Before(until) {
			return true
		}
	}
}

// captchaWatched reports whether somebody who could answer c is watching the
// captcha prompt: a web interface tab or the desktop app's window on screen,
// or an app polling the list for c's kind (see CaptchaSeen). Nobody can
// answer a Cloudflare Turnstile, which the widget page does not run, so the
// solvers never wait for one, and the windows stop counting for a challenge
// one of them could not load (see CaptchaUnanswerable).
func (a *App) captchaWatched(c captcha.Challenge) bool {
	if w, ok := c.Payload.(*captcha.WidgetPayload); ok && w.Vendor == captcha.VendorTurnstile {
		return false
	}
	if !a.captchaUnanswerable(c.ID) && a.Hub.Watched("captcha", captchaWatchGrace) {
		return true
	}
	return a.Hub.Watched(captchaWatchKey(c.Kind), captchaWatchGrace)
}

// solverTakeoverAt is when a held-back solver takes over: wait after now, and
// never later than halfway to the captcha's own deadline, so the solver still
// has time to deliver.
func solverTakeoverAt(now time.Time, wait time.Duration, expires time.Time) time.Time {
	at := now.Add(wait)
	if !expires.IsZero() {
		if half := now.Add(expires.Sub(now) / 2); half.Before(at) {
			at = half
		}
	}
	return at
}

// paidSolvable reports whether c carries what a paid solver works from: an
// image it can read, or a widget's site key.
func paidSolvable(c captcha.Challenge) bool {
	switch p := c.Payload.(type) {
	case *captcha.ImagePayload:
		return p != nil && captcha.ImageReadable(p.DataURL) && (c.Kind == captcha.KindImage || c.Kind == captcha.KindClick)
	case *captcha.WidgetPayload:
		return p != nil && p.SiteKey != "" && c.Kind == captcha.KindWidget
	}
	return false
}

func (a *App) captchaSwitchedOff() bool { return a.ModuleOff("captcha") || a.ModuleOff("jd") }

// captchaPending reports whether id is still waiting for an answer.
func (a *App) captchaPending(id string) bool {
	_, ok := a.captchaStateFor().store.Get(id)
	return ok
}

// reportSolver puts r on challenge id and publishes it, so an open prompt
// shows what the solvers are doing. A challenge already gone is left alone.
func (a *App) reportSolver(id string, r captcha.SolverReport) {
	if c, ok := a.captchaStateFor().store.Report(id, r); ok {
		a.Hub.Broadcast("captcha", c)
	}
}
