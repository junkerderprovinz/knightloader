package hosterauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

func TestPlanAddsWhatJDIsMissing(t *testing.T) {
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	p := plan(desired, nil, map[string]time.Time{}, time.Now())
	if len(p.Add) != 1 || p.Add[0].Host != "rapidgator.net" {
		t.Fatalf("Add = %+v, want the one desired host that JD does not have", p.Add)
	}
	if len(p.Remove) != 0 {
		t.Errorf("Remove = %v, want none", p.Remove)
	}
	if got := p.States["rapidgator.net"].Status; got != StatusQueued {
		t.Errorf("status = %q, want %q for a login just added", got, StatusQueued)
	}
}

func TestPlanRemovesWhatIsNoLongerDesired(t *testing.T) {
	actual := []jdAccount{{UUID: 7, Hostname: "uploaded.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(nil, actual, map[string]time.Time{}, time.Now())
	if len(p.Remove) != 1 || p.Remove[0] != 7 {
		t.Fatalf("Remove = %v, want [7]", p.Remove)
	}
	if len(p.Add) != 0 {
		t.Errorf("Add = %v, want none", p.Add)
	}
}

func TestPlanKeepsDesiredAndPresentAlone(t *testing.T) {
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(desired, actual, map[string]time.Time{}, time.Now())
	if len(p.Add) != 0 || len(p.Remove) != 0 {
		t.Fatalf("Add=%v Remove=%v, want neither", p.Add, p.Remove)
	}
}

// A desired host and JD's reported hostname compare equal regardless of case
// or a leading "www.", which is what plan's byHost map depends on.
func TestPlanHostMatchIsCaseAndWWWInsensitive(t *testing.T) {
	desired := []DesiredLogin{{Host: "WWW.Rapidgator.NET", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(desired, actual, map[string]time.Time{}, time.Now())
	if len(p.Add) != 0 {
		t.Errorf("Add = %v, want none; www. and case must not make this look missing", p.Add)
	}
}

func TestCuratedKnowsAHosterByItsAliasDomains(t *testing.T) {
	for _, host := range []string{"rapidgator.net", "rg.to", "www.rg.to", "ul.to", "desfichiers.com"} {
		if !Curated(host) {
			t.Errorf("Curated(%q) = false, want true", host)
		}
	}
	if Curated("example.com") {
		t.Error("Curated(example.com) = true, want false")
	}
}

// A login JD has not yet validated reads as "still checking" until the grace
// window has elapsed, never as "wrong password".
func TestPlanQueuedWithinGraceNotRejected(t *testing.T) {
	now := time.Now()
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: false}}}
	firstFail := map[string]time.Time{"rapidgator.net": now.Add(-1 * time.Minute)} // inside rejectGrace

	p := plan(desired, actual, firstFail, now)
	got := p.States["rapidgator.net"]
	if got.Status != StatusQueued {
		t.Fatalf("status = %q, want %q; JD has not had rejectGrace to validate this yet", got.Status, StatusQueued)
	}
	if got.Code != codeChecking {
		t.Errorf("code = %q, want %q, which the next pass reads to keep the grace clock", got.Code, codeChecking)
	}
}

// Once the grace window has passed with JD still saying invalid, the status
// flips to rejected so the user is told to fix the password.
func TestPlanRejectedAfterGraceElapses(t *testing.T) {
	now := time.Now()
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: false}}}
	firstFail := map[string]time.Time{"rapidgator.net": now.Add(-3 * time.Minute)} // past rejectGrace

	p := plan(desired, actual, firstFail, now)
	got := p.States["rapidgator.net"]
	if got.Status != StatusRejected {
		t.Fatalf("status = %q, want %q after the grace window", got.Status, StatusRejected)
	}
	if got.Code != codeInvalid {
		t.Errorf("code = %q, want %q", got.Code, codeInvalid)
	}
}

// A login that is in neither actual nor firstFail has not reached JD at all,
// so it reads as queued rather than rejected.
func TestPlanNotYetOnJDIsQueuedNotRejected(t *testing.T) {
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	p := plan(desired, nil, map[string]time.Time{}, time.Now())
	got := p.States["rapidgator.net"]
	if got.Status != StatusQueued {
		t.Fatalf("status = %q, want %q for a login not yet sent to JD at all", got.Status, StatusQueued)
	}
	if got.Code != codeAdding {
		t.Errorf("code = %q, want %q", got.Code, codeAdding)
	}
}

// fakeJD is jdAccounts without a network. It holds the accounts the test seeds
// it with and records the calls Reconcile makes.
type fakeJD struct {
	accounts   []jdAccount
	nextUUID   int64
	added      []DesiredLogin // exactly as addAccount received them
	removedIDs []int64
	hosters    []string
	queryErr   error
}

