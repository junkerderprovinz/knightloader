package captcha

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"
)

// SourceAsk is Challenge.Source for a captcha one of KnightLoader's own hoster
// clients asked for.
const SourceAsk = "knightloader"

const askIDPrefix = "ask-"

// The ways an Ask ends without an answer.
var (
	ErrSkipped    = errors.New("captcha: the captcha was skipped")
	ErrUnanswered = errors.New("captcha: nobody answered the captcha in time")
)

// AskSource holds the captchas KnightLoader's own hoster clients wait on, so
// they reach the same prompts and paid solvers as JDownloader's. Safe for
// concurrent use.
type AskSource struct {
	// TTL is how long a captcha waits for its answer. A Turnstile token lasts
	// five minutes, so the answer has time to reach the hoster.
	TTL time.Duration
	// Changed is called after a captcha was added or taken away, so the app
	// need not wait for its next poll. Nil calls nothing.
	Changed func()

	mu      sync.Mutex
	pending map[string]*ask
	now     func() time.Time
}

type ask struct {
	c      Challenge
	answer chan string
	abort  chan struct{}
}

// NewAskSource returns an AskSource with nothing pending.
func NewAskSource() *AskSource {
	return &AskSource{TTL: 4 * time.Minute, pending: map[string]*ask{}, now: time.Now}
}

// IsAsked reports whether id names a captcha from an AskSource.
func IsAsked(id string) bool { return strings.HasPrefix(id, askIDPrefix) }

// Ask lists c until it is answered and returns the answer. It fails with
// ErrSkipped when the captcha is skipped, ErrUnanswered when its time runs
// out, and ctx's error when the download that asked stops.
func (s *AskSource) Ask(ctx context.Context, c Challenge) (string, error) {
	c.ID = askIDPrefix + rand.Text()
	c.Source = SourceAsk
	c.ExpiresAt = s.now().Add(s.TTL)
	a := &ask{c: c, answer: make(chan string, 1), abort: make(chan struct{}, 1)}
	s.mu.Lock()
	s.pending[c.ID] = a
	s.mu.Unlock()
	s.changed()
	defer func() {
		s.mu.Lock()
		delete(s.pending, c.ID)
		s.mu.Unlock()
		s.changed()
	}()

	t := time.NewTimer(s.TTL)
	defer t.Stop()
	select {
	case text := <-a.answer:
		return text, nil
	case <-a.abort:
		return "", ErrSkipped
	case <-t.C:
		return "", ErrUnanswered
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// List returns every captcha still waiting for its answer.
func (s *AskSource) List() []Challenge {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]Challenge, 0, len(s.pending))
	for _, a := range s.pending {
		if now.Before(a.c.ExpiresAt) {
			out = append(out, a.c)
		}
	}
	return out
}

// Answer hands text to the client waiting on id and reports whether one
// still was.
func (s *AskSource) Answer(id, text string) bool {
	a := s.take(id)
	if a == nil {
		return false
	}
	a.answer <- text
	return true
}

// Abort tells the client waiting on id that the captcha was skipped. A
// captcha already gone is not an error, as for Source.Abort.
func (s *AskSource) Abort(id string) {
	if a := s.take(id); a != nil {
		a.abort <- struct{}{}
	}
}

// take removes id from the pending ones, so it is answered or skipped once.
func (s *AskSource) take(id string) *ask {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.pending[id]
	delete(s.pending, id)
	return a
}

func (s *AskSource) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}
