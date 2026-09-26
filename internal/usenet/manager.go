package usenet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// State is where a job stands on this side.
type State string

const (
	// StateWaiting is a job no service has taken yet. Its NZB is on disk.
	StateWaiting State = "waiting"
	// StateFetching is a job a service is downloading.
	StateFetching State = "fetching"
	// StateStaged is a job whose files have become tasks.
	StateStaged State = "staged"
	// StateFailed is a job that ended without files.
	StateFailed State = "failed"
)

// Job is one NZB on its way through a service.
type Job struct {
	ID string `json:"id"`
	// Name is the release, which the NZB is sent under.
	Name string `json:"name"`
	// Package, Category, Origin and Start are what the tasks are staged with
	// once the files are there. The manager only carries them.
	Package  string    `json:"package,omitempty"`
	Category string    `json:"category,omitempty"`
	Origin   string    `json:"origin,omitempty"`
	Start    bool      `json:"start,omitempty"`
	Added    time.Time `json:"added"`
	// Dir is the folder the tasks are staged into, their package's when
	// empty. Like the fields above, the manager only carries it.
	Dir string `json:"dir,omitempty"`

	State State `json:"state"`
	// Service is the slot of the account that took the job, Label its name,
	// Remote the job's id there, and Taken when it was sent.
	Service string    `json:"service,omitempty"`
	Label   string    `json:"label,omitempty"`
	Remote  string    `json:"remote,omitempty"`
	Taken   time.Time `json:"takenAt,omitzero"`
	// Refused lists the accounts that turned the NZB down, so it is offered
	// to the others.
	Refused []string `json:"refused,omitempty"`
	// Attempts counts the calls for the job that failed in a row, the submit
	// while it waits and the delete once it is staged, and RetryAt is when the
	// next one may go out.
	Attempts int       `json:"attempts,omitempty"`
	RetryAt  time.Time `json:"retryAt,omitzero"`
	// missing is when the service's answers began to leave the job out, zero
	// while they list it.
	missing time.Time

	// Size starts as the size the NZB lists and follows the service's own
	// reading once there is one.
	Size   int64 `json:"size,omitempty"`
	Loaded int64 `json:"loaded,omitempty"`
	Speed  int64 `json:"-"`
	// Reason is why a failed job failed.
	Reason  string   `json:"reason,omitempty"`
	TaskIDs []string `json:"taskIds,omitempty"`
	// Ended is when the job was staged or failed.
	Ended time.Time `json:"ended,omitzero"`
	// Cleared is whether the service's copy is dealt with: deleted, or left
	// there after the deletes kept failing.
	Cleared bool `json:"cleared,omitempty"`
}

// ErrNoService is an NZB offered while no account can take one.
var ErrNoService = errors.New("no TorBox or Premiumize.me account here can fetch an .nzb from Usenet")

const (
	defaultInterval = 5 * time.Second
	defaultBackoff  = time.Minute
	maxBackoff      = 30 * time.Minute
	// maxAttempts is how many calls for one job may fail in a row before it is
	// given up: an unanswered submit fails the job, and a delete that keeps
	// failing leaves the copy on the account. With the backoff doubling from a
	// minute that is a few hours.
	maxAttempts = 10
	// keepEnded is how long a finished job is remembered, so Sonarr's next
	// look at its history still finds it.
	keepEnded = 7 * 24 * time.Hour
	// callTimeout bounds one call a round makes.
	callTimeout = apiTimeout
	// noUsenetFor is how long an account whose plan lacks Usenet is passed
	// over. It is tried again after that, so an upgraded plan is noticed
	// without a restart.
	noUsenetFor = time.Hour
	// goneGrace is how long a job may be left out of the service's answers,
	// round after round, before it counts as gone. Right after the submit, and
	// while a download TorBox queued starts, it can be in neither its queue
	// nor its list for a moment.
	goneGrace = 2 * time.Minute
)

