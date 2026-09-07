package settings

// The two exception tables. Every claim below is one somebody has to be able
// to rely on before writing a single row into either: that an empty table
// changes nothing, that a pattern matches what it looks like it matches, and
// that one field configured leaves every other field alone.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// TestAnEmptyTableIsExactlyTheOldBehaviour is the promise the whole feature
// rests on: an install that never opens the page must retry precisely the way
// it did before either table existed - fifteen seconds, doubling, ten minutes,
// MaxRetries attempts.
func TestAnEmptyTableIsExactlyTheOldBehaviour(t *testing.T) {
	s := Defaults()
	got := s.RetryFor("limit", "rapidgator.net")
	if got.Delay != DefaultRetryDelay {
		t.Errorf("delay = %s, want the built-in %s", got.Delay, DefaultRetryDelay)
	}
	if got.Max != DefaultRetryMax {
		t.Errorf("max = %s, want the built-in %s", got.Max, DefaultRetryMax)
	}
	if got.Tries != s.MaxRetries {
		t.Errorf("tries = %d, want MaxRetries %d", got.Tries, s.MaxRetries)
	}
	if got.Never {
		t.Error("a fresh install refuses to retry something")
	}
	if r := s.HostRuleFor("rapidgator.net"); r != (HostRule{}) {
		t.Errorf("HostRuleFor on an empty table = %+v, want the zero rule", r)
	}
}

// TestHostPatternMatching pins what a person typing a host into the table gets.
// The two rows that matter most are the last pair: a pattern must not match a
// host that merely ENDS in it, and the more specific of two entries has to win
// or a per-server exception could never outrank a per-site one.
func TestHostPatternMatching(t *testing.T) {
	table := map[string]HostRule{
		"example.com":     {MaxPerHost: 2},
		"dl3.example.com": {MaxPerHost: 8},
		"*.mirror.test":   {MaxPerHost: 5},
		"  UPPER.test  ":  {MaxPerHost: 6},
	}
	s := Settings{HostRules: table}
	cases := []struct {
		host string
		want int
	}{
		{"example.com", 2},
		{"files.example.com", 2},
		{"dl3.example.com", 8},       // the longer pattern wins
		{"mirror.test", 5},           // "*.pattern" also matches the bare host
		{"eu.mirror.test", 5},        // and its subdomains
		{"upper.test", 6},            // case and stray whitespace are folded
		{"notexample.com", 0},        // ends in the pattern, is not below it
		{"example.com.evil.test", 0}, // the pattern is not a substring match
		{"somewhere.else", 0},        // nothing claims it
		{"", 0},                      // no host at all
	}
	for _, tc := range cases {
		if got := s.HostRuleFor(tc.host).MaxPerHost; got != tc.want {
			t.Errorf("HostRuleFor(%q).MaxPerHost = %d, want %d", tc.host, got, tc.want)
		}
	}
}

// TestRetryResolvesFieldByField is the reason merge is per field rather than
// per rule: a host entry that says nothing but "wait an hour" must not also
// wipe out the attempt count somebody set against the reason, which is exactly
// what "the most specific whole rule wins" would do.
func TestRetryResolvesFieldByField(t *testing.T) {
	s := Defaults()
	s.MaxRetries = 4
	s.Retry.ByReason = map[string]RetryRule{
		"limit": {Tries: 2},
	}
	s.HostRules = map[string]HostRule{
		"slow.example": {Retry: RetryRule{Delay: 3600}},
	}

	got := s.RetryFor("limit", "slow.example")
	if got.Delay != time.Hour {
		t.Errorf("delay = %s, want the host's hour", got.Delay)
	}
	if got.Tries != 2 {
		t.Errorf("tries = %d, want the reason table's 2 - the host rule said nothing about attempts", got.Tries)
	}
	// A cap left unset must not silently undo the delay: the built-in ten
	// minutes would turn a deliberate one-hour wait back into ten, which is the
	// hammering this table exists to stop.
	if got.Max != time.Hour {
		t.Errorf("max = %s, want the hour the delay asks for rather than the built-in cap", got.Max)
	}

	// The same reason on a host with no entry keeps the reason's attempts and
	// the built-in delay.
	other := s.RetryFor("limit", "elsewhere.example")
	if other.Delay != DefaultRetryDelay || other.Tries != 2 {
		t.Errorf("elsewhere: delay %s tries %d, want %s and 2", other.Delay, other.Tries, DefaultRetryDelay)
	}
	// And an unclassified failure on that host keeps the global attempt count.
	unknown := s.RetryFor("", "elsewhere.example")
	if unknown.Tries != 4 {
		t.Errorf("an unnamed reason gets %d attempts, want the global MaxRetries 4", unknown.Tries)
	}
}

