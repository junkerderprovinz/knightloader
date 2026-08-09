package reconnect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const checkURL = "http://check.invalid/ip"

// step is one scripted answer from the stub client.
type step struct {
	body string
	code int   // 0 means 200
	err  error // when set, the client fails instead of answering
}

// sentRequest is what the stub client saw on a router request.
type sentRequest struct {
	method  string
	url     string
	host    string
	headers http.Header
	body    string
}

// stubClient answers through the Doer interface instead of a socket, so no test
// in this package opens a connection or depends on a machine's network. Requests
// to the check URL walk the checks script, whose last entry repeats forever;
// everything else is a router request and is recorded.
type stubClient struct {
	checks []step
	reply  func(*http.Request) (*http.Response, error)

	mu      sync.Mutex
	checked int
	sent    []sentRequest
}

func (c *stubClient) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		// Put it back so a scripted reply can read it too, the way a real
		// transport hands the same bytes to whatever inspects the request.
		req.Body = io.NopCloser(strings.NewReader(body))
	}
	if req.URL.String() == checkURL {
		c.mu.Lock()
		var s step
		switch {
		case c.checked < len(c.checks):
			s = c.checks[c.checked]
		case len(c.checks) > 0:
			s = c.checks[len(c.checks)-1]
		}
		c.checked++
		c.mu.Unlock()
		if s.err != nil {
			return nil, s.err
		}
		return response(s.code, s.body), nil
	}

	c.mu.Lock()
	c.sent = append(c.sent, sentRequest{
		method:  req.Method,
		url:     req.URL.String(),
		host:    req.Host,
		headers: req.Header.Clone(),
		body:    body,
	})
	c.mu.Unlock()
	if c.reply != nil {
		return c.reply(req)
	}
	return response(http.StatusOK, "ok"), nil
}

func (c *stubClient) counts() (checks, sent int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checked, len(c.sent)
}

func (c *stubClient) request(i int) sentRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent[i]
}

func response(code int, body string) *http.Response {
	if code == 0 {
		code = http.StatusOK
	}
	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// fakeClock advances only when the code under test waits, so a poll loop with a
// two-minute budget runs to its deadline instantly and the test still exercises
// the real number of checks.
type fakeClock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	c.slept = append(c.slept, d)
	return nil
}

func (c *fakeClock) naps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// runRecorder stands in for os/exec: it remembers what would have been run.
type runRecorder struct {
	err    error
	before func()

	mu   sync.Mutex
	n    int
	name string
	args []string
}

func (r *runRecorder) run(_ context.Context, name string, args ...string) error {
	if r.before != nil {
		r.before()
	}
	r.mu.Lock()
	r.n++
	r.name, r.args = name, args
	r.mu.Unlock()
	return r.err
}

func (r *runRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *runRecorder) last() (string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.name, append([]string(nil), r.args...)
}

// newTestReconnector wires a Reconnector to the fakes above.
func newTestReconnector(t *testing.T, cfg Config, client Doer, run Runner, clock *fakeClock) *Reconnector {
	t.Helper()
	rc, err := New(Options{
		Config: func() Config { return cfg },
		HTTP:   client,
		Run:    run,
		Now:    clock.now,
		Sleep:  clock.sleep,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rc
}

func commandConfig() Config {
	return Config{
		Method:          MethodCommand,
		Username:        "admin",
		Password:        "s3cret",
		Command:         "/usr/local/bin/reconnect.sh",
		Args:            []string{"--user", "%%username%%", "--pass", "%%password%%", "--was", "%%ip%%"},
		CheckURL:        checkURL,
		IntervalSeconds: 5,
		TimeoutSeconds:  60,
	}
}

// TestNewRequiresConfig: a Reconnector with no source of configuration would
// report "not configured" for the rest of the process's life, which is a wiring
// mistake wearing the costume of a user who never filled the form in.
func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New accepted Options without a Config function")
	}
}

