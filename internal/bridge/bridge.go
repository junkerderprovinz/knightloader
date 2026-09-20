// Package bridge forwards Click'n'Load submissions to a KnightLoader that runs
// somewhere other than the browser's own machine.
//
// Websites send Click'n'Load to 127.0.0.1:9666, and the protocol cannot aim
// anywhere else. When KnightLoader runs in a container on a NAS, that address
// is not the browser's machine. The bridge runs on the user's desktop, owns
// 127.0.0.1:9666, and relays what it decodes to the remote instance over the
// REST API.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DefaultTimeout bounds everything one Click'n'Load submission triggers. The
// remote stages links synchronously, and the browser waits this long when the
// remote is unreachable.
const DefaultTimeout = 30 * time.Second

// Options configure a Bridge.
type Options struct {
	Remote   string        // base URL of the remote instance, e.g. http://nas:8749
	Password string        // optional; the remote's UI password if it is locked
	Timeout  time.Duration // zero means DefaultTimeout
}

// Bridge forwards Click'n'Load submissions to a remote KnightLoader.
type Bridge struct {
	remote   string
	password string
	timeout  time.Duration
	hc       *http.Client

	// epoch counts successful logins. A request that gets a 401 reports the
	// epoch it used, so concurrent submissions hitting the same expired
	// session share one login.
	mu sync.Mutex
	// loginWait is non-nil while a login is in flight; other callers wait on
	// it rather than retrying with the expired session.
	loginWait chan struct{}
	epoch     uint64
}

// New builds a Bridge aimed at the instance named in o. An unusable address
// is rejected here rather than failing on every submission.
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
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("bridge: cookie jar: %w", err)
	}
	return &Bridge{
		remote:   remote,
		password: o.Password,
		timeout:  timeout,
		// httpx keeps the session cookie from following a redirect to another
		// host.
		hc: httpx.New(httpx.Options{Jar: jar, Timeout: timeout}),
	}, nil
}

// Remote is the base URL the bridge forwards to.
func (b *Bridge) Remote() string { return b.remote }

// Check probes the remote and, when a password is configured, logs in, so a
// wrong address or password shows up at startup rather than as lost links
// later.
func (b *Bridge) Check(ctx context.Context) error {
	epoch := b.sessionEpoch()
	resp, err := b.sendAs(ctx, http.MethodGet, "/api/health", nil, "application/json")
	if err != nil {
		return fmt.Errorf("bridge: %s is unreachable: %w", b.remote, err)
	}
	body := drain(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bridge: %s/api/health answered HTTP %d: %s", b.remote, resp.StatusCode, snippet(body))
	}
	// /api/health is open even on a locked instance, so only a login proves
	// the password.
	if b.password == "" {
		return nil
	}
	return b.login(ctx, epoch)
}

// AddLinksCnL relays one Click'n'Load submission to the remote and matches
// the signature the CnL listener expects. It is synchronous, because the
// listener reports success to the website only once this returns.
func (b *Bridge) AddLinksCnL(urls []string, pkg string, passwords []string) {
	if len(urls) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	// Origin tells the remote these links came through Click'n'Load rather
	// than a paste. It is a field rather than a route so an older remote
	// ignores it instead of answering 404 and losing the submission.
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
		// The website will not offer these links again, so the loss has to
		// be visible.
		log.Printf("%d links dropped, none reached the remote: %v", len(urls), err)
		return
	}
	log.Printf("forwarded %d links to %s (package %q)", len(urls), b.remote, pkg)
}

// AddContainerCnL relays a Click'n'Load v1 ("addcrypted") submission to the
// remote's container upload route, the same one a .dlc upload uses. It
// satisfies cnl.ContainerAdder. It returns an error, so the listener can tell
// the site when the remote has no backend for containers.
func (b *Bridge) AddContainerCnL(data []byte, pkg string) error {
	if len(data) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "clickload.dlc")
	if err != nil {
		return fmt.Errorf("bridge: could not prepare the addcrypted (v1) upload: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("bridge: could not prepare the addcrypted (v1) upload: %w", err)
	}
	if pkg != "" {
		if err := mw.WriteField("package", pkg); err != nil {
			return fmt.Errorf("bridge: could not prepare the addcrypted (v1) upload: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("bridge: could not prepare the addcrypted (v1) upload: %w", err)
	}

	if _, err := b.callAs(ctx, http.MethodPost, "/api/containers", body.Bytes(), mw.FormDataContentType()); err != nil {
		log.Printf("addcrypted (v1) dropped, it did not reach %s: %v", b.remote, err)
		return fmt.Errorf("bridge: %w", err)
	}
	log.Printf("forwarded an addcrypted (v1) submission to %s (package %q)", b.remote, pkg)
	return nil
}

// call performs one JSON API request, logging in and repeating it once if the
// remote answers 401, so a bridge left running for weeks survives an expired
// session.
func (b *Bridge) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return b.callAs(ctx, method, path, body, "application/json")
}

func (b *Bridge) callAs(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	epoch := b.sessionEpoch()
	resp, err := b.sendAs(ctx, method, path, body, contentType)
	if err != nil {
		return nil, fmt.Errorf("%s %s%s: %w", method, b.remote, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		drain(resp)
		if err := b.login(ctx, epoch); err != nil {
			return nil, err
		}
		// One retry only; a fresh session that is refused too will not be
		// fixed by hammering the remote.
		resp, err = b.sendAs(ctx, method, path, body, contentType)
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

// login exchanges the password for a session cookie. seen is the epoch the
// caller's failed request used; if another goroutine has logged in since,
// login does nothing.
func (b *Bridge) login(ctx context.Context, seen uint64) error {
	if b.password == "" {
		return fmt.Errorf("bridge: %s is password locked but no password is configured", b.remote)
	}
	// Only the decision is serialised, not the request, so one hung remote
	// does not block every other submission.
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
	resp, err := b.sendAs(ctx, http.MethodPost, "/api/auth/login", body, "application/json")
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

func (b *Bridge) sessionEpoch() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.epoch
}

// sendAs issues a single request without retry or auth handling. It sets no
// Origin header: the remote's same-origin guard only rejects a mismatched
// one.
func (b *Bridge) sendAs(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.remote+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	return b.hc.Do(req)
}

// drain reads and closes a response body so the connection can be reused.
func drain(resp *http.Response) []byte {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	return b
}

// snippet shortens a response body for a log line, cutting on a rune
// boundary.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
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
