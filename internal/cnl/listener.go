package cnl

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
)

// DefaultPort is the port every Click'n'Load button posts to.
const DefaultPort = 9666

// Listener is the Click'n'Load server as a process runs it: bound at start-up
// unless KL_CNL says otherwise, then started and stopped by the settings switch
// without a restart. The server and the desktop build both run one, so KL_CNL
// means the same in each.
type Listener struct {
	adder Adder
	port  int

	mu     sync.Mutex
	srv    *Server // nil while the listener is down
	err    error
	envOff bool
}

// NewListener returns a listener for 127.0.0.1:port that has not bound yet.
func NewListener(adder Adder, port int) *Listener {
	return &Listener{adder: adder, port: port}
}

// Listen starts the listener KL_CNL describes. KL_CNL=0 leaves it down, and
// turning the switch on afterwards binds DefaultPort; any other number is the
// port. A port that is taken, usually by a running JDownloader, is logged and
// kept in Err rather than stopping the process.
func Listen(adder Adder) *Listener {
	port, atStart := portFromEnv(os.Getenv("KL_CNL"))
	l := NewListener(adder, port)
	if !atStart {
		l.envOff = true
		log.Printf("Click'n'Load is off (KL_CNL=0)")
		return l
	}
	if err := l.Start(); err != nil {
		log.Printf("Click'n'Load not available on %s (%v)", l.Address(), err)
	} else {
		log.Printf("Click'n'Load listening on %s", l.Address())
	}
	return l
}

// portFromEnv reads a KL_CNL value. A value that is not a number counts as
// unset, as it does for every other numeric KL_ variable.
func portFromEnv(v string) (port int, atStart bool) {
	n, err := strconv.Atoi(v)
	switch {
	case err != nil:
		return DefaultPort, true
	case n <= 0:
		return DefaultPort, false
	}
	return n, true
}

// Start binds the port. A running listener is left as it is.
func (l *Listener) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv != nil {
		return nil
	}
	s := New(l.adder)
	if err := s.Start(l.port); err != nil {
		l.err = err
		return err
	}
	l.srv, l.err, l.envOff = s, nil, false
	return nil
}

// Stop closes the listener and forgets why an earlier start failed, since the
// listener is then off by choice.
func (l *Listener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = nil
	if l.srv == nil {
		return
	}
	_ = l.srv.Close()
	l.srv = nil
}

// Toggle is the settings switch.
func (l *Listener) Toggle(on bool) error {
	if on {
		return l.Start()
	}
	l.Stop()
	return nil
}

// Port is the bound port, or 0 while the listener is down.
func (l *Listener) Port() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv == nil {
		return 0
	}
	return l.port
}

// Address is where the listener binds, whether or not it is up.
func (l *Listener) Address() string {
	return fmt.Sprintf("127.0.0.1:%d", l.port)
}

// Err is why the last start failed. It is nil while the listener runs and
// after Stop.
func (l *Listener) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// OffByEnv reports whether KL_CNL=0 kept the listener down at start-up and
// nothing has started it since.
func (l *Listener) OffByEnv() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.envOff
}
