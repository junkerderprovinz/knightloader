package signinbrowser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// errGone is a call the browser never answered because it went away.
var errGone = errors.New("signinbrowser: the browser has gone")

// transport carries whole DevTools messages to and from Chromium.
type transport interface {
	read() ([]byte, error)
	write([]byte) error
	close() error
}

// message is a DevTools message in either direction.
type message struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// event is a DevTools event, with the session of the target it is about.
type event struct {
	session string
	method  string
	params  json.RawMessage
}

// client speaks the DevTools protocol over a transport, with flat sessions:
// every target's calls and events travel on the one connection.
type client struct {
	t   transport
	wmu sync.Mutex

	mu      sync.Mutex
	next    int64
	pending map[int64]chan message
	queue   []event
	wake    chan struct{}
	gone    chan struct{}

	out chan event
}

func newClient(t transport) *client {
	c := &client{
		t:       t,
		pending: map[int64]chan message{},
		wake:    make(chan struct{}, 1),
		gone:    make(chan struct{}),
		out:     make(chan event),
	}
	go c.readLoop()
	go c.pump()
	return c
}

// events delivers the events in the order Chromium sent them, until the
// browser goes.
func (c *client) events() <-chan event { return c.out }

func (c *client) close() { _ = c.t.close() }

// call sends one command and decodes its result into out, which may be nil.
func (c *client) call(ctx context.Context, session, method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		raw = []byte("{}")
	}
	reply := make(chan message, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	b, err := json.Marshal(message{ID: id, Method: method, Params: raw, SessionID: session})
	if err != nil {
		return err
	}
	c.wmu.Lock()
	err = c.t.write(b)
	c.wmu.Unlock()
	if err != nil {
		return errGone
	}
	select {
	case m := <-reply:
		if m.Error != nil {
			return fmt.Errorf("signinbrowser: %s: %s", method, m.Error.Message)
		}
		if out == nil || len(m.Result) == 0 {
			return nil
		}
		return json.Unmarshal(m.Result, out)
	case <-c.gone:
		return errGone
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *client) readLoop() {
	defer close(c.gone)
	for {
		b, err := c.t.read()
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		c.mu.Lock()
		if m.ID != 0 {
			if reply, ok := c.pending[m.ID]; ok {
				reply <- m
			}
		} else if m.Method != "" {
			c.queue = append(c.queue, event{session: m.SessionID, method: m.Method, params: m.Params})
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}
		c.mu.Unlock()
	}
}

// pump hands the queued events on one at a time. The queue has no bound, so
// a slow reader of events never keeps an answer from reaching its call.
func (c *client) pump() {
	defer close(c.out)
	for {
		c.mu.Lock()
		batch := c.queue
		c.queue = nil
		c.mu.Unlock()
		for _, ev := range batch {
			select {
			case c.out <- ev:
			case <-c.gone:
				return
			}
		}
		select {
		case <-c.wake:
		case <-c.gone:
			return
		}
	}
}

// pipe is the transport of --remote-debugging-pipe: messages end in a null
// byte.
type pipe struct {
	r      *bufio.Reader
	w      io.WriteCloser
	closer io.Closer
}

func newPipe(r io.Reader, w io.WriteCloser, closer io.Closer) *pipe {
	return &pipe{r: bufio.NewReaderSize(r, 1<<20), w: w, closer: closer}
}

func (p *pipe) read() ([]byte, error) {
	b, err := p.r.ReadBytes(0)
	if err != nil {
		return nil, err
	}
	return b[:len(b)-1], nil
}

func (p *pipe) write(b []byte) error {
	_, err := p.w.Write(append(b, 0))
	return err
}

func (p *pipe) close() error {
	err := p.w.Close()
	if p.closer != nil {
		err = errors.Join(err, p.closer.Close())
	}
	return err
}