func (f *fakeJD) queryAccounts(context.Context) ([]jdAccount, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	out := make([]jdAccount, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeJD) addAccount(_ context.Context, hoster, username, password string) (bool, error) {
	f.added = append(f.added, DesiredLogin{Host: hoster, Username: username, Password: password})
	f.nextUUID++
	f.accounts = append(f.accounts, jdAccount{UUID: f.nextUUID, Hostname: hoster, InfoMap: &jdAccountInfo{Username: username, Valid: false}})
	return true, nil
}

func (f *fakeJD) removeAccounts(_ context.Context, ids []int64) error {
	f.removedIDs = append(f.removedIDs, ids...)
	kept := f.accounts[:0]
	for _, a := range f.accounts {
		remove := false
		for _, id := range ids {
			if a.UUID == id {
				remove = true
				break
			}
		}
		if !remove {
			kept = append(kept, a)
		}
	}
	f.accounts = kept
	return nil
}

func (f *fakeJD) listPremiumHosters(context.Context) ([]string, error) { return f.hosters, nil }

func newTestReconciler(t *testing.T, jd jdAccounts) (*Reconciler, *Store) {
	t.Helper()
	acc, err := accounts.Open(t.TempDir())
	if err != nil {
		t.Fatalf("accounts.Open: %v", err)
	}
	store := NewStore(acc)
	r := &Reconciler{
		store:     store,
		jdBase:    func() string { return "http://127.0.0.1:0" }, // never dialled, newJD is overridden below
		newJD:     func(string) jdAccounts { return jd },
		states:    map[string]LoginState{},
		firstFail: map[string]time.Time{},
	}
	return r, store
}

func TestReconcileAddsAMissingAccount(t *testing.T) {
	r, store := newTestReconciler(t, &fakeJD{})
	if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	p, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(p.Add) != 1 || p.Add[0].Host != "rapidgator.net" {
		t.Fatalf("Add = %+v, want rapidgator.net added", p.Add)
	}
}

// A switched-off JD still tells which hosts it knows, since routing needs that
// list after a restart, but it is given no account and loses none.
func TestASwitchedOffJDOnlyHasItsHosterListRead(t *testing.T) {
	const host = "modules-off-known.example"
	fake := &fakeJD{
		hosters:  []string{host},
		accounts: []jdAccount{{UUID: 7, Hostname: "uploaded.net", InfoMap: &jdAccountInfo{Valid: true}}},
	}
	r, store := newTestReconciler(t, fake)
	r.Off = func() bool { return true }
	if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { jdresolver.SetKnownHosts(nil) })

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(fake.added) != 0 || len(fake.removedIDs) != 0 {
		t.Fatalf("added %v, removed %v while JD is switched off; want nothing", fake.added, fake.removedIDs)
	}
	if jdresolver.PriorityFor("https://"+host+"/file.bin") <= 40 {
		t.Error("the hoster list was not read, so the host ranks below the direct download")
	}
}

func TestReconcileRemovesANoLongerDesiredAccount(t *testing.T) {
	fake := &fakeJD{accounts: []jdAccount{{UUID: 42, Hostname: "uploaded.net", InfoMap: &jdAccountInfo{Valid: true}}}}
	r, _ := newTestReconciler(t, fake)
	// Nothing stored in KL for uploaded.net: the user removed it here.

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(fake.removedIDs) != 1 || fake.removedIDs[0] != 42 {
		t.Fatalf("removedIDs = %v, want [42]", fake.removedIDs)
	}
}

