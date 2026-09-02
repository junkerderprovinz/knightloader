package hosterauth

// The reconciler: desired state (what Store holds - what the user typed into
// KL's own form) versus actual state (what JD's own account list currently
// says), reconciled by adding what JD is missing and removing what the user
// deleted here. This is a loop, not a one-shot push at boot, and that is load-
// bearing rather than a style choice: a recreated or updated JD sidecar comes
// back with an EMPTY account list, every premium login silently gone, and
// downloads then quietly fall back to free-user speeds with no visible error
// anywhere. Run below is what catches that - see its own doc comment.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// LoginStatus is the three-way state one stored login can be in against JD.
// Kept as three states and not collapsed to a plain ok/fail boolean because
// two of the failing-looking cases mean opposite things to a user staring at
// them: "queued" is a login that is about to start working on its own,
// "rejected" is one that needs a different password. Collapsing them is how a
// user gives up on a login that was seconds from succeeding.
type LoginStatus string

const (
	// StatusQueued is desired here and either not yet confirmed present on JD,
	// or present but not yet validated by JD's own account checker.
	StatusQueued LoginStatus = "queued"
	// StatusActive is confirmed present and valid on JD.
	StatusActive LoginStatus = "active"
	// StatusRejected is present on JD and has stayed invalid past rejectGrace -
	// see plan's comment for why that grace window exists at all.
	StatusRejected LoginStatus = "rejected"
)

// LoginState is one row the accounts page shows - never the password. It has
// no field that could carry one, which is what makes "never appears in a
// snapshot" true by construction rather than by a redaction step somebody
// could forget to call.
type LoginState struct {
	Host     string      `json:"host"`
	Username string      `json:"username"`
	Status   LoginStatus `json:"status"`
	Detail   string      `json:"detail,omitempty"`
}

// DesiredLogin is one row Store wants JD to have - the plain half of a
// credential this package carries only as far as the one addAccount call it
// is used for, never returned, logged or stored a second time.
type DesiredLogin struct {
	Host     string
	Username string
	Password string
}

// rejectGrace is how long a JD account may sit at valid=false before Reconcile
// reads that as a rejection rather than "still checking". JD validates a
// freshly added account asynchronously through its own account checker and
// reports it invalid in the meantime, exactly the same shape as the crawl
// settle-window internal/resolver/jd/backend.go's AddContainer already waits
// out before trusting a link-grabber snapshot - applied here to account
// validation instead of link collection. Long enough that a hoster's own
// checker queue does not read as a wrong password; short enough that a
// genuinely wrong password does not sit at "still checking" for long.
const rejectGrace = 2 * time.Minute

// reconcileInterval is how often Run re-checks JD without being asked. It is
// what makes "runs on every JD reconnect, not only at boot" true: nothing
// here is told when JD comes back after being recreated or updated, so the
// only honest way to catch that is to keep asking, cheaply, rather than
// trying to detect the edge and missing an edge case wired around it.
const reconcileInterval = 30 * time.Second

var errJDNotConfigured = errors.New("hosterauth: no JD sidecar is configured (KL_JD is unset)")

// curatedHosts is the "add a login" picker's fallback list, offered only
// while JD is unreachable or none is configured yet - so the picker is not
// empty on a fresh boot before JD answers. JD's own listPremiumHoster
// (jdclient.go) is the live, complete, always-current list and wins whenever
// it answers; this is not meant to be exhaustive, only enough that the page
// is usable before JD is up.
var curatedHosts = []string{
	"rapidgator.net", "uploaded.net", "nitroflare.com", "turbobit.net",
	"keep2share.cc", "katfile.com", "ddownload.com", "1fichier.com",
	"mega.nz", "filefactory.com", "hitfile.net", "fikper.com",
}

// Host is one entry the "add a login" picker offers.
type Host struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Reconciler owns one App's hoster-login state: the desired side (Store) and
// the last-known actual side (what JD said last reconcile pass).
type Reconciler struct {
	store  *Store
	jdBase func() string // read live, not captured once, so a JD address that changes (a recreated container, a KL_JD edit) is picked up without a restart
	// newJD builds the jdAccounts a reconcile pass talks to. A field rather
	// than a bare call to newJDClient so a test can inject a fake without
	// hitting a real JD - see reconcile_test.go.
	newJD func(base string) jdAccounts

	mu        sync.Mutex
	states    map[string]LoginState
	firstFail map[string]time.Time // host -> when Reconcile first saw it present-but-invalid, for the grace window
}

// NewReconciler builds a Reconciler against the app's shared credential store
// and a live JD base URL. jdBase is called fresh on every reconcile pass.
func NewReconciler(store *Store, jdBase func() string) *Reconciler {
	return &Reconciler{
		store:     store,
		jdBase:    jdBase,
		newJD:     func(base string) jdAccounts { return newJDClient(base) },
		states:    map[string]LoginState{},
		firstFail: map[string]time.Time{},
	}
}

