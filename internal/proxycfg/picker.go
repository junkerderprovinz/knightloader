package proxycfg

import "sync"

// DefaultMaxDownloads is how many downloads may share one connection when
// neither the entry nor the caller says. It matches the per-host default in
// settings, so one connection cannot take the whole queue.
const DefaultMaxDownloads = 2

// Options configures a Picker.
type Options struct {
	// DefaultMaxDownloads applies to entries that set no limit of their own.
	// Zero means DefaultMaxDownloads.
	DefaultMaxDownloads int
	// Bans is the refusal list to respect. It belongs to the app, not the
	// picker, which is rebuilt on every save. Nil is an empty list.
	Bans *Bans
}

// Picker hands out the next connection to use. It starts no goroutines and
// owns only a cursor.
//
// The in-flight counts stay with the caller, who knows when a download ends
// and who keeps them across the rebuild on every save. Pick only reads them,
// so the caller must hold its own lock across Pick and the increment that
// records it; otherwise two goroutines can both take a connection's last slot.
// TestPickNeverGoesOverALimitUnderConcurrency shows the correct shape.
type Picker struct {
	entries []Entry // never mutated after New, so reads need no lock
	def     int
	bans    *Bans // shared and outlives this picker

	mu     sync.Mutex
	cursor int
}

// NewPicker builds a picker over entries, sanitizing them on the way in.
// Building a picker also settles the ban list against the new rows (see
// Bans.observe), since a picker is only built when the list is saved.
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

// Bans is the refusal list this picker consults.
func (p *Picker) Bans() *Bans { return p.bans }

// Entries returns the sanitized list in the order the picker walks it, which
// is also what the caller should persist and show. Every entry is a deep copy,
// so editing it cannot reach the running picker.
func (p *Picker) Entries() []Entry {
	out := make([]Entry, len(p.entries))
	for i, e := range p.entries {
		out[i] = e.clone()
	}
	return out
}

// Limit is how many downloads may share e at once. The cap is applied here too
// because callers pass entries that may not have been sanitized.
func (p *Picker) Limit(e Entry) int {
	// The direct gateway is limited only by the app's own concurrency; the
	// list default would cap a user with no proxies at two downloads.
	if e.isGateway() {
		return maxDownloadsCap
	}
	if e.MaxDownloads > 0 {
		return min(e.MaxDownloads, maxDownloadsCap)
	}
	return p.def
}

// Pick returns the connection the next download to host should use. inUse
// maps an entry ID to how many downloads use it now; Pick only reads it, and a
// nil map means nothing is running. The caller records the answer under its
// own lock.
//
// The two negative answers differ:
//
//	Direct(), true:  no entry claims this host, so download normally. An
//	                 empty list or a mistyped filter must not freeze the queue.
//	Entry{}, false:  an entry claims this host but none that does can take the
//	                 download now, because it is at its limit or banned. Wait
//	                 and ask again; going direct would bypass the proxy the
//	                 user chose for this host.
//
// Entries are walked in order from where the previous pick stopped, so
// downloads spread across the list.
func (p *Picker) Pick(host string, inUse map[string]int) (Entry, bool) {
	host = normalizeHost(host)
	n := len(p.entries)
	if n == 0 {
		return Direct(), true
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// An entry whose filter names this host beats one with no filter, which is
	// what lets a direct entry for "nas.local" keep the catch-all proxy off it.
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
		// A claimed host considers only filtered entries, any other host only
		// unfiltered ones.
		if (len(e.Filter) > 0) != claimed {
			continue
		}
		if claimed && !e.Matches(host) {
			continue
		}
		// Counted before the ban and limit checks, so a host whose only proxy
		// is banned waits instead of falling through to direct.
		candidates++
		if p.bans.Banned(e.ID, host) {
			continue
		}
		if inUse[e.ID] >= p.Limit(e) {
			continue
		}
		p.cursor = (i + 1) % n
		// Cloned so the caller and the picker do not share the filter slice.
		return e.clone(), true
	}
	// Nothing claimed this host at all.
	if candidates == 0 {
		return Direct(), true
	}
	return Entry{}, false
}

// PickFor is Pick for a download whose task names a connection by id:
//
//	"":          nothing was chosen, so the rotation decides (Pick).
//	DirectID:    the direct gateway; no filter, limit or ban applies to it.
//	a live row:  that row if it can take the download, otherwise wait. It is
//	             never swapped for another connection.
//	anything     the row was deleted, switched off or never existed, so the
//	else:        rotation decides.
//
// Falling back to the rotation keeps a download from being stranded on a
// deleted row, and the rotation still honours every host filter. As with Pick,
// the caller records the answer under its own lock.
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

// find returns the usable entry with this id. It takes no lock: the entries
// are fixed after New, and PickFor calls Pick, which does.
func (p *Picker) find(id string) (Entry, bool) {
	for _, e := range p.entries {
		if e.ID == id && e.usable() {
			return e.clone(), true
		}
	}
	return Entry{}, false
}
