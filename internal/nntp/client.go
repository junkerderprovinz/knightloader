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
	cp   Copier
	next atomic.Uint64
	now  func() time.Time

	mu    sync.RWMutex
	pools []*pool
}

// NewClient builds a client for servers. cp carries every article off the
// connection, and nil copies without a limit.
func NewClient(servers []Server, cp Copier) *Client {
	if cp == nil {
		cp = plainCopy{}
	}
	c := &Client{cp: cp, now: time.Now}
	c.SetServers(servers)
	return c
}

// SetServers replaces the servers, at once for every fetch that starts after
// it. A server whose address and login stay the same keeps its connections
// and their count, so the fetches still running on them and the ones to come
// together open no more than the provider allows. The connections of any
// other server are hung up as their fetches end.
func (c *Client) SetServers(servers []Server) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pools := make([]*pool, 0, len(servers))
	kept := map[*line]bool{}
	for _, s := range servers {
		s.Connections = max(s.Connections, 1)
		var l *line
		for _, p := range c.pools {
			if !kept[p.line] && p.s.sameLine(s) {
				l = p.line
				break
			}
		}
		if l == nil {
			l = &line{free: make(chan struct{})}
		}
		kept[l] = true
		l.setLimit(s.Connections)
		pools = append(pools, &pool{s: s, line: l})
	}
	for _, p := range c.pools {
		if !kept[p.line] {
			p.retire()
		}
	}
	slices.SortStableFunc(pools, func(a, b *pool) int { return a.s.Level - b.s.Level })
	c.pools = pools
}

// Connections is how many connections the client may hold at once, which is
// how many articles it can fetch in parallel.
func (c *Client) Connections() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := 0
	for _, p := range c.pools {
		n += p.s.Connections
	}
	return n
}

// Close hangs up the idle connections. Fetches still running finish on theirs.
func (c *Client) Close() {
	c.mu.RLock()
	defer c.mu.RUnlock()
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
	var down downs
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
				down.answered = true
			case errors.Is(err, yenc.ErrCRC), errors.Is(err, yenc.ErrFormat):
				damaged, down.answered = true, true
			default:
				down.note(p, err)
			}
		}
	}
	if err := down.err(); err != nil {
		return yenc.Part{}, err
	}
	if damaged {
		return yenc.Part{}, ErrDamaged
	}
	return yenc.Part{}, ErrMissing
}

// downs collects the servers that could not be reached while one article was
// asked for.
type downs struct {
	required, optional error
	answered           bool
}

func (d *downs) note(p *pool, err error) {
	if p.s.Optional {
		d.optional = err
	} else {
		d.required = err
	}
}

// err is ErrUnavailable when a server that is not optional could not be
// reached, or when only optional servers were asked and none answered. An
// optional server is passed over only in favour of one that did answer.
func (d downs) err() error {
	switch {
	case d.required != nil:
		return fmt.Errorf("%w: %v", ErrUnavailable, d.required)
	case d.optional != nil && !d.answered:
		return fmt.Errorf("%w: %v", ErrUnavailable, d.optional)
	}
	return nil
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
	var down downs
	for _, level := range c.levels() {
		for _, p := range level {
			err := p.with(ctx, c.now, func(conn *conn) error { return conn.stat(ctx, id) })
			switch {
			case err == nil:
				return nil
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, ErrNoArticle):
				down.answered = true
			default:
				down.note(p, err)
			}
		}
	}
	if err := down.err(); err != nil {
		return err
	}
	return ErrMissing
}

// levels groups the pools by level, lowest first.
func (c *Client) levels() [][]*pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
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

// pool is one server as it is set up, and its connections.
type pool struct {
	s Server
	*line
}

// line is one server's connections, which outlive a change of its settings
// that leaves its address and login alone. limit is how many may be open at
// once, busy how many fetches hold one or are dialling it; free is closed
// and replaced whenever a fetch could go ahead that had to wait.
type line struct {
	mu        sync.Mutex
	limit     int
	busy      int
	free      chan struct{}
	idle      []*conn
	downUntil time.Time
	lastErr   error
	// retired is set once the server is gone or reached another way, after
	// which no connection goes back to idle.
	retired bool
}

// sameLine reports whether t reaches the server the way s does.
func (s Server) sameLine(t Server) bool {
	return s.ID == t.ID && s.Host == t.Host && s.Port == t.Port && s.TLS == t.TLS &&
		s.Username == t.Username && s.Password == t.Password && s.TLSConfig == t.TLSConfig
}

func (l *line) setLimit(n int) {
	l.mu.Lock()
	lower := n < l.limit
	l.limit = n
	l.wakeLocked()
	l.mu.Unlock()
	if lower {
		l.closeIdle()
	}
}

func (l *line) wakeLocked() {
	close(l.free)
	l.free = make(chan struct{})
}

// acquire waits until one more connection may be in use.
func (l *line) acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.busy < l.limit {
			l.busy++
			l.mu.Unlock()
			return nil
		}
		free := l.free
		l.mu.Unlock()
		select {
		case <-free:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *line) release() {
	l.mu.Lock()
	l.busy--
	l.wakeLocked()
	l.mu.Unlock()
}

func (l *line) retire() {
	l.mu.Lock()
	l.retired = true
	l.mu.Unlock()
	l.closeIdle()
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
	if err := p.acquire(ctx); err != nil {
		return err
	}
	defer p.release()
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

// put keeps c for the next fetch, unless the line is retired or more fetches
// are busy than a lowered limit allows, the caller among them. The idle
// connections stay out of that sum: a fetch that has just put its connection
// back, or is about to take one, still counts as busy, and counting it twice
// would hang up connections the limit has room for.
func (l *line) put(c *conn) {
	l.mu.Lock()
	if l.retired || l.busy > l.limit {
		l.mu.Unlock()
		c.close()
		return
	}
	l.idle = append(l.idle, c)
	l.mu.Unlock()
}

func (l *line) markDown(at time.Time, err error) {
	wait := downFor
	if errors.Is(err, ErrAuth) {
		wait = authDownFor
	}
	l.mu.Lock()
	l.downUntil, l.lastErr = at.Add(wait), err
	l.mu.Unlock()
}

func (l *line) closeIdle() {
	l.mu.Lock()
	idle := l.idle
	l.idle = nil
	l.mu.Unlock()
	for _, c := range idle {
		c.close()
	}
}
