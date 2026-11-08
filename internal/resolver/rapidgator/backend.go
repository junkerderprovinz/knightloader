package rapidgator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader interface {
	// Handover starts the transfer of url. relink asks the API again for a
	// fresh url, for what is left when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

const (
	// sessionAge is how long a token is used before signing in afresh, as
	// long as JDownloader's plugin keeps one. Rapidgator blocks accounts that
	// sign in too often, so a token is kept rather than fetched per download.
	sessionAge = 45 * time.Minute
	// loginWait is how long a download waits when Rapidgator asks for a pause
	// between sign-ins, which JDownloader's plugin waits as well.
	loginWait = 5 * time.Minute
	// maxBusy is how many rounds of loginWait a download sits out before it
	// fails and the app's retries take over.
	maxBusy = 3
	// spentBench keeps a login without traffic out of routing, so the next
	// links go another way, until it is tried again.
	spentBench = time.Hour
	// smallFile is the size up to which a file gets one connection.
	// JDownloader's plugin found small files breaking on more.
	smallFile = 2 << 20
)

// Status is what Rapidgator made of the stored login the last time it was
// asked.
type Status struct {
	// Checked is set once Rapidgator has answered for the login.
	Checked bool
	// Refused is Rapidgator turning the login down, and Err says why.
	Refused bool
	Err     string
	Premium bool
	Expiry  time.Time
	// TrafficLeft and TrafficMax are bytes, both 0 for a plan without a
	// traffic quota.
	TrafficLeft, TrafficMax int64
}

// Backend downloads Rapidgator files on the stored premium login: it asks the
// API for the download link and hands it to the engine. A wait Rapidgator
// asks for keeps the task running with the time left in its note.
type Backend struct {
	client   *Client
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Checked is called when Rapidgator's word on the login changes what it
	// can download. Nil calls nothing.
	Checked func()

	sleep func(ctx context.Context, d time.Duration) error
	// inspectPause spaces the questions of one Inspect, so a long paste does
	// not look like a crawler to Rapidgator.
	inspectPause time.Duration

	// smu serialises signing in, so downloads that start together share one
	// sign-in.
	smu sync.Mutex

	mu      sync.Mutex
	email   string
	pass    string
	current session
	status  Status
	benched time.Time
	cancel  map[string]context.CancelFunc
	link    map[string]string
	conns   map[string]int
	handed  map[string]bool
	waiting map[string]bool
}

type session struct {
	token string
	at    time.Time
	acct  Account
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		client: c, eng: eng, onUpdate: onUpdate, sleep: sleep, inspectPause: 500 * time.Millisecond,
		cancel:  map[string]context.CancelFunc{},
		link:    map[string]string{},
		conns:   map[string]int{},
		handed:  map[string]bool{},
		waiting: map[string]bool{},
	}
}

// SetLogin stores the login downloads go out on, "" for none, and reports
// whether it differs from the one before. A different login starts unchecked.
func (b *Backend) SetLogin(email, password string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if email == b.email && password == b.pass {
		return false
	}
	b.email, b.pass = email, password
	b.current, b.status, b.benched = session{}, Status{}, time.Time{}
	return true
}

// Ready reports whether the stored login can be expected to download: there
// is one, Rapidgator has not turned it down or called it free, and its
// traffic has not just run out. It takes no lock but the backend's, so the
// app may ask while holding its own.
func (b *Backend) Ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.email != "" && !b.status.Refused && (!b.status.Checked || b.status.Premium) && time.Now().After(b.benched)
}

// Status is what Rapidgator made of the stored login.
func (b *Backend) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// Check asks Rapidgator about the stored login and returns its answer.
func (b *Backend) Check(ctx context.Context) Status {
	_, _ = b.session(ctx, false)
	return b.Status()
}

