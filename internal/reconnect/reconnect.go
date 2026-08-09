// Package reconnect gets the box a new public IP address, which is the only
// thing that lifts a hoster's free-user limit when the limit is keyed to the
// address. It is KnightLoader's answer to JDownloader's Reconnect and speaks the
// same dialects: ask the gateway over UPnP, run an external program, hand a
// script to an interpreter, replay a recorded list of HTTP requests against the
// router's admin interface, or nothing at all.
//
// UPnP is the one to reach for first, because it is the only method that needs
// nothing from the user: the gateway is found by asking the network. The other
// three exist for the routers that have UPnP switched off, and ImportScript
// reads JDownloader's LiveHeader and curl scripts into the HTTP method so a
// router that already has a script written for it does not need a new one.
//
// The address is the ground truth. Every run reads the public address before the
// method fires and polls for it afterwards, and a run that ends on the address
// it started with is reported as a failure even when the router said it was
// happy. That is the whole point: the caller's next move after a successful
// reconnect is to retry a download, and retrying from the same address is
// exactly what got it limited in the first place.
//
// The router password never leaves here in an error string. It is substituted
// into program arguments, URLs and request bodies, so every error is filtered
// through one redaction step on its way out.
//
// One caveat comes with the territory: a request that tells a router to reboot
// often kills the connection before an answer arrives, and that transport error
// fails the run. Point the last step of a script at something that answers, or
// use the command method with a wrapper that swallows the error.
package reconnect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Doer is the part of an HTTP client this package uses. *http.Client satisfies
// it; tests substitute something that answers without a socket.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Runner executes an external program. It is a function field rather than a call
// to os/exec so a test can prove which program would have run without any test
// run ever spawning a process.
type Runner func(ctx context.Context, name string, args ...string) error

// Options configures a Reconnector. Every field but Config has a working
// default, so the production caller sets one thing and the tests set the rest.
type Options struct {
	// Config returns the current configuration. It is a function rather than a
	// value because a reconnect triggered after the user edited the settings
	// must use the credentials that are current now, not the ones that happened
	// to be loaded when the process started.
	Config func() Config

	HTTP  Doer   // defaults to a dedicated client with a timeout
	Run   Runner // defaults to os/exec
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	// Discover finds UPnP gateways. It defaults to an SSDP multicast search;
	// tests replace it so that no test in this package sends a datagram.
	Discover Discoverer

	// TempDir is where the script method writes the script it is about to hand
	// to an interpreter. Empty means the system temporary directory.
	TempDir string
}

// Result describes what one reconnect achieved. It is filled in as far as the
// run got, so a failed run still reports the address it was stuck on.
type Result struct {
	OldIP  netip.Addr    // the address before the method ran
	NewIP  netip.Addr    // the address afterwards; invalid unless the run succeeded
	Checks int           // how many times the check URL was polled after the method
	Took   time.Duration // wall time from the first check to the verdict
}

// Reconnector runs reconnects, one at a time. It owns no goroutines: a run
// happens on the goroutine that called Do and stops when that caller's context
// is cancelled.
type Reconnector struct {
	config   func() Config
	http     Doer
	run      Runner
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
	discover Discoverer
	tempDir  string

	mu       sync.Mutex
	inflight *call
}

// call is one in-progress run that later callers attach to instead of starting
// their own.
type call struct {
	done chan struct{}
	res  Result
	err  error

	// settled is false while the run is still going and stays false if it
	// unwound through a panic, which is the only way a call can reach release
	// with no verdict in it.
	settled bool
}

// errAbandoned is the verdict of a run that did not finish. It exists because
// the alternative is worse than any error: a panicking run leaves the zero
// Result and a nil error behind, and a waiting caller reads that pair as a
// successful reconnect and retries the download from the address that got it
// limited.
var errAbandoned = errors.New("reconnect: the run did not finish")

