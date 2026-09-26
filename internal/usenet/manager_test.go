package usenet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const sampleNZB = `<?xml version="1.0" encoding="iso-8859-1" ?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="someone" date="1700000000" subject="Show.S01E01 [1/2] - &#34;show.r00&#34; yEnc">
  <groups><group>alt.binaries.example</group></groups>
  <segments><segment bytes="739067" number="1">abcdef0123456789@news</segment></segments>
 </file>
</nzb>`

// fakeService is an account whose answers a test sets directly.
type fakeService struct {
	slot    string
	perHour int

	mu        sync.Mutex
	submitted []string
	submitErr []error
	// status is the reading of every job asked about, and gone leaves them
	// all out of the answer instead.
	status    Status
	gone      bool
	statusErr error
	asked     int
	linkErr   []error
	deleted   []string
}

func (f *fakeService) Slot() string        { return f.slot }
func (f *fakeService) Label() string       { return "Fake " + f.slot }
func (f *fakeService) SubmitsPerHour() int { return f.perHour }

func (f *fakeService) Submit(_ context.Context, name string, _ []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submitErr) > 0 {
		err := f.submitErr[0]
		f.submitErr = f.submitErr[1:]
		if err != nil {
			return "", err
		}
	}
	f.submitted = append(f.submitted, name)
	return f.slot + "-" + strconv.Itoa(len(f.submitted)), nil
}

func (f *fakeService) Status(_ context.Context, ids []string) (map[string]Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	out := map[string]Status{}
	if !f.gone {
		for _, id := range ids {
			out[id] = f.status
		}
	}
	return out, nil
}

func (f *fakeService) Link(_ context.Context, id, fileID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.linkErr) > 0 {
		err := f.linkErr[0]
		f.linkErr = f.linkErr[1:]
		return "", err
	}
	return "https://cdn.example/" + id + "/" + fileID, nil
}

func (f *fakeService) set(fn func(*fakeService)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeService) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeService) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.submitted)
}

// clock is a time a test moves by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// harness is a manager with the files it staged and the jobs that failed
// kept in memory.
type harness struct {
	m        *Manager
	clock    *clock
	dir      string
	staged   map[string][]File
	stagedAs map[string]Job
	failed   []Job
	pending  []int
	finished func([]string) bool
	// beforeStage, when set, runs as the files are being staged.
	beforeStage func()
}