// Options configures a Manager.
type Options struct {
	// Dir keeps the job list and the NZBs of waiting jobs.
	Dir string
	// Stage turns a finished job's files into tasks and returns their ids.
	Stage func(Job, []File) ([]string, error)
	// Finished reports whether every task is done or gone, after which the
	// service's copy is deleted.
	Finished func(taskIDs []string) bool
	// Failed hears about each job that ends without files. Nil for none.
	Failed func(Job)
	// Pending hears how many jobs are waiting for an account or being
	// fetched, each time that number changes. Nil for none.
	Pending func(n int)
	// Taken hears the account each NZB went to and the id the job goes by
	// there, and again when the service gives it another. Nil for none.
	Taken func(slot, remote string)
	// Interval is the pause between two rounds of each of Run's loops,
	// Backoff the first wait after a service declined. Zero means five
	// seconds and a minute.
	Interval time.Duration
	Backoff  time.Duration
	// Now is the clock, time.Now when nil.
	Now func() time.Time
}

// account is what the manager knows about one service account's limits.
type account struct {
	// sent holds the submits of the last hour.
	sent []time.Time
	// submits holds back NZBs after the account declined one. calls holds
	// back reading and deleting jobs after the service asked for fewer calls,
	// which a declined NZB alone does not mean: a full account still reports
	// on the jobs it has.
	submits backoff
	calls   backoff
	// noUsenetUntil is set while the account's plan is known to lack Usenet.
	noUsenetUntil time.Time
}

// backoff keeps calls away from an account that answered busy, for a wait
// that doubles each time it does so again.
type backoff struct {
	until time.Time
	wait  time.Duration
}

func (b *backoff) holds(now time.Time) bool { return now.Before(b.until) }

// busy starts the next wait: first after an answer that was not busy, twice
// the last one otherwise, up to maxBackoff.
func (b *backoff) busy(now time.Time, first time.Duration) time.Duration {
	if b.wait == 0 {
		b.wait = first
	} else {
		b.wait = min(b.wait*2, maxBackoff)
	}
	b.until = now.Add(b.wait)
	return b.wait
}

// Manager keeps the jobs, offers waiting NZBs to the accounts and follows the
// taken ones until their files can be staged.
type Manager struct {
	o Options

	mu       sync.Mutex
	jobs     map[string]*Job
	services []Service
	accounts map[string]*account
	// failed holds the jobs that failed since the Failed hook last heard,
	// which it hears outside mu.
	failed []Job

	// stageMu is held while a job's files become tasks, so Cancel sees a job
	// either before its tasks exist or after they are recorded on it.
	stageMu sync.Mutex

	// pendingMu orders the Pending hook's calls, so the last one it hears is
	// the current count; lastPending is the count it last heard.
	pendingMu   sync.Mutex
	lastPending int

	wake chan struct{}
}

// New loads the job list from o.Dir. A list that cannot be read starts empty
// rather than keeping the app from starting.
func New(o Options) *Manager {
	if o.Interval <= 0 {
		o.Interval = defaultInterval
	}
	if o.Backoff <= 0 {
		o.Backoff = defaultBackoff
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := &Manager{o: o, jobs: map[string]*Job{}, accounts: map[string]*account{}, wake: make(chan struct{}, 1)}
	raw, err := os.ReadFile(m.listPath())
	var list []*Job
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		log.Printf("usenet: the job list could not be read (%v); starting without it", err)
	default:
		if err := json.Unmarshal(raw, &list); err != nil {
			log.Printf("usenet: the job list is damaged (%v); starting without it", err)
		}
	}
	for _, j := range list {
		if j != nil && j.ID != "" {
			m.jobs[j.ID] = j
		}
	}
	return m
}

// SetServices replaces the accounts jobs can go to, in the order they are
// offered an NZB.
func (m *Manager) SetServices(s []Service) {
	m.mu.Lock()
	m.services = slices.Clone(s)
	m.mu.Unlock()
	m.Kick()
}

// Available names the first account an NZB would be offered to, and reports
// false when there is none. An account whose plan turned out to lack Usenet
// does not count.
func (m *Manager) Available() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.o.Now()
	for _, s := range m.services {
		if acc := m.accounts[s.Slot()]; acc != nil && now.Before(acc.noUsenetUntil) {
			continue
		}
		return s.Label(), true
	}
	return "", false
}

// Service is the account in slot, or nil.
func (m *Manager) Service(slot string) Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.serviceLocked(slot)
}

func (m *Manager) serviceLocked(slot string) Service {
	for _, s := range m.services {
		if s.Slot() == slot {
			return s
		}
	}
	return nil
}