// session returns a working token, the one in hand unless fresh is set or it
// is too old or Rapidgator has forgotten it.
func (b *Backend) session(ctx context.Context, fresh bool) (session, error) {
	b.smu.Lock()
	defer b.smu.Unlock()
	b.mu.Lock()
	email, pass, cur := b.email, b.pass, b.current
	b.mu.Unlock()
	if email == "" {
		return session{}, ErrNoLogin
	}
	if !fresh && cur.token != "" && time.Since(cur.at) < sessionAge {
		acct, err := b.client.User(ctx, cur.token)
		if err == nil {
			cur.acct = acct
			b.record(email, pass, cur, nil)
			return cur, nil
		}
		if !errors.Is(err, errSession) {
			return session{}, err
		}
	}
	token, acct, err := b.client.Login(ctx, email, pass)
	s := session{token: token, at: time.Now(), acct: acct}
	b.record(email, pass, s, err)
	if err != nil {
		return session{}, err
	}
	return s, nil
}

// record keeps what signing in with email and pass came to, unless the login
// was changed meanwhile.
func (b *Backend) record(email, pass string, s session, err error) {
	var refused *LoginError
	if err != nil && !errors.As(err, &refused) {
		return
	}
	b.mu.Lock()
	if b.email != email || b.pass != pass {
		b.mu.Unlock()
		return
	}
	before := b.status
	if refused != nil {
		b.current = session{}
		b.status = Status{Checked: true, Refused: true, Err: err.Error()}
	} else {
		b.current = s
		b.status = Status{Checked: true, Premium: s.acct.Premium, Expiry: s.acct.Expiry,
			TrafficLeft: s.acct.TrafficLeft, TrafficMax: s.acct.TrafficMax}
	}
	changed := before.Checked != b.status.Checked || before.Refused != b.status.Refused || before.Premium != b.status.Premium
	b.mu.Unlock()
	if changed && b.Checked != nil {
		b.Checked()
	}
}

// bench takes the login out of routing for spentBench.
func (b *Backend) bench() {
	b.mu.Lock()
	b.benched = time.Now().Add(spentBench)
	b.mu.Unlock()
	if b.Checked != nil {
		b.Checked()
	}
}

// Waiting reports whether the task is sitting out a wait Rapidgator asked for.
// It moves no bytes meanwhile, and that is no stall.
func (b *Backend) Waiting(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waiting[taskID]
}

func (b *Backend) Download(taskID, link string, _ map[string]string, conns int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.conns[taskID] = conns
	b.handed[taskID] = false
	b.mu.Unlock()
	b.start(taskID, link)
}

// start begins a run. Its cancel stays in b.cancel until the next start or a
// Remove replaces it, so a run that ends cannot take out the entry of the run
// a Resume has just started.
func (b *Backend) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link)
}

func (b *Backend) run(ctx context.Context, taskID, link string) {
	busy := 0
	for {
		direct, size, err := b.unlock(ctx, taskID, link, func(d time.Duration) error {
			return b.wait(ctx, taskID, d, "Waiting for Rapidgator: %s left")
		})
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			b.handOn(ctx, taskID, link, direct, size)
			return
		case errors.Is(err, ErrBusy) && busy < maxBusy:
			busy++
			if b.wait(ctx, taskID, loginWait, "Rapidgator asks for a pause between sign-ins, next try in %s") != nil {
				return
			}
		default:
			b.fail(taskID, err)
			return
		}
	}
}

