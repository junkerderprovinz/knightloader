package hosterauth

// Desired state (what Store holds) against actual state (JD's own account
// list), reconciled by adding what JD is missing and removing what was deleted
// here. It runs as a loop because a recreated or updated JD sidecar comes back
// with an empty account list, and downloads then fall back to free-user speeds
// without an error anywhere.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// LoginStatus is the state one stored login is in against JD. Queued and
// rejected stay apart because a queued login is about to start working on its
// own while a rejected one needs a different password.
type LoginStatus string

const (
	// StatusQueued is desired here and either not yet present on JD, or present
	// but not yet validated by JD's own account checker.
	StatusQueued LoginStatus = "queued"
	// StatusActive is confirmed present and valid on JD.
	StatusActive LoginStatus = "active"
	// StatusRejected is present on JD and has stayed invalid past rejectGrace.
	StatusRejected LoginStatus = "rejected"
	// StatusOff is a login the user switched off. JD does not have it at all,
	// so it is none of the other three.
	StatusOff LoginStatus = "off"
)

// The codes a LoginState's Detail comes with, which the accounts page words in
// the reader's language.
const (
	codeAdding   = "adding"
	codeChecking = "checking"
	codeInvalid  = "invalid"
	codeOff      = "off"
	codeWaiting  = "waiting"
)

