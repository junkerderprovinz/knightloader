// Package bridge forwards Click'n'Load submissions to a KnightLoader that runs
// somewhere other than the browser's own machine.
//
// Click'n'Load is hard-wired to 127.0.0.1:9666 by every website that implements
// it, and nothing in the protocol lets a site aim anywhere else. KnightLoader's
// primary deployment is a container on a NAS, where 127.0.0.1 inside the
// container is a different machine from the browser's loopback — so CnL cannot
// reach the app at all, which kills the feature for the main deployment target.
//
// The bridge is the missing hop. The user runs it on their own desktop, where
// it owns 127.0.0.1:9666 and speaks CnL to the website, and it relays whatever
// it decodes to the remote instance over the ordinary REST API.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DefaultTimeout bounds everything one Click'n'Load submission triggers. It is
// generous because the remote stages the links synchronously, and the browser
// is left waiting for exactly this long when the remote is unreachable.
const DefaultTimeout = 30 * time.Second

// Options configure a Bridge.
type Options struct {
	Remote   string        // base URL of the remote instance, e.g. http://nas:8749
	Password string        // optional; the remote's UI password if it is locked
	Timeout  time.Duration // zero means a sane default
}

// Bridge forwards Click'n'Load submissions to a remote KnightLoader.
type Bridge struct {
	remote   string
	password string
	timeout  time.Duration
	hc       *http.Client

	// mu guards epoch, which counts successful logins. A request that comes
	// back 401 reports the epoch it rode on, so concurrent submissions that all
	// trip over the same expired session cost one login between them instead of
	// one each.
	mu sync.Mutex
	// loginWait is non-nil while a login is in flight. A second caller waits on
	// it instead of starting its own, and instead of returning early — a
	// caller that gave up waiting would retry with the session that just
	// expired and fail for the same reason all over again.
	loginWait chan struct{}
	epoch     uint64
}

// New builds a Bridge aimed at the instance named in o. The address is the one
// thing the user has to get right, so an unusable one is rejected here rather
// than logged as a mangled URL on every submission for the rest of the run.
func New(o Options) (*Bridge, error) {
	remote := strings.TrimRight(strings.TrimSpace(o.Remote), "/")
	if remote == "" {
		return nil, errors.New("bridge: a remote URL is required")
	}
	u, err := url.Parse(remote)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("bridge: the remote must be an http(s) URL, got %q", o.Remote)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// The session lives in a cookie jar, so one login carries over to every
	// later request this client makes.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("bridge: cookie jar: %w", err)
	}
	return &Bridge{
		remote:   remote,
		password: o.Password,
		timeout:  timeout,
		// The shared policy, not a bare client. The jar is the reason it matters
		// here more than anywhere else: this client carries a session cookie for
		// the remote instance, and httpx is what stops that cookie following a
		// redirect onto a host it was never issued for.
		hc: httpx.New(httpx.Options{Jar: jar, Timeout: timeout}),
	}, nil
}

// Remote is the base URL the bridge forwards to.
func (b *Bridge) Remote() string { return b.remote }

// Check probes the remote and, when a password is configured, logs in. Running
// it at startup turns a typo in the address or the password into one clear
// error on the spot, instead of into links quietly lost hours later on the
// first CnL click.
func (b *Bridge) Check(ctx context.Context) error {
	epoch := b.sessionEpoch()
	resp, err := b.send(ctx, http.MethodGet, "/api/health", nil)
	if err != nil {
		return fmt.Errorf("bridge: %s is unreachable: %w", b.remote, err)
	}
	body := drain(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bridge: %s/api/health answered HTTP %d: %s", b.remote, resp.StatusCode, snippet(body))
	}
	// /api/health stays open even on a locked instance, so reaching it proves
	// the address is right but says nothing about the password. Only a login
	// does that.
	if b.password == "" {
		return nil
	}
	return b.login(ctx, epoch)
}