// The per-request ceilings. They exist because the injected client may have no
// timeout of its own, and a router that accepts a connection and then stops
// talking would otherwise hold the reconnect open indefinitely.
const (
	checkTimeout   = 20 * time.Second
	requestTimeout = 30 * time.Second
)

// maxCheckBody caps what an IP check may return. The answer is a handful of
// bytes; anything larger means the URL points at something that is not an IP
// check, and reading it in full would be a self-inflicted denial of service.
const maxCheckBody = 64 << 10

// maxDrain is how much of a router's answer is read before moving to the next
// step. The body is drained so the connection can be reused by the next request
// in the script, but an admin page is not worth holding in memory.
const maxDrain = 64 << 10

// maxCommandOutput is how much of a failing program's output is quoted in the
// error. A script that dumps a megabyte on failure must not put a megabyte into
// the log line that reports it.
const maxCommandOutput = 512

// New builds a Reconnector.
func New(o Options) (*Reconnector, error) {
	if o.Config == nil {
		// Falling back to an empty config would make every reconnect report
		// "not configured" forever, so a wiring mistake would look exactly like
		// a user who never filled the form in.
		return nil, errors.New("reconnect: Config is required")
	}
	r := &Reconnector{
		config:   o.Config,
		http:     o.HTTP,
		run:      o.Run,
		now:      o.Now,
		sleep:    o.Sleep,
		discover: o.Discover,
		tempDir:  o.TempDir,
	}
	if r.http == nil {
		// A dedicated client rather than http.DefaultClient: the default has no
		// timeout at all, and a router that hangs mid-answer would then hold a
		// reconnect open for as long as the process lives.
		//
		// This is the last hand-built client in the tree and it stays that way on
		// purpose. Everything else now comes from internal/httpx, and the app does
		// hand this package an httpx client - see app.New. Importing httpx here
		// would couple a package whose whole shape is "one injectable Doer" to the
		// policy it is meant to be given, and would leave the fallback path
		// importing a package the injected path never uses.
		r.http = &http.Client{Timeout: requestTimeout}
	}
	if r.run == nil {
		r.run = execRunner
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = sleepCtx
	}
	if r.discover == nil {
		r.discover = ssdpSearch
	}
	return r, nil
}

// Busy reports whether a reconnect is running, so a UI can show it without
// having to start one to find out.
func (r *Reconnector) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inflight != nil
}