func newHarness(t *testing.T, services ...Service) *harness {
	t.Helper()
	h := &harness{
		clock:    &clock{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		dir:      t.TempDir(),
		staged:   map[string][]File{},
		stagedAs: map[string]Job{},
		finished: func([]string) bool { return false },
	}
	h.m = h.open()
	h.m.SetServices(services)
	return h
}

// open builds a manager over the harness's folder, as a restart would.
func (h *harness) open() *Manager {
	return New(Options{
		Dir: h.dir,
		Stage: func(j Job, files []File) ([]string, error) {
			if h.beforeStage != nil {
				h.beforeStage()
			}
			h.staged[j.ID] = files
			h.stagedAs[j.ID] = j
			ids := make([]string, len(files))
			for i := range files {
				ids[i] = j.ID + "-task-" + strconv.Itoa(i)
			}
			return ids, nil
		},
		Finished: func(ids []string) bool { return h.finished(ids) },
		Failed:   func(j Job) { h.failed = append(h.failed, j) },
		Pending:  func(n int) { h.pending = append(h.pending, n) },
		Now:      h.clock.Now,
	})
}

// round runs each of Run's loops once, the sends first.
func (m *Manager) round(ctx context.Context) {
	m.sendRound(ctx)
	m.followRound(ctx)
}

func (h *harness) add(t *testing.T, name string) Job {
	t.Helper()
	j, err := h.m.Add(Job{Name: name, Package: name}, []byte(sampleNZB))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func (h *harness) job(t *testing.T, id string) Job {
	t.Helper()
	j, ok := h.m.Get(id)
	if !ok {
		t.Fatalf("job %s is gone", id)
	}
	return j
}

func TestAnNZBComesBackAsTheFilesTheServiceFetched(t *testing.T) {
	fake := &fakeTorBox{t: t, fetchRounds: 1}
	h := newHarness(t, NewTorBox(fake.server().URL, "torbox", testKey))
	j := h.add(t, "Show.S01E01")
	if j.State != StateWaiting {
		t.Fatalf("a new job is %s, want waiting", j.State)
	}

	h.m.round(context.Background())
	j = h.job(t, j.ID)
	if j.State != StateFetching || j.Remote != "77" || j.Label != "TorBox" {
		t.Fatalf("after the first round the job is %+v, want it fetching at TorBox as job 77", j)
	}
	if j.Loaded != 250 || j.Size != 1000 {
		t.Errorf("progress = %d of %d, want TorBox's own reading", j.Loaded, j.Size)
	}
	if _, err := os.Stat(filepath.Join(h.dir, j.ID+".nzb")); !os.IsNotExist(err) {
		t.Errorf("the NZB is still on disk after TorBox took it (%v)", err)
	}

	h.m.round(context.Background())
	j = h.job(t, j.ID)
	if j.State != StateStaged || len(j.TaskIDs) != 2 {
		t.Fatalf("after the second round the job is %+v, want its two files staged", j)
	}
	files := h.staged[j.ID]
	if len(files) != 2 || files[0].Name != "show.s01e01.mkv" {
		t.Errorf("staged %+v, want TorBox's files", files)
	}
}

func TestABusyAccountHoldsTheNZBBackAndTriesAgainLater(t *testing.T) {
	fake := &fakeTorBox{t: t, fetchRounds: 5, busyNext: 1}
	h := newHarness(t, NewTorBox(fake.server().URL, "torbox", testKey))
	j := h.add(t, "Show.S01E02")
	ctx := context.Background()

	h.m.round(ctx)
	if got := h.job(t, j.ID); got.State != StateWaiting {
		t.Fatalf("a busy TorBox left the job %s, want it waiting", got.State)
	}
	h.m.round(ctx)
	fake.mu.Lock()
	creates := fake.creates
	fake.mu.Unlock()
	if creates != 1 {
		t.Fatalf("TorBox was asked %d times within the backoff, want once", creates)
	}

	h.clock.advance(defaultBackoff + time.Second)
	h.m.round(ctx)
	if got := h.job(t, j.ID); got.State != StateFetching {
		t.Fatalf("after the backoff the job is %s, want it taken", got.State)
	}
}

func TestTheHourlyLimitHoldsBackTheNZBsBeyondIt(t *testing.T) {
	svc := &fakeService{slot: "torbox", perHour: 2, status: Status{Phase: PhaseFetching}}
	h := newHarness(t, svc)
	first, second, third := h.add(t, "one"), h.add(t, "two"), h.add(t, "three")
	// Added within one tick of a coarse clock, so ordered by time alone they
	// could come out any way round.
	for _, j := range []Job{first, second, third} {
		h.clock.advance(time.Millisecond)
		h.m.update(j.ID, func(x *Job) { x.Added = h.clock.Now() })
	}

	h.m.round(context.Background())
	if got := svc.names(); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("submitted %v, want the first two only", got)
	}
	if got := h.job(t, third.ID); got.State != StateWaiting {
		t.Fatalf("the third NZB is %s, want it waiting for the hour to pass", got.State)
	}

	h.clock.advance(time.Hour)
	h.m.round(context.Background())
	if got := svc.names(); len(got) != 3 || got[2] != "three" {
		t.Fatalf("submitted %v, want the third once the hour is over", got)
	}
}

func TestARefusedNZBIsOfferedToTheNextAccount(t *testing.T) {
	first := &fakeService{slot: "torbox", submitErr: []error{errors.New("torbox: plan does not include usenet")}}
	second := &fakeService{slot: "premiumize", status: Status{Phase: PhaseFetching}}
	h := newHarness(t, first, second)
	j := h.add(t, "Film")

	h.m.round(context.Background())
	h.m.round(context.Background())
	got := h.job(t, j.ID)
	if got.State != StateFetching || got.Service != "premiumize" {
		t.Fatalf("job = %+v, want it taken by the second account", got)
	}
}

func TestAnNZBEveryAccountRefusesFailsWithTheReason(t *testing.T) {
	svc := &fakeService{slot: "torbox", submitErr: []error{errors.New("torbox createusenetdownload: BOZO_NZB invalid")}}
	h := newHarness(t, svc)
	j := h.add(t, "Broken")

	h.m.round(context.Background())
	got := h.job(t, j.ID)
	if got.State != StateFailed || got.Reason != "torbox createusenetdownload: BOZO_NZB invalid" {
		t.Fatalf("job = %+v, want it failed with TorBox's refusal", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, j.ID+".nzb")); !os.IsNotExist(err) {
		t.Errorf("a failed job's NZB is left on disk (%v)", err)
	}
}

func TestAnUnansweredSubmitIsTriedAgainAndThenGivenUp(t *testing.T) {
	down := unreachable{errors.New("torbox createusenetdownload: connection refused")}
	errs := make([]error, maxAttempts)
	for i := range errs {
		errs[i] = down
	}
	svc := &fakeService{slot: "torbox", submitErr: errs}
	h := newHarness(t, svc)
	j := h.add(t, "Show")

	for i := 0; i < maxAttempts; i++ {
		if got := h.job(t, j.ID); got.State != StateWaiting {
			t.Fatalf("after %d unanswered submits the job is %s, want it still waiting", i, got.State)
		}
		h.m.round(context.Background())
		h.clock.advance(maxBackoff)
	}
	if got := h.job(t, j.ID); got.State != StateFailed {
		t.Fatalf("after %d unanswered submits the job is %s, want it failed", maxAttempts, got.State)
	}
}

func TestJobsOutliveARestart(t *testing.T) {
	svc := &fakeService{slot: "torbox", perHour: 1, status: Status{Phase: PhaseFetching}}
	h := newHarness(t, svc)
	taken, waiting := h.add(t, "taken"), h.add(t, "waiting")
	h.m.update(waiting.ID, func(j *Job) { j.Added = j.Added.Add(time.Second) })
	h.m.round(context.Background())

	h.m = h.open()
	h.m.SetServices([]Service{svc})
	if got := h.job(t, taken.ID); got.State != StateFetching || got.Remote != "torbox-1" {
		t.Errorf("after a restart the taken job is %+v, want it still followed at the service", got)
	}
	if got := h.job(t, waiting.ID); got.State != StateWaiting {
		t.Errorf("after a restart the waiting job is %s", got.State)
	}
	h.clock.advance(time.Hour)
	h.m.round(context.Background())
	if got := h.job(t, waiting.ID); got.State != StateFetching {
		t.Errorf("the waiting NZB was not sent after the restart: %s (%s)", got.State, got.Reason)
	}
}

func TestTheServicesCopyIsDeletedOnceTheTasksAreDone(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseReady, Files: []File{{ID: "1", Name: "a.mkv", Size: 5}}}}
	h := newHarness(t, svc)
	j := h.add(t, "Done")

	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateStaged {
		t.Fatalf("job is %s, want staged", got.State)
	}
	if len(svc.deleted) != 0 {
		t.Fatalf("the service's copy went while its tasks were still running: %v", svc.deleted)
	}

	h.finished = func([]string) bool { return true }
	h.m.round(context.Background())
	if got := h.job(t, j.ID); !got.Cleared || len(svc.deleted) != 1 || svc.deleted[0] != "torbox-1" {
		t.Fatalf("cleared = %v, deleted %v; want the copy gone once the tasks are done", got.Cleared, svc.deleted)
	}

	h.clock.advance(keepEnded + time.Hour)
	h.m.round(context.Background())
	if _, ok := h.m.Get(j.ID); ok {
		t.Error("an ended job is remembered past its keep time")
	}
}

