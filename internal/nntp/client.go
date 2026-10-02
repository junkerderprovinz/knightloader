package nntp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// ErrMissing is an article no server has.
var ErrMissing = errors.New("nntp: no server has this article")

// ErrDamaged is an article every server that has it hands out damaged.
var ErrDamaged = errors.New("nntp: every copy of this article is damaged")

// ErrUnavailable is an article that could not be fetched because a server
// that might have it could not be reached. Asking again later may work.
var ErrUnavailable = errors.New("nntp: a server that may have this article cannot be reached")

const (
	// crcTries is how often one server is asked again for an article that
	// arrived damaged, since the damage is often on the way and not on the
	// server's disk.
	crcTries = 3
	// idleMax is how long an unused connection is kept. Providers close quiet
	// connections after a few minutes, and a fresh one costs a login.
	idleMax = 2 * time.Minute
	// downFor is how long a server that could not be reached is left alone,
	// and authDownFor one that refused the login.
	downFor     = 30 * time.Second
	authDownFor = 5 * time.Minute
)

// Client fetches articles from several servers through one pool of
// connections each. It is safe for concurrent use.
type Client struct {
	pools []*pool
	cp    Copier
	next  atomic.Uint64
	now   func() time.Time
}

// NewClient builds a client for servers. cp carries every article off the
// connection, and nil copies without a limit.
func NewClient(servers []Server, cp Copier) *Client {
	if cp == nil {
		cp = plainCopy{}
	}
	c := &Client{cp: cp, now: time.Now}
	for _, s := range servers {
		if s.Connections <= 0 {
			s.Connections = 1
		}
		c.pools = append(c.pools, &pool{s: s, slots: make(chan struct{}, s.Connections)})
	}
	slices.SortStableFunc(c.pools, func(a, b *pool) int { return a.s.Level - b.s.Level })
	return c
}

// Connections is how many connections the client may hold at once, which is
// how many articles it can fetch in parallel.
func (c *Client) Connections() int {
	n := 0
	for _, p := range c.pools {
		n += p.s.Connections
	}
	return n
}

// Close hangs up the idle connections. Fetches still running finish on theirs.
func (c *Client) Close() {
	for _, p := range c.pools {
		p.closeIdle()
	}
}

// Fetch returns the decoded article id, posted at posted (zero when unknown).
//
// The servers are asked level by level. On one level the starting server
// changes from article to article, so their connections share the work. A
// server that does not have the article passes it to the next server on its
// level and then to the next level; one that hands it out damaged is asked
// again a few times first. The errors are ErrMissing, ErrDamaged and
// ErrUnavailable, or the context's.
func (c *Client) Fetch(ctx context.Context, id string, posted time.Time) (yenc.Part, error) {
	id = cleanID(id)
	if !validID(id) {
		return yenc.Part{}, fmt.Errorf("%w: %q is not a message id", ErrMissing, id)
	}
	start := int(c.next.Add(1))
	var damaged bool
	var unreachable error
	for _, level := range c.levels() {
		for i := range level {
			p := level[(start+i)%len(level)]
			if p.tooOld(posted, c.now()) {
				continue
			}
			part, err := c.fetchFrom(ctx, p, id)
			switch {
			case err == nil:
				return part, nil
			case ctx.Err() != nil:
				return yenc.Part{}, ctx.Err()
			case errors.Is(err, ErrNoArticle):
			case errors.Is(err, yenc.ErrCRC), errors.Is(err, yenc.ErrFormat):
				damaged = true
			default:
				if !p.s.Optional {
					unreachable = err
				}
			}
		}
	}
	switch {
	case unreachable != nil:
		return yenc.Part{}, fmt.Errorf("%w: %v", ErrUnavailable, unreachable)
	case damaged:
		return yenc.Part{}, ErrDamaged
	}
	return yenc.Part{}, ErrMissing
}

