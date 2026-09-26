// Package reconnect gets the box a new public IP address, which lifts a
// hoster's free-user limit when the limit is keyed to the address. It speaks
// JDownloader's reconnect dialects: ask the gateway over UPnP, run an external
// program, hand a script to an interpreter, or replay a recorded list of HTTP
// requests against the router's admin interface.
//
// UPnP needs nothing from the user, since the gateway is found by asking the
// network; the other methods are for routers with UPnP switched off.
// ImportScript reads JDownloader's LiveHeader and curl scripts into the HTTP
// method.
//
// The public address is the ground truth. Every run reads it before the
// method fires and polls it afterwards, and a run that ends on the address it
// started with fails even if the router reported success, because the caller
// will retry a download next.
//
// The router password is substituted into arguments, URLs and bodies, so every
// error passes through one redaction step on its way out.
//
// A request that reboots the router often kills the connection before an
// answer arrives, and that transport error fails the run. Point the last step
// of a script at something that answers, or use the command method with a
// wrapper that swallows the error.
package reconnect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/execx"
)

// Doer is the part of an HTTP client this package uses. *http.Client satisfies
// it; tests substitute something that answers without a socket.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Runner executes an external program. Tests replace it so no test spawns a
// process.
type Runner func(ctx context.Context, name string, args ...string) error

// Options configures a Reconnector. Every field but Config has a working
// default.
type Options struct {
	// Config returns the current configuration, so a reconnect after a
	// settings change uses the credentials that are current.
	Config func() Config

	HTTP  Doer   // defaults to a dedicated client with a timeout
	Run   Runner // defaults to os/exec
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	// Discover finds UPnP gateways. It defaults to an SSDP multicast search;
	// tests replace it so no test sends a datagram.
	Discover Discoverer

	// TempDir is where the script method writes the script for the
	// interpreter. Empty means the system temporary directory.
	TempDir string
}

// Result describes what one reconnect achieved, as far as the run got, so a
// failed run still reports the address it was stuck on.
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

	// settled stays false if the run unwound through a panic.
	settled bool
}

// errAbandoned is the verdict of a run that did not finish. Without it a
// panicking run would leave a zero Result and a nil error, which a waiting
// caller would read as success.
var errAbandoned = errors.New("reconnect: the run did not finish")

// Per-request ceilings, since the injected client may have no timeout and a
// router can accept a connection and stop talking.
const (
	checkTimeout   = 20 * time.Second
	requestTimeout = 30 * time.Second
)

// maxCheckBody caps what an IP check may return. The answer is a handful of
// bytes; anything larger is not an IP check.
const maxCheckBody = 64 << 10

// maxDrain is how much of a router's answer is read so the connection can be
// reused by the next request.
const maxDrain = 64 << 10

// maxCommandOutput is how much of a failing program's output is quoted in the
// error.
const maxCommandOutput = 512

// New builds a Reconnector.
func New(o Options) (*Reconnector, error) {
	if o.Config == nil {
		// An empty fallback would make a wiring mistake look like a user who
		// never filled in the form.
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
		// http.DefaultClient has no timeout. The app passes an internal/httpx
		// client; this fallback keeps the package free of that dependency.
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

// Busy reports whether a reconnect is running.
func (r *Reconnector) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inflight != nil
}

// Do performs one reconnect and reports whether the address changed.
//
// A caller arriving while a run is in progress waits for that run's result.
// Two runs at once would fight over the router and each would take the other's
// address change as its own success. The first caller's context governs the
// run; a waiting caller can still give up on its own.
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

	// Deferred because the run calls injected code, and a panic there must
	// not leave inflight set with a done channel nobody closes.
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
	// Closed last, so waiters read the final res and err.
	close(c.done)
}

// reconnect is one run and the only place an error escapes from, redacted.
// Sanitizing here keeps a hand-edited settings file from producing a zero poll
// interval.
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

	// Without a baseline no success could be proven.
	old, err := r.currentIP(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	res := Result{OldIP: old}

	// Checked right before the step that changes something outside the
	// process. A Runner may ignore the context, and rebooting the router
	// during a shutdown drops every running download.
	if err := ctx.Err(); err != nil {
		res.Took = r.now().Sub(start)
		return res, err
	}

	if err := r.invoke(ctx, cfg, old); err != nil {
		res.Took = r.now().Sub(start)
		return res, err
	}

	// The wait budget starts once the method is done, so a slow router command
	// does not eat it.
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
				// A check URL that was down all along and a router that ignored
				// the request need different fixes, so say both.
				return res, fmt.Errorf("%w: still %s, and the last check failed: %v", ErrUnchanged, old, checkErr)
			}
			return res, fmt.Errorf("%w: still %s after %s", ErrUnchanged, old, res.Took.Round(time.Second))
		}
		// A failing check mid-poll is expected while the router reboots; only
		// the deadline ends the wait.
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
				// The unexpanded URL, which says %%password%% rather than the
				// password.
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

	// Sorted so names differing only in case apply in a fixed order.
	names := make([]string, 0, len(q.Headers))
	for k := range q.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := expandVars(q.Headers[k], vars)
		// net/http ignores a Host in the header map; firmware that
		// virtual-hosts its admin page needs req.Host set.
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		// Most router firmware answers a post without a content type with a
		// parse error rather than a session.
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
	// Several echo services serve a full HTML page unless asked for text.
	req.Header.Set("Accept", "text/plain, */*")

	resp, err := r.http.Do(req)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", err)
	}
	if resp == nil {
		return netip.Addr{}, fmt.Errorf("reconnect: ip check: %w", errNoResponse)
	}
	// One byte past the cap, so a body exactly at the limit is recognised as
	// complete.
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
	// PublicIP refuses a LAN address from a router status page or captive
	// portal. The check URL is added here, the only layer that knows it.
	addr, err := PublicIP(string(raw))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %s", err, cfg.CheckURL)
	}
	return addr, nil
}

func ok2xx(code int) bool { return code >= 200 && code <= 299 }

// statusText prefers the status line the server sent and falls back to the
// bare code, which is all a hand-built response carries.
func statusText(resp *http.Response) string {
	if resp.Status != "" {
		return resp.Status
	}
	return strconv.Itoa(resp.StatusCode)
}

// errNoResponse is returned when a Doer gives neither a response nor an error.
// *http.Client never does, but another Doer might.
var errNoResponse = errors.New("the HTTP client returned no response")

// bodyOf and closeBody tolerate a response without a body, which a Doer other
// than *http.Client might return.
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

// execRunner is the default Runner. It starts the program the way every
// configured program is started (see execx.Run), so a cancelled run ends
// whatever the program started too. It quotes the program's output in the
// error, since the exit status alone never says which line of a script gave up.
func execRunner(ctx context.Context, name string, args ...string) error {
	out, err := execx.Run(ctx, name, args, nil)
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(out); text != "" {
		if len(text) > maxCommandOutput {
			text = text[:maxCommandOutput] + "..."
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return err
}

// sleepCtx is the default wait. It returns as soon as the context is
// cancelled.
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