// unlock asks the API for the file's download link. delay waits out the pause
// the link comes with before it is used.
func (b *Backend) unlock(ctx context.Context, taskID, link string, delay func(time.Duration) error) (string, int64, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", 0, fmt.Errorf("rapidgator: %s is not a link to a file", link)
	}
	id := fileIDOf(norm)
	for retried := false; ; retried = true {
		s, err := b.session(ctx, retried)
		if err != nil {
			return "", 0, err
		}
		if !s.acct.Premium {
			return "", 0, ErrNotPremium
		}
		if s.acct.TrafficMax > 0 && s.acct.TrafficLeft <= 0 {
			b.bench()
			return "", 0, ErrTraffic
		}
		f, err := b.client.File(ctx, s.token, id)
		if errors.Is(err, errSession) && !retried {
			continue
		}
		if err != nil {
			return "", 0, err
		}
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: f.Name, Size: f.Size})
		if s.acct.TrafficMax > 0 && f.Size > s.acct.TrafficLeft {
			return "", 0, fmt.Errorf("%w for this file, which is %s while %s are left", ErrTraffic, sizeText(f.Size), sizeText(s.acct.TrafficLeft))
		}
		direct, wait, err := b.client.Link(ctx, s.token, id)
		if errors.Is(err, errSession) && !retried {
			continue
		}
		if errors.Is(err, ErrTraffic) {
			b.bench()
		}
		if err != nil {
			return "", 0, err
		}
		if wait > 0 {
			if err := delay(wait); err != nil {
				return "", 0, err
			}
		}
		return direct, f.Size, nil
	}
}

// handOn gives the link to the engine, on one connection for a small file.
func (b *Backend) handOn(ctx context.Context, taskID, link, direct string, size int64) {
	// Pause and Remove cancel under b.mu, so one that came after the API
	// answered is seen here and the link is not handed on.
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.handed[taskID] = true
	conns := b.conns[taskID]
	b.mu.Unlock()
	if size <= smallFile || conns < 1 {
		conns = 1
	}
	b.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		direct, _, err := b.unlock(ctx, taskID, link, func(d time.Duration) error { return b.sleep(ctx, d) })
		return direct, err
	})
}

// wait sits out d, keeping the time left in the task's note.
func (b *Backend) wait(ctx context.Context, taskID string, d time.Duration, note string) error {
	b.setWaiting(taskID, true)
	defer b.setWaiting(taskID, false)
	for left := d; left > 0; {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: fmt.Sprintf(note, span(left))})
		// Every note reaches every open browser, so a long wait counts down
		// in coarser steps.
		step := time.Second
		if left > 2*time.Minute {
			step = 30 * time.Second
		}
		step = min(step, left)
		if err := b.sleep(ctx, step); err != nil {
			return err
		}
		left -= step
	}
	return nil
}

// span writes a wait as people read it, rounded up: "37 min", "45 s".
func span(d time.Duration) string {
	if d >= 2*time.Minute {
		return fmt.Sprintf("%d min", (d+time.Minute-1)/time.Minute)
	}
	return fmt.Sprintf("%d s", (d+time.Second-1)/time.Second)
}

// sizeText writes a size in binary units, as Rapidgator does.
func sizeText(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 3 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGT"[i])
}

func (b *Backend) setWaiting(taskID string, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.waiting[taskID] = true
	} else {
		delete(b.waiting, taskID)
	}
}

// fail reports err with the cause the backend knows.
func (b *Backend) fail(taskID string, err error) {
	u := core.Update{Status: core.StatusError, Err: err.Error()}
	var refused *LoginError
	var api *APIError
	switch {
	case errors.Is(err, ErrGone):
		u.Reason = core.ReasonGone
	case errors.As(err, &refused), errors.Is(err, ErrNotPremium), errors.Is(err, ErrNoLogin):
		u.Reason = core.ReasonAuth
	case errors.Is(err, ErrTraffic), errors.Is(err, ErrDenied):
		u.Reason = core.ReasonLimit
	case errors.Is(err, ErrBusy), errors.Is(err, errSession), errors.As(err, &api) && api.Status >= http.StatusInternalServerError:
		u.Reason = core.ReasonUnavailable
	}
	b.onUpdate(taskID, u)
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	if cancel := b.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

// Resume asks the API again, as the wait it was paused in may be over.
func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	handed, link := b.handed[taskID], b.link[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link)
	}
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if cancel := b.cancel[taskID]; cancel != nil {
		cancel()
	}
	handed := b.handed[taskID]
	delete(b.cancel, taskID)
	delete(b.link, taskID)
	delete(b.conns, taskID)
	delete(b.handed, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID, deleteFiles)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