func TestAJobTheServiceLostFails(t *testing.T) {
	svc := &fakeService{slot: "torbox", gone: true}
	h := newHarness(t, svc)
	j := h.add(t, "Lost")
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFetching {
		t.Fatalf("job is %s right after it was sent, want it still fetching", got.State)
	}

	h.clock.advance(goneGrace)
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFailed {
		t.Fatalf("job is %s, want failed once the service no longer knows it", got.State)
	}
	if len(h.failed) != 1 || h.failed[0].ID != j.ID || !strings.Contains(h.failed[0].Reason, "no longer has") {
		t.Errorf("the failure was reported as %+v, want the lost job with its reason", h.failed)
	}
}

func TestAJobLongAtTheServiceSurvivesOneAnswerWithoutIt(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching}}
	h := newHarness(t, svc)
	j := h.add(t, "Queued")
	h.m.round(context.Background())

	h.clock.advance(20 * time.Minute)
	h.m.round(context.Background())
	svc.set(func(f *fakeService) { f.gone = true })
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFetching {
		t.Fatalf("job is %s after one answer left it out, want it still fetching", got.State)
	}

	svc.set(func(f *fakeService) { f.gone = false })
	h.m.round(context.Background())
	svc.set(func(f *fakeService) { f.gone = true })
	h.clock.advance(goneGrace / 2)
	h.m.round(context.Background())
	h.clock.advance(goneGrace / 2)
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFetching {
		t.Fatalf("job is %s, want the grace counted from the first answer that left it out", got.State)
	}
	h.clock.advance(goneGrace)
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFailed {
		t.Fatalf("job is %s after the grace passed without it, want failed", got.State)
	}
}