// Add queues an NZB. The job is offered to an account on the next round,
// which Add starts at once.
func (m *Manager) Add(j Job, nzb []byte) (Job, error) {
	if _, ok := m.Available(); !ok {
		return Job{}, ErrNoService
	}
	id, err := newJobID()
	if err != nil {
		return Job{}, err
	}
	if err := writeFile(m.nzbPath(id), nzb); err != nil {
		return Job{}, err
	}
	j.ID, j.State, j.Added = id, StateWaiting, m.o.Now()
	if j.Size == 0 {
		j.Size = NZBSize(nzb)
	}
	m.mu.Lock()
	m.jobs[id] = &j
	m.saveLocked()
	out := m.copyLocked(&j)
	m.mu.Unlock()
	m.notePending()
	m.Kick()
	return out, nil
}

// Get returns a copy of one job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return m.copyLocked(j), true
}

// Cancel drops a job whose files are not tasks yet, and deletes it at the
// service when one has taken it. A staged job is left alone and its task ids
// are returned: the caller removes them the ordinary way, and the service's
// copy goes once they are gone. A staging under way is waited for, so the
// answer is never a job half staged.
func (m *Manager) Cancel(id string) (taskIDs []string) {
	m.stageMu.Lock()
	defer m.stageMu.Unlock()
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	if j.State == StateStaged {
		ids := slices.Clone(j.TaskIDs)
		m.mu.Unlock()
		return ids
	}
	delete(m.jobs, id)
	svc := m.serviceLocked(j.Service)
	remote := j.Remote
	m.saveLocked()
	m.mu.Unlock()
	m.notePending()

	_ = os.Remove(m.nzbPath(id))
	if svc != nil && remote != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()
			if err := svc.Delete(ctx, remote); err != nil && !errors.Is(err, ErrGone) {
				log.Printf("usenet: %s job %s could not be deleted there: %v", svc.Label(), remote, err)
			}
		}()
	}
	return nil
}

// Kick offers the waiting NZBs at once rather than at the next tick.
func (m *Manager) Kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run works the jobs until ctx is cancelled. The taken jobs are followed apart
// from the uploads, so a long run of them cannot keep the account import from
// hearing a queued download's new id in time.
func (m *Manager) Run(ctx context.Context) {
	followed := make(chan struct{})
	go func() {
		defer close(followed)
		m.every(ctx, nil, m.followRound)
	}()
	m.every(ctx, m.wake, m.sendRound)
	<-followed
}

// every runs round now and then after each tick or wake-up until ctx ends.
func (m *Manager) every(ctx context.Context, wake <-chan struct{}, round func(context.Context)) {
	t := time.NewTicker(m.o.Interval)
	defer t.Stop()
	for {
		round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-wake:
		}
	}
}

// sendRound offers the waiting NZBs and deletes the services' copies of what
// is already on disk.
func (m *Manager) sendRound(ctx context.Context) {
	m.submit(ctx)
	m.clear(ctx)
	m.prune()
	m.reportFailed()
	m.notePending()
}

// followRound reads how far the services have got with the taken jobs.
func (m *Manager) followRound(ctx context.Context) {
	m.follow(ctx)
	m.reportFailed()
	m.notePending()
}

// ids lists the jobs in one state, oldest first, so NZBs go out in the order
// they came in.
func (m *Manager) ids(state State) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idsLocked(state)
}

func (m *Manager) idsLocked(state State) []string {
	var list []*Job
	for _, j := range m.jobs {
		if j.State == state {
			list = append(list, j)
		}
	}
	sort.Slice(list, func(a, b int) bool {
		if !list[a].Added.Equal(list[b].Added) {
			return list[a].Added.Before(list[b].Added)
		}
		return list[a].ID < list[b].ID
	})
	out := make([]string, len(list))
	for i, j := range list {
		out[i] = j.ID
	}
	return out
}

func (m *Manager) submit(ctx context.Context) {
	for _, id := range m.ids(StateWaiting) {
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		j, ok := m.jobs[id]
		if !ok || j.State != StateWaiting || m.o.Now().Before(j.RetryAt) {
			m.mu.Unlock()
			continue
		}
		svc, left := m.pickLocked(j)
		if svc == nil {
			if !left {
				m.failLocked(j, refusedReason(j))
				m.saveLocked()
			}
			m.mu.Unlock()
			continue
		}
		m.noteSubmitLocked(svc.Slot())
		name := j.Name
		m.mu.Unlock()

		data, err := os.ReadFile(m.nzbPath(id))
		if err != nil {
			m.update(id, func(j *Job) { m.failLocked(j, "the .nzb could not be read back: "+err.Error()) })
			continue
		}
		// Shutdown does not cut the upload short: once the NZB is through, the
		// service may have made the job, and only its answer keeps the next
		// start from sending the NZB again. Run waits for it.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
		remote, err := svc.Submit(cctx, name, data)
		cancel()
		m.submitted(id, svc, remote, err)
	}
}