// fetchFrom asks one server, again while the copy it hands out is damaged.
func (c *Client) fetchFrom(ctx context.Context, p *pool, id string) (yenc.Part, error) {
	var err error
	for range crcTries {
		var body []byte
		err = p.with(ctx, c.now, func(conn *conn) error {
			var berr error
			body, berr = conn.body(ctx, id, c.cp)
			return berr
		})
		if err != nil {
			return yenc.Part{}, err
		}
		var part yenc.Part
		if part, err = yenc.Decode(body); err == nil {
			return part, nil
		}
	}
	return yenc.Part{}, err
}

// Stat reports whether any server has the article, asking level by level.
func (c *Client) Stat(ctx context.Context, id string) error {
	id = cleanID(id)
	if !validID(id) {
		return fmt.Errorf("%w: %q is not a message id", ErrMissing, id)
	}
	var unreachable error
	for _, level := range c.levels() {
		for _, p := range level {
			err := p.with(ctx, c.now, func(conn *conn) error { return conn.stat(ctx, id) })
			switch {
			case err == nil:
				return nil
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, ErrNoArticle):
			default:
				if !p.s.Optional {
					unreachable = err
				}
			}
		}
	}
	if unreachable != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, unreachable)
	}
	return ErrMissing
}

// levels groups the pools by level, lowest first.
func (c *Client) levels() [][]*pool {
	var out [][]*pool
	for i, p := range c.pools {
		if i == 0 || p.s.Level != c.pools[i-1].s.Level {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], p)
	}
	return out
}

// Check logs in to s and asks it for an article that does not exist, which
// proves the login is good enough to read with. It is the test button on the
// accounts page.
func Check(ctx context.Context, s Server) error {
	c, err := dial(ctx, s)
	if err != nil {
		return err
	}
	defer c.close()
	if err := c.stat(ctx, "check@knightloader.invalid"); err != nil && !errors.Is(err, ErrNoArticle) {
		return err
	}
	return nil
}

// pool is one server's connections. slots holds a token for each connection
// in use, so no more are open than the provider allows.
type pool struct {
	s     Server
	slots chan struct{}

	mu        sync.Mutex
	idle      []*conn
	downUntil time.Time
	lastErr   error
}

func (p *pool) tooOld(posted, now time.Time) bool {
	return p.s.RetentionDays > 0 && !posted.IsZero() && now.Sub(posted) > time.Duration(p.s.RetentionDays)*24*time.Hour
}

// with runs fn on a connection of this server. An answer about the article
// keeps the connection for the next one; anything else closes it. An idle
// connection the server has dropped meanwhile is replaced once.
func (p *pool) with(ctx context.Context, now func() time.Time, fn func(*conn) error) error {
	p.mu.Lock()
	down, last := now().Before(p.downUntil), p.lastErr
	p.mu.Unlock()
	if down {
		return fmt.Errorf("%s is left alone for a while after: %w", p.s.name(), last)
	}
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.slots }()
	for {
		c, fresh, err := p.get(ctx, now)
		if err != nil {
			if ctx.Err() == nil {
				p.markDown(now(), err)
			}
			return err
		}
		err = fn(c)
		if err == nil || errors.Is(err, ErrNoArticle) {
			c.idle = now()
			p.put(c)
			return err
		}
		c.close()
		if fresh || ctx.Err() != nil {
			if errors.Is(err, ErrAuth) {
				p.markDown(now(), err)
			}
			return err
		}
	}
}

// get takes an idle connection or dials a new one, and reports which.
func (p *pool) get(ctx context.Context, now func() time.Time) (*conn, bool, error) {
	p.mu.Lock()
	for len(p.idle) > 0 {
		c := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		if now().Sub(c.idle) < idleMax {
			p.mu.Unlock()
			return c, false, nil
		}
		go c.close()
	}
	p.mu.Unlock()
	c, err := dial(ctx, p.s)
	return c, true, err
}

func (p *pool) put(c *conn) {
	p.mu.Lock()
	p.idle = append(p.idle, c)
	p.mu.Unlock()
}

func (p *pool) markDown(at time.Time, err error) {
	wait := downFor
	if errors.Is(err, ErrAuth) {
		wait = authDownFor
	}
	p.mu.Lock()
	p.downUntil, p.lastErr = at.Add(wait), err
	p.mu.Unlock()
}

func (p *pool) closeIdle() {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, c := range idle {
		c.close()
	}
}