func TestCancelDeletesATakenJobAtTheService(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching}}
	h := newHarness(t, svc)
	j := h.add(t, "Unwanted")
	h.m.round(context.Background())

	h.m.Cancel(j.ID)
	if _, ok := h.m.Get(j.ID); ok {
		t.Fatal("the cancelled job is still listed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		svc.mu.Lock()
		n := len(svc.deleted)
		svc.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the service's copy of a cancelled job was never deleted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEachAccountIsAskedOnceARoundForAllItsJobs(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching, Size: 100, Progress: 0.5}}
	h := newHarness(t, svc)
	for _, name := range []string{"one", "two", "three"} {
		h.add(t, name)
	}

	for round := 1; round <= 2; round++ {
		h.m.round(context.Background())
		svc.mu.Lock()
		asked := svc.asked
		svc.mu.Unlock()
		if asked != round {
			t.Fatalf("after %d rounds with three jobs the account was asked %d times, want once a round", round, asked)
		}
	}
}

func TestProgressIsKeptInMemoryRatherThanWrittenOut(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching, Size: 1000, Progress: 0.1}}
	h := newHarness(t, svc)
	j := h.add(t, "Show")
	h.m.round(context.Background())
	list := filepath.Join(h.dir, "jobs.json")
	before, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}

	svc.set(func(f *fakeService) { f.status.Progress = 0.6 })
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.Loaded != 600 {
		t.Fatalf("loaded = %d, want the new reading", got.Loaded)
	}
	after, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a progress reading rewrote the job list")
	}
}

func TestAServiceWithoutSizesIsReadAgainstTheNZBsOwn(t *testing.T) {
	svc := &fakeService{slot: "premiumize", status: Status{Phase: PhaseFetching, Progress: 0.5}}
	h := newHarness(t, svc)
	j := h.add(t, "Film")
	if j.Size != 739067 {
		t.Fatalf("a new job's size is %d, want the articles the NZB lists", j.Size)
	}
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.Size != 739067 || got.Loaded != 369533 {
		t.Errorf("progress = %d of %d, want half of the NZB's size", got.Loaded, got.Size)
	}
}