// pickLocked chooses the first account that may take j now. With none, left
// reports whether an account that has not refused j is still set up, which
// means j waits rather than fails. An account without Usenet is not one j
// waits for.
func (m *Manager) pickLocked(j *Job) (svc Service, left bool) {
	now := m.o.Now()
	for _, s := range m.services {
		acc := m.accountLocked(s.Slot())
		if slices.Contains(j.Refused, s.Slot()) || now.Before(acc.noUsenetUntil) {
			continue
		}
		left = true
		if acc.submits.holds(now) {
			continue
		}
		if limit := s.SubmitsPerHour(); limit > 0 {
			acc.sent = slices.DeleteFunc(acc.sent, func(at time.Time) bool { return now.Sub(at) >= time.Hour })
			if len(acc.sent) >= limit {
				continue
			}
		}
		return s, true
	}
	return nil, left
}

func (m *Manager) accountLocked(slot string) *account {
	acc := m.accounts[slot]
	if acc == nil {
		acc = &account{}
		m.accounts[slot] = acc
	}
	return acc
}

// noteSubmitLocked counts a submit before it goes out, since the service
// counts the attempt whatever it answers.
func (m *Manager) noteSubmitLocked(slot string) {
	acc := m.accountLocked(slot)
	acc.sent = append(acc.sent, m.o.Now())
}

// submitted records what one submit came back with.
func (m *Manager) submitted(id string, svc Service, remote string, err error) {
	// Heard even for a job cancelled during the upload: the service holds it
	// until the delete below reaches it.
	if err == nil {
		m.taken(svc.Slot(), remote)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		// Cancelled while the upload was under way.
		if err == nil {
			go func() {
				dctx, cancel := context.WithTimeout(context.Background(), callTimeout)
				defer cancel()
				_ = svc.Delete(dctx, remote)
			}()
		}
		return
	}
	acc := m.accountLocked(svc.Slot())
	switch {
	case err == nil:
		acc.submits = backoff{}
		j.State, j.Service, j.Label, j.Remote, j.Taken = StateFetching, svc.Slot(), svc.Label(), remote, m.o.Now()
		j.Attempts, j.RetryAt = 0, time.Time{}
		_ = os.Remove(m.nzbPath(id))
		log.Printf("usenet: %s went to %s", j.Name, svc.Label())
	case errors.Is(err, ErrBusy):
		// The account, not the NZB: every waiting job holds back from it.
		wait := acc.submits.busy(m.o.Now(), m.o.Backoff)
		log.Printf("usenet: %s is not taking jobs for now (%v); trying again in %s", svc.Label(), err, wait)
	case errors.Is(err, ErrNoUsenet):
		acc.noUsenetUntil = m.o.Now().Add(noUsenetFor)
		log.Printf("usenet: %s is passed over for NZBs for now: %v", svc.Label(), err)
		m.refuseLocked(j, svc, err)
	case temporary(err):
		if !m.retryLocked(j) {
			m.failLocked(j, err.Error())
		}
	default:
		m.refuseLocked(j, svc, err)
	}
	m.saveLocked()
}

// retryLocked puts off the next call for j after one more failed, the wait
// doubling from Backoff with each failure in a row. It reports false once
// maxAttempts calls have failed.
func (m *Manager) retryLocked(j *Job) bool {
	j.Attempts++
	if j.Attempts >= maxAttempts {
		return false
	}
	wait := m.o.Backoff << min(j.Attempts-1, 10)
	j.RetryAt = m.o.Now().Add(min(wait, maxBackoff))
	return true
}

// refuseLocked notes that svc turned j down, which fails j once no other
// account is left to try.
func (m *Manager) refuseLocked(j *Job, svc Service, err error) {
	j.Refused = append(j.Refused, svc.Slot())
	j.Reason = err.Error()
	if _, left := m.pickLocked(j); !left {
		m.failLocked(j, err.Error())
	}
}