// Do performs one reconnect and reports whether the address actually changed.
//
// A second caller that arrives while a run is in progress waits for that run's
// result instead of starting its own. Two reconnects at once would fight over
// the same router, and worse, each would see the other's address change and
// report a success for work it did not do. The first caller's context governs
// the run itself; a waiting caller can still give up on its own context.
func (r *Reconnector) Do(ctx context.Context) (Result, error) {
	r.mu.Lock()
	if c := r.inflight; c != nil {
		r.mu.Unlock()
		select {
		case <-c.done:
			return c.res, c.err
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	r.inflight = c
	r.mu.Unlock()

	// The slot is given back through a defer because the run calls out to code
	// this package does not own - an injected Runner, an injected Doer - and a
	// panic in any of it would otherwise leave inflight pointing at a call whose
	// done channel nothing ever closes. That is not a failed reconnect, it is a
	// reconnect subsystem that is finished for the life of the process: Busy
	// stays true and every later Do blocks until its own context expires.
	defer r.release(c)

	c.res, c.err = r.reconnect(ctx)
	c.settled = true
	return c.res, c.err
}

// release ends a run and wakes everyone waiting on it.
func (r *Reconnector) release(c *call) {
	if !c.settled {
		c.res, c.err = Result{}, errAbandoned
	}
	r.mu.Lock()
	r.inflight = nil
	r.mu.Unlock()
	// Closed last, so a caller that attached to this run cannot read res and err
	// before they are final.
	close(c.done)
}

// reconnect is one run, and the only place an error is allowed to escape from.
// Sanitising here rather than trusting the caller means a configuration loaded
// from a hand-edited settings file cannot produce a zero poll interval.
func (r *Reconnector) reconnect(ctx context.Context) (Result, error) {
	cfg := Sanitize(r.config())
	res, err := r.attempt(ctx, cfg)
	return res, cfg.redact(err)
}

func (r *Reconnector) attempt(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}
	start := r.now()

	// The "before" read has to succeed. Without it there is nothing to compare
	// against, and a run with no baseline could only ever report a success it
	// cannot prove - which is the failure this package refuses to produce.
	old, err := r.currentIP(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	res := Result{OldIP: old}

	// Cancellation is checked here, immediately before the one step of a run that
	// changes something outside this process. The method is not reliably
	// cancellable on its own: a Runner is free to ignore the context, and a shell
	// wrapper that has already been handed the reboot URL will finish the job
	// whatever happens to the process that started it. Rebooting the router on
	// the way out of a shutdown drops every download that was still running.
	if err := ctx.Err(); err != nil {
		res.Took = r.now().Sub(start)
		return res, err
	}

	if err := r.invoke(ctx, cfg, old); err != nil {
		res.Took = r.now().Sub(start)
		return res, err
	}

	// The wait budget starts once the method is done: a router command that
	// takes half a minute of its own should not eat the time we are willing to
	// spend waiting for the new address.
	deadline := r.now().Add(cfg.Timeout())
	for {
		if err := r.sleep(ctx, cfg.Interval()); err != nil {
			res.Took = r.now().Sub(start)
			return res, err
		}
		res.Checks++
		cur, checkErr := r.currentIP(ctx, cfg)
		if checkErr == nil && cur != old {
			res.NewIP = cur
			res.Took = r.now().Sub(start)
			return res, nil
		}
		if !r.now().Before(deadline) {
			res.Took = r.now().Sub(start)
			if checkErr != nil {
				// The address never moved and the last look also failed, so say
				// both: a check URL that has been down all along and a router
				// that ignored us need different fixes.
				return res, fmt.Errorf("%w: still %s, and the last check failed: %v", ErrUnchanged, old, checkErr)
			}
			return res, fmt.Errorf("%w: still %s after %s", ErrUnchanged, old, res.Took.Round(time.Second))
		}
		// A check that fails mid-poll is expected rather than fatal: the router
		// is rebooting, which is precisely what we asked it to do, and the box
		// has no route out until it comes back. Only the deadline ends the wait.
	}
}

// invoke runs the configured method once.
func (r *Reconnector) invoke(ctx context.Context, cfg Config, ip netip.Addr) error {
	vars := cfg.vars(ip)
	switch cfg.Method {
	case MethodCommand:
		args := make([]string, len(cfg.Args))
		for i, a := range cfg.Args {
			args[i] = expandVars(a, vars)
		}
		if err := r.run(ctx, expandVars(cfg.Command, vars), args...); err != nil {
			return fmt.Errorf("reconnect: %s: %w", cfg.Command, err)
		}
		return nil
	case MethodHTTP:
		for i, q := range cfg.Requests {
			if err := r.request(ctx, q, vars); err != nil {
				// The unexpanded URL goes into the message, not the one the
				// request was made with: the template says %%password%% where
				// the real URL says the password.
				return fmt.Errorf("reconnect: request %d (%s %s): %w", i+1, q.Method, q.URL, err)
			}
		}
		return nil
	case MethodUPnP:
		return r.upnp(ctx, cfg)
	case MethodScript:
		return r.script(ctx, cfg, vars)
	}
	return fmt.Errorf("%w: reconnect is switched off", ErrNotConfigured)
}

// request performs one step of an HTTP script.
func (r *Reconnector) request(ctx context.Context, q Request, vars map[string]string) error {
	method := q.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if b := expandVars(q.Body, vars); b != "" {
		body = strings.NewReader(b)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, expandVars(q.URL, vars), body)
	if err != nil {
		return err
	}

	// Header names are applied in a fixed order so the same script always
	// produces the same request, which stops mattering only if no two names in
	// one script ever differ by case alone.
	names := make([]string, 0, len(q.Headers))
	for k := range q.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := expandVars(q.Headers[k], vars)
		// A Host set through the header map is ignored by net/http; the field on
		// the request is what reaches the wire. Router firmware that virtual-
		// hosts its admin page needs it, and getting this wrong produces a
		// script that looks right and never logs in.
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		// A recorded login post carries a form body. Most router firmware
		// answers a post with no content type with a parse error rather than a
		// session, so the usual type is filled in when the script did not say.
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	if resp == nil {
		return errNoResponse
	}
	drain(resp)
	if !ok2xx(resp.StatusCode) {
		return fmt.Errorf("unexpected status %s", statusText(resp))
	}
	return nil
}

// currentIP reads the public address off the check URL.
func (r *Reconnector) currentIP(ctx context.Context, cfg Config) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.CheckURL, nil)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", err)
	}
	// Several echo services serve a full HTML page unless asked for text. The
	// parser copes with either, but the small answer is cheaper and safer.
	req.Header.Set("Accept", "text/plain, */*")

	resp, err := r.http.Do(req)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", err)
	}
	if resp == nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", errNoResponse)
	}
	// One byte past the cap, so a body that is exactly at the limit is still
	// recognised as complete.
	raw, err := io.ReadAll(io.LimitReader(bodyOf(resp), maxCheckBody+1))
	closeBody(resp)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", err)
	}
	if !ok2xx(resp.StatusCode) {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: unexpected status %s", statusText(resp))
	}
	if len(raw) > maxCheckBody {
		raw = dropPartialTail(raw[:maxCheckBody])
	}
	// PublicIP rather than FindIP: finding an address is not the same as finding
	// this box's address on the internet. A router's own status page and a
	// captive portal both answer with an address from the wrong side of the
	// router, and taking one of those as the public address fails in the way
	// that is hardest to diagnose - the LAN address holds still, so every run
	// reports that the address did not change and the reconnect gets blamed for
	// a router that did exactly as it was told. The error names the address and
	// the range it falls in; the check URL is added here because only this layer
	// knows which URL produced the answer.
	addr, err := PublicIP(string(raw))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %s", err, cfg.CheckURL)
	}
	return addr, nil
}