func TestReconcileMarksHostActiveOncePresentAndValid(t *testing.T) {
	fake := &fakeJD{accounts: []jdAccount{{UUID: 1, Hostname: "priority-reconcile-test.example", InfoMap: &jdAccountInfo{Valid: true}}}}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("priority-reconcile-test.example", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	defer setHostActiveForTest(t, "priority-reconcile-test.example", false)

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := r.States()[0]; got.Status != StatusActive {
		t.Fatalf("status = %q, want %q", got.Status, StatusActive)
	}
}

func TestReconcileNoJDConfiguredIsAQuietError(t *testing.T) {
	r, store := newTestReconciler(t, &fakeJD{})
	r.jdBase = func() string { return "" }
	_ = store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"})

	_, err := r.Reconcile(context.Background())
	if !errors.Is(err, errJDNotConfigured) {
		t.Fatalf("err = %v, want errJDNotConfigured", err)
	}
	// The diagnostics log shows the error, so it names the module as the pages do.
	if !strings.Contains(err.Error(), "JDownloader backend") {
		t.Errorf("err = %q, which does not name the JDownloader backend", err)
	}
}

// LoginState is what every API response and every log line built from
// Reconciler's state can see, and it has no field a password could occupy.
func TestLoginStateNeverCarriesTheCredential(t *testing.T) {
	const secret = "hunter2-do-not-leak-me"
	st := LoginState{Host: "rapidgator.net", Username: "u", Status: StatusQueued, Detail: "waiting"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("marshalled LoginState contains the secret: %s", b)
	}
}

// A failing pass formats an error that ends up in a log line, so it is checked
// against a real-shaped username and password.
func TestReconcileErrorsNeverContainTheCredential(t *testing.T) {
	const user, pass = "victim-user", "hunter2-do-not-leak-me"
	fake := &fakeJD{queryErr: errors.New("jd accounts/queryAccounts: HTTP 500")}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("rapidgator.net", accounts.Credential{Username: user, Password: pass}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("want an error from the failing queryAccounts")
	}
	if strings.Contains(err.Error(), pass) || strings.Contains(err.Error(), user) {
		t.Fatalf("Reconcile error leaked the credential: %v", err)
	}
}

// setHostActiveForTest clears internal/resolver/jd's package-level active-host
// state, so one test does not leave state another one observes.
func setHostActiveForTest(t *testing.T, host string, active bool) {
	t.Helper()
	jdresolver.SetHostActive(host, active)
}

// A switched-off login is not in `desired`, so plan sees a JD account nobody
// wants and asks for it to go. Leaving it in JD and only greying the row would
// change what the page says and nothing about what downloads.
func TestDisabledLoginIsRemovedFromJD(t *testing.T) {
	fake := &fakeJD{accounts: []jdAccount{{UUID: 9, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}
	off := map[string]bool{"rapidgator.net": true}
	r.Enabled = func(host string) bool { return !off[host] }

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(fake.removedIDs) != 1 || fake.removedIDs[0] != 9 {
		t.Fatalf("removed = %v, want [9]; a switched-off login must leave JD's account list", fake.removedIDs)
	}
	// Off is not delete: the credential stays.
	cred, err := store.Get("rapidgator.net")
	if err != nil || cred.IsZero() {
		t.Fatalf("credential after switching off = %+v (err %v), want it kept", cred, err)
	}
}

// States answers from the switch, not from the last state a reconcile pass
// left behind. The cached one would read "active" for a login JD was told to
// drop.
func TestDisabledLoginReadsAsOffNotAsActive(t *testing.T) {
	fake := &fakeJD{accounts: []jdAccount{{UUID: 3, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}
	// Writes the "active" state the assertion below must not come back to.
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := r.States(); len(got) != 1 || got[0].Status != StatusActive || !got[0].Enabled {
		t.Fatalf("state while on = %+v, want active and enabled", got)
	}

	r.Enabled = func(string) bool { return false }
	got := r.States()
	if len(got) != 1 {
		t.Fatalf("States returned %d rows, want the one stored login", len(got))
	}
	if got[0].Status != StatusOff || got[0].Code != codeOff {
		t.Errorf("status = %q with code %q, want %q with code %q", got[0].Status, got[0].Code, StatusOff, codeOff)
	}
	if got[0].Enabled {
		t.Error("Enabled is true on a switched-off row")
	}
	if got[0].Username != "u" {
		t.Errorf("username = %q, want it still shown while switched off", got[0].Username)
	}
}

// A nil Enabled means every stored login is on.
func TestEnabledNilMeansEverythingOn(t *testing.T) {
	fake := &fakeJD{}
	r, store := newTestReconciler(t, fake)
	if err := store.Set("rapidgator.net", accounts.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(fake.added) != 1 {
		t.Fatalf("added = %+v, want the one login pushed to JD with no Enabled set", fake.added)
	}
}

// JD stores an account under the hoster's main domain whatever it was added
// as, so a login saved under an alias must match it rather than be added again
// on every pass.
func TestPlanMatchesALoginSavedUnderAnAlias(t *testing.T) {
	desired := []DesiredLogin{{Host: "rg.to", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(desired, actual, map[string]time.Time{}, time.Now())
	if len(p.Add) != 0 || len(p.Remove) != 0 {
		t.Fatalf("Add=%v Remove=%v, want neither for rg.to against JD's rapidgator.net", p.Add, p.Remove)
	}
	if got := p.States["rg.to"].Status; got != StatusActive {
		t.Errorf("status = %q, want %q under the host the login was saved as", got, StatusActive)
	}

	// The other way round as well: JD could report the alias.
	desired[0].Host = "rapidgator.net"
	actual[0].Hostname = "www.RG.to"
	if p := plan(desired, actual, map[string]time.Time{}, time.Now()); len(p.Add) != 0 || len(p.Remove) != 0 {
		t.Errorf("Add=%v Remove=%v, want neither for rapidgator.net against JD's rg.to", p.Add, p.Remove)
	}
}

// The grace clock is kept under the same key the match uses, or an alias login
// that JD is still checking would start a fresh clock on every pass and never
// read as rejected.
func TestAnAliasLoginIsRejectedOnceTheGraceWindowHasPassed(t *testing.T) {
	now := time.Now()
	desired := []DesiredLogin{{Host: "rg.to", Username: "u", Password: "wrong"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: false}}}
	firstFail := map[string]time.Time{}

	updateFirstFail(firstFail, plan(desired, actual, firstFail, now), now)
	later := now.Add(rejectGrace + time.Minute)
	if got := plan(desired, actual, firstFail, later).States["rg.to"].Status; got != StatusRejected {
		t.Errorf("status = %q after the grace window, want %q", got, StatusRejected)
	}
}
