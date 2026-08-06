package proxycfg

import "sync"

// DefaultMaxDownloads is how many downloads may share one connection when
// neither the entry nor the caller says. Two matches the per-host default in
// settings: spreading downloads is the reason the list exists, and letting a
// single connection take the whole queue would defeat it.
const DefaultMaxDownloads = 2

// Options configures a Picker.
type Options struct {
	// DefaultMaxDownloads applies to entries that set no limit of their own.
	// Zero means DefaultMaxDownloads.
	DefaultMaxDownloads int
}

// Picker hands out the next connection to use. It starts no goroutines.
//
// It owns nothing but a cursor. The in-flight counts stay with the caller,
// because only the caller knows when a download has finished, and because the
// picker is rebuilt whenever the connection list is saved: a counter living here
// would reset to zero underneath downloads that are still running and overshoot
// every limit at once.
//
// That split is also the one thing a caller has to get right. The picker's own
// state is locked, so concurrent Picks cannot tear the cursor, but Pick is a
// pure function of the counts it is handed and cannot reserve anything in a map
// it does not own. The caller must therefore hold its own lock across the Pick
// and the increment that records it; two goroutines that Pick against the same
// map before either has recorded its answer both read the same count and both
// take the last slot on one connection. See the example in
// TestPickNeverGoesOverALimitUnderConcurrency for the shape that is correct.
type Picker struct {
	entries []Entry // never mutated after New, so reads need no lock
	def     int

	mu     sync.Mutex
	cursor int
}

// NewPicker builds a picker over entries. The list goes through Sanitize on the
// way in, so a caller that forgot cannot round-robin onto a half-configured
// proxy; Sanitize is idempotent, so doing it twice costs nothing.
func NewPicker(entries []Entry, o Options) *Picker {
	def := o.DefaultMaxDownloads
	if def <= 0 {
		def = DefaultMaxDownloads
	}
	if def > maxDownloadsCap {
		def = maxDownloadsCap
	}
	return &Picker{entries: Sanitize(entries), def: def}
}

// Entries returns the sanitized list in the order the picker walks it, which is
// also the list the caller should persist and show. Every entry is a copy down
// to its filter, so an API handler editing the list it got back - redacting a
// password, renaming a filter - cannot reach into the running picker.
func (p *Picker) Entries() []Entry {
	out := make([]Entry, len(p.entries))
	for i, e := range p.entries {
		out[i] = e.clone()
	}
	return out
}

// Limit is how many downloads may share e at once. The cap is applied here as
// well as in Sanitize because this method is exported and will be called with
// whatever entry the caller has to hand, and a limit above the cap is not a
// limit at all.
func (p *Picker) Limit(e Entry) int {
	if e.MaxDownloads > 0 {
		return min(e.MaxDownloads, maxDownloadsCap)
	}
	return p.def
}

// Pick returns the connection the next download to host should use. inUse maps
// an entry ID to how many downloads are on that entry right now; the caller owns
// that map and Pick only reads it, so a nil map means nothing is running. Pick
// does not record the answer, so the caller has to - under its own lock, see the
// type comment.
//
// The two negative answers are different and must not be collapsed into one:
//
//	Direct(), true  - no configured entry claims this host, so download normally.
//	                  An empty list, or a list whose filters all point elsewhere,
//	                  must never stop downloads: one mistyped filter freezing the
//	                  entire queue is a far worse failure than a download going
//	                  out unproxied.
//	Entry{}, false  - an entry does claim this host, but every entry that claims
//	                  it is at its limit. Wait and ask again. Going direct here
//	                  would route around the proxy the user chose for exactly
//	                  this host, which is the leak the feature exists to prevent.
//
// Entries are walked in list order from wherever the previous pick left off, so
// successive downloads spread across the list instead of piling onto the first
// entry that happens to fit.
func (p *Picker) Pick(host string, inUse map[string]int) (Entry, bool) {
	host = normalizeHost(host)
	n := len(p.entries)
	if n == 0 {
		return Direct(), true
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// An entry whose filter names this host beats one with no filter at all.
	// That preference is what makes a direct entry filtered to "nas.local"
	// actually exclude the NAS: without it the catch-all proxy would still take
	// its turn on that host and half the transfers would leave the LAN.
	claimed := false
	for _, e := range p.entries {
		if e.usable() && len(e.Filter) > 0 && e.Matches(host) {
			claimed = true
			break
		}
	}

	candidates := 0
	for k := 0; k < n; k++ {
		i := (p.cursor + k) % n
		e := p.entries[i]
		if !e.usable() {
			continue
		}
		// In the claimed round only filtered entries count, in the other round
		// only unfiltered ones; mixing them is what the preference forbids.
		if (len(e.Filter) > 0) != claimed {
			continue
		}
		if claimed && !e.Matches(host) {
			continue
		}
		candidates++
		if inUse[e.ID] >= p.Limit(e) {
			continue
		}
		p.cursor = (i + 1) % n
		// Cloned, because the caller is handed this entry and the picker keeps
		// walking the original: a shared filter slice would let the two edit each
		// other. Direct() below is freshly built and needs no copy.
		return e.clone(), true
	}
	// Nothing claimed this host at all, which is a configuration question rather
	// than a busy one, so it is answered with a plain download.
	if candidates == 0 {
		return Direct(), true
	}
	return Entry{}, false
}