// refusedReason is the reason a job with no account left fails with.
func refusedReason(j *Job) string {
	if j.Reason != "" {
		return j.Reason
	}
	return ErrNoService.Error()
}

// follow reads every fetching job, with one call per account for all of its
// jobs.
func (m *Manager) follow(ctx context.Context) {
	type watched struct{ id, remote string }
	var slots []string
	groups := map[string][]watched{}
	services := map[string]Service{}

	m.mu.Lock()
	now := m.o.Now()
	changed := false
	for _, id := range m.idsLocked(StateFetching) {
		j := m.jobs[id]
		svc := m.serviceLocked(j.Service)
		if svc == nil {
			m.failLocked(j, fmt.Sprintf("the %s account this .nzb was sent to is no longer set up", j.Label))
			changed = true
			continue
		}
		if m.accountLocked(j.Service).calls.holds(now) {
			continue
		}
		if services[j.Service] == nil {
			services[j.Service] = svc
			slots = append(slots, j.Service)
		}
		groups[j.Service] = append(groups[j.Service], watched{id, j.Remote})
	}
	if changed {
		m.saveLocked()
	}
	m.mu.Unlock()

	for _, slot := range slots {
		if ctx.Err() != nil {
			return
		}
		list := groups[slot]
		remotes := make([]string, len(list))
		for i, w := range list {
			remotes[i] = w.remote
		}
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		sts, err := services[slot].Status(cctx, remotes)
		cancel()
		m.heard(services[slot], err)
		if err != nil {
			// Asked again next round.
			continue
		}
		for _, w := range list {
			st, found := sts[w.remote]
			m.observe(w.id, st, found)
		}
	}
}

// heard notes how an account answered a call other than a submit. A busy
// answer holds back its next reads and deletes.
func (m *Manager) heard(svc Service, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc := m.accountLocked(svc.Slot())
	switch {
	case err == nil:
		acc.calls = backoff{}
	case errors.Is(err, ErrBusy):
		wait := acc.calls.busy(m.o.Now(), m.o.Backoff)
		log.Printf("usenet: %s asks for fewer calls (%v); asking again in %s", svc.Label(), err, wait)
	}
}

// callsHeld reports whether the account in slot is still waiting out a busy
// answer.
func (m *Manager) callsHeld(slot string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accountLocked(slot).calls.holds(m.o.Now())
}

// observe applies one reading of a fetching job. found is false when the
// service's answer did not list it.
func (m *Manager) observe(id string, st Status, found bool) {
	var slot, renamed string
	defer func() { m.taken(slot, renamed) }()
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok || j.State != StateFetching {
		m.mu.Unlock()
		return
	}
	now := m.o.Now()
	if !found {
		if j.missing.IsZero() {
			j.missing = now
		}
		if now.Sub(j.missing) >= goneGrace {
			m.failLocked(j, j.Label+" no longer has this download")
			m.saveLocked()
		}
		m.mu.Unlock()
		return
	}
	j.missing = time.Time{}
	if st.ID != "" && st.ID != j.Remote {
		j.Remote = st.ID
		slot, renamed = j.Service, st.ID
		m.saveLocked()
	}
	switch st.Phase {
	case PhaseFailed:
		m.failLocked(j, st.Reason)
		m.saveLocked()
	case PhaseFetching:
		// Progress alone is not worth a write; the next round reads it again.
		if st.Size > 0 {
			j.Size = st.Size
		}
		j.Loaded, j.Speed = int64(st.Progress*float64(j.Size)), st.Speed
	}
	m.mu.Unlock()
	if st.Phase == PhaseReady {
		m.stage(id, st.Files)
	}
}

// taken passes a job's id at its account on to Options.Taken.
func (m *Manager) taken(slot, remote string) {
	if m.o.Taken != nil && remote != "" {
		m.o.Taken(slot, remote)
	}
}

// stage hands a finished job's files to the app.
func (m *Manager) stage(id string, files []File) {
	m.stageMu.Lock()
	defer m.stageMu.Unlock()
	j, ok := m.Get(id)
	if !ok || j.State != StateFetching {
		return
	}
	ids, err := m.o.Stage(j, withoutCommonDir(files))
	m.update(id, func(j *Job) {
		switch {
		case err != nil:
			m.failLocked(j, err.Error())
		case len(ids) == 0:
			m.failLocked(j, "every file of this download is already in the list")
		default:
			j.State, j.TaskIDs, j.Ended = StateStaged, ids, m.o.Now()
			j.Loaded, j.Speed = j.Size, 0
		}
	})
}