// LoginState is one row the accounts page shows. It has no field that could
// carry a password, so a snapshot cannot leak one even where a redaction step
// is forgotten.
type LoginState struct {
	Host     string      `json:"host"`
	Username string      `json:"username"`
	Status   LoginStatus `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	// Code names Detail, so the interface can translate it.
	Code string `json:"code,omitempty"`
	// Enabled is the user's own switch: Status says what JD thinks, Enabled
	// says whether JD was ever asked. The row needs both.
	Enabled bool `json:"enabled"`

	// What JD knows about the account itself. Every field is optional: JD
	// answers -1 or 0 for an account it has nothing to say about, and that has
	// to reach the page as "nothing said" rather than as a zero.
	//
	// Tier is "premium" or "free", derived from validUntil and trafficMax,
	// the only two things JD reports about a plan. "" means JD has not
	// answered yet.
	Tier string `json:"tier,omitempty"`
	// Expiry is RFC3339, or "" for an account with nothing to expire.
	Expiry string `json:"expiry,omitempty"`
	// TrafficLeft and TrafficMax are bytes, 0 when JD states neither.
	TrafficLeft int64 `json:"trafficLeft,omitempty"`
	TrafficMax  int64 `json:"trafficMax,omitempty"`

	// Multihoster is set by internal/app like Host.Multihoster, so the page can
	// list the login among the debrid accounts.
	Multihoster bool `json:"multihoster,omitempty"`
}

// DesiredLogin is one row Store wants JD to have. The password travels only as
// far as the one addAccount call it is used for.
type DesiredLogin struct {
	Host     string
	Username string
	Password string
}

// rejectGrace is how long a JD account may sit at valid=false before Reconcile
// reads that as a rejection rather than "still checking". JD validates a
// freshly added account asynchronously through its own account checker and
// reports it invalid in the meantime. Long enough that a hoster's checker
// queue does not read as a wrong password, short enough that a wrong password
// does not sit at "still checking".
const rejectGrace = 2 * time.Minute

// reconcileInterval is how often Run re-checks JD without being asked. Nothing
// here is told when JD comes back after being recreated or updated, so it
// keeps asking instead of trying to detect that moment.
const reconcileInterval = 30 * time.Second

var errJDNotConfigured = errors.New("hosterauth: no JD sidecar is configured (KL_JD is unset)")

// curatedHosts keeps the "add a login" picker usable while JD is unreachable
// or none is configured yet. JD's own listPremiumHoster is the live list and
// wins whenever it answers.
var curatedHosts = []string{
	"rapidgator.net", "uploaded.net", "nitroflare.com", "turbobit.net",
	"keep2share.cc", "katfile.com", "ddownload.com", "1fichier.com",
	"mega.nz", "filefactory.com", "hitfile.net", "fikper.com",
}

// Curated reports whether host is one of curatedHosts or an alias domain of
// one, such as rg.to. Every one of them is a file hoster, which routing needs
// to know before JD has sent its own list.
func Curated(host string) bool {
	return slices.Contains(curatedHosts, hostalias.Canonical(host))
}

// Host is one entry the "add a login" picker offers.
type Host struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Multihoster marks a service that unlocks other hosts rather than hosting
	// files itself. Filled in by internal/app from a list kept by hand, see
	// app_multihoster.go.
	//
	// omitempty: only a handful of the hosts carry it, and an explicit
	// `"multihoster":false` on every other row is noise on a long response.
	Multihoster bool `json:"multihoster,omitempty"`
}

// Reconciler owns one App's hoster-login state: the desired side (Store) and
// what JD said on the last reconcile pass.
type Reconciler struct {
	store  *Store
	jdBase func() string // read live, so a changed JD address is picked up without a restart
	// newJD builds the jdAccounts a reconcile pass talks to, as a field so a
	// test can inject a fake.
	newJD func(base string) jdAccounts
	// Enabled answers the user's own on/off switch for one host. nil means
	// every stored login is on.
	//
	// A function rather than a flag stored here: the switch lives in the app's
	// own account_meta.json beside every other account's Enabled (see
	// App.accountEnabled), and a copy here would be a second thing to keep in
	// step with it.
	Enabled func(host string) bool

	// Off answers whether JD is switched off on the modules page. A pass then
	// only reads JD's hoster list: without it a hoster link would rank below
	// the direct download after a restart and fetch the hoster's page. nil
	// means on.
	Off func() bool

	mu        sync.Mutex
	states    map[string]LoginState
	firstFail map[string]time.Time // host -> when Reconcile first saw it present but invalid
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
// done. The first pass gives a fresh boot real state instead of an interval's
// worth of "queued"; the loop pushes every login back onto a JD container that
// came back with its account list wiped.
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

// Plan is what one reconcile pass decided.
type Plan struct {
	Add    []DesiredLogin
	Remove []int64
	States map[string]LoginState
}

// plan compares desired against actual and decides what to add, what to
// remove and each desired host's status. It reads no store, makes no HTTP call
// and mutates nothing, so the decision can be tested against fixed inputs.
//
// A desired host is matched to a JD account on hostname alone, case- and
// www.-insensitively and with alias domains folded (accountKey).
// addAccount's premiumHoster argument is resolved through JD's own
// PluginFinder.assignHost before being stored (see jdclient.go), so a login
// saved as rg.to comes back as rapidgator.net; compared as typed, it would
// look missing on every pass and be added again each time.
func plan(desired []DesiredLogin, actual []jdAccount, firstFail map[string]time.Time, now time.Time) Plan {
	byHost := map[string]jdAccount{}
	for _, a := range actual {
		byHost[accountKey(a.Hostname)] = a
	}
	wanted := map[string]bool{}
	p := Plan{States: map[string]LoginState{}}
	for _, d := range desired {
		h := accountKey(d.Host)
		wanted[h] = true
		acc, present := byHost[h]
		switch {
		case !present:
			p.Add = append(p.Add, d)
			p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusQueued,
				Detail: "waiting for JDownloader to accept this login", Code: codeAdding}
		case acc.InfoMap != nil && acc.InfoMap.Valid:
			st := LoginState{Host: d.Host, Username: d.Username, Status: StatusActive}
			describeAccount(&st, acc.InfoMap)
			p.States[d.Host] = st
		default:
			// JD reports a freshly added account as invalid until its own
			// account checker has had a turn, so only a rejection that held
			// for the full grace window is reported as one.
			if first, seen := firstFail[h]; seen && now.Sub(first) > rejectGrace {
				p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusRejected,
					Detail: "JDownloader could not validate this login", Code: codeInvalid}
			} else {
				p.States[d.Host] = LoginState{Host: d.Host, Username: d.Username, Status: StatusQueued,
					Detail: "JDownloader is still checking this login", Code: codeChecking}
			}
		}
	}
	for _, a := range actual {
		h := accountKey(a.Hostname)
		if !wanted[h] {
			p.Remove = append(p.Remove, a.UUID)
		}
	}
	return p
}

// desired reads Store into the plain-credential rows plan needs, skipping
// anything that no longer carries a secret and anything the user switched off.
//
// Leaving a switched-off login out is the mechanism: plan then sees a JD
// account nobody wants and puts it in Remove, so JD stops using that hoster
// within one pass while the credential stays sealed in the store.
func (r *Reconciler) desired() []DesiredLogin {
	var out []DesiredLogin
	for _, h := range r.store.Hosts() {
		cred, err := r.store.Get(h)
		if err != nil || cred.IsZero() {
			continue
		}
		if !r.enabled(h) {
			continue
		}
		out = append(out, DesiredLogin{Host: h, Username: cred.Username, Password: cred.Password})
	}
	return out
}

// enabled is Enabled with its nil case folded in.
func (r *Reconciler) enabled(host string) bool {
	if r.Enabled == nil {
		return true
	}
	return r.Enabled(host)
}

// Reconcile runs one pass: read Store, ask JD, add what is missing, remove
// what is no longer desired, and update each host's routing priority
// (internal/resolver/jd.SetHostActive) to match what JD just confirmed. It
// returns the plan it acted on, and errJDNotConfigured when no JD sidecar is
// set up, which Run keeps out of the log.
func (r *Reconciler) Reconcile(ctx context.Context) (Plan, error) {
	base := strings.TrimSpace(r.jdBase())
	if base == "" {
		return Plan{}, errJDNotConfigured
	}
	jd := r.newJD(base)
	if r.Off != nil && r.Off() {
		if hosts, err := jd.listPremiumHosters(ctx); err == nil {
			jdresolver.SetKnownHosts(hosts)
		}
		return Plan{}, nil
	}

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

	// The hosts JD has a plugin for, pushed on the same pass for a related
	// purpose: JD can fetch those in free mode (the wait, the countdown, the
	// captcha), so they beat an anonymous GET even when nobody has a login for
	// them. See jd.PriorityFor.
	//
	// A failure here is not fatal: the accounts half of the pass is already
	// applied, and keeping the last routing hints beats discarding them.
	if hosts, err := jd.listPremiumHosters(ctx); err != nil {
		log.Printf("hosterauth: could not read JD's hoster list (%v); keeping the last routing hints", err)
	} else {
		jdresolver.SetKnownHosts(hosts)
	}
	return p, nil
}

// describeAccount folds what JD says about an account into the row: the plan,
// when it runs out, and how much traffic is left.
//
// The plan is derived, because JD reports no field called "premium". It does
// report validUntil, a real timestamp on a paid account and -1 on a free one,
// and trafficMax, a quota only a plan with one carries. Either means premium,
// neither means free.
//
// JD states every timestamp in milliseconds. Reading validUntil as seconds
// would put a 2027 expiry in 1970 and draw a working account as expired.
func describeAccount(st *LoginState, info *jdAccountInfo) {
	if info == nil {
		return
	}
	st.TrafficLeft, st.TrafficMax = info.TrafficLeft, info.TrafficMax
	if info.ValidUntil > 0 {
		st.Expiry = time.UnixMilli(info.ValidUntil).UTC().Format(time.RFC3339)
	}
	if info.ValidUntil > 0 || info.TrafficMax > 0 {
		st.Tier = "premium"
	} else {
		st.Tier = "free"
	}
}

// updateFirstFail keeps firstFail in step with what this pass saw. A host that
// JD reports invalid gets a first-seen timestamp; one that came back active or
// is no longer desired has its timestamp cleared, so a login does not inherit
// a stale grace-window clock from an earlier failure.
func updateFirstFail(firstFail map[string]time.Time, p Plan, now time.Time) {
	seen := map[string]bool{}
	for host, st := range p.States {
		h := accountKey(host)
		seen[h] = true
		if st.Status == StatusQueued && st.Code == codeChecking || st.Status == StatusRejected {
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

// States lists every stored login's current status, reporting a login no pass
// has covered yet as queued. A switched-off login is answered from here alone:
// no pass writes a state for it, and its last state would still read "active"
// for a login JD has been told to drop.
func (r *Reconciler) States() []LoginState {
	r.mu.Lock()
	defer r.mu.Unlock()
	hosts := r.store.Hosts()
	out := make([]LoginState, 0, len(hosts))
	for _, h := range hosts {
		cred, _ := r.store.Get(h)
		if !r.enabled(h) {
			out = append(out, LoginState{Host: h, Username: cred.Username, Status: StatusOff,
				Detail: "switched off, so JDownloader is not using this login", Code: codeOff})
			continue
		}
		if st, ok := r.states[h]; ok {
			st.Enabled = true
			out = append(out, st)
			continue
		}
		out = append(out, LoginState{Host: h, Username: cred.Username, Status: StatusQueued,
			Detail: "waiting for the next check", Code: codeWaiting, Enabled: true})
	}
	return out
}

// Hosts returns the "add a login" picker's list: JD's own premium-hoster list
// when JD is reachable, curatedHosts otherwise.
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

// SetLogin stores or updates one host's login. A password equal to
// accounts.Redacted means the caller did not retype it, so re-saving a row
// whose password field only ever showed asterisks keeps the stored secret.
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
// priority, so a deleted login stops outranking Direct at once instead of
// waiting for the next pass. JD's own account is dropped by that next pass,
// which keeps a Reconcile in flight from racing a removal against an add of
// the same host.
func (r *Reconciler) RemoveLogin(host string) error {
	host = normalizeHost(host)
	if err := r.store.Remove(host); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.states, host)
	delete(r.firstFail, accountKey(host))
	r.mu.Unlock()
	jdresolver.SetHostActive(host, false)
	return nil
}
