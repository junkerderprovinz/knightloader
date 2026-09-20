package proxycfg

// The ban list: which connections a host has refused, so the picker stops
// offering them for that host. It lives in memory only, because a ban reflects
// how a hoster behaves now; saved to disk it would outlast its reason and keep
// a working proxy refused with nothing on any page to explain why.

import (
	"sort"
	"sync"
)

// Bans records the connection/host pairs that have been refused. Every method
// tolerates a nil receiver, so a Picker without one behaves as if the list
// were empty.
type Bans struct {
	mu sync.Mutex
	// hosts maps a connection id to the hosts that have refused it.
	hosts map[string]map[string]struct{}
	// seen is each row's state when a picker was last built, for observe.
	seen map[string]rowState
}

// rowState is what decides whether a row's bans still apply.
type rowState struct {
	enabled bool
	// endpoint is where the row pointed, so a row edited to another proxy does
	// not inherit the old one's refusals. Entry.String gives kind, user and
	// address without the password.
	endpoint string
}

// NewBans returns an empty ban list.
func NewBans() *Bans {
	return &Bans{hosts: map[string]map[string]struct{}{}, seen: map[string]rowState{}}
}

// Ban records that host refused the connection with this id. It is idempotent.
// The direct gateway and a blank id are never banned, so a host whose proxies
// were all refused still has a way out.
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

// Hosts is the sorted list of hosts that have refused this connection, for a
// page to show. Sorted so two views of the same state look the same.
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

// Clear forgets everything held against one connection, which is what "try
// this proxy again" means.
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
// the bans the list itself invalidated. NewPicker calls it, because a picker
// is rebuilt exactly when the list is saved, so no caller has to remember.
//
// Three things clear a row's bans:
//
//	off to on:  switching a row back on means "try this again".
//	edited:     the row points at a different proxy now.
//	deleted:    identify reuses the lowest free id, so a new row would
//	            otherwise inherit its predecessor's bans.
//
// A row seen for the first time is only recorded, or every row would count as
// changed at boot.
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
