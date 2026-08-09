package proxycfg

// The ban list: which connections a host has refused, so the picker stops
// offering them for that host.
//
// It is deliberately in memory only. A ban is a statement about how a hoster is
// behaving now, not a setting, and one written to disk would outlive the reason
// for it - a proxy blocked during an evening's rate limit would still be refused
// for that hoster weeks later, with nothing on any page to explain why downloads
// were queuing up behind a connection that works.

import (
	"sort"
	"sync"
)

// Bans records the connection/host pairs that have been refused.
//
// Every method tolerates a nil receiver, so a Picker built without one behaves
// exactly like a Picker whose list is empty. That matters because the ban list
// is an addition to a feature that already worked: a caller that has not been
// taught about it yet must not have to be, and must not crash for not being.
type Bans struct {
	mu sync.Mutex
	// hosts maps a connection id to the hosts that have refused it.
	hosts map[string]map[string]struct{}
	// seen is what the list said about each row the last time a picker was built
	// over it, and is the whole of the edge detection - see observe.
	seen map[string]rowState
}

// rowState is the little about a row that decides whether its bans still apply.
type rowState struct {
	enabled bool
	// endpoint is where the row pointed, so that a row edited to a different
	// proxy does not inherit the refusals of the one it used to be. Entry.String
	// is exactly the right amount of it: kind, user and address, never the
	// password.
	endpoint string
}

// NewBans returns an empty ban list.
func NewBans() *Bans {
	return &Bans{hosts: map[string]map[string]struct{}{}, seen: map[string]rowState{}}
}

// Ban records that host refused the connection with this id. It is idempotent.
//
// The direct gateway is never banned, and neither is a blank id. Banning the
// gateway would leave a host with no connection at all once its proxies were
// refused too, and the queue would stall on downloads that had nowhere left to
// go - the failure Pick's fallback to direct exists to prevent, arrived at from
// the other side.
func (b *Bans) Ban(id, host string) {
	if b == nil || id == "" || id == DirectID {
		return
	}
	host = normalizeHost(host)
	if host == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hosts == nil {
		b.hosts = map[string]map[string]struct{}{}
	}
	if b.hosts[id] == nil {
		b.hosts[id] = map[string]struct{}{}
	}
	b.hosts[id][host] = struct{}{}
}

// Banned reports whether host has refused this connection.
func (b *Bans) Banned(id, host string) bool {
	if b == nil || id == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, yes := b.hosts[id][normalizeHost(host)]
	return yes
}

// Hosts is the sorted list of hosts that have refused this connection, so a page
// can show what a row is currently being kept away from. Sorted rather than in
// insertion order: this is read to be looked at, and a list that reshuffles
// between two views of the same state reads as changing when it has not.
func (b *Bans) Hosts(id string) []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.hosts[id]) == 0 {
		return nil
	}
	out := make([]string, 0, len(b.hosts[id]))
	for h := range b.hosts[id] {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Clear forgets everything held against one connection, which is what "try this
// proxy again" means.
func (b *Bans) Clear(id string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.hosts, id)
}

// ClearAll forgets every ban.
func (b *Bans) ClearAll() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hosts = map[string]map[string]struct{}{}
}

// observe takes in the sanitized list a picker was just built over and clears
// the bans that the list itself has just invalidated.
//
// It is called from NewPicker and nowhere else, and that is not laziness. A
// picker is rebuilt exactly when the connection list is saved, so the
// construction is the edit - there is no second moment to hook, and a caller
// asked to remember to call this after every save is a caller who will forget
// once and leave a row switched back on that is still silently refused.
//
// Three things clear a row's bans:
//
//	off -> on   The Use switch going false->true is the user saying "try this
//	            again". Re-enabling a connection that came back still carrying
//	            yesterday's refusals is an inheritance nobody asked for, and it
//	            is invisible: the row is on, and downloads still avoid it.
//	edited      The row now points at a different proxy. The refusals belonged
//	            to the machine it used to name, not to the row.
//	deleted     Its bans go with it. This one is not tidiness. identify hands out
//	            the LOWEST FREE decimal id, so deleting row "2" and adding a new
//	            one makes the newcomer "2" as well - and without this it would be
//	            born already banned from the hosts that refused its predecessor.
//
// A row seen for the first time is recorded, never treated as an edge: at boot
// every row would otherwise look like one, and clearing an empty ban list to
// celebrate is only misleading in the log somebody eventually adds here.
func (b *Bans) observe(entries []Entry) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[string]rowState{}
	}
	now := make(map[string]rowState, len(entries))
	for _, e := range entries {
		s := rowState{enabled: e.usable(), endpoint: e.String()}
		now[e.ID] = s
		was, known := b.seen[e.ID]
		if !known {
			continue
		}
		if (s.enabled && !was.enabled) || s.endpoint != was.endpoint {
			delete(b.hosts, e.ID)
		}
	}
	for id := range b.seen {
		if _, still := now[id]; !still {
			delete(b.hosts, id)
		}
	}
	b.seen = now
}