// TestSwitchedOffTouchesNothing proves the "none" method is genuinely off: no
// request goes out, not even the address check.
func TestSwitchedOffTouchesNothing(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}}}
	runner := &runRecorder{}
	rc := newTestReconnector(t, Config{Method: MethodNone, CheckURL: checkURL}, client, runner.run, newClock())

	_, err := rc.Do(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Do() = %v, want ErrNotConfigured", err)
	}
	if checks, sent := client.counts(); checks != 0 || sent != 0 {
		t.Errorf("a switched-off reconnect made %d checks and %d requests", checks, sent)
	}
	if runner.calls() != 0 {
		t.Error("a switched-off reconnect ran a program")
	}
}

// TestCommandMethodExpandsVariables covers the whole happy path of the command
// method, including that the address handed to the script is the one the box had
// before the reconnect - that is the session a router script has to identify.
func TestCommandMethodExpandsVariables(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7\n"}}}
	runner := &runRecorder{}
	clock := newClock()
	rc := newTestReconnector(t, commandConfig(), client, runner.run, clock)

	res, err := rc.Do(context.Background())
	if err != nil {
		t.Fatalf("Do() = %v", err)
	}
	if res.OldIP.String() != "203.0.113.9" || res.NewIP.String() != "198.51.100.7" {
		t.Errorf("Result = %s -> %s, want 203.0.113.9 -> 198.51.100.7", res.OldIP, res.NewIP)
	}
	if res.Checks != 1 {
		t.Errorf("Checks = %d, want 1", res.Checks)
	}
	if res.Took != 5*time.Second {
		t.Errorf("Took = %v, want 5s", res.Took)
	}

	name, args := runner.last()
	if name != "/usr/local/bin/reconnect.sh" {
		t.Errorf("ran %q", name)
	}
	want := []string{"--user", "admin", "--pass", "s3cret", "--was", "203.0.113.9"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %q, want %q", args, want)
	}
}

// TestHTTPMethodReplaysRequestsInOrder pins the shape of the requests a recorded
// router script produces, including the two details that are easy to get wrong:
// Host has to land on the request field, and a form post needs a content type.
func TestHTTPMethodReplaysRequestsInOrder(t *testing.T) {
	cfg := Config{
		Method:   MethodHTTP,
		Username: "admin",
		Password: "s3cret",
		Requests: []Request{
			{
				Method:  http.MethodPost,
				URL:     "http://router.invalid/login",
				Headers: map[string]string{"Host": "fritz.box", "X-From": "%%ip%%"},
				Body:    "user=%%username%%&pass=%%password%%",
			},
			{Method: http.MethodGet, URL: "http://router.invalid/reconnect"},
		},
		CheckURL:        checkURL,
		IntervalSeconds: 5,
		TimeoutSeconds:  60,
	}
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, newClock())

	if _, err := rc.Do(context.Background()); err != nil {
		t.Fatalf("Do() = %v", err)
	}
	if _, sent := client.counts(); sent != 2 {
		t.Fatalf("sent %d requests, want 2", sent)
	}

	login := client.request(0)
	if login.method != http.MethodPost || login.url != "http://router.invalid/login" {
		t.Errorf("first request = %s %s", login.method, login.url)
	}
	if login.body != "user=admin&pass=s3cret" {
		t.Errorf("body = %q", login.body)
	}
	if login.host != "fritz.box" {
		t.Errorf("Host = %q; a Host left in the header map never reaches the wire", login.host)
	}
	if got := login.headers.Get("Host"); got != "" {
		t.Errorf("Host was also written into the header map as %q", got)
	}
	if got := login.headers.Get("X-From"); got != "203.0.113.9" {
		t.Errorf("X-From = %q, want the old address", got)
	}
	if got := login.headers.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want the form default", got)
	}

	reboot := client.request(1)
	if reboot.method != http.MethodGet || reboot.url != "http://router.invalid/reconnect" {
		t.Errorf("second request = %s %s", reboot.method, reboot.url)
	}
	if got := reboot.headers.Get("Content-Type"); got != "" {
		t.Errorf("a bodyless request got Content-Type %q", got)
	}
}