// clear deletes the service's copy of a staged job once its tasks are done or
// gone, so the account does not fill up with downloads already on disk.
func (m *Manager) clear(ctx context.Context) {
	for _, id := range m.ids(StateStaged) {
		if ctx.Err() != nil {
			return
		}
		j, ok := m.Get(id)
		if !ok || j.Cleared || m.o.Now().Before(j.RetryAt) || !m.o.Finished(j.TaskIDs) {
			continue
		}
		svc := m.Service(j.Service)
		if svc != nil && j.Remote != "" {
			if m.callsHeld(svc.Slot()) {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			err := svc.Delete(cctx, j.Remote)
			cancel()
			if ctx.Err() != nil {
				return
			}
			m.heard(svc, err)
			switch {
			case errors.Is(err, ErrBusy):
				continue
			case err != nil && !errors.Is(err, ErrGone):
				m.update(id, func(j *Job) {
					if !m.retryLocked(j) {
						j.Cleared = true
						log.Printf("usenet: %s stays on %s, deleting it there failed %d times: %v", j.Name, j.Label, maxAttempts, err)
					}
				})
				continue
			}
		}
		m.update(id, func(j *Job) { j.Cleared = true })
	}
}

// prune forgets ended jobs after keepEnded. A staged job stays until the
// service's copy is cleared.
func (m *Manager) prune() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.o.Now()
	changed := false
	for id, j := range m.jobs {
		ended := j.State == StateFailed || (j.State == StateStaged && j.Cleared)
		if ended && now.Sub(j.Ended) > keepEnded {
			delete(m.jobs, id)
			changed = true
		}
	}
	if changed {
		m.saveLocked()
	}
}

// update applies fn to a job that still exists and saves the list.
func (m *Manager) update(id string, fn func(*Job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		fn(j)
		m.saveLocked()
	}
}

func (m *Manager) failLocked(j *Job, reason string) {
	j.State, j.Reason, j.Ended, j.Speed = StateFailed, reason, m.o.Now(), 0
	_ = os.Remove(m.nzbPath(j.ID))
	log.Printf("usenet: %s failed: %s", j.Name, reason)
	m.failed = append(m.failed, m.copyLocked(j))
}

// reportFailed tells the Failed hook about the jobs that failed since it last
// heard. It runs outside mu, since the hook calls into the app.
func (m *Manager) reportFailed() {
	m.mu.Lock()
	failed := m.failed
	m.failed = nil
	m.mu.Unlock()
	if m.o.Failed == nil {
		return
	}
	for _, j := range failed {
		m.o.Failed(j)
	}
}

// notePending tells the Pending hook how many jobs are still waiting or being
// fetched, when that has changed. It runs outside mu, like reportFailed.
func (m *Manager) notePending() {
	if m.o.Pending == nil {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	m.mu.Lock()
	n := 0
	for _, j := range m.jobs {
		if j.State == StateWaiting || j.State == StateFetching {
			n++
		}
	}
	m.mu.Unlock()
	if n != m.lastPending {
		m.lastPending = n
		m.o.Pending(n)
	}
}

// saveLocked writes the list beside the NZBs, through a temporary file so a
// crash mid-write leaves the previous list. A failed write is logged and
// retried by the next change; the jobs in memory stay right either way.
func (m *Manager) saveLocked() {
	list := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		list = append(list, j)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].ID < list[b].ID })
	raw, err := json.Marshal(list)
	if err == nil {
		err = writeFile(m.listPath(), raw)
	}
	if err != nil {
		log.Printf("usenet: the job list could not be saved: %v", err)
	}
}

func (m *Manager) listPath() string {
	return filepath.Join(m.o.Dir, "jobs.json")
}

func (m *Manager) copyLocked(j *Job) Job {
	c := *j
	c.Refused = slices.Clone(j.Refused)
	c.TaskIDs = slices.Clone(j.TaskIDs)
	return c
}

func (m *Manager) nzbPath(id string) string {
	return filepath.Join(m.o.Dir, id+".nzb")
}

// writeFile replaces path with data, creating the folder on first use.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// newJobID is random, so the file an NZB waits in cannot be guessed from
// another.
func newJobID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
