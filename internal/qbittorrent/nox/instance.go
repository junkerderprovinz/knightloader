package nox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
)

// The states Status reports.
const (
	StateStarting = "starting"
	StateRunning  = "running"
	// StateFailed is a start that did not come up or a qBittorrent that
	// stopped by itself; it is started again after a pause that grows with
	// every failure in a row.
	StateFailed  = "failed"
	StateStopped = "stopped"
)

// stopGrace is how long qBittorrent gets to write its resume data and quit
// after each way of asking, before the next and the last, a kill.
var stopGrace = 15 * time.Second

const (
	readyTimeout = time.Minute
	// A run this long counts as one that worked, so the next failure waits
	// the shortest pause again.
	healthyRun = 2 * time.Minute
	maxBackoff = time.Minute
)

// hardening is set after every start. KnightLoader hands qBittorrent the
// .torrent bytes and magnets itself, so nothing needs qBittorrent to fetch
// from the web on its own, and 4.x does not check TLS certificates when it
// does (CVE-2024-51774).
var hardening = map[string]any{
	"rss_processing_enabled":       false,
	"rss_auto_downloading_enabled": false,
	"resolve_peer_countries":       false,
	"web_ui_upnp":                  false,
	"web_ui_address":               "127.0.0.1",
}

// Options says which qBittorrent to run and where it keeps its profile.
type Options struct {
	Binary  string
	Profile string
	// Version is the version of Binary.
	Version string
	// Proxy is the proxy qBittorrent starts behind, asked before each start;
	// nil, or a nil answer, for none.
	Proxy func() *Proxy
	// Key seals the WebUI password at rest; nil keeps it in a file only this
	// account can read.
	Key *appkey.Key
	// Prepare runs after each start, once the WebUI answers, and before the
	// instance counts as running. With a Prepare, qBittorrent starts tied to
	// no interface, and Prepare names the real one.
	Prepare func(ctx context.Context, c *qbittorrent.Client) error
	Logf    func(format string, args ...any)
}