// TestNonSuccessStatusFails: a router that answers a login with 401 has not
// reconnected anything, and the status has to be in the error or nobody can tell
// a wrong password from a wrong URL.
func TestNonSuccessStatusFails(t *testing.T) {
	cfg := Config{
		Method:          MethodHTTP,
		Requests:        []Request{{Method: http.MethodGet, URL: "http://router.invalid/login"}},
		CheckURL:        checkURL,
		IntervalSeconds: 5,
		TimeoutSeconds:  60,
	}
	client := &stubClient{
		checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}},
		reply:  func(*http.Request) (*http.Response, error) { return response(http.StatusUnauthorized, "no"), nil },
	}
	clock := newClock()
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, clock)

	_, err := rc.Do(context.Background())
	if err == nil {
		t.Fatal("a 401 was reported as a successful reconnect")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not name the status", err)
	}
	if !strings.Contains(err.Error(), "request 1") {
		t.Errorf("error %q does not name the failing step", err)
	}
	// A failed script must not be followed by the wait: the address is not going
	// to change on its own, and the caller should hear about it now.
	if naps := clock.naps(); len(naps) != 0 {
		t.Errorf("the poll loop ran anyway: %v", naps)
	}
}

// TestUnchangedAddressIsAFailure is the rule the whole package exists for. The
// method ran and reported success; the address did not move; the caller must be
// told this failed, or it retries the download from the address that got it
// limited in the first place.
func TestUnchangedAddressIsAFailure(t *testing.T) {
	cfg := commandConfig()
	cfg.TimeoutSeconds = 20
	client := &stubClient{checks: []step{{body: "203.0.113.9"}}}
	runner := &runRecorder{}
	clock := newClock()
	rc := newTestReconnector(t, cfg, client, runner.run, clock)

	res, err := rc.Do(context.Background())
	if !errors.Is(err, ErrUnchanged) {
		t.Fatalf("Do() = %v, want ErrUnchanged", err)
	}
	if runner.calls() != 1 {
		t.Errorf("the command ran %d times, want 1", runner.calls())
	}
	if res.OldIP.String() != "203.0.113.9" {
		t.Errorf("OldIP = %s", res.OldIP)
	}
	if res.NewIP.IsValid() {
		t.Errorf("NewIP = %s on a failed run, want nothing", res.NewIP)
	}
	if res.Checks != 4 {
		t.Errorf("Checks = %d, want 4 (20s of budget at 5s apart)", res.Checks)
	}
	if !strings.Contains(err.Error(), "203.0.113.9") {
		t.Errorf("error %q does not say which address it is stuck on", err)
	}
}

// TestMappedAddressIsNotAChange: a check page that switches between
// "::ffff:203.0.113.9" and "203.0.113.9" is showing one address in two
// spellings. Reading that as a change would report a success for every single
// reconnect, whatever the router did.
func TestMappedAddressIsNotAChange(t *testing.T) {
	cfg := commandConfig()
	cfg.TimeoutSeconds = 10
	client := &stubClient{checks: []step{{body: "::ffff:203.0.113.9"}, {body: "203.0.113.9"}}}
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, newClock())

	if _, err := rc.Do(context.Background()); !errors.Is(err, ErrUnchanged) {
		t.Fatalf("Do() = %v, want ErrUnchanged", err)
	}
}