// Run reconciles once immediately, then on reconcileInterval until ctx is
// done. The immediate pass is what makes a fresh boot show real state right
// away instead of the interval's worth of "queued"; the loop after it is what
// makes a JD container that comes back from being recreated - with its
// account list wiped - get everything pushed back on its own, without anyone
// noticing it needed to.
func (r *Reconciler) Run(ctx context.Context) {
	r.reconcileAndLog(ctx)
	t := time.NewTicker(reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reconcileAndLog(ctx)
		}
	}
}

func (r *Reconciler) reconcileAndLog(ctx context.Context) {
	if _, err := r.Reconcile(ctx); err != nil && !errors.Is(err, errJDNotConfigured) {
		log.Printf("hosterauth: reconcile against JD failed (will retry): %v", err)
	}
}

// Plan is what one reconcile pass decided, returned so a test can assert on
// it directly without threading a fake JD client through Reconciler's own
// locking - see plan below, which computes this without touching the network
// or the store.
type Plan struct {
	Add    []DesiredLogin
	Remove []int64
	States map[string]LoginState
}

// plan compares desired against actual and decides what to add, what to
// remove, and each desired host's three-way status right now. It is the pure
// half of a reconcile pass - no store read, no HTTP call, no mutation of
// firstFail - so the add/remove/status decision can be tested against fixed
// inputs without a fake JD client or a real Store.
//
// Matching a desired host to an actual JD account is done on hostname alone,
// case- and www.-insensitively. This is a documented judgment call, not a
// verified guarantee: addAccount's premiumHoster argument is resolved through
// JD's own PluginFinder.assignHost before being stored (see jdclient.go's doc
// comment), so the exact string this reconciler gets back as an account's
// Hostname could in principle differ from what curatedHosts or
// listPremiumHoster handed it for an alias JD's plugin finder folds together.
// Untested against a real JD, this is the seam most likely to need
// adjustment first.
func plan(desired []DesiredLogin, actual []jdAccount, firstFail map[string]time.Time, now time.Time) Plan {
	byHost := map[string]jdAccount{}
	for _, a := range actual {
		byHost[normalizeHost(a.Hostname)] = a
	}
	wanted := map[string]bool{}
	p := Plan{States: map[string]LoginState{}}
	for _, d := range desired {
		h := normalizeHost(d.Host)
		wanted[h] = true
		acc, present := byHost[h]
		switch {
		case !present:
			p.Add = append(p.Add, d)
			p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusQueued,
				Detail: "waiting for JDownloader to accept this login"}
		case acc.InfoMap != nil && acc.InfoMap.Valid:
			p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusActive}
		default:
			// JD reports a freshly added account as invalid too, until its own
			// account checker has had a turn - see rejectGrace's doc comment.
			// Only a rejection that has held for the full grace window is
			// reported as one; everything before that reads as still queued,
			// because it might still turn out to be exactly that.
			if first, seen := firstFail[h]; seen && now.Sub(first) > rejectGrace {
				p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusRejected,
					Detail: "JDownloader could not validate this login"}
			} else {
				p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusQueued,
					Detail: "JDownloader is still checking this login"}
			}
		}
	}
	for _, a := range actual {
		h := normalizeHost(a.Hostname)
		if !wanted[h] {
			p.Remove = append(p.Remove, a.UUID)
		}
	}
	return p
}

// desired reads Store into the plain-credential rows plan needs, skipping
// anything that no longer carries a secret (Store.Remove leaves a zero
// Credential behind exactly as accounts.Store always has for a cleared one).
func (r *Reconciler) desired() []DesiredLogin {
	var out []DesiredLogin
	for _, h := range r.store.Hosts() {
		cred, err := r.store.Get(h)
		if err != nil || cred.IsZero() {
			continue
		}
		out = append(out, DesiredLogin{Host: h, Username: cred.Username, Password: cred.Password})
	}
	return out
}

// Reconcile runs one pass: read Store, ask JD, add what is missing, remove
// what is no longer desired, and update each host's routing priority
// (internal/resolver/jd.SetHostActive) to match what JD just confirmed. It
// returns the plan it acted on so a caller (and a test) can see exactly what
// happened, and errJDNotConfigured when no JD sidecar is set up at all -
// which Run treats as quiet, not a failure to log on every tick.
func (r *Reconciler) Reconcile(ctx context.Context) (Plan, error) {
	base := strings.TrimSpace(r.jdBase())
	if base == "" {
		return Plan{}, errJDNotConfigured
	}
	jd := r.newJD(base)

	desired := r.desired()
	actual, err := jd.queryAccounts(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("hosterauth: querying JD's accounts: %w", err)
	}

	now := time.Now()
	r.mu.Lock()
	p := plan(desired, actual, r.firstFail, now)
	updateFirstFail(r.firstFail, p, now)
	r.states = p.States
	r.mu.Unlock()

	for _, d := range p.Add {
		if _, err := jd.addAccount(ctx, d.Host, d.Username, d.Password); err != nil {
			log.Printf("hosterauth: adding %s to JD failed: %v", d.Host, err)
		}
	}
	if len(p.Remove) > 0 {
		if err := jd.removeAccounts(ctx, p.Remove); err != nil {
			log.Printf("hosterauth: removing %d stale JD account(s) failed: %v", len(p.Remove), err)
		}
	}
	for host, st := range p.States {
		jdresolver.SetHostActive(host, st.Status == StatusActive)
	}

	// The hosts JD has a PLUGIN for, pushed on the same pass and for a related
	// but different purpose: a host in this list is one JD can fetch from in
	// free mode - the wait, the countdown, the captcha - and so beats an
	// anonymous GET even when nobody has a login for it. See
	// jd.PriorityFor for what that ranking prevents.
	//
	// Failure here is quiet and non-fatal: the accounts half of this pass has
	// already been applied by the time we get here, and a routing hint that did
	// not refresh is a worse reason to discard it than to keep the last one.
	if hosts, err := jd.listPremiumHosters(ctx); err != nil {
		log.Printf("hosterauth: could not read JD's hoster list (%v); keeping the last routing hints", err)
	} else {
		jdresolver.SetKnownHosts(hosts)
	}
	return p, nil
}

