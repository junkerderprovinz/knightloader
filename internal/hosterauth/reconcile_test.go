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

// ---- plan(): the pure add/remove/status decision -------------------------

func TestPlanAddsWhatJDIsMissing(t *testing.T) {
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	p := plan(desired, nil, map[string]time.Time{}, time.Now())
	if len(p.Add) != 1 || p.Add[0].Host != "rapidgator.net" {
		t.Fatalf("Add = %+v, want the one desired host that JD does not have", p.Add)
	}
	if len(p.Remove) != 0 {
		t.Errorf("Remove = %v, want none - nothing was actually there to remove", p.Remove)
	}
	if got := p.States["rapidgator.net"].Status; got != StatusQueued {
		t.Errorf("status = %q, want %q for a login just added", got, StatusQueued)
	}
}

func TestPlanRemovesWhatIsNoLongerDesired(t *testing.T) {
	actual := []jdAccount{{UUID: 7, Hostname: "uploaded.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(nil, actual, map[string]time.Time{}, time.Now())
	if len(p.Remove) != 1 || p.Remove[0] != 7 {
		t.Fatalf("Remove = %v, want [7] - the user deleted this login in KL", p.Remove)
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
		t.Fatalf("Add=%v Remove=%v, want neither - this login is desired and already present", p.Add, p.Remove)
	}
}

// TestPlanHostMatchIsCaseAndWWWInsensitive pins the normalizeHost contract
// plan's byHost map depends on: a desired host and JD's reported hostname
// must compare equal regardless of case or a leading "www.".
func TestPlanHostMatchIsCaseAndWWWInsensitive(t *testing.T) {
	desired := []DesiredLogin{{Host: "WWW.Rapidgator.NET", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: true}}}
	p := plan(desired, actual, map[string]time.Time{}, time.Now())
	if len(p.Add) != 0 {
		t.Errorf("Add = %v, want none - www./case must not make this look missing", p.Add)
	}
}

// ---- the three-way status: queued must never collapse into rejected ------

// TestPlanQueuedWithinGraceNotRejected is requirement 2, pinned directly: a
// login JD has not yet validated must read as "still checking", never as
// "wrong password", until the grace window has actually elapsed.
func TestPlanQueuedWithinGraceNotRejected(t *testing.T) {
	now := time.Now()
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: false}}}
	firstFail := map[string]time.Time{"rapidgator.net": now.Add(-1 * time.Minute)} // well inside rejectGrace (2m)

	p := plan(desired, actual, firstFail, now)
	got := p.States["rapidgator.net"]
	if got.Status != StatusQueued {
		t.Fatalf("status = %q, want %q - JD has not had rejectGrace to validate this yet", got.Status, StatusQueued)
	}
}

// TestPlanRejectedAfterGraceElapses is the other half: once the grace window
// has genuinely passed with JD still saying invalid, the status must flip to
// rejected so the user is told to fix the password instead of waiting forever.
func TestPlanRejectedAfterGraceElapses(t *testing.T) {
	now := time.Now()
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	actual := []jdAccount{{UUID: 1, Hostname: "rapidgator.net", InfoMap: &jdAccountInfo{Valid: false}}}
	firstFail := map[string]time.Time{"rapidgator.net": now.Add(-3 * time.Minute)} // past rejectGrace (2m)

	p := plan(desired, actual, firstFail, now)
	got := p.States["rapidgator.net"]
	if got.Status != StatusRejected {
		t.Fatalf("status = %q, want %q - the grace window has elapsed", got.Status, StatusRejected)
	}
}

// TestPlanNotYetOnJDIsQueuedNotRejected is the other shape of "not active
// yet": a login this reconciler is about to add for the first time (present
// in neither actual nor firstFail) must never read as rejected - it hasn't
// even reached JD yet, let alone been checked.
func TestPlanNotYetOnJDIsQueuedNotRejected(t *testing.T) {
	desired := []DesiredLogin{{Host: "rapidgator.net", Username: "u", Password: "p"}}
	p := plan(desired, nil, map[string]time.Time{}, time.Now())
	if got := p.States["rapidgator.net"].Status; got != StatusQueued {
		t.Fatalf("status = %q, want %q for a login not yet sent to JD at all", got, StatusQueued)
	}
}

// ---- Reconcile against a fake JD client (never a real one) ---------------

// fakeJD is jdAccounts without a network - the accounts it "has" are exactly
// what the test seeds it with, and addAccount/removeAccounts record what
// Reconcile asked for so the test can assert on the calls themselves, not
// just their side effects.
type fakeJD struct {
	accounts   []jdAccount
	nextUUID   int64
	added      []DesiredLogin // hoster/username/password exactly as addAccount received them
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
		jdBase:    func() string { return "http://127.0.0.1:0" }, // never dialled: newJD is overridden below
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
}

// ---- the credential never leaves as anything but the one addAccount call -

// TestLoginStateNeverCarriesTheCredential is a structural guarantee as much
// as a test: LoginState (what every API response and every log line built
// from Reconciler's own state can see) has no field a password could occupy.
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

// TestReconcileErrorsNeverContainTheCredential drives a failing addAccount
// through Reconcile with a real-shaped username/password and checks the
// error text - the one place a bug could format a credential into a message
// meant for a log line.
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
// state after a test that set it, so one test cannot leave state another test
// (or resolver_test.go's own tests) observes.
func setHostActiveForTest(t *testing.T, host string, active bool) {
	t.Helper()
	jdresolver.SetHostActive(host, active)
}