// TestFailingChecksWhileRebootingAreTolerated: the router is down because we
// told it to go down, so the box has no route out for a while. Only the deadline
// ends the wait, never a single failed look.
func TestFailingChecksWhileRebootingAreTolerated(t *testing.T) {
	cfg := commandConfig()
	client := &stubClient{checks: []step{
		{body: "203.0.113.9"},
		{err: errors.New("dial tcp: connect: network is unreachable")},
		{code: http.StatusBadGateway, body: "bad gateway"},
		{body: "198.51.100.7"},
	}}
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, newClock())

	res, err := rc.Do(context.Background())
	if err != nil {
		t.Fatalf("Do() = %v, want the reconnect to survive two failed checks", err)
	}
	if res.Checks != 3 {
		t.Errorf("Checks = %d, want 3", res.Checks)
	}
	if res.NewIP.String() != "198.51.100.7" {
		t.Errorf("NewIP = %s", res.NewIP)
	}
}

// TestBaselineFailuresAbortBeforeTheMethodRuns covers the three ways the first
// look can fail. All of them have to stop the run: without a "before" address
// there is nothing to compare against, so a reconnect done here could only ever
// be reported as a success nobody can prove.
func TestBaselineFailuresAbortBeforeTheMethodRuns(t *testing.T) {
	oversized := strings.Repeat("x", maxCheckBody-11) + "203.0.113.99"

	tests := []struct {
		name    string
		first   step
		wantErr error
	}{
		{"transport failure", step{err: errors.New("no such host")}, nil},
		{"http error", step{code: http.StatusServiceUnavailable, body: "later"}, nil},
		{"page holds no address", step{body: "<html>please enable javascript</html>"}, ErrNoAddress},
		// A check URL pointed at something inside the network answers with an
		// address from the wrong side of the router. Refusing it here is the
		// whole point: taken as the baseline, a LAN address holds still, so
		// every run afterwards reports that the address did not change and the
		// reconnect is blamed for a router that did as it was told.
		{"the router's own status page", step{body: "IP: 192.168.1.1"}, ErrNotPublic},
		// Carrier-grade NAT is the cruel one. It parses, it is not RFC 1918, and
		// it is the single line on which a reconnect can never change anything -
		// the address that moves belongs to the carrier, not to this box.
		{"a carrier-grade NAT address", step{body: "100.64.12.9"}, ErrNotPublic},
		// The truncation guard: cutting "203.0.113.99" at the read limit leaves
		// the valid, wrong address 203.0.113.9, and a baseline that is wrong
		// makes every later check look like a change.
		{"body cut at the read limit", step{body: oversized}, ErrNoAddress},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &stubClient{checks: []step{tc.first, {body: "198.51.100.7"}}}
			runner := &runRecorder{}
			rc := newTestReconnector(t, commandConfig(), client, runner.run, newClock())

			res, err := rc.Do(context.Background())
			if err == nil {
				t.Fatalf("Do() succeeded with %s -> %s", res.OldIP, res.NewIP)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("Do() = %v, want %v", err, tc.wantErr)
			}
			if runner.calls() != 0 {
				t.Error("the reconnect method ran without a baseline address")
			}
		})
	}
}

// TestConfigIsSanitisedOnEveryRun: a settings file edited by hand can carry a
// zero interval or a method spelled the JDownloader way, and neither may reach
// the poll loop.
func TestConfigIsSanitisedOnEveryRun(t *testing.T) {
	cfg := Config{
		Method:   "LiveHeader",
		Requests: []Request{{URL: "http://router.invalid/reconnect"}},
		CheckURL: checkURL,
	}
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	clock := newClock()
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, clock)

	if _, err := rc.Do(context.Background()); err != nil {
		t.Fatalf("Do() = %v", err)
	}
	if _, sent := client.counts(); sent != 1 {
		t.Errorf("sent %d requests; the JD spelling of the method was not recognised", sent)
	}
	naps := clock.naps()
	if len(naps) != 1 || naps[0] != defaultIntervalSeconds*time.Second {
		t.Errorf("naps = %v, want one wait of %ds", naps, defaultIntervalSeconds)
	}
}