// TestNeverIsAnInstructionFromEitherLevel: "do not try this again" written
// against a reason must survive a host rule that says nothing about it, and
// the reverse. A fallback would let the silent level cancel the loud one.
func TestNeverIsAnInstructionFromEitherLevel(t *testing.T) {
	s := Defaults()
	s.Retry.ByReason = map[string]RetryRule{"gone": {Never: true}}
	s.HostRules = map[string]HostRule{"host.example": {Retry: RetryRule{Delay: 30}}}
	if !s.RetryFor("gone", "host.example").Never {
		t.Error("a reason marked never is retried anyway once a host rule sets a delay")
	}
	if s.RetryFor("network", "host.example").Never {
		t.Error("never leaked onto a reason nobody wrote it against")
	}

	s2 := Defaults()
	s2.HostRules = map[string]HostRule{"host.example": {Retry: RetryRule{Never: true}}}
	if !s2.RetryFor("network", "host.example").Never {
		t.Error("a host marked never is retried anyway")
	}
	if s2.RetryFor("network", "other.example").Never {
		t.Error("one host's never reached every other host")
	}
}

// TestSanitizeBoundsBothTables. Every clamp here is a number that would
// otherwise be shown on a page as if it were in force while something
// downstream quietly cut it to something else.
func TestSanitizeBoundsBothTables(t *testing.T) {
	n := sanitize(Settings{
		MaxConcurrent: 4,
		HostRules: map[string]HostRule{
			"  ":           {MaxPerHost: 3},
			"*.":           {Chunks: 2},
			"host.example": {MaxPerHost: -1, Chunks: 999, Retry: RetryRule{Delay: -5, Tries: 900}},
		},
		Retry: RetryPolicy{
			Delay:    -1,
			ByReason: map[string]RetryRule{" ": {Tries: 1}, "limit": {Max: 99999999}},
		},
	})

	if _, ok := n.HostRules["  "]; ok {
		t.Error("a blank host pattern survived; it can never match and can never be explained")
	}
	if _, ok := n.HostRules["*."]; ok {
		t.Error("a pattern that normalises to nothing survived")
	}
	got := n.HostRules["host.example"]
	if got.MaxPerHost != 0 {
		t.Errorf("MaxPerHost = %d, want a negative clamped to 0 (no opinion)", got.MaxPerHost)
	}
	if got.Chunks != rules.MaxChunks {
		t.Errorf("Chunks = %d, want the engine's own bound %d", got.Chunks, rules.MaxChunks)
	}
	if got.Retry.Delay != 0 {
		t.Errorf("a negative delay became %d, want 0", got.Retry.Delay)
	}
	if got.Retry.Tries != maxRetryTries {
		t.Errorf("tries = %d, want the same ceiling MaxRetries has, %d", got.Retry.Tries, maxRetryTries)
	}
	if _, ok := n.Retry.ByReason[" "]; ok {
		t.Error("a blank reason key survived")
	}
	if n.Retry.ByReason["limit"].Max != maxRetryWait {
		t.Errorf("max = %d, want it clamped to %d seconds", n.Retry.ByReason["limit"].Max, maxRetryWait)
	}
	if n.Retry.Delay != 0 {
		t.Errorf("a negative global delay became %d, want 0", n.Retry.Delay)
	}
}

// TestSanitizeDoesNotEditTheCallersMap: the caller still holds the map it
// handed in, and a document that keeps changing underneath whoever submitted
// it is a bug found months later, by which time nobody remembers who edited it.
func TestSanitizeDoesNotEditTheCallersMap(t *testing.T) {
	mine := map[string]HostRule{"host.example": {Chunks: 999}}
	n := sanitize(Settings{MaxConcurrent: 4, HostRules: mine})
	if mine["host.example"].Chunks != 999 {
		t.Errorf("the caller's own map was rewritten to %d", mine["host.example"].Chunks)
	}
	if n.HostRules["host.example"].Chunks != rules.MaxChunks {
		t.Error("the stored copy was not clamped")
	}
}

// TestStallTimeoutHasAFloor. Ten seconds of no bytes is a chunk handover, not a
// dead connection, so a timeout that small would mark healthy downloads instead
// of finding stopped ones. Zero stays zero, because zero is the off switch and
// not a small number.
func TestStallTimeoutHasAFloor(t *testing.T) {
	if got := sanitize(Settings{MaxConcurrent: 4, StallTimeout: 10}).StallTimeout; got != MinStallTimeout {
		t.Errorf("StallTimeout = %d, want the floor %d", got, MinStallTimeout)
	}
	if got := sanitize(Settings{MaxConcurrent: 4, StallTimeout: 0}).StallTimeout; got != 0 {
		t.Errorf("StallTimeout = %d, want 0 - the feature is off, not floored on", got)
	}
	if got := sanitize(Settings{MaxConcurrent: 4, StallTimeout: -30}).StallTimeout; got != 0 {
		t.Errorf("a negative timeout became %d, want the off state", got)
	}
	if got := sanitize(Settings{MaxConcurrent: 4, StallMaxRestarts: 999}).StallMaxRestarts; got != maxStallRestarts {
		t.Errorf("StallMaxRestarts = %d, want %d", got, maxStallRestarts)
	}
}