func TestAJobTakesTheIDItStartsUnder(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching, ID: "88"}}
	h := newHarness(t, svc)
	j := h.add(t, "Queued")
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.Remote != "88" {
		t.Fatalf("remote = %q, want the id the download started under", got.Remote)
	}

	svc.set(func(f *fakeService) { f.status = Status{Phase: PhaseReady, Files: []File{{ID: "1", Name: "a.mkv"}}} })
	h.m.round(context.Background())
	if got := h.stagedAs[j.ID]; got.Remote != "88" {
		t.Errorf("the files were staged from job %q, want 88", got.Remote)
	}
}

func TestAnAccountWithoutUsenetIsPassedOverForAWhile(t *testing.T) {
	first := &fakeService{slot: "torbox", submitErr: []error{fmt.Errorf("torbox: PLAN_RESTRICTED_FEATURE: %w", ErrNoUsenet)}}
	second := &fakeService{slot: "premiumize", status: Status{Phase: PhaseFetching}}
	h := newHarness(t, first, second)
	j := h.add(t, "Film")

	h.m.round(context.Background())
	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFetching || got.Service != "premiumize" {
		t.Fatalf("job = %+v, want it taken by the account that has Usenet", got)
	}
	if label, _ := h.m.Available(); label != second.Label() {
		t.Errorf("an NZB is offered to %q first, want the account that has Usenet", label)
	}
	h.add(t, "Other")
	h.m.round(context.Background())
	if got := first.names(); len(got) != 0 {
		t.Errorf("the account without Usenet was sent %v", got)
	}

	h.clock.advance(noUsenetFor)
	if label, _ := h.m.Available(); label != first.Label() {
		t.Errorf("after a while an NZB is offered to %q first, want the first account tried again", label)
	}
}

func TestWithOnlyAPlanWithoutUsenetNoNZBIsTaken(t *testing.T) {
	svc := &fakeService{slot: "torbox", submitErr: []error{fmt.Errorf("torbox: PLAN_RESTRICTED_FEATURE: %w", ErrNoUsenet)}}
	h := newHarness(t, svc)
	j := h.add(t, "Film")

	h.m.round(context.Background())
	if got := h.job(t, j.ID); got.State != StateFailed || !strings.Contains(got.Reason, "plan") {
		t.Fatalf("job = %+v, want it failed with the plan as the reason", got)
	}
	if _, ok := h.m.Available(); ok {
		t.Error("an account known to lack Usenet still counts as one that takes NZBs")
	}
	if _, err := h.m.Add(Job{Name: "next"}, []byte(sampleNZB)); !errors.Is(err, ErrNoService) {
		t.Errorf("the next NZB was queued (%v), want it refused at the door", err)
	}
}

func TestCancelDuringStagingHandsBackTheTasks(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseReady, Files: []File{{ID: "1", Name: "a.mkv"}}}}
	h := newHarness(t, svc)
	j := h.add(t, "Racing")
	staging, release := make(chan struct{}), make(chan struct{})
	h.beforeStage = func() {
		close(staging)
		<-release
	}
	done := make(chan struct{})
	go func() {
		h.m.round(context.Background())
		close(done)
	}()
	<-staging

	cancelled := make(chan []string, 1)
	go func() { cancelled <- h.m.Cancel(j.ID) }()
	select {
	case ids := <-cancelled:
		t.Fatalf("Cancel answered %v while the files were still being staged", ids)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	ids := <-cancelled
	<-done

	if len(ids) != 1 || ids[0] != j.ID+"-task-0" {
		t.Errorf("Cancel handed back %v, want the task the files became", ids)
	}
	if got := h.job(t, j.ID); got.State != StateStaged {
		t.Errorf("job is %s, want it kept as staged until its tasks are gone", got.State)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.deleted) != 0 {
		t.Errorf("the service's copy went while its files were becoming tasks: %v", svc.deleted)
	}
}