// TestCancellationEndsTheWait: a shutdown must not have to sit out a poll
// interval, and the error has to stay recognisable as a cancellation.
func TestCancellationEndsTheWait(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}}}
	runner := &runRecorder{}
	rc := newTestReconnector(t, commandConfig(), client, runner.run, newClock())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := rc.Do(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = %v, want context.Canceled", err)
	}
	if res.Checks != 0 {
		t.Errorf("Checks = %d after cancellation", res.Checks)
	}
}

// TestCancellationDoesNotRebootTheRouter is the half of cancellation that costs
// something to get wrong. A Runner is under no obligation to honour a context -
// the injected one here does not, and neither does a shell script that has
// already been handed the reboot URL - so a cancelled run that still calls the
// method drops every download that was in flight, on the way out of a shutdown
// that was supposed to leave them alone.
func TestCancellationDoesNotRebootTheRouter(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	runner := &runRecorder{}
	rc := newTestReconnector(t, commandConfig(), client, runner.run, newClock())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rc.Do(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = %v, want context.Canceled", err)
	}
	if runner.calls() != 0 {
		t.Errorf("a cancelled reconnect ran the command %d time(s)", runner.calls())
	}
}

// TestPanicDoesNotWedgeTheReconnector: the method is other people's code - an
// injected Runner, an injected Doer - and a panic in it must cost one run, not
// the process. Releasing the in-flight slot anywhere but a defer leaves Busy
// stuck at true and every later Do blocked on a channel nobody will close.
func TestPanicDoesNotWedgeTheReconnector(t *testing.T) {
	// Two baselines, because the panicking run reads one before it dies.
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	// Only the first run blows up, so the second one can prove the Reconnector is
	// still usable rather than merely unblocked.
	runner := &runRecorder{}
	first := true
	blowUpOnce := func(ctx context.Context, name string, args ...string) error {
		if first {
			first = false
			panic("the runner blew up")
		}
		return runner.run(ctx, name, args...)
	}
	rc := newTestReconnector(t, commandConfig(), client, blowUpOnce, newClock())

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic was swallowed; a caller has to see its own bug")
			}
		}()
		_, _ = rc.Do(context.Background())
	}()

	if rc.Busy() {
		t.Fatal("Busy() is still true after a panicking run")
	}

	type outcome struct {
		res Result
		err error
	}
	next := make(chan outcome, 1)
	go func() {
		res, err := rc.Do(context.Background())
		next <- outcome{res, err}
	}()
	select {
	case got := <-next:
		if got.err != nil {
			t.Fatalf("the run after a panic failed: %v", got.err)
		}
		if got.res.NewIP.String() != "198.51.100.7" {
			t.Errorf("NewIP = %s", got.res.NewIP)
		}
		if runner.calls() != 1 {
			t.Errorf("the second run called the command %d times", runner.calls())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a later Do blocked; the in-flight slot was never released")
	}
}

// TestPanicIsNotReportedAsSuccess is the same failure seen from the waiting
// caller's side. A run that unwound left the zero Result and a nil error in the
// slot, and (no address, no error) reads as "the reconnect worked" - the exact
// lie this package exists to refuse.
func TestPanicIsNotReportedAsSuccess(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	entered := make(chan struct{})
	release := make(chan struct{})
	panicking := func(context.Context, string, ...string) error {
		close(entered)
		// Parked until the follower below has had time to attach, so the
		// follower cannot win the race and start a run of its own.
		<-release
		panic("the runner blew up")
	}
	rc := newTestReconnector(t, commandConfig(), client, panicking, newClock())

	go func() {
		defer func() { _ = recover() }()
		_, _ = rc.Do(context.Background())
	}()
	<-entered
	time.AfterFunc(50*time.Millisecond, func() { close(release) })

	res, err := rc.Do(context.Background())
	if err == nil {
		t.Fatalf("a run that panicked was reported to the waiting caller as a success: %+v", res)
	}
	if res.NewIP.IsValid() {
		t.Errorf("NewIP = %s on a run that never finished", res.NewIP)
	}
}