func ok2xx(code int) bool { return code >= 200 && code <= 299 }

// statusText prefers the status line the server sent and falls back to the bare
// code, which is what a hand-built response carries.
func statusText(resp *http.Response) string {
	if resp.Status != "" {
		return resp.Status
	}
	return strconv.Itoa(resp.StatusCode)
}

// errNoResponse is what a Doer that returns neither a response nor an error
// gets. *http.Client never does this, but Doer is an interface, and the whole
// point of the interface is that something else can be behind it - a caching
// wrapper, a proxy-aware client, a test double. Turning that into an error
// rather than a nil dereference keeps a third-party bug from taking down the
// reconnect subsystem with it.
var errNoResponse = errors.New("the HTTP client returned no response")

// bodyOf and closeBody tolerate a response without a body. *http.Client always
// provides one, but Doer is an interface and a nil body from some other
// implementation would panic inside the reconnect rather than fail it.
func bodyOf(resp *http.Response) io.Reader {
	if resp.Body == nil {
		return strings.NewReader("")
	}
	return resp.Body
}

func closeBody(resp *http.Response) {
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func drain(resp *http.Response) {
	if resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	_ = resp.Body.Close()
}

// execRunner is the default Runner. It quotes the program's own output in the
// error because a reconnect script that fails silently is untraceable: the exit
// status alone never says which line of the script gave up.
func execRunner(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		if len(text) > maxCommandOutput {
			text = text[:maxCommandOutput] + "..."
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return err
}

// sleepCtx is the default wait: it gives up the moment the caller's context is
// cancelled, so a shutdown does not have to sit out a poll interval.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
