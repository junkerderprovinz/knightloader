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
	// Bans is the refusals the picker must respect, and it is long-lived: it
	// belongs to whoever owns the app, not to the picker, because a picker is
	// thrown away and rebuilt on every save and bans that went with it would be
	// forgotten by the act of renaming a row. Nil is a list with nothing in it.
	Bans *Bans
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
	bans    *Bans // shared and outlives this picker; see Options.Bans

	mu     sync.Mutex
	cursor int
}

// NewPicker builds a picker over entries. The list goes through Sanitize on the
// way in, so a caller that forgot cannot round-robin onto a half-configured
// proxy; Sanitize is idempotent, so doing it twice costs nothing.
//
// Building a picker is also what settles the ban list against the new list of
// rows - a row switched back on loses its refusals here. That is deliberate: the
// only moment a picker is built is the moment the list was saved, so the two
// cannot drift apart and no caller has to remember a second call.
func NewPicker(entries []Entry, o Options) *Picker {
	def := o.DefaultMaxDownloads
	if def <= 0 {
		def = DefaultMaxDownloads
	}
	if def > maxDownloadsCap {
		def = maxDownloadsCap
	}
	p := &Picker{entries: Sanitize(entries), def: def, bans: o.Bans}
	p.bans.observe(p.entries)
	return p
}

// Bans is the refusal list this picker consults, so a caller that has the picker
// has the thing that clears them too.
func (p *Picker) Bans() *Bans { return p.bans }

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
	// The direct gateway is not a connection anybody is spreading load over: it
	// is the absence of a proxy, and the only ceiling on it is the app's own
	// concurrency. Left to fall through, it would take the list's default of two
	// - so a user with no proxies at all, whose every download is direct, would
	// find the queue capped at two by a list they never wrote a row in.
	if e.isGateway() {
		return maxDownloadsCap
	}
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
//	Entry{}, false  - an entry does claim this host, but no entry that claims it
//	                  can take the download right now. Wait and ask again. Going
//	                  direct here would route around the proxy the user chose for
//	                  exactly this host, which is the leak the feature exists to
//	                  prevent.
//
// "Cannot take it right now" is two things and they are answered as one: the
// entry is at its limit, or the host has refused it and it is on the ban list.
// Collapsing them is the decision. What the caller does is identical - keep the
// download queued and ask again - and the only difference is what clears the
// condition, which is the caller's business in neither case. A ban that answered
// differently would have to answer "give up", and giving up means either
// stranding the download or sending it out over a connection the user pointed
// away from this host.
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
		// Counted as a candidate before either refusal is checked, because a
		// candidate is what keeps the answer at "wait". An entry dropped from the
		// count would let a host whose only proxy is banned fall through to the
		// direct fallback below, and the download the ban is about would go out
		// over the plain connection instead.
		candidates++
		if p.bans.Banned(e.ID, host) {
			continue
		}
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

// PickFor is Pick for a download that has been given a connection by name - the
// id on the task, which is what per-download routing amounts to.
//
// The answers are Pick's two, reached differently:
//
//	""          Nothing was chosen for this download, so the rotation decides.
//	            This is Pick, unchanged.
//	DirectID    The direct gateway was chosen. It is a real choice and it is
//	            honoured: no filter, no limit and no ban applies, because there
//	            is nothing between the download and the machine's own connection
//	            for any of them to be about.
//	a live row  That row, if it can take the download. If it cannot - at its
//	            limit, or refused by this host - the answer is wait, exactly as
//	            in Pick. It is never quietly swapped for another connection: the
//	            user named this one.
//	anything    The row is gone, switched off, or was never there. The rotation
//	 else       decides, which is the answer this download would have had if it
//	            had never named anything.
//
// That last case is the one worth arguing about, so: the alternative is to
// strand the download for ever on a row somebody deleted months ago, or on one
// they switched off for the evening. A per-task connection is a preference, not
// a lock, and the rotation it falls back to still honours every host filter - so
// a host the user deliberately pointed somewhere else is not reached by this
// door either.
//
// The caller still owns the in-flight counts and must record the answer under
// its own lock; see the type comment. Naming a connection changes nothing about
// that.
func (p *Picker) PickFor(id, host string, inUse map[string]int) (Entry, bool) {
	switch id {
	case "":
		return p.Pick(host, inUse)
	case DirectID:
		return Direct(), true
	}
	e, found := p.find(id)
	if !found {
		return p.Pick(host, inUse)
	}
	if p.bans.Banned(e.ID, host) || inUse[e.ID] >= p.Limit(e) {
		return Entry{}, false
	}
	return e, true
}

// find returns the usable entry with this id. The entries are fixed after New,
// so this needs no lock and must not take one: PickFor calls Pick on the way
// out, and Pick takes it.
func (p *Picker) find(id string) (Entry, bool) {
	for _, e := range p.entries {
		if e.ID == id && e.usable() {
			return e.clone(), true
		}
	}
	return Entry{}, false
}