// updateFirstFail keeps firstFail in step with what this pass just saw: a
// host newly at StatusRejected or still-checking-with-JD-reporting-invalid
// gets a first-seen timestamp if it does not have one yet; a host that came
// back active, or is no longer desired at all, has its timestamp cleared, so
// a login that starts working - or is removed and reconfigured later - does
// not inherit a stale grace-window clock from a previous failure.
func updateFirstFail(firstFail map[string]time.Time, p Plan, now time.Time) {
	seen := map[string]bool{}
	for host, st := range p.States {
		h := normalizeHost(host)
		seen[h] = true
		if st.Status == StatusQueued && st.Detail == "JDownloader is still checking this login" || st.Status == StatusRejected {
			if _, ok := firstFail[h]; !ok {
				firstFail[h] = now
			}
			continue
		}
		delete(firstFail, h)
	}
	for h := range firstFail {
		if !seen[h] {
			delete(firstFail, h)
		}
	}
}

// States lists every stored login's current status, filling in a login
// Reconcile has never reported on yet (a fresh save, before the first pass
// has run) as queued rather than leaving it out - the row exists, so it has
// to show something, and "queued, waiting for the next check" is what it
// actually is.
func (r *Reconciler) States() []LoginState {
	r.mu.Lock()
	defer r.mu.Unlock()
	hosts := r.store.Hosts()
	out := make([]LoginState, 0, len(hosts))
	for _, h := range hosts {
		if st, ok := r.states[h]; ok {
			out = append(out, st)
			continue
		}
		cred, _ := r.store.Get(h)
		out = append(out, LoginState{Host: h, Username: cred.Username, Status: StatusQueued, Detail: "waiting for the next check"})
	}
	return out
}

// Hosts returns the "add a login" picker's list - JD's own premium-hoster
// list when JD is reachable, curatedHosts otherwise. Whether the live list or
// the fallback answered is not exposed here: both are just a list of ids to
// pick from, and the picker does not need to explain which source they came
// from to be useful.
func (r *Reconciler) Hosts(ctx context.Context) []Host {
	if base := strings.TrimSpace(r.jdBase()); base != "" {
		if list, err := r.newJD(base).listPremiumHosters(ctx); err == nil && len(list) > 0 {
			out := make([]Host, len(list))
			for i, h := range list {
				out[i] = Host{ID: h, Label: h}
			}
			return out
		}
	}
	out := make([]Host, len(curatedHosts))
	for i, h := range curatedHosts {
		out[i] = Host{ID: h, Label: h}
	}
	return out
}

// SetLogin stores (or updates) one host's login. A password equal to
// accounts.Redacted is read the same way every other credential form in this
// app reads it: "the caller did not retype this", so re-saving a row whose
// password field a browser only ever showed as asterisks does not seal the
// literal placeholder in place of the real secret.
func (r *Reconciler) SetLogin(host, username, password string) error {
	host = normalizeHost(host)
	if host == "" {
		return errors.New("hosterauth: host is required")
	}
	prev, err := r.store.Get(host)
	if err != nil {
		return err
	}
	cred := accounts.Credential{Username: username, Password: password}.WithSecretsFrom(prev)
	return r.store.Set(host, cred)
}

// RemoveLogin clears host's stored login, its cached status and its routing
// priority - a login the user deleted here must stop outranking Direct for
// that host immediately, not wait for the next reconcile pass to notice.
// JD's own account is dropped by the next Reconcile's plan (host will no
// longer be in desired), not here directly, so a Reconcile that is mid-flight
// when this is called cannot race a removal against an add of the same host.
func (r *Reconciler) RemoveLogin(host string) error {
	host = normalizeHost(host)
	if err := r.store.Remove(host); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.states, host)
	delete(r.firstFail, host)
	r.mu.Unlock()
	jdresolver.SetHostActive(host, false)
	return nil
}