// Status is how the instance stands, for the Components card.
type Status struct {
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Port    int    `json:"port,omitempty"`
	// PID is the process while it runs.
	PID      int    `json:"pid,omitempty"`
	Restarts int    `json:"restarts,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Instance is one qbittorrent-nox KnightLoader runs and watches over.
type Instance struct {
	opts   Options
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	status  Status
	client  *qbittorrent.Client
	changed chan struct{}
}

// Start runs qBittorrent from opts in the background and starts it again
// whenever it stops by itself, until Stop.
func Start(opts Options) *Instance {
	ctx, cancel := context.WithCancel(context.Background())
	in := &Instance{
		opts:    opts,
		cancel:  cancel,
		done:    make(chan struct{}),
		status:  Status{State: StateStarting},
		changed: make(chan struct{}),
	}
	go in.supervise(ctx)
	return in
}

// Stop asks qBittorrent to quit, ends it if it does not, and returns once it
// is gone. It may be called more than once.
func (in *Instance) Stop() error {
	in.cancel()
	<-in.done
	return nil
}

// Status is a copy of how the instance stands.
func (in *Instance) Status() Status {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.status
}

// Ready waits until qBittorrent runs and returns the client for it. The error
// names the last failure when ctx ends first.
func (in *Instance) Ready(ctx context.Context) (*qbittorrent.Client, error) {
	for {
		in.mu.Lock()
		st, c, changed := in.status, in.client, in.changed
		in.mu.Unlock()
		switch st.State {
		case StateRunning:
			return c, nil
		case StateStopped:
			return nil, errors.New("qBittorrent was stopped")
		}
		select {
		case <-changed:
		case <-ctx.Done():
			if st.Detail != "" {
				return nil, fmt.Errorf("qBittorrent is not running: %s", st.Detail)
			}
			return nil, ctx.Err()
		}
	}
}

func (in *Instance) update(f func(s *Status), client *qbittorrent.Client) {
	in.mu.Lock()
	defer in.mu.Unlock()
	f(&in.status)
	in.client = client
	close(in.changed)
	in.changed = make(chan struct{})
}

func (in *Instance) logf(format string, args ...any) {
	if in.opts.Logf != nil {
		in.opts.Logf(format, args...)
	}
}

func (in *Instance) supervise(ctx context.Context) {
	defer close(in.done)
	backoff := time.Second
	for first := true; ; first = false {
		if !first {
			in.update(func(s *Status) { s.Restarts++; s.State, s.PID = StateStarting, 0 }, nil)
		}
		began := time.Now()
		err := in.run(ctx)
		if ctx.Err() != nil {
			in.update(func(s *Status) { s.State, s.PID, s.Detail = StateStopped, 0, "" }, nil)
			return
		}
		in.logf("qBittorrent: %v", err)
		in.update(func(s *Status) { s.State, s.PID, s.Detail = StateFailed, 0, err.Error() }, nil)
		if time.Since(began) > healthyRun {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			in.update(func(s *Status) { s.State, s.PID = StateStopped, 0 }, nil)
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// run is one life of qBittorrent: it comes up, runs until it stops or ctx
// ends, and is shut down in the second case.
func (in *Instance) run(ctx context.Context) error {
	profile, err := filepath.Abs(in.opts.Profile)
	if err != nil {
		return err
	}
	login, err := LoadLogin(profile, in.opts.Key)
	if err != nil {
		return err
	}
	in.evict(ctx, login)
	if login.Port == 0 || !portFree(login.Port) {
		if login.Port, err = pickPort(); err != nil {
			return err
		}
		if err := SaveLogin(profile, in.opts.Key, login); err != nil {
			return err
		}
	}
	conf := Config{Port: login.Port, Username: login.Username, Password: login.Password, Version: in.opts.Version, Hold: in.opts.Prepare != nil}
	if in.opts.Proxy != nil {
		conf.Proxy = in.opts.Proxy()
	}
	if err := WriteConfig(profile, conf); err != nil {
		return err
	}

	var out tail
	cmd := exec.Command(in.opts.Binary, "--profile="+profile)
	cmd.Dir = profile
	cmd.Stdout = &out
	cmd.Stderr = &out
	tieToParent(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("qbittorrent-nox could not be started: %w", err)
	}
	release := afterStart(cmd)
	defer release()
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	ended := func(what string) error {
		msg := what
		if waitErr != nil {
			msg += " (" + waitErr.Error() + ")"
		}
		if last := out.String(); last != "" {
			msg += ": " + last
		}
		return errors.New(msg)
	}

	c, err := qbittorrent.New("http://127.0.0.1:"+strconv.Itoa(login.Port), login.Username, login.Password)
	if err != nil {
		in.end(cmd, nil, exited)
		return err
	}
	version, err := in.await(ctx, c, exited)
	if err != nil {
		select {
		case <-exited:
			return ended("qBittorrent quit while starting")
		default:
		}
		in.end(cmd, c, exited)
		return err
	}
	if err := c.SetPreferences(ctx, hardening); err != nil {
		in.logf("qBittorrent: the settings KnightLoader keeps off could not be set: %v", err)
	}
	if in.opts.Prepare != nil {
		if err := in.opts.Prepare(ctx, c); err != nil {
			in.end(cmd, c, exited)
			return fmt.Errorf("qBittorrent could not be set up: %w", err)
		}
	}
	in.update(func(s *Status) {
		s.State, s.Version, s.Port, s.PID, s.Detail = StateRunning, version, login.Port, cmd.Process.Pid, ""
	}, c)

	select {
	case <-exited:
		return ended("qBittorrent stopped")
	case <-ctx.Done():
		in.end(cmd, c, exited)
		return nil
	}
}

// await polls the WebUI until the login works, and returns the version.
func (in *Instance) await(ctx context.Context, c *qbittorrent.Client, exited <-chan struct{}) (string, error) {
	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		v, err := c.Version(call)
		cancel()
		if err == nil {
			return strings.TrimPrefix(v, "v"), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-exited:
			return "", errors.New("qBittorrent quit while starting")
		case <-deadline.C:
			return "", fmt.Errorf("qBittorrent did not answer within %s: %s", readyTimeout, qbittorrent.Reason(err))
		case <-tick.C:
		}
	}
}

// end shuts qBittorrent down: through its API, then by signal where the
// system has one, then by a kill.
func (in *Instance) end(cmd *exec.Cmd, c *qbittorrent.Client, exited <-chan struct{}) {
	wait := func() bool {
		select {
		case <-exited:
			return true
		case <-time.After(stopGrace):
			return false
		}
	}
	if c != nil {
		call, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.Shutdown(call)
		cancel()
		if err == nil && wait() {
			return
		}
	}
	if askToQuit(cmd) == nil && wait() {
		return
	}
	_ = cmd.Process.Kill()
	<-exited
}

// evict asks a qBittorrent still running on the saved port with the saved
// login to quit. That is one a crash of KnightLoader left behind, and it
// holds the profile: a second qBittorrent on the same profile hands its
// arguments to the first and exits.
func (in *Instance) evict(ctx context.Context, login Login) {
	if login.Port == 0 || portFree(login.Port) {
		return
	}
	c, err := qbittorrent.New("http://127.0.0.1:"+strconv.Itoa(login.Port), login.Username, login.Password)
	if err != nil {
		return
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c.Login(call) != nil || c.Shutdown(call) != nil {
		return
	}
	in.logf("qBittorrent: asked the one left from the last run to quit")
	deadline := time.Now().Add(stopGrace)
	for !portFree(login.Port) && time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func pickPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// tail keeps the last few hundred bytes qBittorrent printed, for the reason
// a start failed.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailBytes = 600

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailBytes {
		t.buf = t.buf[len(t.buf)-tailBytes:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(strings.Fields(string(t.buf)), " ")
}