// TestNilResponseFails: Doer exists so something other than *http.Client can sit
// behind it, and something else is allowed to be buggy. Neither an answer nor an
// error must fail the run rather than dereference nil, because a panic here is
// held inside the in-flight slot and takes the whole subsystem with it.
func TestNilResponseFails(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		doer Doer
	}{
		{"on the ip check", commandConfig(), nilDoer{}},
		{
			"on a router request",
			Config{
				Method:          MethodHTTP,
				Requests:        []Request{{URL: "http://router.invalid/reconnect"}},
				CheckURL:        checkURL,
				IntervalSeconds: 5,
				TimeoutSeconds:  30,
			},
			&stubClient{
				checks: []step{{body: "203.0.113.9"}},
				reply:  func(*http.Request) (*http.Response, error) { return nil, nil },
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := newTestReconnector(t, tc.cfg, tc.doer, (&runRecorder{}).run, newClock())
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("a nil response panicked instead of failing the run: %v", p)
				}
			}()
			if _, err := rc.Do(context.Background()); err == nil {
				t.Error("a nil response was accepted as an answer")
			}
			if rc.Busy() {
				t.Error("the run held on to the in-flight slot")
			}
		})
	}
}

// nilDoer answers every request with neither a response nor an error, which is
// the one thing *http.Client promises never to do.
type nilDoer struct{}

func (nilDoer) Do(*http.Request) (*http.Response, error) { return nil, nil }

// TestSecondCallerWaitsForTheFirst: two reconnects at once would fight over the
// same router, and each would see the other's address change and claim a success
// for work it did not do. The second caller gets the first one's verdict.
func TestSecondCallerWaitsForTheFirst(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	release := make(chan struct{})
	entered := make(chan struct{})
	runner := &runRecorder{before: func() {
		close(entered)
		<-release
	}}
	rc := newTestReconnector(t, commandConfig(), client, runner.run, newClock())

	var (
		leaderRes Result
		leaderErr error
		done      = make(chan struct{})
	)
	go func() {
		defer close(done)
		leaderRes, leaderErr = rc.Do(context.Background())
	}()

	// The run is provably in flight once the runner has been entered, so the
	// call below can only attach to it. The leader is let go on a timer because
	// this goroutine is about to block inside that attachment.
	<-entered
	if !rc.Busy() {
		t.Error("Busy() is false during a run")
	}
	time.AfterFunc(50*time.Millisecond, func() { close(release) })

	followerRes, followerErr := rc.Do(context.Background())
	<-done

	if runner.calls() != 1 {
		t.Fatalf("the command ran %d times; the second caller started its own reconnect", runner.calls())
	}
	if checks, _ := client.counts(); checks != 2 {
		t.Errorf("the check URL was polled %d times, want 2", checks)
	}
	if followerErr != leaderErr || followerRes != leaderRes {
		t.Errorf("follower got (%v, %v), leader got (%v, %v)", followerRes, followerErr, leaderRes, leaderErr)
	}
	if leaderErr != nil {
		t.Fatalf("Do() = %v", leaderErr)
	}
	if rc.Busy() {
		t.Error("Busy() is still true after the run finished")
	}
}