// AddLinksCnL relays one Click'n'Load submission to the remote. It matches the
// signature the CnL listener expects, so a Bridge can stand in for the local
// app wherever an adder is wanted.
//
// It works synchronously: the CnL handler reports "success" to the website only
// once this returns, and claiming success for links that never left the desktop
// would be worse than making the button spin for a moment. The timeout bounds
// how long that can last.
func (b *Bridge) AddLinksCnL(urls []string, pkg string, passwords []string) {
	if len(urls) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	// Passwords travel with the links in one request. Sending them separately
	// meant the endpoint could only take one, and every password past the first
	// was quietly lost on the way to the remote.
	// The entrance is named explicitly. These links reached this process through
	// Click'n'Load and leave it as an ordinary REST call, so the remote has no
	// way to tell them from a paste — and it would file them as one, which is
	// wrong for every deployment a bridge exists to serve. An older remote that
	// does not read the field ignores it and behaves exactly as before, which is
	// why this is a field and not a route of its own: a bridge that 404s drops
	// the submission, and the website will not offer those links again.
	body, err := json.Marshal(struct {
		Links     string   `json:"links"`
		Package   string   `json:"package"`
		Origin    string   `json:"origin"`
		Passwords []string `json:"passwords,omitempty"`
	}{Links: strings.Join(urls, "\n"), Package: pkg, Origin: "cnl", Passwords: passwords})
	if err != nil {
		log.Printf("could not encode %d links for %s: %v", len(urls), b.remote, err)
		return
	}

	if _, err := b.call(ctx, http.MethodPost, "/api/links", body); err != nil {
		// These links are gone: the website will not offer them a second time.
		// Say loudly what was lost, because a bridge that drops links in silence
		// looks exactly like one that works.
		log.Printf("%d links dropped, none reached the remote: %v", len(urls), err)
		return
	}
	log.Printf("forwarded %d links to %s (package %q)", len(urls), b.remote, pkg)
}

// call performs one API request and, if the remote answers 401, logs in and
// repeats it exactly once. A bridge is meant to sit running for weeks, so an
// expired session has to heal itself rather than become a stream of lost links.
func (b *Bridge) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	epoch := b.sessionEpoch()
	resp, err := b.send(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s %s%s: %w", method, b.remote, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		drain(resp)
		if err := b.login(ctx, epoch); err != nil {
			return nil, err
		}
		// One retry only. If the freshly minted session is refused as well,
		// something is wrong that retrying will not fix, and a loop here would
		// hammer the remote for as long as the timeout allows.
		resp, err = b.send(ctx, method, path, body)
		if err != nil {
			return nil, fmt.Errorf("%s %s%s: %w", method, b.remote, path, err)
		}
	}
	out := drain(resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s%s answered HTTP %d: %s", method, b.remote, path, resp.StatusCode, snippet(out))
	}
	return out, nil
}

// login exchanges the password for a session cookie, which the jar then replays
// on every later request. seen is the epoch the caller's failed request rode
// on: if another goroutine has logged in since, this call is already covered by
// that session and does nothing.
func (b *Bridge) login(ctx context.Context, seen uint64) error {
	if b.password == "" {
		return fmt.Errorf("bridge: %s is password locked but no password is configured", b.remote)
	}
	// Only the decision to log in is serialised, never the request itself.
	// Holding the mutex across the round trip would make one hung remote block
	// every other submission before it even reached the network, and the CnL
	// handler is synchronous, so every browser tab would hang with it.
	b.mu.Lock()
	if b.epoch != seen {
		b.mu.Unlock()
		return nil
	}
	if wait := b.loginWait; wait != nil {
		b.mu.Unlock()
		select {
		case <-wait:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	b.loginWait = done
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.loginWait = nil
		b.mu.Unlock()
		close(done)
	}()

	body, err := json.Marshal(struct {
		Password string `json:"password"`
	}{Password: b.password})
	if err != nil {
		return fmt.Errorf("bridge: could not encode the login for %s: %w", b.remote, err)
	}
	resp, err := b.send(ctx, http.MethodPost, "/api/auth/login", body)
	if err != nil {
		return fmt.Errorf("bridge: login at %s failed: %w", b.remote, err)
	}
	out := drain(resp)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("bridge: %s rejected the configured password", b.remote)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("bridge: login at %s answered HTTP %d: %s", b.remote, resp.StatusCode, snippet(out))
	}
	b.mu.Lock()
	b.epoch++
	b.mu.Unlock()
	return nil
}

// sessionEpoch reports which login the next request will be riding on.
func (b *Bridge) sessionEpoch() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.epoch
}

// send issues a single request without any retry or auth handling.
func (b *Bridge) send(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.remote+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// No Origin header is set on purpose: the remote's same-origin guard only
	// rejects requests that carry one that does not match, and this is not a
	// browser.
	return b.hc.Do(req)
}

// taskIDs pulls the ids out of the task array /api/links answers with.
func taskIDs(raw []byte) []string {
	var tasks []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &tasks); err != nil {
		log.Printf("could not read the task list returned by /api/links: %v", err)
		return nil
	}
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t.ID != "" {
			out = append(out, t.ID)
		}
	}
	return out
}

// drain reads and closes a response body, so the connection can go back to the
// pool and be reused for the retry.
func drain(resp *http.Response) []byte {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	return b
}

// snippet keeps an unexpected response body loggable: an error page can be a
// whole megabyte of HTML, and none of it belongs in a log line.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		// Cut on a rune boundary: a remote error page is often UTF-8, and slicing
		// bytes puts half a character into the log.
		cut := 0
		for i := range s {
			if i > 200 {
				break
			}
			cut = i
		}
		s = s[:cut] + "…"
	}
	return s
}