func TestThePendingCountFollowsTheJobs(t *testing.T) {
	svc := &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching}}
	h := newHarness(t, svc)
	h.add(t, "one")
	two := h.add(t, "two")
	h.m.round(context.Background())
	if !slices.Equal(h.pending, []int{1, 2}) {
		t.Fatalf("heard %v, want the count once per change", h.pending)
	}

	h.m.Cancel(two.ID)
	svc.set(func(f *fakeService) { f.status = Status{Phase: PhaseReady, Files: []File{{ID: "1", Name: "a.mkv"}}} })
	h.m.round(context.Background())
	if !slices.Equal(h.pending, []int{1, 2, 1, 0}) {
		t.Errorf("heard %v, want it to drop with the cancel and the staging", h.pending)
	}
}

// slowUpload is a fakeService whose uploads hang until release is closed. An
// upload whose context has ended by then fails, as a real one cut short does.
type slowUpload struct {
	*fakeService
	uploading chan struct{}
	release   chan struct{}
}

func (s *slowUpload) Submit(ctx context.Context, name string, nzb []byte) (string, error) {
	select {
	case s.uploading <- struct{}{}:
	default:
	}
	<-s.release
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.fakeService.Submit(ctx, name, nzb)
}

func TestAQueuedDownloadsNewIDIsHeardWhileAnUploadHangs(t *testing.T) {
	svc := &slowUpload{
		fakeService: &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching, ID: "88"}},
		uploading:   make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	heard := make(chan string, 1)
	m := New(Options{
		Dir:      t.TempDir(),
		Stage:    func(Job, []File) ([]string, error) { return nil, nil },
		Finished: func([]string) bool { return false },
		Taken: func(_, remote string) {
			select {
			case heard <- remote:
			default:
			}
		},
		Interval: 10 * time.Millisecond,
	})
	m.SetServices([]Service{svc})
	queued, err := m.Add(Job{Name: "queued"}, []byte(sampleNZB))
	if err != nil {
		t.Fatal(err)
	}
	m.update(queued.ID, func(j *Job) {
		j.State, j.Service, j.Label, j.Remote, j.Taken = StateFetching, "torbox", "TorBox", queuedID("5", "abc"), time.Now()
	})
	if _, err := m.Add(Job{Name: "next"}, []byte(sampleNZB)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		close(svc.release)
		<-done
	}()
	<-svc.uploading
	select {
	case remote := <-heard:
		if remote != "88" {
			t.Errorf("heard %q, want the id the queued download started under", remote)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the queued download's new id was not heard while another NZB was uploading")
	}
}

func TestAnUploadUnderWayAtShutdownIsKept(t *testing.T) {
	svc := &slowUpload{
		fakeService: &fakeService{slot: "torbox", status: Status{Phase: PhaseFetching}},
		uploading:   make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	h := newHarness(t, svc)
	j := h.add(t, "Film")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.m.Run(ctx)
		close(done)
	}()
	<-svc.uploading
	cancel()
	close(svc.release)
	<-done

	h.m = h.open()
	h.m.SetServices([]Service{svc})
	if got := h.job(t, j.ID); got.State != StateFetching || got.Remote != "torbox-1" {
		t.Errorf("after a restart the job is %+v, want it at the service as the upload left it", got)
	}
}

func TestAddRefusesWithoutAnAccount(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Add(Job{Name: "x"}, []byte(sampleNZB)); !errors.Is(err, ErrNoService) {
		t.Errorf("err = %v, want ErrNoService", err)
	}
}

func TestIsNZBTellsARealNZBFromALinkList(t *testing.T) {
	for _, c := range []struct {
		name string
		data string
		want bool
	}{
		{"a real NZB", sampleNZB, true},
		{"one without a declaration", "<nzb>\n<file subject=\"x\"><segments/></file></nzb>", true},
		{"a DDL indexer's link list", "<links><item>https://host.example/a.rar</item></links>", false},
		{"an NZB with no files", "<nzb xmlns=\"x\"></nzb>", false},
		{"another element starting with nzb", "<nzbinfo><file/></nzbinfo>", false},
		{"plain text", "https://host.example/a.rar\n", false},
	} {
		if got := IsNZB([]byte(c.data)); got != c.want {
			t.Errorf("%s: IsNZB = %v, want %v", c.name, got, c.want)
		}
	}
}