// TestPasswordNeverReachesTheError is the promise the package doc makes. The
// password is substituted into program arguments, into URLs and into bodies, so
// it can arrive in an error through several routes at once - each row here is a
// route that really does carry it.
func TestPasswordNeverReachesTheError(t *testing.T) {
	tests := []struct {
		name     string
		password string
		cfg      func(pw string) Config
		reply    func(*http.Request) (*http.Response, error)
		runErr   func(pw string) error
	}{
		{
			name:     "command output quoted in the error",
			password: "s3cret-pass",
			cfg: func(pw string) Config {
				c := commandConfig()
				c.Password = pw
				return c
			},
			runErr: func(pw string) error {
				return fmt.Errorf("exit status 2: usage: reconnect.sh --pass %s", pw)
			},
		},
		{
			// The URL cannot be parsed, and url.Error prints the raw URL it
			// choked on - password and all - before a single byte goes out.
			name:     "unparsable url with credentials",
			password: "spaced out",
			cfg: func(pw string) Config {
				return Config{
					Method:          MethodHTTP,
					Username:        "admin",
					Password:        pw,
					Requests:        []Request{{URL: "http://%%username%%:%%password%%@router.invalid/reboot"}},
					CheckURL:        checkURL,
					IntervalSeconds: 5,
					TimeoutSeconds:  30,
				}
			},
		},
		{
			// Here the URL parses, so what reaches the error is the userinfo
			// encoding of the password, which matches neither the plain text nor
			// the query or path escaping of it.
			name:     "transport error quoting the request url",
			password: "Fri;tz!Box@2026",
			cfg: func(pw string) Config {
				return Config{
					Method:          MethodHTTP,
					Username:        "admin",
					Password:        pw,
					Requests:        []Request{{URL: "http://%%username%%:%%password%%@router.invalid/reboot"}},
					CheckURL:        checkURL,
					IntervalSeconds: 5,
					TimeoutSeconds:  30,
				}
			},
			reply: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("dial tcp: %s: connection refused", req.URL.String())
			},
		},
		{
			name:     "body echoed back by the router",
			password: "s3cret-pass",
			cfg: func(pw string) Config {
				return Config{
					Method:          MethodHTTP,
					Username:        "admin",
					Password:        pw,
					Requests:        []Request{{Method: http.MethodPost, URL: "http://router.invalid/login", Body: "pw=%%password%%"}},
					CheckURL:        checkURL,
					IntervalSeconds: 5,
					TimeoutSeconds:  30,
				}
			},
			reply: func(req *http.Request) (*http.Response, error) {
				b, _ := io.ReadAll(req.Body)
				return nil, fmt.Errorf("write failed after sending %q", string(b))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &stubClient{checks: []step{{body: "203.0.113.9"}}, reply: tc.reply}
			runner := &runRecorder{}
			if tc.runErr != nil {
				runner.err = tc.runErr(tc.password)
			}
			rc := newTestReconnector(t, tc.cfg(tc.password), client, runner.run, newClock())

			_, err := rc.Do(context.Background())
			if err == nil {
				t.Fatal("Do() succeeded, so there is no error to inspect")
			}
			for _, rendered := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err)} {
				if strings.Contains(rendered, tc.password) {
					t.Fatalf("the password is in the error: %s", rendered)
				}
			}
			if !strings.Contains(err.Error(), RedactedPassword) {
				t.Errorf("nothing was redacted out of %q, so the leak may simply have moved", err)
			}
		})
	}
}

// TestRunnerFailureSkipsTheWait: a command that failed has not reconnected
// anything, so the caller hears about it now instead of after the full budget.
func TestRunnerFailureSkipsTheWait(t *testing.T) {
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	runner := &runRecorder{err: errors.New("exit status 127")}
	clock := newClock()
	rc := newTestReconnector(t, commandConfig(), client, runner.run, clock)

	res, err := rc.Do(context.Background())
	if err == nil {
		t.Fatal("a failed command was reported as a successful reconnect")
	}
	if !strings.Contains(err.Error(), "exit status 127") {
		t.Errorf("error %q loses the reason", err)
	}
	if res.OldIP.String() != "203.0.113.9" {
		t.Errorf("OldIP = %s, want the address the run was stuck on", res.OldIP)
	}
	if naps := clock.naps(); len(naps) != 0 {
		t.Errorf("the poll loop ran after a failed command: %v", naps)
	}
}
